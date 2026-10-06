package session

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sandwich-labs/vibe-app/internal/secrets"
)

// Session is the DuckDB hub: one DuckDB instance whose attached catalogs are
// the user's connections. Callers name data as catalog.schema.table and never
// assume the default catalog is called "main" or "memory": ask DefaultCatalog.
type Session interface {
	// DefaultCatalog is the catalog unqualified names resolve against.
	DefaultCatalog() string
	// DefaultSchema is catalog's default schema ("" catalog = DefaultCatalog).
	DefaultSchema(ctx context.Context, catalog string) (string, error)
	// Resolve fills the catalog and schema a TableRef leaves empty.
	Resolve(ctx context.Context, ref TableRef) (TableRef, error)

	// DB is the connection pool. Transitional handle for consumers that need
	// database/sql transactions (crud, importer) and for the agent SQL
	// classifier's EXPLAIN. Every pooled connection's default catalog is
	// DefaultCatalog. Do not run USE on it: use Conn / WithCatalog.
	DB() *sql.DB

	// Query runs a statement with cancellation, the statement timeout and the
	// row cap that apply (per call, per connection, then session default).
	// The caller must Close the Rows.
	Query(ctx context.Context, query string, args ...any) (*Rows, error)
	QueryWith(ctx context.Context, query string, args []any, opts ...QueryOption) (*Rows, error)
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)
	ExecWith(ctx context.Context, query string, args []any, opts ...QueryOption) (sql.Result, error)
	// Conn pins one connection for a run. WithCatalog issues USE on it; Close
	// restores the default before the connection returns to the pool.
	Conn(ctx context.Context, opts ...QueryOption) (*Conn, error)

	// SetDefaultCatalog makes an attached connection the catalog unqualified
	// names resolve against (the workspace's default_connection, #109). Only a
	// hub that owns its instance (New) can move it; a Wrap'd container hub keeps
	// the file as its default and returns an error. The catalog fields are
	// guarded, but connections already pinned or checked out keep the catalog
	// they started in, so call it right after attaching and before the session
	// is shared with other goroutines.
	SetDefaultCatalog(ctx context.Context, name string) error

	// Register declares a connection without dialling (lazy attach); Attach
	// registers and connects; Connect dials a registered one.
	Register(spec Spec) error
	Attach(ctx context.Context, spec Spec) error
	Connect(ctx context.Context, name string) error
	Detach(ctx context.Context, name string) error
	Connections() []Status

	ListCatalogs(ctx context.Context) ([]CatalogInfo, error)
	ListSchemas(ctx context.Context, catalog string) ([]string, error)
	ListTables(ctx context.Context, catalog, schema string) ([]TableInfo, error)
	DescribeTable(ctx context.Context, qualified string) (*TableDescription, error)
	// InvalidateSchema drops cached introspection (all catalogs when name is "").
	// Session-run DDL, attach and detach invalidate on their own.
	InvalidateSchema(name string)

	// Redact scrubs every secret value this session knows about, plus
	// credential-shaped patterns (password='...'), from a message. Errors
	// the session returns are already scrubbed; use it for text that did
	// not come through the session.
	Redact(s string) string
	// Redactor is the redactor behind Redact. Give it to the secrets
	// Resolver so resolved credentials are scrubbed too.
	Redactor() *secrets.Redactor

	// CreateTemporarySecret runs CREATE OR REPLACE TEMPORARY SECRET on the
	// dedicated admin connection, under the attach mutex. Values are quoted
	// with QuoteLiteral, registered with the redactor, and neither the
	// statement nor the values are ever logged or put in an error.
	CreateTemporarySecret(ctx context.Context, name, secretType string, params []SecretParam) error
	// DropSecret drops a temporary secret (no error if it does not exist).
	DropSecret(ctx context.Context, name string) error

	Close() error
}

type entry struct {
	spec   Spec
	status Status
	attach bool // currently attached
}

type hub struct {
	db      *sql.DB
	ownsDB  bool
	cfg     *config
	logger  *slog.Logger
	types   *Registry
	tempDir string // removed on Close when ownsTemp
	ownTemp bool

	defMu          sync.RWMutex // guards defaultCatalog and defSchemaName
	defaultCatalog string

	redactor *secrets.Redactor

	adminMu sync.Mutex // serialises ATTACH / DETACH / secret DDL

	mu    sync.RWMutex
	conns map[string]*entry

	gen   atomic.Int64
	cache cacheStore

	schemaMu      sync.Mutex
	defaultSchema map[string]string
	// defSchemaName is the default catalog's default schema at open, the
	// state every pooled connection must be in when it is handed back.
	defSchemaName string
}

