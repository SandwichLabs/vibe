package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/marcboeker/go-duckdb/mapping"
	_ "github.com/marcboeker/go-duckdb/v2" // DuckDB driver, for the dedicated parser connection below

	"github.com/sandwich-labs/vibe-app/internal/secrets"
	"github.com/sandwich-labs/vibe-app/internal/usersql"
)

// sqlClass is what a run_query statement is allowed to do, used to gate it per
// statement instead of by the blanket "data" category (#63, #116).
//
// The decision is an ALLOW-LIST (sql_policy.go): a statement is classified only
// when DuckDB's own parser and the policy both understand every part of it, and
// anything else is denied with a reason ("couldn't verify this query"). There
// is no tier in which the never-list (ATTACH, SET, secrets, passthroughs, ...)
// is allowed.
type sqlClass int

const (
	// sqlClassRead is a statement over attached catalogs that changes nothing:
	// SELECT / WITH / FROM-first / VALUES, DESCRIBE, SHOW, SUMMARIZE, EXPLAIN
	// (not ANALYZE), an allow-listed PRAGMA or CALL. Allowed in every tier.
	sqlClassRead sqlClass = iota
	// sqlClassWrite is row-level DML (INSERT, UPDATE, DELETE, MERGE) into
	// writable catalogs. Needs read-write or full.
	sqlClassWrite
	// sqlClassDDL changes a catalog's shape (CREATE, DROP, ALTER) or writes a
	// file (COPY ... TO a data/ path). Needs full.
	sqlClassDDL
	// sqlClassOther is denied in every tier: the never-list, anything the
	// policy could not verify, and anything not on the allow-list. Its verdict
	// always carries a reason.
	sqlClassOther
)

func (c sqlClass) String() string {
	switch c {
	case sqlClassRead:
		return "read"
	case sqlClassWrite:
		return "write"
	case sqlClassDDL:
		return "ddl"
	default:
		return "denied"
	}
}

// sqlTarget is one catalog a write statement changes.
type sqlTarget struct {
	Catalog  string // lower-case catalog (connection) name
	ReadOnly bool   // the catalog or its connection is read-only
	Local    bool   // a local duckdb/sqlite file (anything else always needs approval)
	System   bool   // DuckDB's own system catalog
}

// sqlVerdict is the policy's answer for one run_query call.
type sqlVerdict struct {
	Class   sqlClass
	Reason  string // set whenever Class is sqlClassOther, and for gate refusals
	Targets []sqlTarget

	readers int // file/URL readers vetted in the statement (for a nested query())
}

func deny(reason string) *sqlVerdict { return &sqlVerdict{Class: sqlClassOther, Reason: reason} }

const (
	// externalAccessDeniedMsg is the generic refusal for something that reaches
	// outside the allow-list.
	externalAccessDeniedMsg = "this query reaches outside what the agent may read (files and URLs are limited to the workspace data/ folder and storage scopes)"
	couldntVerifyMsg        = "couldn't verify this query"
)

// secretsDeniedMsg is the reason for a statement that creates, drops, lists or
// attaches with a DuckDB secret. It is denied in EVERY sandbox mode, full
// included, and no approval can lift it: credentials are the session's to
// create (Session.CreateTemporarySecret), never the agent's (#114).
const secretsDeniedMsg = "run_query: secrets are managed by Vibe and cannot be created, listed or referenced from SQL"

var plainIdentifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// unresolvedTableDeniedMsg is the refusal for a table reference that did not
// resolve. An unqualified plain identifier is the ordinary "that table does
// not exist" typo and says so plainly, without suggesting a privilege problem.
func unresolvedTableDeniedMsg(catalogName, schema, table string) string {
	if catalogName == "" && schema == "" && plainIdentifierRe.MatchString(table) {
		return fmt.Sprintf("table %q not found", table)
	}
	return fmt.Sprintf("%s (%q)", externalAccessDeniedMsg, dotJoin(catalogName, schema, table))
}

func dotJoin(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, ".")
}

// --- the parser connections ------------------------------------------------
//
// Both are hardened: external access off, no extension loading, no secrets, and
// the configuration locked. They only ever PARSE agent SQL (json_serialize_sql
// and duckdb_extract_statements never bind, so they cannot touch a file or the
// network), but if a parser bug ever let one execute, it would have no door to
// open. They are separate from the hub on purpose (spike #107, finding B).

