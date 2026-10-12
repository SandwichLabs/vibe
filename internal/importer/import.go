package importer

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// Mode selects how Import writes into an existing (or new) table.
type Mode string

const (
	ModeCreate  Mode = "create"
	ModeReplace Mode = "replace"
	ModeAppend  Mode = "append"
	ModeUpsert  Mode = "upsert"
)

// Options parameterizes one Import call.
type Options struct {
	Source
	Table  string             `json:"table"`
	Mode   Mode               `json:"mode"`
	Key    string             `json:"key"`
	Types  map[string]ColType `json:"types"`
	DryRun bool               `json:"dry_run"`
}

// BadValue is one pre-flight failure: a specific (row, column) whose value
// can't be written for the given Reason ("type", "missing_parent",
// "duplicate_key", or "null_key").
type BadValue struct {
	Row    int    `json:"row"`
	Column string `json:"column"`
	Value  string `json:"value"`
	Want   string `json:"want"`
	Reason string `json:"reason"`
}

// Summary is the outcome of an Import call: what would happen (DryRun) or
// what happened. OK=false with no Go error means validation failed for a
// reason a human can act on (Blocked, or Missing/Extra/BadValues) — nothing
// was written.
type Summary struct {
	OK           bool       `json:"ok"`
	Mode         Mode       `json:"mode"`
	Table        string     `json:"table"`
	Exists       bool       `json:"exists"`
	ExistingRows int        `json:"existing_rows"`
	TableKey     string     `json:"table_key"`
	Modes        []Mode     `json:"modes"`
	Rows         int        `json:"rows"`
	Inserted     int        `json:"inserted"`
	Updated      int        `json:"updated"`
	Unchanged    int        `json:"unchanged"`
	Replaced     int        `json:"replaced"`
	ExactDupes   int        `json:"exact_dupes"`
	Missing      []string   `json:"missing"`
	Extra        []string   `json:"extra"`
	BadValues    []BadValue `json:"bad_values"`
	BadCount     int        `json:"bad_count"`
	Blocked      string     `json:"blocked"`
}

// beforeCommit is a test hook: when non-nil, it runs just before the write
// transaction commits and, if it returns an error, the transaction is rolled
// back instead — proving a mid-import failure leaves the original table,
// rows, and sequence fully intact.
var beforeCommit func(*sql.Tx) error

// targetCol is one column of the table Import writes into, with enough
// information to build both the CREATE TABLE DDL (create/replace) and the
// per-row cast/fits expression against its matching staged source column.
type targetCol struct {
	Name        string  // target (table) column identifier
	Friendly    ColType // valid when hasFriendly
	HasFriendly bool
	RawType     string     // target's raw DuckDB type, used when !hasFriendly (append/upsert only)
	Staged      *stagedCol // matching staged source column, nil if the file doesn't have this column
	HasDefault  bool       // append/upsert: column has a DEFAULT, so a missing file column is fine
}

// existingTable describes a table already present in the file, gathered
// once up front for append/upsert/replace decisions.
type existingTable struct {
	tableName string // the catalog's own spelling — see describeExistingTable
	rowCount  int
	columns   []targetCol
	pkCol     string // single-column PK name, "" if none/composite
	fkOut     bool   // this table has an outgoing FK (references another table)
	fkIn      bool   // another table has an FK referencing this table
	fks       []fkInfo
}

// fkInfo is one single-column outgoing foreign key on a table.
type fkInfo struct {
	Column    string
	RefTable  string
	RefColumn string
}