var _ Session = (*hub)(nil)

// DefaultDirs returns the app-private extension and secret directories.
func DefaultDirs() (extensionDir, secretDir string) {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = filepath.Join(os.TempDir(), fmt.Sprintf("vibe-cache-%d", os.Getuid()))
	}
	root := filepath.Join(base, "vibe", "duckdb")
	return filepath.Join(root, "extensions"), filepath.Join(root, "secrets")
}

// New opens an in-memory hub (no data of its own; connections attach into it)
// with open-time hardening applied and verified. This is the shape every
// workspace will use from #109.
func New(opts ...Option) (Session, error) {
	c := newConfig(opts)
	ownTemp := false
	if c.tempDir == "" {
		d, err := os.MkdirTemp("", fmt.Sprintf("vibe-duckdb-%d-hub-*", os.Getpid()))
		if err != nil {
			return nil, fmt.Errorf("failed to create hub temp directory: %w", err)
		}
		if err := os.Chmod(d, 0o700); err != nil {
			_ = os.RemoveAll(d)
			return nil, fmt.Errorf("failed to secure hub temp directory: %w", err)
		}
		c.tempDir, ownTemp = d, true
	}
	applyDirDefaults(c)
	db, err := openDB("", c)
	if err != nil {
		if ownTemp {
			_ = os.RemoveAll(c.tempDir)
		}
		return nil, err
	}
	h, err := wrap(db, c, true)
	if err != nil {
		_ = db.Close()
		if ownTemp {
			_ = os.RemoveAll(c.tempDir)
		}
		return nil, err
	}
	h.ownTemp = ownTemp
	return h, nil
}

// Wrap builds a Session over an existing DuckDB instance opened with OpenDB
// (the legacy container's materialised file: its default catalog is that
// file). It applies the SET-only hardening, locks the configuration and reads
// every option back; it fails closed if the instance was not opened hardened.
// The caller keeps ownership of db: Close does not close it.
func Wrap(db *sql.DB, opts ...Option) (Session, error) {
	c := newConfig(opts)
	applyDirDefaults(c)
	h, err := wrap(db, c, false)
	if err != nil {
		return nil, err
	}
	return h, nil
}

func applyDirDefaults(c *config) {
	ext, sec := DefaultDirs()
	if c.extensionDir == "" {
		c.extensionDir = ext
	}
	if c.secretDir == "" {
		c.secretDir = sec
	}
}

func wrap(db *sql.DB, c *config, owns bool) (*hub, error) {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open session connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	if err := applyHardening(ctx, conn, c); err != nil {
		return nil, err
	}
	var def, defSchema string
	if err := conn.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&def, &defSchema); err != nil {
		return nil, fmt.Errorf("failed to read default catalog: %w", err)
	}
	h := newHub(db, c, def, defSchema, owns)
	return h, nil
}

// newHub builds the hub value; def is the instance's current_database().
func newHub(db *sql.DB, c *config, def, defSchema string, owns bool) *hub {
	h := &hub{
		db: db, ownsDB: owns, cfg: c, types: NewRegistry(), redactor: c.redactor,
		logger:  slog.New(secrets.NewRedactingHandler(c.logger.Handler(), c.redactor)),
		tempDir: c.tempDir, defaultCatalog: def,
		conns:         map[string]*entry{},
		defaultSchema: map[string]string{def: defSchema},
		defSchemaName: defSchema,
	}
	if defSchema == "" {
		delete(h.defaultSchema, def)
	}
	h.cache.init()
	h.conns[def] = &entry{
		spec:   Spec{Name: def, Type: "duckdb", Timeout: c.defaultTO, RowCap: c.defaultRowCap},
		status: Status{Name: def, Type: "duckdb", Kind: StatusOK, Default: true},
		attach: true,
	}
	return h
}

func (h *hub) DefaultCatalog() string { return h.defCat() }

// defCat and defSchema read the default catalog and its schema under defMu,
// which SetDefaultCatalog (the only writer) holds for writing.
func (h *hub) defCat() string {
	h.defMu.RLock()
	defer h.defMu.RUnlock()
	return h.defaultCatalog
}

func (h *hub) defSchema() string {
	h.defMu.RLock()
	defer h.defMu.RUnlock()
	return h.defSchemaName
}
func (h *hub) DB() *sql.DB { return h.db }