var hardenedParserSettings = [][2]string{
	{"enable_external_access", "false"},
	{"autoinstall_known_extensions", "false"},
	{"autoload_known_extensions", "false"},
	{"allow_community_extensions", "false"},
	{"allow_persistent_secrets", "false"},
	{"lock_configuration", "true"},
}

var (
	stmtConnOnce sync.Once
	stmtConnDB   mapping.Database
	stmtConn     mapping.Connection
	stmtConnErr  error
	stmtConnMu   sync.Mutex // DuckDB connections are not safe for concurrent use
)

func statementConn() (mapping.Connection, error) {
	stmtConnOnce.Do(func() {
		var cfg mapping.Config
		if mapping.CreateConfig(&cfg) == mapping.StateError {
			stmtConnErr = errors.New("failed to create the statement-extraction configuration")
			return
		}
		defer mapping.DestroyConfig(&cfg)
		for _, kv := range hardenedParserSettings {
			if mapping.SetConfig(cfg, kv[0], kv[1]) == mapping.StateError {
				stmtConnErr = fmt.Errorf("failed to harden the statement-extraction database (%s)", kv[0])
				return
			}
		}
		var errMsg string
		if mapping.OpenExt("", &stmtConnDB, cfg, &errMsg) == mapping.StateError {
			stmtConnErr = fmt.Errorf("failed to open statement-extraction database: %s", errMsg)
			return
		}
		if mapping.Connect(stmtConnDB, &stmtConn) == mapping.StateError {
			stmtConnErr = errors.New("failed to open statement-extraction connection")
		}
	})
	return stmtConn, stmtConnErr
}

// countStatements returns the number of SQL statements raw contains, via
// DuckDB's own duckdb_extract_statements (syntax only, no binding). It is the
// authority on the count: the policy's own lexer must agree with it, or the
// query is denied.
func countStatements(raw string) (int, error) {
	conn, err := statementConn()
	if err != nil {
		return 0, err
	}
	stmtConnMu.Lock()
	defer stmtConnMu.Unlock()

	var extracted mapping.ExtractedStatements
	n := mapping.ExtractStatements(conn, raw, &extracted)
	errStr := mapping.ExtractStatementsError(extracted)
	mapping.DestroyExtracted(&extracted)
	if errStr != "" {
		return 0, errors.New(errStr)
	}
	return int(n), nil
}

// queryDB is the read surface the policy needs from the user's database.
// *sql.DB satisfies it, and so does a pinned *sql.Conn: a statement bound to a
// connection (notebook format v2) is classified on a connection whose default
// catalog is that connection, so its unqualified names resolve the way they
// will when it runs.
type queryDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// nilDB reports whether q is nil or a nil *sql.DB.
func nilDB(q queryDB) bool {
	if q == nil {
		return true
	}
	d, ok := q.(*sql.DB)
	return ok && d == nil
}

var (
	classifierDBOnce sync.Once
	classifierDBConn *sql.DB
	classifierDBErr  error
)

// classifierDB is the hardened, external-access-off, in-memory DuckDB the AST
// pre-check parses on. It is never the hub.
func classifierDB() (*sql.DB, error) {
	classifierDBOnce.Do(func() {
		dsn := "?"
		for i, kv := range hardenedParserSettings {
			if i > 0 {
				dsn += "&"
			}
			dsn += kv[0] + "=" + kv[1]
		}
		db, err := sql.Open("duckdb", dsn)
		if err != nil {
			classifierDBErr = err
			return
		}
		db.SetMaxOpenConns(1)
		var v string
		if err := db.QueryRow("SELECT current_setting('enable_external_access')").Scan(&v); err != nil || !strings.EqualFold(v, "false") {
			_ = db.Close()
			classifierDBErr = fmt.Errorf("the SQL parser connection is not hardened (enable_external_access=%q, %v)", v, err)
			return
		}
		classifierDBConn = db
	})
	return classifierDBConn, classifierDBErr
}

// serializedSQLDoc is the shape of json_serialize_sql's output. On success
// (error: false) Statements holds the parsed statement(s) as generic trees;
// json_serialize_sql only succeeds for a batch of pure SELECT statements.
type serializedSQLDoc struct {
	Error      bool             `json:"error"`
	ErrorType  string           `json:"error_type"`
	Statements []map[string]any `json:"statements"`
}