// Import stages the file, resolves the write target for o.Mode, pre-flights
// every row against the target types/constraints, and — unless DryRun or the
// pre-flight found a problem — writes everything in a single transaction
// (all-or-nothing: any failure rolls the whole import back).
//
// o.Table may be catalog-qualified ("catalog.schema.table" or "schema.table"):
// the whole import then runs on one pinned connection with USE pointing at that
// catalog and schema, so every unqualified statement below (and the table's
// auto-number sequence) lands there. An unqualified name targets the
// connection's default catalog, exactly as before. Summary.Table is the
// canonical name in the same form the caller used.
func Import(ctx context.Context, db *sql.DB, o Options) (*Summary, error) {
	if db == nil {
		return nil, fmt.Errorf("no database available")
	}
	ref, err := engine.ParseTable(o.Table)
	if err != nil {
		return nil, fmt.Errorf("invalid table name %q", o.Table)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	restore, err := pinTarget(ctx, conn, ref)
	if err != nil {
		return nil, err
	}
	defer restore()
	defer dropStage(ctx, conn)

	o.Table = ref.Name
	sum, err := importOn(ctx, conn, o)
	if sum != nil && (ref.Catalog != "" || ref.Schema != "") {
		sum.Table = qualifyLike(ref, sum.Table)
	}
	return sum, err
}

// qualifyLike re-applies the caller's qualification to the canonical table
// name Import resolved.
func qualifyLike(ref session.TableRef, name string) string {
	ref.Name = name
	return ref.String()
}

// pinTarget points an already-pinned connection at the target catalog and
// schema (USE) and returns a restore func. If the restore fails, the
// connection is discarded rather than returned to the pool.
func pinTarget(ctx context.Context, conn *sql.Conn, ref session.TableRef) (func(), error) {
	noop := func() {}
	if ref.Catalog == "" && ref.Schema == "" {
		return noop, nil
	}
	var origCat, origSchema string
	if err := conn.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&origCat, &origSchema); err != nil {
		return noop, fmt.Errorf("failed to read current catalog: %w", err)
	}
	if ref.Catalog == "" && ref.Schema != "" {
		// "x.t": a schema of the current catalog if it has one called x,
		// otherwise an attached catalog x (DuckDB resolves it the same way).
		var isSchema, isCatalog bool
		if err := conn.QueryRowContext(ctx, `SELECT
			EXISTS (SELECT 1 FROM duckdb_schemas() WHERE database_name = current_database() AND schema_name = $1),
			EXISTS (SELECT 1 FROM duckdb_databases() WHERE database_name = $1 AND NOT internal)`, ref.Schema).Scan(&isSchema, &isCatalog); err != nil {
			return noop, fmt.Errorf("failed to resolve import target %q: %w", ref.String(), err)
		}
		if !isSchema && isCatalog {
			ref.Catalog, ref.Schema = ref.Schema, ""
		}
	}
	cat := ref.Catalog
	if cat == "" {
		cat = origCat
	} else if cat != origCat {
		// Only a real, attached database is a valid target: never DuckDB's own
		// "system" or "temp" catalogs.
		var ok bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM duckdb_databases() WHERE database_name = $1 AND NOT internal)`, cat).Scan(&ok); err != nil {
			return noop, fmt.Errorf("failed to resolve import target %q: %w", ref.String(), err)
		}
		if !ok {
			return noop, fmt.Errorf("unknown connection %q", cat)
		}
	}
	target := session.QuoteIdent(cat)
	if ref.Schema != "" {
		target += "." + session.QuoteIdent(ref.Schema)
	}
	if _, err := conn.ExecContext(ctx, "USE "+target); err != nil {
		return noop, fmt.Errorf("failed to select import target %q: %w", ref.String(), err)
	}
	return func() {
		if _, err := conn.ExecContext(context.Background(), "USE "+session.QuoteIdent(origCat)+"."+session.QuoteIdent(origSchema)); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}, nil
}

// importOn is Import's body, run on a connection whose current catalog and
// schema are the target (o.Table is the bare table name).
func importOn(ctx context.Context, conn *sql.Conn, o Options) (*Summary, error) {
	// Every statement below names the target table by its resolved
	// catalog.schema.table, so neither the connection's search path nor a
	// same-named TEMP table can redirect a write.
	var tcat, tschema string
	if err := conn.QueryRowContext(ctx, "SELECT current_database(), current_schema()").Scan(&tcat, &tschema); err != nil {
		return nil, fmt.Errorf("failed to resolve import target: %w", err)
	}
	ctx = context.WithValue(ctx, targetKey{}, session.TableRef{Catalog: tcat, Schema: tschema})
	sum := &Summary{Table: o.Table, Mode: o.Mode}

	if msg := checkDecimalScales(o.Types); msg != "" {
		sum.Blocked = msg
		return sum, nil
	}

	existing, err := describeExistingTable(ctx, conn, o.Table)
	if err != nil {
		return nil, err
	}
	sum.Exists = existing != nil
	if existing != nil {
		// The catalog's own spelling, not the caller's — DuckDB's quoted-
		// identifier lookup is case-insensitive (QuoteIdent("customers")
		// resolves a table actually stored as "Customers"), but nothing
		// downstream may keep using the caller's casing once a match is
		// found: the auto-number sequence was named from the table's
		// ORIGINAL casing at CREATE time (_vibe_seq_Customers_id), and a
		// later reference built from "customers" wouldn't find it,
		// silently skipping the bump. Every later step (write, Replace,
		// Summary.Table, and the skill this Import's caller writes from
		// Summary.Table) uses this canonical name from here on.
		o.Table = existing.tableName
		sum.Table = existing.tableName
		sum.ExistingRows = existing.rowCount
		sum.TableKey = existing.pkCol
	}

	// Text overrides are forwarded to stage() so a column overridden to Text
	// keeps its original file text rather than a lossy sniffed DOUBLE.
	staged, err := stage(ctx, conn, o.Source, textOverrides(o.Types))
	if err != nil {
		return nil, err
	}
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+stageTable).Scan(&sum.Rows); err != nil {
		return nil, fmt.Errorf("failed to count file rows: %w", err)
	}
	if sum.Rows == 0 {
		// An empty or header-only file has nothing to write; without this,
		// create/replace built a schema-only "junk table" from a single
		// synthesized column, and KeyAuto's own name-collision dedupe could
		// even produce a second, pointless id column. Every mode is
		// refused the same way — there's no useful "append/upsert zero
		// rows" either.
		sum.Blocked = "the file has no rows"
		return sum, nil
	}

	run := importRun{conn: conn, o: o, sum: sum, existing: existing, staged: staged}
	if sum.Exists {
		err = run.withExisting(ctx)
	} else {
		err = run.create(ctx)
	}
	if err != nil {
		return nil, err
	}
	return sum, nil
}

// checkDecimalScales returns the reason an override is unusable, or "". It is
// caught here, not left to DuckDB: an out-of-range scale otherwise reaches
// CREATE TABLE/the fits scan as a raw "Scale cannot be bigger than width"
// parser error.
func checkDecimalScales(types map[string]ColType) string {
	for name, ct := range types {
		if ct.Type == TypeDecimal && (ct.Scale < 0 || ct.Scale > maxDecimalScale) {
			return fmt.Sprintf("invalid decimal scale %d for column %q (must be 0-%d)", ct.Scale, name, maxDecimalScale)
		}
	}
	return ""
}

// textOverrides names the columns overridden to Text.
func textOverrides(types map[string]ColType) map[string]bool {
	textCols := map[string]bool{}
	for name, ct := range types {
		if ct.Type == TypeText {
			textCols[name] = true
		}
	}
	return textCols
}

// importRun is one import past staging: the connection, the options (with the
// canonical table name), the summary being filled in, the table it found (nil
// when it does not exist) and the staged file columns.
type importRun struct {
	conn     *sql.Conn
	o        Options
	sum      *Summary
	existing *existingTable
	staged   []stagedCol
}

// create handles a table that does not exist yet: only mode create applies.
func (r importRun) create(ctx context.Context) error {
	o, sum := r.o, r.sum
	sum.Modes = []Mode{ModeCreate}
	if o.Mode == "" {
		o.Mode, sum.Mode = ModeCreate, ModeCreate
	}
	if o.Mode != ModeCreate {
		sum.Blocked = fmt.Sprintf("table %q does not exist; pass mode create", o.Table)
		return nil
	}
	target := createTargetColumns(ctx, r.conn, r.staged, o.Types)
	if err := preflightCreate(ctx, r.conn, sum, target, o.Key); err != nil {
		return err
	}
	if sum.OK && !o.DryRun {
		return writeCreate(ctx, r.conn, o.Table, target, o.Key, sum.Rows)
	}
	return nil
}

// withExisting checks the requested mode against the ones available for the
// table that is already there, then runs it.
func (r importRun) withExisting(ctx context.Context) error {
	o, sum := r.o, r.sum
	sum.Modes = allowedModes(r.existing, r.staged)
	if o.Mode == "" {
		sum.Blocked = fmt.Sprintf("table %q already exists (%d rows); pass mode %s",
			o.Table, sum.ExistingRows, joinModes(sum.Modes))
		return nil
	}
	if !modeAllowed(sum.Modes, o.Mode) {
		sum.Blocked = fmt.Sprintf("mode %q is not available for %q; available: %s", o.Mode, o.Table, joinModes(sum.Modes))
		return nil
	}
	switch o.Mode {
	case ModeReplace:
		return r.replace(ctx)
	case ModeAppend:
		return r.append(ctx)
	case ModeUpsert:
		return r.upsert(ctx)
	}
	return nil
}

func (r importRun) replace(ctx context.Context) error {
	o, sum := r.o, r.sum
	target := createTargetColumns(ctx, r.conn, r.staged, o.Types)
	if err := preflightCreate(ctx, r.conn, sum, target, o.Key); err != nil {
		return err
	}
	if !sum.OK {
		return nil
	}
	// The old row count is known as soon as the pre-flight passes — a dry run
	// can report it without touching the table.
	sum.Replaced = r.existing.rowCount
	if o.DryRun {
		return nil
	}
	return writeReplace(ctx, r.conn, o.Table, r.existing, target, o.Key, sum.Rows)
}

func (r importRun) append(ctx context.Context) error {
	o, sum := r.o, r.sum
	target, missing, extra := matchExistingColumns(r.existing, r.staged)
	sum.Missing, sum.Extra = missing, extra
	if err := preflightAppend(ctx, r.conn, sum, r.existing, target); err != nil {
		return err
	}
	if !sum.OK || o.DryRun {
		return nil
	}
	n, err := writeAppendOrUpsert(ctx, r.conn, o.Table, r.existing, target, false)
	if err != nil {
		return err
	}
	sum.Inserted = n
	return nil
}

func (r importRun) upsert(ctx context.Context) error {
	o, sum := r.o, r.sum
	target, missing, extra := matchExistingColumns(r.existing, r.staged)
	sum.Missing, sum.Extra = missing, extra
	if err := preflightUpsert(ctx, r.conn, sum, r.existing, target); err != nil {
		return err
	}
	if !sum.OK {
		return nil
	}
	ins, upd, unch, err := countUpsert(ctx, r.conn, o.Table, r.existing, target)
	if err != nil {
		return err
	}
	sum.Inserted, sum.Updated, sum.Unchanged = ins, upd, unch
	if o.DryRun {
		return nil
	}
	_, err = writeAppendOrUpsert(ctx, r.conn, o.Table, r.existing, target, true)
	return err
}

// describeExistingTable gathers everything Import needs to know about a
// table that might already exist: row count, columns (with raw type,
// default), single-column PK (if any), and whether it has any foreign key
// relationship at all (incoming or outgoing) — the Replace pre-flight only
// needs to know "any FK", not which one.
func describeExistingTable(ctx context.Context, conn *sql.Conn, table string) (*existingTable, error) {
	// DuckDB's catalog lookup for a QUOTED identifier is case-insensitive
	// (SELECT * FROM "customers" resolves a table actually named
	// "Customers"), but information_schema/duckdb_constraints report the
	// table's stored (original-case) name and a plain string_name=$1
	// comparison is case-sensitive — so a lowercase table_name=$1 filter
	// here would silently miss "Customers", the dry run would say "create"
	// (existing==nil), and the real run would hit a raw CREATE TABLE
	// "already exists" catalog error instead of matching the same table.
	// tableSQL(ctx, table) below still resolves correctly regardless
	// of case (confirmed against DuckDB 1.4.1), so this lower() compare is
	// the only fix needed to FIND the table — but every step after this
	// function returns must use the catalog's own spelling (et.tableName),
	// not the caller's, or a case-different reference elsewhere (the
	// auto-number sequence name, a manual `information_schema` lookup that
	// isn't lower()-compared) silently stops matching. See Import's own
	// comment where it reassigns o.Table from et.tableName.
	var canonical sql.NullString
	if err := conn.QueryRowContext(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_catalog=current_database() AND table_schema=current_schema() AND lower(table_name)=lower($1) LIMIT 1", table,
	).Scan(&canonical); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to check for existing table: %w", err)
	}
	if !canonical.Valid {
		return nil, nil
	}
	table = canonical.String

	et := &existingTable{tableName: table}
	if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableSQL(ctx, table))).Scan(&et.rowCount); err != nil {
		return nil, fmt.Errorf("failed to count rows in %q: %w", table, err)
	}

	if err := et.loadColumns(ctx, conn); err != nil {
		return nil, err
	}
	if err := et.loadConstraints(ctx, conn); err != nil {
		return nil, err
	}
	return et, nil
}

// loadColumns reads the table's columns (raw type, default) in order.
func (et *existingTable) loadColumns(ctx context.Context, conn *sql.Conn) error {
	table := et.tableName
	rows, err := conn.QueryContext(ctx,
		`SELECT column_name, data_type, column_default IS NOT NULL
		   FROM information_schema.columns
		  WHERE table_catalog=current_database() AND table_schema=current_schema() AND lower(table_name)=lower($1) ORDER BY ordinal_position`, table)
	if err != nil {
		return fmt.Errorf("failed to describe %q: %w", table, err)
	}
	for rows.Next() {
		var c targetCol
		var raw string
		if err := rows.Scan(&c.Name, &raw, &c.HasDefault); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan column: %w", err)
		}
		c.RawType = raw
		if ct, ok := FromDuckDB(raw); ok {
			c.Friendly, c.HasFriendly = ct, true
		}
		et.columns = append(et.columns, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate columns: %w", err)
	}
	return nil
}

// loadConstraints reads the single-column primary key and the foreign keys
// that touch the table, incoming or outgoing.
func (et *existingTable) loadConstraints(ctx context.Context, conn *sql.Conn) error {
	table := et.tableName
	crows, err := conn.QueryContext(ctx,
		`SELECT constraint_type, constraint_column_names, referenced_table, referenced_column_names, table_name
		   FROM duckdb_constraints()
		  WHERE database_name=current_database() AND schema_name=current_schema() AND (lower(table_name)=lower($1) OR lower(referenced_table)=lower($1))
		    AND constraint_type IN ('PRIMARY KEY','FOREIGN KEY')`, table)
	if err != nil {
		return fmt.Errorf("failed to read constraints for %q: %w", table, err)
	}
	for crows.Next() {
		var ctype string
		var cols, refCols []any
		var refTable, ownTable sql.NullString
		if err := crows.Scan(&ctype, &cols, &refTable, &refCols, &ownTable); err != nil {
			crows.Close()
			return fmt.Errorf("failed to scan constraint: %w", err)
		}
		switch ctype {
		case "PRIMARY KEY":
			et.notePrimaryKey(cols)
		case "FOREIGN KEY":
			et.noteForeignKey(table, ownTable, cols, refTable, refCols)
		}
	}
	crows.Close()
	if err := crows.Err(); err != nil {
		return fmt.Errorf("failed to iterate constraints: %w", err)
	}
	return nil
}

func (et *existingTable) notePrimaryKey(cols []any) {
	if len(cols) != 1 {
		return
	}
	if s, ok := cols[0].(string); ok {
		et.pkCol = s
	}
}

func (et *existingTable) noteForeignKey(table string, ownTable sql.NullString, cols []any, refTable sql.NullString, refCols []any) {
	if !(ownTable.Valid && strings.EqualFold(ownTable.String, table)) {
		et.fkIn = true
		return
	}
	et.fkOut = true
	if len(cols) != 1 || len(refCols) != 1 {
		return
	}
	col, _ := cols[0].(string)
	refCol, _ := refCols[0].(string)
	if col != "" && refCol != "" && refTable.Valid {
		et.fks = append(et.fks, fkInfo{Column: col, RefTable: refTable.String, RefColumn: refCol})
	}
}

// createTargetColumns builds the target column list for create/replace:
// every staged (file) column, normalized/deduped to an identifier, typed by
// inference unless overridden by o.Types (keyed by the normalized name).
func createTargetColumns(ctx context.Context, conn *sql.Conn, staged []stagedCol, overrides map[string]ColType) []targetCol {
	used := map[string]bool{}
	out := make([]targetCol, 0, len(staged))
	for i := range staged {
		sc := &staged[i]
		name := dedupeName(NormalizeName(sc.SourceName), used)
		var ct ColType
		if overrides != nil {
			if o, ok := overrides[name]; ok {
				ct = o
			} else {
				ct = inferType(ctx, conn, *sc)
			}
		} else {
			ct = inferType(ctx, conn, *sc)
		}
		out = append(out, targetCol{Name: name, Friendly: ct, HasFriendly: true, Staged: sc})
	}
	return out
}

// matchExistingColumns matches an existing table's columns to the staged
// file columns by NormalizeName, case-insensitive, on both sides. missing is
// table columns absent from the file with no DEFAULT; extra is file columns
// absent from the table (including every column PAST the first when two
// file headers collide on the same normalized name — see dedupeFileColumns).
func matchExistingColumns(existing *existingTable, staged []stagedCol) (target []targetCol, missing, extra []string) {
	byNorm, collided := dedupeFileColumns(staged)
	fileMatched := map[string]bool{}
	target = make([]targetCol, len(existing.columns))
	for i, c := range existing.columns {
		key := strings.ToLower(NormalizeName(c.Name))
		c.Staged = byNorm[key]
		target[i] = c
		if c.Staged != nil {
			fileMatched[key] = true
		} else if !c.HasDefault {
			missing = append(missing, c.Name)
		}
	}
	extra = append(extra, collided...)
	for i := range staged {
		key := strings.ToLower(NormalizeName(staged[i].SourceName))
		if !fileMatched[key] && byNorm[key] == &staged[i] {
			extra = append(extra, staged[i].SourceName)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return target, missing, extra
}

// dedupeFileColumns builds the normalized-name -> staged-column lookup
// matchExistingColumns needs, but — unlike a plain map literal — the FIRST
// file column to normalize to a given name wins that name; every later
// column colliding on it (e.g. "Order Date" then "order_date") is reported
// back as collided instead of silently overwriting the first one's match
// and vanishing. This mirrors createTargetColumns' dedupeName pass over the
// same file, just reporting collisions instead of renaming them (append/
// upsert can't rename a column that must match an existing one).
func dedupeFileColumns(staged []stagedCol) (byNorm map[string]*stagedCol, collided []string) {
	byNorm = map[string]*stagedCol{}
	for i := range staged {
		key := strings.ToLower(NormalizeName(staged[i].SourceName))
		if _, taken := byNorm[key]; taken {
			collided = append(collided, staged[i].SourceName)
			continue
		}
		byNorm[key] = &staged[i]
	}
	return byNorm, collided
}

// allowedModes lists the modes usable for an existing table + file: upsert
// (needs a single-column PK present in the file), replace (unless the table
// has any FK relationship), append. The first entry is the UI's/CLI's
// default, chosen so it never duplicates rows on a re-import.
func allowedModes(existing *existingTable, staged []stagedCol) []Mode {
	var modes []Mode
	if existing.pkCol != "" {
		for _, sc := range staged {
			if strings.EqualFold(NormalizeName(sc.SourceName), existing.pkCol) {
				modes = append(modes, ModeUpsert)
				break
			}
		}
	}
	if !existing.fkOut && !existing.fkIn {
		modes = append(modes, ModeReplace)
	}
	modes = append(modes, ModeAppend)
	return modes
}

func modeAllowed(modes []Mode, m Mode) bool {
	for _, x := range modes {
		if x == m {
			return true
		}
	}
	return false
}

func joinModes(modes []Mode) string {
	s := make([]string, len(modes))
	for i, m := range modes {
		s[i] = string(m)
	}
	return strings.Join(s, "|")
}

type targetKey struct{}

// tableSQL renders table as the resolved, quoted catalog.schema.table the
// import was pinned to (see importOn); outside an import it is just a quoted
// identifier.
func tableSQL(ctx context.Context, table string) string {
	ref, _ := ctx.Value(targetKey{}).(session.TableRef)
	ref.Name = table
	return ref.SQL()
}