// Close releases the session. A hub that owns its DuckDB instance closes it
// and removes its temp directory; a wrapped instance is left to its owner.
func (h *hub) Close() error {
	var first error
	if h.ownsDB {
		if err := h.db.Close(); err != nil {
			first = fmt.Errorf("failed to close hub: %w", err)
		}
	}
	if h.ownTemp && h.tempDir != "" {
		if err := os.RemoveAll(h.tempDir); err != nil && first == nil {
			first = fmt.Errorf("failed to remove hub temp directory: %w", err)
		}
	}
	return first
}

// --- running statements ---

func (h *hub) callCfg(opts []QueryOption) callConfig {
	var cc callConfig
	for _, o := range opts {
		o(&cc)
	}
	catalog := cc.catalog
	if catalog == "" {
		catalog = h.defCat()
	}
	h.mu.RLock()
	e := h.conns[catalog]
	h.mu.RUnlock()
	if !cc.hasTimeout {
		cc.timeout = h.cfg.defaultTO
		if e != nil && e.spec.Timeout > 0 {
			cc.timeout = e.spec.Timeout
		}
	}
	if !cc.hasRowCap {
		cc.rowCap = h.cfg.defaultRowCap
		if e != nil && e.spec.RowCap > 0 {
			cc.rowCap = e.spec.RowCap
		}
	}
	if cc.catalog == h.defCat() {
		cc.catalog = ""
	}
	return cc
}

func (h *hub) Query(ctx context.Context, query string, args ...any) (*Rows, error) {
	return h.QueryWith(ctx, query, args)
}

func (h *hub) QueryWith(ctx context.Context, query string, args []any, opts ...QueryOption) (*Rows, error) {
	cc := h.callCfg(opts)
	ctx, cancel := withTimeout(ctx, cc)
	if cc.catalog != "" {
		conn, err := h.pin(ctx, cc.catalog)
		if err != nil {
			cancel()
			return nil, err
		}
		r, err := conn.c.QueryContext(ctx, query, args...)
		if err != nil {
			_ = conn.Close()
			cancel()
			return nil, h.wrapErr("failed to run query", err)
		}
		return &Rows{Rows: r, cap: cc.rowCap, cancel: cancel, release: conn.Close}, nil
	}
	// Default-catalog statements run on a pinned connection too, so a USE (or
	// SET search_path) in user SQL cannot leak into the pool: Close verifies
	// the connection is back on the default catalog and schema, restores it,
	// or discards it.
	conn, err := h.acquire(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	r, err := conn.c.QueryContext(ctx, query, args...)
	if isDDL(query) {
		h.InvalidateSchema("")
	}
	if err != nil {
		_ = conn.Close()
		cancel()
		return nil, h.wrapErr("failed to run query", err)
	}
	return &Rows{Rows: r, cap: cc.rowCap, cancel: cancel, release: conn.Close}, nil
}

// acquire pins a connection on the session default catalog.
func (h *hub) acquire(ctx context.Context) (*Conn, error) {
	c, err := h.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to pin connection: %w", err)
	}
	return &Conn{c: c, h: h}, nil
}

func (h *hub) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return h.ExecWith(ctx, query, args)
}

func (h *hub) ExecWith(ctx context.Context, query string, args []any, opts ...QueryOption) (sql.Result, error) {
	cc := h.callCfg(opts)
	ctx, cancel := withTimeout(ctx, cc)
	defer cancel()
	var (
		res sql.Result
		err error
	)
	if cc.catalog != "" {
		var conn *Conn
		conn, err = h.pin(ctx, cc.catalog)
		if err != nil {
			return nil, err
		}
		defer conn.Close() //nolint:errcheck
		res, err = conn.c.ExecContext(ctx, query, args...)
	} else {
		var conn *Conn
		conn, err = h.acquire(ctx)
		if err != nil {
			return nil, err
		}
		defer conn.Close() //nolint:errcheck
		res, err = conn.c.ExecContext(ctx, query, args...)
	}
	if isDDL(query) {
		h.InvalidateSchema("")
	}
	if err != nil {
		return nil, h.wrapErr("failed to execute statement", err)
	}
	return res, nil
}

// Conn pins one connection. With WithCatalog it runs USE on it.
func (h *hub) Conn(ctx context.Context, opts ...QueryOption) (*Conn, error) {
	cc := h.callCfg(opts)
	if cc.catalog != "" {
		return h.pin(ctx, cc.catalog)
	}
	conn, err := h.acquire(ctx)
	if err != nil {
		return nil, err
	}
	conn.cfg = cc
	return conn, nil
}