// serializeSQL asks the parser connection to parse raw via json_serialize_sql.
// raw is a bound parameter, never concatenated into SQL text.
func serializeSQL(ctx context.Context, raw string) (*serializedSQLDoc, error) {
	db, err := classifierDB()
	if err != nil {
		return nil, fmt.Errorf("parser connection unavailable: %w", err)
	}
	var out string
	err = db.QueryRowContext(ctx, "SELECT CAST(json_serialize_sql(?::VARCHAR) AS VARCHAR)", raw).Scan(&out)
	if err != nil {
		return nil, fmt.Errorf("json_serialize_sql failed: %w", err)
	}
	var doc serializedSQLDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return nil, fmt.Errorf("failed to parse json_serialize_sql output: %w", err)
	}
	return &doc, nil
}

// --- the catalog: tables, views, databases, functions ---------------------

// catEntry is one table or view of the live catalogs.
type catEntry struct {
	view bool
	sql  string // a view's CREATE VIEW text
}

// catalog is a snapshot of every table and view that exists, loaded fresh for
// each classification (the decision must never rest on a stale schema) and
// only when a reference needs it.
type catalog struct {
	defaultCatalog string
	defaultSchema  string
	entries        map[string]catEntry // catalogKey(catalog, schema, table)
	byName         map[string]catEntry // catalogKey(catalog, "", table): any schema
}

func catalogKey(catalogName, schema, table string) string {
	return strings.ToLower(catalogName) + "\x00" + strings.ToLower(schema) + "\x00" + strings.ToLower(table)
}

// allowedSystemObjects is a CLOSED allow-list of DuckDB's own introspection
// views a statement may read (keyed "schema.table", lower-cased).
// duckdb_databases and pragma_database_list are deliberately absent: they list
// every attached database's file path.
var allowedSystemObjects = map[string]bool{
	"information_schema.tables":                  true,
	"information_schema.columns":                 true,
	"information_schema.schemata":                true,
	"information_schema.views":                   true,
	"information_schema.table_constraints":       true,
	"information_schema.key_column_usage":        true,
	"information_schema.referential_constraints": true,
	"main.duckdb_tables":                         true,
	"main.duckdb_columns":                        true,
	"main.duckdb_views":                          true,
	"main.duckdb_constraints":                    true,
	"main.duckdb_indexes":                        true,
	"main.duckdb_sequences":                      true,
	"main.duckdb_types":                          true,
}

// deniedCatalogObjects is a redundant belt-and-braces deny for system objects a
// security review called out by name.
var deniedCatalogObjects = map[string]bool{
	"pg_settings": true,
	"duckdb_logs": true,
}

func systemObjectKey(schema, table string) string {
	return strings.ToLower(schema) + "." + strings.ToLower(table)
}

// lookup finds the table or view a reference names, applying DuckDB's own
// current-database and current-schema defaults. System objects resolve only
// when they are on allowedSystemObjects.
func (c *catalog) lookup(catalogName, schema, table string) (catEntry, bool) {
	if c == nil || table == "" || deniedCatalogObjects[strings.ToLower(table)] {
		return catEntry{}, false
	}
	type cand struct{ cat, schema string }
	var cands []cand
	switch {
	case catalogName != "":
		cands = append(cands, cand{catalogName, schema})
	case schema != "":
		cands = append(cands, cand{c.defaultCatalog, schema}, cand{schema, ""}, cand{"temp", schema}, cand{"system", schema})
	default:
		cands = append(cands, cand{c.defaultCatalog, c.defaultSchema}, cand{"temp", "main"}, cand{"system", "main"})
	}
	for _, cd := range cands {
		var e catEntry
		var ok bool
		if cd.schema == "" {
			e, ok = c.byName[catalogKey(cd.cat, "", table)]
		} else {
			e, ok = c.entries[catalogKey(cd.cat, cd.schema, table)]
		}
		if !ok {
			continue
		}
		if strings.EqualFold(cd.cat, "system") {
			sch := cd.schema
			if sch == "" {
				sch = "main"
			}
			if !allowedSystemObjects[systemObjectKey(sch, table)] {
				continue
			}
		}
		return e, true
	}
	return catEntry{}, false
}