func (h *hub) pin(ctx context.Context, catalog string) (*Conn, error) {
	h.mu.RLock()
	e := h.conns[catalog]
	h.mu.RUnlock()
	if e == nil || !e.attach {
		return nil, fmt.Errorf("connection %q is not attached", catalog)
	}
	c, err := h.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to pin connection: %w", err)
	}
	if _, err := c.ExecContext(ctx, "USE "+QuoteIdent(catalog)); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("failed to switch to connection %q: %w", catalog, err)
	}
	return &Conn{c: c, h: h, catalog: catalog, cfg: h.callCfg([]QueryOption{WithCatalog(catalog)})}, nil
}

// Conn is one pinned connection. Not safe for concurrent use.
type Conn struct {
	c       *sql.Conn
	h       *hub
	catalog string
	cfg     callConfig
	closed  bool
}

// SQLConn exposes the pinned *sql.Conn (for BeginTx).
func (c *Conn) SQLConn() *sql.Conn { return c.c }

// Catalog is the catalog USE selected ("" = session default).
func (c *Conn) Catalog() string { return c.catalog }

// Query runs a statement on the pinned connection with its timeout and cap.
func (c *Conn) Query(ctx context.Context, query string, args ...any) (*Rows, error) {
	ctx, cancel := withTimeout(ctx, c.cfg)
	r, err := c.c.QueryContext(ctx, query, args...)
	if err != nil {
		cancel()
		return nil, c.h.wrapErr("failed to run query", err)
	}
	return &Rows{Rows: r, cap: c.cfg.rowCap, cancel: cancel}, nil
}

// Exec runs a statement on the pinned connection.
func (c *Conn) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	ctx, cancel := withTimeout(ctx, c.cfg)
	defer cancel()
	res, err := c.c.ExecContext(ctx, query, args...)
	if isDDL(query) {
		c.h.InvalidateSchema("")
	}
	if err != nil {
		return nil, c.h.wrapErr("failed to execute statement", err)
	}
	return res, nil
}

// Close verifies the connection is back on the session's default catalog and
// schema (the run may have issued USE, pinned or not), restores it if not,
// and returns it to the pool. If the check or the restore fails the connection
// is discarded, never reused.
func (c *Conn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	ctx := context.Background()
	// A run that opened a transaction and never ended it (BEGIN with no
	// COMMIT) must not leave it open on a pooled connection: later writes
	// would join it, fail with "transaction is aborted", or block CHECKPOINT.
	// Roll it back; "no transaction is active" is the normal case. A run is at
	// most one transaction, and Rows.Close releases the conn only after the
	// result is fully consumed and closed, so this never cuts a read short.
	if _, err := c.c.ExecContext(ctx, "ROLLBACK"); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no transaction is active") {
		_ = c.c.Raw(func(any) error { return driver.ErrBadConn })
		return c.c.Close()
	}
	var cat, schema string
	err := c.c.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&cat, &schema)
	if err != nil || cat != c.h.defCat() || schema != c.h.defSchema() {
		if _, uerr := c.c.ExecContext(ctx, "USE "+QuoteQualified(c.h.defCat(), c.h.defSchema())); uerr != nil {
			_ = c.c.Raw(func(any) error { return driver.ErrBadConn })
		}
	}
	return c.c.Close()
}

// --- connections ---

func (h *hub) Register(spec Spec) error {
	if err := ValidateName(spec.Name); err != nil {
		return err
	}
	if spec.Type == "" {
		spec.Type = "duckdb"
	}
	t, err := h.types.Get(spec.Type)
	if err != nil {
		return err
	}
	if err := t.Validate(spec); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.conns[spec.Name]; ok && old.attach {
		return fmt.Errorf("connection %q is already attached", spec.Name)
	}
	h.conns[spec.Name] = &entry{spec: spec, status: Status{
		Name: spec.Name, Type: spec.Type, Kind: StatusNotConnected, ReadOnly: spec.ReadOnly,
	}}
	return nil
}

func (h *hub) Attach(ctx context.Context, spec Spec) error {
	if err := h.Register(spec); err != nil {
		return err
	}
	return h.Connect(ctx, spec.Name)
}

func (h *hub) Connect(ctx context.Context, name string) error {
	h.mu.RLock()
	e := h.conns[name]
	h.mu.RUnlock()
	if e == nil {
		return fmt.Errorf("unknown connection %q", name)
	}
	if e.attach {
		return nil
	}
	t, err := h.types.Get(e.spec.Type)
	if err != nil {
		return err
	}
	stmts, err := t.AttachSQL(e.spec)
	if err != nil {
		h.setStatus(name, StatusError, h.redact(err.Error()), 0)
		return fmt.Errorf("failed to attach %q: %s", name, h.redact(err.Error()))
	}

	// Attach / secret DDL is serialised on a dedicated connection and is never
	// logged: not the statements, not the parameters.
	h.adminMu.Lock()
	defer h.adminMu.Unlock()
	conn, err := h.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get admin connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	for _, s := range stmts {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			kind, msg, pid := classifyAttachError(err, h.redact)
			h.setStatus(name, kind, msg, pid)
			return fmt.Errorf("failed to attach %q: %s", name, msg)
		}
	}
	h.mu.Lock()
	e.attach = true
	e.status = Status{Name: name, Type: e.spec.Type, Kind: StatusOK, ReadOnly: e.spec.ReadOnly}
	h.mu.Unlock()
	h.InvalidateSchema("")
	h.logger.Info("connection attached", "name", name, "type", e.spec.Type)
	return nil
}

func (h *hub) SetDefaultCatalog(ctx context.Context, name string) error {
	if name == h.defCat() {
		return nil
	}
	if !h.ownsDB {
		return fmt.Errorf("the default catalog of a wrapped session cannot be changed")
	}
	h.mu.RLock()
	e := h.conns[name]
	h.mu.RUnlock()
	if e == nil || !e.attach {
		return fmt.Errorf("connection %q is not attached", name)
	}
	n := name
	h.cfg.initCatalog.Store(&n)
	// Recycle idle pooled connections so every one re-runs the USE above.
	h.db.SetMaxIdleConns(0)
	h.db.SetMaxIdleConns(2)

	conn, err := h.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	var cat, schema string
	if err := conn.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&cat, &schema); err != nil {
		return fmt.Errorf("failed to read default catalog: %w", err)
	}
	if cat != name {
		return fmt.Errorf("failed to switch the default catalog to %q", name)
	}
	h.mu.Lock()
	h.defMu.Lock()
	if old := h.conns[h.defaultCatalog]; old != nil {
		old.status.Default = false
	}
	e.status.Default = true
	h.defaultCatalog = name
	h.defSchemaName = schema
	h.defMu.Unlock()
	h.mu.Unlock()
	h.schemaMu.Lock()
	h.defaultSchema = map[string]string{name: schema}
	h.schemaMu.Unlock()
	h.InvalidateSchema("")
	return nil
}

func (h *hub) setStatus(name string, kind StatusKind, msg string, pid int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e := h.conns[name]; e != nil {
		e.status.Kind, e.status.Message, e.status.LockedByPID = kind, msg, pid
	}
}

func (h *hub) Detach(ctx context.Context, name string) error {
	if name == h.defCat() {
		return fmt.Errorf("cannot detach the default catalog %q", name)
	}
	h.mu.RLock()
	e := h.conns[name]
	h.mu.RUnlock()
	if e == nil {
		return fmt.Errorf("unknown connection %q", name)
	}
	if e.attach {
		h.adminMu.Lock()
		conn, err := h.db.Conn(ctx)
		if err != nil {
			h.adminMu.Unlock()
			return fmt.Errorf("failed to get admin connection: %w", err)
		}
		_, err = conn.ExecContext(ctx, "DETACH "+QuoteIdent(name))
		_ = conn.Close()
		h.adminMu.Unlock()
		if err != nil {
			return fmt.Errorf("failed to detach %q: %s", name, h.redact(err.Error()))
		}
	}
	h.mu.Lock()
	delete(h.conns, name)
	h.mu.Unlock()
	h.InvalidateSchema("")
	return nil
}

func (h *hub) Connections() []Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]Status, 0, len(h.conns))
	for _, e := range h.conns {
		out = append(out, e.status)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (h *hub) isRemote(catalog string) bool {
	h.mu.RLock()
	e := h.conns[catalog]
	h.mu.RUnlock()
	if e == nil {
		return false
	}
	t, err := h.types.Get(e.spec.Type)
	return err == nil && t.Remote()
}

// isDDL reports whether the statement can change a catalog's shape.
func isDDL(q string) bool {
	q = strings.TrimSpace(q)
	for strings.HasPrefix(q, "--") {
		i := strings.IndexByte(q, '\n')
		if i < 0 {
			return false
		}
		q = strings.TrimSpace(q[i+1:])
	}
	fields := strings.FieldsFunc(q, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '(' || r == ';' })
	if len(fields) == 0 {
		return false
	}
	switch strings.ToUpper(fields[0]) {
	case "CREATE", "DROP", "ALTER", "ATTACH", "DETACH", "COMMENT", "IMPORT", "TRUNCATE", "CALL":
		return true
	}
	return false
}