// loadCatalog queries the user's own DuckDB for every table and view. On error
// the catalog is empty and resolves nothing (fail closed).
func loadCatalog(ctx context.Context, userDB queryDB) (*catalog, error) {
	c := &catalog{entries: map[string]catEntry{}, byName: map[string]catEntry{}}
	if nilDB(userDB) {
		return c, errors.New("no duckdb connection available")
	}
	if err := userDB.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&c.defaultCatalog, &c.defaultSchema); err != nil {
		return &catalog{}, err
	}
	load := func(query string, view bool) error {
		rows, err := userDB.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck
		for rows.Next() {
			var dbName, schema, name string
			var sqlText sql.NullString
			if view {
				if err := rows.Scan(&dbName, &schema, &name, &sqlText); err != nil {
					return err
				}
			} else if err := rows.Scan(&dbName, &schema, &name); err != nil {
				return err
			}
			e := catEntry{view: view, sql: sqlText.String}
			c.entries[catalogKey(dbName, schema, name)] = e
			c.byName[catalogKey(dbName, "", name)] = e
		}
		return rows.Err()
	}
	if err := load("SELECT database_name, schema_name, table_name FROM duckdb_tables()", false); err != nil {
		return &catalog{}, err
	}
	if err := load("SELECT database_name, schema_name, view_name, sql FROM duckdb_views()", true); err != nil {
		return &catalog{}, err
	}
	return c, nil
}

// dbRow is one attached database as DuckDB reports it. The path is never read.
type dbRow struct {
	name     string
	typ      string
	readOnly bool
	internal bool
}

func loadDatabases(ctx context.Context, userDB queryDB) (map[string]dbRow, error) {
	out := map[string]dbRow{}
	if nilDB(userDB) {
		return out, errors.New("no duckdb connection available")
	}
	rows, err := userDB.QueryContext(ctx, "SELECT database_name, type, readonly, internal FROM duckdb_databases()")
	if err != nil {
		return out, err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var r dbRow
		var typ sql.NullString
		if err := rows.Scan(&r.name, &typ, &r.readOnly, &r.internal); err != nil {
			return out, err
		}
		r.typ = typ.String
		out[strings.ToLower(r.name)] = r
	}
	return out, rows.Err()
}

// funcInfo is what the live catalog says about function names: which are table
// functions (a table position needs the allow-list) and which are user macros
// (whose definition is analysed).
type funcInfo struct {
	table  map[string]bool
	macros map[string]string // lower name -> definition (scalar: an expression; table macro: a query)
	tmacro map[string]bool
}

func loadFunctions(ctx context.Context, userDB queryDB) (*funcInfo, error) {
	f := &funcInfo{table: map[string]bool{}, macros: map[string]string{}, tmacro: map[string]bool{}}
	if nilDB(userDB) {
		return f, errors.New("no duckdb connection available")
	}
	rows, err := userDB.QueryContext(ctx, `SELECT lower(function_name), function_type, macro_definition, internal FROM duckdb_functions()
		WHERE function_type IN ('table','table_macro','pragma','macro')`)
	if err != nil {
		return f, err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var name, typ string
		var def sql.NullString
		var internal bool
		if err := rows.Scan(&name, &typ, &def, &internal); err != nil {
			return f, err
		}
		switch typ {
		case "table", "pragma":
			f.table[name] = true
		case "table_macro":
			f.table[name] = true
			if !internal { // built-in macros are DuckDB's own and not analysed
				f.tmacro[name] = true
				f.macros[name] = def.String
			}
		case "macro":
			if !internal {
				f.macros[name] = def.String
			}
		}
	}
	return f, rows.Err()
}

// --- the plan check (second layer) ------------------------------------------

// planNode is one node of EXPLAIN (FORMAT JSON)'s physical_plan tree.
type planNode struct {
	Name      string         `json:"name"`
	Children  []planNode     `json:"children"`
	ExtraInfo map[string]any `json:"extra_info"`
}

var planDeniedFuncRe = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(getenv|current_setting|duckdb_secrets|which_secret|duckdb_settings|duckdb_extensions|postgres_query|postgres_execute|postgres_scan_pushdown|query_table|(md|motherduck)_[a-z0-9_]*)\s*\(`)

// planCheck runs EXPLAIN (FORMAT JSON) for stmt on the user's real connection —
// ONLY after the syntactic checks have passed, because binding has side effects
// (EXPLAIN SELECT * FROM read_csv('https://...') sniffs the file and sends an
// HTTP request; spike #107 finding B) — and walks the physical plan. The plan
// reveals what the AST cannot: a view or macro that expands to a reader or a
// table function. It fails closed: an operator not on the allow-list, a plan
// that cannot be read, or more reader operators than the statement wrote out
// (and vetted) is a denial.
//
// Plain EXPLAIN does not execute anything; EXPLAIN ANALYZE would, and is never
// used here.
func planCheck(ctx context.Context, userDB queryDB, stmt string, args []any, vettedReaders int) string {
	if nilDB(userDB) {
		return "could not validate query: no database connection available"
	}
	rows, err := userDB.QueryContext(ctx, "EXPLAIN (FORMAT JSON) "+stmt, args...)
	if err != nil {
		return fmt.Sprintf("could not validate query: %s", secrets.RedactPatterns(err.Error()))
	}
	defer rows.Close() //nolint:errcheck

	var planJSON string
	found := false
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Sprintf("could not validate query: %v", err)
		}
		if key == "physical_plan" {
			planJSON = value
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Sprintf("could not validate query: %s", secrets.RedactPatterns(err.Error()))
	}
	if !found {
		return "could not validate query: EXPLAIN (FORMAT JSON) returned no physical_plan"
	}
	if planDeniedFuncRe.MatchString(planJSON) {
		return externalAccessDeniedMsg
	}
	var nodes []planNode
	if err := json.Unmarshal([]byte(planJSON), &nodes); err != nil {
		return externalAccessDeniedMsg
	}
	readers := 0
	for i := range nodes {
		if reason := checkPlanNode(&nodes[i], &readers); reason != "" {
			return reason
		}
	}
	if readers > vettedReaders {
		return "this query reads a file or URL that is not written out as a literal inside the statement (a view or macro that wraps a reader?), so it can't be verified"
	}
	return ""
}

func checkPlanNode(n *planNode, readers *int) string {
	name := strings.ToUpper(strings.TrimSpace(n.Name))
	table, hasTable := n.ExtraInfo["Table"].(string)
	_, hasFunc := n.ExtraInfo["Function"]
	switch {
	case readerPlanOps[name]:
		*readers++
	case hasTable && table != "" && !hasFunc && strings.HasSuffix(name, "_SCAN"):
		// A scan of a catalog table: SEQ_SCAN, TABLE_SCAN, POSTGRES_SCAN, ...
	case allowedPlanOperators[name], isRemoteWriteOp(name):
	default:
		return fmt.Sprintf("%s (plan operator %s is not on the allow-list)", externalAccessDeniedMsg, name)
	}
	for i := range n.Children {
		if reason := checkPlanNode(&n.Children[i], readers); reason != "" {
			return reason
		}
	}
	return ""
}

// --- secrets (a token screen: secret DDL is not serializable) ---------------

// touchesSecrets reports whether the statement(s) create, drop, list or use a
// DuckDB secret. It works on the comment-free token stream from usersql, so
// comments, quoting and string literals cannot hide a keyword, and it errs
// towards denying. It stays a token screen (not an AST check) because secret
// DDL is not a SELECT and so is never serializable; the AST policy refuses the
// secret table functions as well.
func touchesSecrets(raw string) bool { return tokensTouchSecrets(usersql.Tokens(raw), 0) }

var secretDDLModifiers = map[string]bool{
	"OR": true, "REPLACE": true, "PERSISTENT": true, "TEMPORARY": true, "TEMP": true,
	"IF": true, "NOT": true, "EXISTS": true,
}

func tokensTouchSecrets(stmts [][]usersql.Token, depth int) bool {
	if depth > 4 {
		return true
	}
	isName := func(t usersql.Token, name string) bool {
		return (t.Kind == usersql.TokWord || t.Kind == usersql.TokIdent) && strings.EqualFold(t.Text, name)
	}
	for _, st := range stmts {
		first := ""
		if len(st) > 0 && st[0].Kind == usersql.TokWord {
			first = st[0].Text
		}
		for i, t := range st {
			if isName(t, "duckdb_secrets") || isName(t, "which_secret") || isName(t, "duckdb_secret_types") {
				return true
			}
			if (t.Kind == usersql.TokWord || t.Kind == usersql.TokIdent) && strings.EqualFold(t.Text, "secret") {
				if first == "ATTACH" {
					return true
				}
				for j := i - 1; j >= 0; j-- {
					if st[j].Kind != usersql.TokWord {
						break
					}
					if secretDDLModifiers[st[j].Text] {
						continue
					}
					if st[j].Text == "CREATE" || st[j].Text == "DROP" || st[j].Text == "ALTER" {
						return true
					}
					break
				}
			}
			if (first == "SET" || first == "RESET") && strings.Contains(strings.ToUpper(t.Text), "SECRET") {
				return true
			}
			if (isName(t, "query") || isName(t, "query_table")) && i+1 < len(st) && st[i+1].Kind == usersql.TokParen && st[i+1].Text == "(" {
				// The SQL a literal argument carries is checked in turn. A
				// non-literal argument is refused by the policy itself, with its
				// own reason (it is not a secret problem).
				if i+3 < len(st) && st[i+2].Kind == usersql.TokString && st[i+3].Kind == usersql.TokParen && st[i+3].Text == ")" {
					if tokensTouchSecrets(usersql.Tokens(st[i+2].Text), depth+1) {
						return true
					}
				}
				continue
			}
		}
	}
	return false
}

// --- tier gates -------------------------------------------------------------

// checkSQLClass gates a classified statement against the sandbox tier: reads
// are allowed everywhere, DML needs read-write or full, DDL and COPY ... TO
// need full, and a denied statement is refused in every tier. reason, when
// non-empty, is surfaced verbatim instead of the generic per-tier message.
//
// This is the first of the three write gates (tier). The other two are that
// the target connection is writable and that the person approves
// (Handler.gateWrite, handleRunQuery).
func (sc *SandboxConfig) checkSQLClass(class sqlClass, reason string) error {
	if reason == secretsDeniedMsg {
		return errors.New(secretsDeniedMsg)
	}
	if class == sqlClassOther {
		if reason == "" {
			reason = couldntVerifyMsg
		}
		return fmt.Errorf("run_query: %s", reason)
	}
	switch sc.Mode {
	case ModeReadOnly:
		if class != sqlClassRead {
			return fmt.Errorf("run_query: the agent is read-only here: only read statements (SELECT, WITH...SELECT, SHOW, DESCRIBE, EXPLAIN, SUMMARIZE, or a read-style PRAGMA) are allowed. Changing data needs the read-write tier")
		}
	case ModeReadWrite:
		if class == sqlClassDDL {
			return fmt.Errorf("run_query: this statement changes a connection's structure or writes a file, which needs the full tier (read-write allows INSERT, UPDATE and DELETE only)")
		}
	case ModeFull:
	default:
		return fmt.Errorf("unknown sandbox mode: %q", sc.Mode)
	}
	return nil
}

// errReadOnlyBindingDenied is the ONE message checkSQLClassReadOnly returns for
// a denial: the render-time gate is read-only in every tier, so the reason the
// classifier gave (which can name a tier requirement) would be false for a
// full-tier caller. Callers log the reason themselves.
var errReadOnlyBindingDenied = errors.New("live bindings are read-only queries over this workspace's connections (no writes, and files only inside data/)")

// checkSQLClassReadOnly is the render-time gate for a live A2UI binding or a
// notebook cell: sqlClassRead regardless of the tier, because the statement
// re-runs on every render.
func (sc *SandboxConfig) checkSQLClassReadOnly(class sqlClass, _ string) error {
	if class == sqlClassRead {
		return nil
	}
	return errReadOnlyBindingDenied
}

// classifySQL classifies raw (with its $name params) with no workspace context:
// no data/ folder and no storage scopes, so no file or URL read classifies as a
// read. Handlers use Handler.classify, which supplies the workspace.
func classifySQL(ctx context.Context, userDB queryDB, raw string, params map[string]any) (sqlClass, string) {
	v := analyzeSQL(ctx, userDB, nil, raw, params)
	return v.Class, v.Reason
}

// IsReadOnlyStatement reports whether sql (with the given bound params)
// classifies as a pure read against db: exported so a caller outside this
// package that only has the original run_query request (the TUI's unsaved-
// changes tracker, the MCP debounced saver) can ask "did this statement
// write?" without duplicating classification logic.
func IsReadOnlyStatement(ctx context.Context, db *sql.DB, sqlText string, params map[string]any) bool {
	if db == nil {
		return false
	}
	class, _ := classifySQL(ctx, db, sqlText, params)
	return class == sqlClassRead
}
