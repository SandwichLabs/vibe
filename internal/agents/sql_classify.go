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
	_ "github.com/marcboeker/go-duckdb/v2" // DuckDB driver, for the dedicated classifier connection below

	"github.com/sandwich-labs/vibe-app/internal/engine"
)

// sqlClass is the risk tier a run_query statement is classified into, used
// to gate it per-statement instead of by the blanket "data" category (#63).
//
// Nothing about this classifier is guesswork on the read decision, the
// statement count, or the external-access decision — all three are answered
// by asking the REAL DuckDB parser/catalog, never by hand-written lexing or
// pattern-matching against file extensions:
//   - statement count: duckdb_extract_statements (via countStatements) —
//     the same C API DuckDB's own CLI uses to split a script into
//     statements. Round 1/2 counted semicolons after a hand-rolled
//     comment/string stripper; that stripper didn't understand DuckDB
//     dollar-quoting ($$...$$) or E' escape strings, so a second, smuggled
//     statement hidden behind one of those could slip through uncounted.
//   - the read decision: json_serialize_sql — DuckDB only serializes a
//     batch of pure SELECT statements, so success (plus the AST checks in
//     checkExternalAccess) is the read signal.
//   - external-access: real catalog membership (duckdb_tables()/
//     duckdb_views() on the user's own database), not a guess based on a
//     table name containing a slash or ending in ".csv" — round 2's
//     extension list missed .tsv/.jsonl entirely and couldn't see through
//     `FROM s.csv` (schema "s", table "csv" — no extension in the table
//     name field at all) or DESCRIBE/SHOW/SUMMARIZE's string-literal-target
//     shorthand (DuckDB's documented way to read an arbitrary file, and
//     structurally indistinguishable in the AST from a bare identifier —
//     see classifyDescribeShowSummarize).
//
// Hand-written lexing (classifyFallback, stripCommentsAndStrings) survives
// only as the DML-vs-other tier for statements the real parser has already
// confirmed are a single statement but not a pure SELECT — and it can never
// grant sqlClassRead, only Write or Other, so even where its comment/string
// stripping is imperfect the worst it can do is be too strict.
type sqlClass int

const (
	// sqlClassRead is a statement DuckDB's parser confirms is a single,
	// pure-read statement (SELECT/CTE/SET-operation, or one of a small
	// explicit non-SELECT allow-list — EXPLAIN, PRAGMA introspection,
	// DESCRIBE/SHOW/SUMMARIZE of a catalog-resolved bare identifier) that
	// also passes the external-access checks in checkExternalAccess.
	// Allowed in every sandbox, including read-only.
	sqlClassRead sqlClass = iota
	// sqlClassWrite is row-level DML (INSERT/UPDATE/DELETE/MERGE/REPLACE)
	// that also passes the Write-tier bound-plan check
	// (checkPlanExternalAccess). Requires read-write or full.
	sqlClassWrite
	// sqlClassOther is everything else: DDL, ATTACH/DETACH, COPY/EXPORT/
	// IMPORT, INSTALL/LOAD, administrative SET/PRAGMA, external file/network
	// access (in any tier), multiple statements, unparseable input, or
	// anything unrecognized. Requires full — "be conservative": an unknown
	// or ambiguous shape is never treated as safe.
	sqlClassOther
)

// externalAccessDeniedMsg is surfaced whenever a statement reaches outside
// the database — the filesystem, the network, or environment/session state
// like current_setting (which can expose configured S3 credentials) — no
// matter which tier it was otherwise headed for. Read-only and read-write
// must never touch any of that; only full may (review fix, #63 round 2/3).
// This intentionally does NOT touch DuckDB's global enable_external_access
// setting — the importer (internal/importer, and full-sandbox run_query)
// still needs it enabled.
const externalAccessDeniedMsg = "external file/network access requires the full sandbox"

// plainIdentifierRe matches an unquoted SQL identifier — letters, digits,
// underscore, not starting with a digit. BASE_TABLE's table_name is always
// parsed out as a bare identifier (never a string literal or a path), so
// this is really just "does it look like a table name someone typed", used
// to give a clearer denial than externalAccessDeniedMsg for the extremely
// common case of a misspelled/nonexistent table (see
// unresolvedTableDeniedMsg).
var plainIdentifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// unresolvedTableDeniedMsg returns the denial reason for a BASE_TABLE
// reference that didn't resolve against the real catalog. An unqualified,
// plain-identifier table name (the ordinary "the model/user typed a table
// that doesn't exist" case) gets a plain "table \"x\" not found" — no
// mention of external access or the full sandbox. The generic
// externalAccessDeniedMsg previously used here read as a security refusal
// (misleading the model into thinking a missing table was a privilege
// problem, not a typo it should fix via introspect_schema) and, worse for a
// read-only *user* hitting an ordinary typo, actively suggested escalating
// to the full sandbox over what is almost always just a misspelled table
// name (review fix, #67 step 2 round 2: leads with the plain fact, drops the
// escalation nudge). A catalog- or schema-qualified reference, or anything
// that isn't a plain identifier, keeps the generic externalAccessDeniedMsg —
// those shapes are the ones that can actually be disguised external access,
// where the full-sandbox explanation is accurate and relevant.
func unresolvedTableDeniedMsg(catalogName, schema, table string) string {
	if catalogName == "" && schema == "" && plainIdentifierRe.MatchString(table) {
		return fmt.Sprintf("table %q not found", table)
	}
	return externalAccessDeniedMsg
}

// --- Statement counting: duckdb_extract_statements, not ';' -------------
//
// A single, lazily-created, in-memory DuckDB connection opened through the
// low-level mapping package (the thin Go wrapper around DuckDB's C API)
// purely to call duckdb_extract_statements — a syntax-only parse that
// splits raw into however many statements it actually contains, with no
// semantic binding (so, unlike Prepare/PrepareContext, it never fails just
// because a referenced table doesn't happen to exist on this isolated
// connection — verified empirically before choosing this API, per the
// review's "verify that behaviour with a test first").

var (
	stmtConnOnce sync.Once
	stmtConnDB   mapping.Database
	stmtConn     mapping.Connection
	stmtConnErr  error
	stmtConnMu   sync.Mutex // DuckDB connections are not safe for concurrent use
)

func statementConn() (mapping.Connection, error) {
	stmtConnOnce.Do(func() {
		var errMsg string
		if mapping.OpenExt("", &stmtConnDB, mapping.Config{}, &errMsg) == mapping.StateError {
			stmtConnErr = fmt.Errorf("failed to open statement-extraction database: %s", errMsg)
			return
		}
		if mapping.Connect(stmtConnDB, &stmtConn) == mapping.StateError {
			stmtConnErr = fmt.Errorf("failed to open statement-extraction connection")
		}
	})
	return stmtConn, stmtConnErr
}

// countStatements returns the number of SQL statements raw contains, via
// DuckDB's own duckdb_extract_statements. Confirmed empirically: a batch of
// N valid statements of any kind yields N; the proven bypass strings (a
// dollar-quoted or E'-escaped string that a naive lexer thinks is still
// "open," hiding a smuggled second statement behind it) correctly extract
// as 2; unparseable input yields 0 with a non-empty error message.
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

// --- Read-path AST connection (json_serialize_sql) -----------------------

var (
	classifierDBOnce sync.Once
	classifierDBConn *sql.DB
	classifierDBErr  error
)

func classifierDB() (*sql.DB, error) {
	classifierDBOnce.Do(func() {
		classifierDBConn, classifierDBErr = sql.Open("duckdb", "")
	})
	return classifierDBConn, classifierDBErr
}

// serializedSQLDoc is the shape of json_serialize_sql's output. On success
// (error: false) Statements holds exactly the parsed statement(s) as
// generic trees; json_serialize_sql only succeeds for a batch of pure
// SELECT statements — anything else (DML, DDL, PRAGMA, EXPLAIN, ...) comes
// back with error: true instead.
type serializedSQLDoc struct {
	Error      bool             `json:"error"`
	Statements []map[string]any `json:"statements"`
}

// serializeSQL asks the classifier connection to parse raw via
// json_serialize_sql. raw is always passed as a bound parameter — never
// concatenated into SQL text — so it is only ever treated as data, never as
// executable SQL by the classifier connection itself.
func serializeSQL(ctx context.Context, raw string) (*serializedSQLDoc, error) {
	db, err := classifierDB()
	if err != nil {
		return nil, fmt.Errorf("classifier connection unavailable: %w", err)
	}
	var out string
	// The explicit ::VARCHAR cast on the parameter and the outer CAST(...
	// AS VARCHAR) are both required: go-duckdb otherwise can't infer the
	// bound parameter's type, and json_serialize_sql's return value decodes
	// as a native map through the driver rather than a JSON string.
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

// --- Catalog: the real allow-list for table references ------------------

// catalog is a snapshot of every table and view that actually exists in the
// user's DuckDB, loaded fresh for each classifySQL call (the security
// decision must never be based on a stale view of the schema — e.g. a table
// the agent just dropped must stop resolving immediately). A zero-value
// catalog (entries == nil) resolves nothing, so a failure to load it fails
// closed rather than granting access.
type catalog struct {
	defaultCatalog string
	defaultSchema  string
	entries        map[string]bool // key: catalogKey(catalog, schema, table) — exact, catalog-qualified lookups
}

func catalogKey(catalogName, schema, table string) string {
	return strings.ToLower(catalogName) + "\x00" + strings.ToLower(schema) + "\x00" + strings.ToLower(table)
}

// allowedSystemObjects is a CLOSED allow-list of DuckDB's own introspection
// views a non-full sandbox may read (review fix, round 5 follow-up —
// replacing an any-catalog fallback that let ANY system view resolve, not
// just these). Keyed by "schema.table", lowercased. duckdb_databases and
// pragma_database_list are deliberately NOT here even though they're
// equally real, resolvable system views: both list every attached
// database's file path, which for this container's DuckDB blob is the
// decrypted temp database file — exactly what a non-full sandbox must never
// be able to read. Same reasoning excludes the rest of pg_catalog (e.g.
// pg_settings, still separately denied below) and duckdb_logs.
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
	"main.duckdb_types":                          true,
}

// deniedCatalogObjects is an extra, redundant-by-design belt-and-suspenders
// check for the handful of system objects a security review specifically
// called out (pg_settings can expose configured secrets such as S3
// credentials set via SET; duckdb_logs is DuckDB's internal log buffer):
// even if allowedSystemObjects were ever accidentally widened to include
// them, this still denies them by bare name, case-insensitively.
var deniedCatalogObjects = map[string]bool{
	"pg_settings": true,
	"duckdb_logs": true,
}

// systemObjectKey is the lookup key into allowedSystemObjects.
func systemObjectKey(schema, table string) string {
	return strings.ToLower(schema) + "." + strings.ToLower(table)
}

// resolves reports whether (catalogName, schema, table) names a real table
// or view this sandbox may read, applying DuckDB's own current-database/
// current-schema defaults for any part left empty (as an unqualified
// reference in the original SQL would).
//
// User tables/views — anything in the caller's own default catalog (or an
// explicitly-named non-"system" catalog, e.g. one the user ATTACHed under
// full sandbox) — resolve exactly as they exist, no extra restriction.
// Anything that only resolves under the "system" catalog (DuckDB's own
// introspection views: information_schema.*, pg_catalog.*, duckdb_* views,
// ...) additionally has to be on the closed allowedSystemObjects list —
// round 5 follow-up: the previous "any catalog matches" fallback correctly
// fixed information_schema.* but as a side effect also allowed
// duckdb_databases and pragma_database_list, both of which expose the
// decrypted temp database's file path.
func (c *catalog) resolves(catalogName, schema, table string) bool {
	if c == nil || table == "" || deniedCatalogObjects[strings.ToLower(table)] {
		return false
	}
	if schema == "" {
		schema = c.defaultSchema
	}

	if catalogName != "" {
		if !c.entries[catalogKey(catalogName, schema, table)] {
			return false
		}
		if strings.EqualFold(catalogName, "system") {
			return allowedSystemObjects[systemObjectKey(schema, table)]
		}
		return true
	}

	// Unqualified: try the user's own database first.
	if c.entries[catalogKey(c.defaultCatalog, schema, table)] {
		return true
	}
	// Only the closed allow-list resolves from the system catalog.
	if c.entries[catalogKey("system", schema, table)] {
		return allowedSystemObjects[systemObjectKey(schema, table)]
	}
	return false
}

// loadCatalog queries the user's own DuckDB (via the handler's real engine
// connection — never the isolated classifier connection above, which has no
// tables at all) for every table and view that exists, plus the current
// default catalog/schema for resolving unqualified references. It always
// returns a non-nil *catalog; on error the catalog is the zero value, which
// resolves nothing (fail closed).
func loadCatalog(ctx context.Context, userDB *sql.DB) (*catalog, error) {
	c := &catalog{entries: map[string]bool{}}
	if userDB == nil {
		return &catalog{}, fmt.Errorf("no duckdb connection available")
	}
	if err := userDB.QueryRowContext(ctx, "SELECT current_database()").Scan(&c.defaultCatalog); err != nil {
		return &catalog{}, err
	}
	if err := userDB.QueryRowContext(ctx, "SELECT current_schema()").Scan(&c.defaultSchema); err != nil {
		return &catalog{}, err
	}
	load := func(query string) error {
		rows, err := userDB.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var dbName, schema, name string
			if err := rows.Scan(&dbName, &schema, &name); err != nil {
				return err
			}
			c.entries[catalogKey(dbName, schema, name)] = true
		}
		return rows.Err()
	}
	if err := load("SELECT database_name, schema_name, table_name FROM duckdb_tables()"); err != nil {
		return &catalog{}, err
	}
	if err := load("SELECT database_name, schema_name, view_name FROM duckdb_views()"); err != nil {
		return &catalog{}, err
	}
	return c, nil
}

// --- External-access AST checks (SELECT/CTE path) ------------------------

// allowedTableFunctions are pure catalog/generator table functions — they
// never touch the filesystem or network, only DuckDB's in-memory catalog or
// a numeric sequence — so they're safe to allow from any sandbox that can
// read at all. Unlike a BASE_TABLE reference, these have no "catalog
// resolution" equivalent (they're functions, not tables), so they keep
// their own allow-list.
// duckdb_databases is deliberately NOT here (round 5 follow-up): the
// function-call form (`FROM duckdb_databases()`) returns the exact same
// per-attached-database file paths as the view form (`FROM
// duckdb_databases`), which allowedSystemObjects also excludes for that
// reason — this container's DuckDB blob is a decrypted temp file, and its
// path must never be readable outside the full sandbox.
var allowedTableFunctions = map[string]bool{
	"range": true, "generate_series": true, "unnest": true,
	"duckdb_tables": true, "duckdb_columns": true, "duckdb_views": true,
	"duckdb_schemas": true, "pragma_table_info": true,
	"duckdb_constraints": true, "duckdb_indexes": true,
	"duckdb_keywords": true, "duckdb_functions": true, "duckdb_types": true,
	"generate_subscripts": true,
}

// isDeniedScalarFunc flags scalar/table function calls that read the
// filesystem, network, or session/environment state wherever they appear in
// the AST — a SELECT list, a WHERE clause, or a FROM-clause table function
// — not just as a top-level TABLE_FUNCTION (e.g. `SELECT
// read_csv('/etc/passwd')` has no FROM clause at all, so the TABLE_FUNCTION
// allow-list check alone would miss it). current_setting is denied in every
// sandbox, including read-only (review fix, round 3 item D): it can expose
// configured secrets such as S3 credentials.
func isDeniedScalarFunc(name string) bool {
	lower := strings.ToLower(name)
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "read_") {
		return true
	}
	switch lower {
	case "getenv", "current_setting":
		return true
	}
	return false
}

// mutatingScalarFuncs are scalar functions that mutate persistent database
// state even though they can appear in a SELECT's expression list and look
// like an ordinary pure read — most notably nextval(), which advances a
// sequence's counter every time it's evaluated, a side effect exactly as
// real as an INSERT even though the statement carrying it (`SELECT
// nextval('s')`) parses as a plain SELECT with no BASE_TABLE write and no
// external-access concern at all. A "read" classification (sqlClassRead)
// must never let one of these through — confirmed live: a reviewer found
// `SELECT nextval('s')` classified as a pure read here, which is genuinely
// wrong (it mutates the sequence) regardless of which caller (run_query's
// own sandbox gating, or the TUI's unsaved-changes tracker via
// IsReadOnlyStatement) relies on the classification.
var mutatingScalarFuncs = map[string]bool{
	"nextval": true,
	"setval":  true,
}

// checkMutatingFunc walks stmt's AST for a call to one of mutatingScalarFuncs
// and returns a non-empty reason if found — separate from checkExternalAccess
// (whose reason text and framing are specifically about filesystem/network/
// session-state access) since a sequence mutation is a different kind of
// concern, not "external access".
func checkMutatingFunc(stmt map[string]any) string {
	reason := ""
	walkJSON(stmt, func(m map[string]any) bool {
		if reason != "" {
			return false
		}
		if m["class"] == "FUNCTION" {
			if name, _ := m["function_name"].(string); mutatingScalarFuncs[strings.ToLower(name)] {
				reason = fmt.Sprintf("%q mutates database state (a sequence) and is never classified as a read", name)
				return false
			}
		}
		return true
	})
	return reason
}

// checkExternalAccess walks one parsed statement's AST (as decoded from
// json_serialize_sql's JSON) and returns externalAccessDeniedMsg the moment
// it finds a BASE_TABLE reference that isn't a real catalog table/view or a
// CTE defined in this same statement, a TABLE_FUNCTION not on the allow
// list, or a denied scalar function call — or "" if the statement is clean.
// json_serialize_sql's schema varies by node type, so this walks the
// generic decoded tree (map[string]any / []any) rather than binding to
// typed structs for DuckDB's whole (large, version-specific) AST grammar.
func checkExternalAccess(cat *catalog, stmt map[string]any) string {
	cteNames := map[string]bool{}
	collectCTENames(stmt, cteNames)

	reason := ""
	walkJSON(stmt, func(m map[string]any) bool {
		if reason != "" {
			return false
		}
		switch m["type"] {
		case "TABLE_FUNCTION":
			if fn, ok := m["function"].(map[string]any); ok {
				name, _ := fn["function_name"].(string)
				if name != "" && !allowedTableFunctions[strings.ToLower(name)] {
					reason = externalAccessDeniedMsg
					return false
				}
			}
		case "BASE_TABLE":
			catalogName, _ := m["catalog_name"].(string)
			schema, _ := m["schema_name"].(string)
			table, _ := m["table_name"].(string)
			if catalogName == "" && schema == "" && cteNames[strings.ToLower(table)] {
				// A reference to a CTE defined earlier in this same
				// statement, not a real catalog object — nothing to
				// resolve.
				return true
			}
			if !cat.resolves(catalogName, schema, table) {
				reason = unresolvedTableDeniedMsg(catalogName, schema, table)
				return false
			}
		}
		if m["class"] == "FUNCTION" {
			if name, _ := m["function_name"].(string); isDeniedScalarFunc(name) {
				reason = externalAccessDeniedMsg
				return false
			}
		}
		return true
	})
	return reason
}

// --- Bound-plan external-access check (EXPLAIN FORMAT JSON) -------------
//
// json_serialize_sql (checkExternalAccess above) can only parse a pure
// SELECT, so it's blind to DML entirely — and a raw-text scan over
// untrusted SQL (the original Write-tier check) turned out to be
// bypassable just by quoting a function name ("read_csv"(...)) or wrapping
// it in a comment, since a scan over TEXT has no idea what DuckDB will
// actually bind that text to (review fix, round 4). EXPLAIN (FORMAT JSON)
// instead asks DuckDB itself to fully bind the statement against the real
// catalog and hand back its physical plan — the literal set of operators
// it would execute — with no way to hide a table-function scan behind
// quoting, a comment, or a replacement-scan shorthand, because by the time
// it reaches the plan it has already been resolved to a concrete operator.
//
// Plain EXPLAIN does not execute anything — verified with a test
// (TestCheckPlanExternalAccess_EXPLAINDoesNotExecute) that runs EXPLAIN
// (FORMAT JSON) on a DELETE and confirms the row count is unchanged
// afterward. EXPLAIN ANALYZE is never used here — that DOES execute.
//
// checkPlanExternalAccess is used both for the Write tier (replacing the
// text scan as the allow decision, per review instruction — the text scan
// stays on only as an extra deny layer ahead of this) and, as defence in
// depth, for the SELECT/read path alongside the existing AST check: two
// independently-derived answers (the parsed AST's declared table refs, and
// DuckDB's own bound plan) have to agree a statement is clean.

// planNode is one node of EXPLAIN (FORMAT JSON)'s physical_plan tree.
// extra_info's shape varies by operator (a bare table scan carries "Table";
// a table-function scan carries "Function"; everything is inconsistent
// enough that this only extracts what's needed by name, not a full typed
// grammar).
type planNode struct {
	Name      string         `json:"name"`
	Children  []planNode     `json:"children"`
	ExtraInfo map[string]any `json:"extra_info"`
}

// allowedPlanOperators is a closed allow-list of physical-plan operator
// names that never themselves read outside the database: relational
// combinators (join/filter/projection/aggregate/sort/limit/...), the DML
// operators, and the same small set of pure catalog/generator functions
// allowedTableFunctions already permits in the AST path. SEQ_SCAN/
// TABLE_SCAN/INDEX_SCAN are handled separately (case below) since they need
// catalog resolution, not a bare allow/deny. Anything not listed here and
// not a scan operator is denied — deny-by-default, not an enumerated
// deny-list of "READ_*" names, so an operator this list doesn't yet know
// about (a future DuckDB version, an extension) is refused rather than
// silently let through.
var allowedPlanOperators = map[string]bool{
	// Relational / structural — combine or transform rows already produced
	// by a leaf operator; no data source of their own.
	"PROJECTION": true, "FILTER": true, "HASH_JOIN": true, "LEFT_DELIM_JOIN": true,
	"RIGHT_DELIM_JOIN": true, "CROSS_PRODUCT": true, "NESTED_LOOP_JOIN": true,
	"PIECEWISE_MERGE_JOIN": true, "IE_JOIN": true, "ASOF_JOIN": true,
	"BLOCKWISE_NL_JOIN": true, "POSITIONAL_JOIN": true,
	"HASH_GROUP_BY": true, "PERFECT_HASH_GROUP_BY": true, "UNGROUPED_AGGREGATE": true,
	"SIMPLE_AGGREGATE": true, "WINDOW": true,
	"STREAMING_LIMIT": true, "LIMIT": true, "LIMIT_PERCENT": true, "TOP_N": true,
	"ORDER_BY": true, "UNION": true, "DISTINCT": true, "RESERVOIR_SAMPLE": true,
	"PIVOT": true, "UNPIVOT": true, "STRICT_UNPIVOT": true,
	"RECURSIVE_CTE": true, "CTE": true, "CTE_SCAN": true, "MATERIALIZED_CTE": true,
	"DELIM_SCAN": true, "COLUMN_DATA_SCAN": true, "CHUNK_SCAN": true,
	"EXPRESSION_SCAN": true, "DUMMY_SCAN": true, "EMPTY_RESULT": true,
	"RESULT_COLLECTOR": true, "EXPLAIN": true,
	// DML.
	"INSERT": true, "BATCH_INSERT": true, "UPDATE": true, "DELETE": true, "MERGE_INTO": true,
	// Pure catalog/generator functions — no filesystem or network access.
	// Mirrors allowedTableFunctions (the AST-path equivalent).
	"RANGE": true, "UNNEST": true, "GENERATE_SERIES": true, "GENERATE_SUBSCRIPTS": true,
	"DUCKDB_TABLES": true, "DUCKDB_COLUMNS": true, "DUCKDB_VIEWS": true,
	"DUCKDB_SCHEMAS": true, "DUCKDB_DATABASES": true, "DUCKDB_CONSTRAINTS": true,
	"DUCKDB_INDEXES": true, "DUCKDB_KEYWORDS": true, "DUCKDB_FUNCTIONS": true,
	"DUCKDB_TYPES": true, "PRAGMA_TABLE_INFO": true,
}

// planScanOperators are the operator names that read a real base
// table/view and therefore need catalog resolution rather than a bare
// allow/deny.
var planScanOperators = map[string]bool{
	"SEQ_SCAN": true, "TABLE_SCAN": true, "INDEX_SCAN": true,
}

// checkPlanExternalAccess runs EXPLAIN (FORMAT JSON) for stmt — the BOUND
// form, with args supplied positionally for every "?" BindParams left
// behind — on the user's real DuckDB connection, and walks the returned
// physical plan. It returns externalAccessDeniedMsg the moment it finds
// anything not on allowedPlanOperators / planScanOperators (catalog-checked)
// / a denied scalar function substring in the plan text, or "" if the plan
// is clean.
//
// If EXPLAIN itself fails to bind, that's still a denial (fail closed) —
// but NOT with externalAccessDeniedMsg, which would be misleading for an
// ordinary bind/syntax problem (review fix, round 5: an unbound `$id`
// placeholder used to reach this function directly, since classifySQL
// didn't bind parameters before classifying, and a failed-to-bind EXPLAIN
// was reported back as "requires the full sandbox" — wrong reason,
// coincidentally correct denial). It reports the real bind error instead.
func checkPlanExternalAccess(ctx context.Context, userDB *sql.DB, cat *catalog, stmt string, args []any) string {
	if userDB == nil {
		return "could not validate query: no database connection available"
	}
	rows, err := userDB.QueryContext(ctx, "EXPLAIN (FORMAT JSON) "+stmt, args...)
	if err != nil {
		return fmt.Sprintf("could not validate query: %v", err)
	}
	defer rows.Close()

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
		return fmt.Sprintf("could not validate query: %v", err)
	}
	if !found {
		return "could not validate query: EXPLAIN (FORMAT JSON) returned no physical_plan"
	}

	// Defence in depth: a denied scalar function (read_*, getenv,
	// current_setting) can appear buried inside an expression string deep
	// in extra_info rather than as its own node — a whole-text substring
	// scan over DuckDB's own bound plan JSON catches that regardless of
	// exactly which field it landed in, the same way the operator-name
	// check below catches it when it IS its own leaf node.
	if reason := planTextExternalAccessReason(planJSON); reason != "" {
		return reason
	}

	var nodes []planNode
	if err := json.Unmarshal([]byte(planJSON), &nodes); err != nil {
		return externalAccessDeniedMsg
	}
	for i := range nodes {
		if reason := checkPlanNode(cat, &nodes[i]); reason != "" {
			return reason
		}
	}
	return ""
}

func checkPlanNode(cat *catalog, n *planNode) string {
	name := strings.ToUpper(strings.TrimSpace(n.Name))
	switch {
	case planScanOperators[name]:
		table, _ := n.ExtraInfo["Table"].(string)
		if table == "" {
			return externalAccessDeniedMsg
		}
		if !cat.resolves("", "", table) {
			return unresolvedTableDeniedMsg("", "", table)
		}
	case !allowedPlanOperators[name]:
		return externalAccessDeniedMsg
	}
	for i := range n.Children {
		if reason := checkPlanNode(cat, &n.Children[i]); reason != "" {
			return reason
		}
	}
	return ""
}

// planTextExternalAccessReason scans DuckDB's own bound-plan JSON text
// (not the untrusted user SQL) for a denied scalar function name. Unlike
// the pre-round-4 Write-tier scan, this operates on text DuckDB itself
// produced after fully resolving the statement — there is no quoting,
// commenting, or replacement-scan trick left to hide behind by the time it
// reaches here.
func planTextExternalAccessReason(planJSON string) string {
	lower := strings.ToLower(planJSON)
	if strings.Contains(lower, "getenv") || strings.Contains(lower, "current_setting") {
		return externalAccessDeniedMsg
	}
	if strings.Contains(lower, "read_") {
		return externalAccessDeniedMsg
	}
	return ""
}

// collectCTENames walks the whole tree collecting every CTE name defined
// anywhere in the statement (DuckDB repeats a WITH clause's cte_map into
// every nested SELECT_NODE that can see it, so one pass over the whole tree
// picks up all of them regardless of nesting depth).
func collectCTENames(node any, names map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		if cm, ok := v["cte_map"].(map[string]any); ok {
			if arr, ok := cm["map"].([]any); ok {
				for _, item := range arr {
					if im, ok := item.(map[string]any); ok {
						if k, ok := im["key"].(string); ok && k != "" {
							names[strings.ToLower(k)] = true
						}
					}
				}
			}
		}
		for _, val := range v {
			collectCTENames(val, names)
		}
	case []any:
		for _, item := range v {
			collectCTENames(item, names)
		}
	}
}

// walkJSON depth-first-walks a tree decoded from encoding/json (so nodes
// are map[string]any / []any / scalars), calling visit on every object.
// visit returns false to skip descending into that object's children (the
// caller uses this to short-circuit once it has found what it's looking
// for; remaining sibling branches are still visited, which is harmless —
// just a little extra, bounded work).
func walkJSON(node any, visit func(map[string]any) bool) {
	switch v := node.(type) {
	case map[string]any:
		if !visit(v) {
			return
		}
		for _, val := range v {
			walkJSON(val, visit)
		}
	case []any:
		for _, item := range v {
			walkJSON(item, visit)
		}
	}
}

// containsShowRef reports whether the parsed statement contains a SHOW_REF
// node anywhere — the AST shape DuckDB rewrites DESCRIBE/SHOW/SUMMARIZE
// into. Those three commands get special handling (classifyDescribeShow
// Summarize) instead of the generic checkExternalAccess walk above: their
// string-literal-target shorthand (`SHOW '/etc/passwd'`) is DuckDB's
// documented way to read an arbitrary file, and it is structurally
// IDENTICAL in the AST to a bare identifier target (`SHOW t`) — both
// produce the same BASE_TABLE{table_name: "..."} shape, confirmed
// empirically — so this distinction can only be made by looking at the
// original SQL text, not the parsed tree.
func containsShowRef(stmt map[string]any) bool {
	found := false
	walkJSON(stmt, func(m map[string]any) bool {
		if found {
			return false
		}
		if m["type"] == "SHOW_REF" {
			found = true
			return false
		}
		return true
	})
	return found
}

// --- DESCRIBE / SHOW / SUMMARIZE: bare catalog-resolved identifier only -

const identPattern = `[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?`

var (
	reDescribeShowSummarizePrefix = regexp.MustCompile(`(?i)^(DESCRIBE|SHOW|SUMMARIZE)\b`)
	reShowTables                  = regexp.MustCompile(`(?i)^SHOW\s+TABLES\s*;?\s*$`)
	reDescribeIdent               = regexp.MustCompile(`(?i)^DESCRIBE\s+(` + identPattern + `)\s*;?\s*$`)
	reShowIdent                   = regexp.MustCompile(`(?i)^SHOW\s+(` + identPattern + `)\s*;?\s*$`)
	reSummarizeIdent              = regexp.MustCompile(`(?i)^SUMMARIZE\s+(` + identPattern + `)\s*;?\s*$`)
)

// splitSchemaTable splits an identPattern match ("t" or "s.t") into its
// schema and table parts.
func splitSchemaTable(ident string) (schema, table string) {
	if i := strings.IndexByte(ident, '.'); i >= 0 {
		return ident[:i], ident[i+1:]
	}
	return "", ident
}

// classifyDescribeShowSummarize implements item B's second half: DESCRIBE,
// SHOW and SUMMARIZE allow ONLY a bare (unquoted), optionally
// schema-qualified identifier that resolves in the real catalog — nothing
// quoted, and no nested SELECT. handled reports whether trimmed even starts
// with one of these three keywords (false means "not this family, try
// something else").
func classifyDescribeShowSummarize(cat *catalog, trimmed string) (cls sqlClass, reason string, handled bool) {
	if !reDescribeShowSummarizePrefix.MatchString(trimmed) {
		return sqlClassOther, "", false
	}
	if reShowTables.MatchString(trimmed) {
		// Bare "SHOW TABLES" lists table NAMES from DuckDB's own catalog —
		// no different in kind from introspect_schema, no file/network
		// concern.
		return sqlClassRead, "", true
	}
	for _, re := range []*regexp.Regexp{reDescribeIdent, reShowIdent, reSummarizeIdent} {
		if m := re.FindStringSubmatch(trimmed); m != nil {
			schema, table := splitSchemaTable(m[1])
			if cat.resolves("", schema, table) {
				return sqlClassRead, "", true
			}
			return sqlClassOther, unresolvedTableDeniedMsg("", schema, table), true
		}
	}
	// Starts with DESCRIBE/SHOW/SUMMARIZE but isn't a bare-identifier
	// target: a quoted string (`SHOW '/etc/passwd'`) or a full nested
	// SELECT (`DESCRIBE SELECT * FROM ...`). Dropped entirely in non-full
	// tiers (review spec, round 3 item B) — these are DuckDB's documented
	// shorthand for reading an arbitrary file, and there is no way to tell
	// them apart from a legitimate bare reference once parsed, only in the
	// original text.
	return sqlClassOther, externalAccessDeniedMsg, true
}

// --- EXPLAIN / PRAGMA (non-SELECT reads the AST path doesn't reach) -----

var (
	reExplainAnalyze = regexp.MustCompile(`(?is)^EXPLAIN\s+ANALYZE\b`)
	reExplain        = regexp.MustCompile(`(?is)^EXPLAIN\s+(.*)$`)
	rePragmaRead     = regexp.MustCompile(`(?i)^PRAGMA\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:\([^()]*\))?\s*;?\s*$`)
)

// pragmaReadAllowlist is the exact list from the review spec (round 2 item
// C): introspection-only PRAGMAs. Every other PRAGMA (force_checkpoint,
// enable_profiling, memory_limit=..., ...) is sqlClassOther.
//
// database_list is deliberately absent (round 6 review fix): PRAGMA
// database_list exposes the decrypted temp DB file's on-disk path, the same
// leak duckdb_databases()/pragma_database_list() were closed off for
// earlier. It is denied in every non-full sandbox mode.
var pragmaReadAllowlist = map[string]bool{
	"table_info": true, "show_tables": true, "show_tables_expanded": true,
	"database_size": true, "storage_info": true,
	"version": true, "platform": true,
}

// --- classifySQL: the orchestrator ---------------------------------------

// classifySQL classifies a single run_query call's risk tier for sandbox
// gating, and — when the tier is sqlClassOther because of an
// external-access violation, or the query couldn't be validated at all —
// a human-readable reason to surface to the caller. userDB is the caller's
// REAL DuckDB connection (never the isolated classifier/statement-counting
// connections above), used to resolve table/view references against the
// actual catalog and to run EXPLAIN.
//
// raw and params are exactly what run_query received — the query text with
// $name placeholders, plus the named parameter values. Everything downstream
// (statement counting, the AST check, EXPLAIN, the obfuscation check)
// classifies the BOUND form (round 5 review fix): binding happens once,
// here, before any of it. Classifying the raw, unbound text was itself a
// bug, not just an inconvenience — EXPLAIN can't bind a bare `$id`
// placeholder with no value, so a perfectly ordinary parameterized SELECT
// failed EXPLAIN and was denied with a misleading "requires the full
// sandbox" message, and looksObfuscated rejected any `$` on sight,
// including a legitimate `$name` reference. Binding first also means a
// leftover `$` after binding can ONLY be a dollar-quote delimiter (or a
// reference to a param that plain doesn't exist, itself a bind error) —
// exactly the case that must keep failing closed.
func classifySQL(ctx context.Context, userDB *sql.DB, raw string, params map[string]any) (sqlClass, string) {
	boundSQL, args, err := engine.BindParams(raw, params)
	if err != nil {
		return sqlClassOther, fmt.Sprintf("could not validate query: %v", err)
	}
	return classifyBound(ctx, userDB, boundSQL, args)
}

// classifyBound is classifySQL's implementation, operating on the already
// name-vs-positional-bound form. Split out from classifySQL so the EXPLAIN
// recursive case (below) can reclassify a substring of an already-bound
// statement without re-running BindParams against text that no longer has
// any $name placeholders to bind.
func classifyBound(ctx context.Context, userDB *sql.DB, boundSQL string, args []any) (sqlClass, string) {
	trimmed := strings.TrimSpace(boundSQL)
	if trimmed == "" {
		return sqlClassOther, ""
	}

	// Statement count comes from DuckDB itself (item A) — never from
	// counting ';' after our own comment/string stripping, which the two
	// proven bypass strings defeated by hiding a second statement behind
	// dollar-quoting/E'' escapes a naive lexer thinks is still "open".
	if n, err := countStatements(boundSQL); err != nil || n != 1 {
		return sqlClassOther, ""
	}

	// Catalog snapshot for this call, used by both the AST path below and
	// the DESCRIBE/SHOW/SUMMARIZE / fallback paths further down. Always
	// non-nil; resolves nothing on load failure (fail closed).
	cat, _ := loadCatalog(ctx, userDB)

	if doc, err := serializeSQL(ctx, boundSQL); err == nil && !doc.Error && len(doc.Statements) == 1 {
		stmt := doc.Statements[0]
		if containsShowRef(stmt) {
			cls, reason, _ := classifyDescribeShowSummarize(cat, trimmed)
			return cls, reason
		}
		if reason := checkExternalAccess(cat, stmt); reason != "" {
			return sqlClassOther, reason
		}
		// Defence in depth (round 4): also run the bound-plan check on the
		// read path, not just the AST check above. They're independently
		// derived (the parsed-but-unbound AST vs. DuckDB's own fully bound
		// physical plan), so a gap in one is unlikely to be a gap in both.
		if reason := checkPlanExternalAccess(ctx, userDB, cat, boundSQL, args); reason != "" {
			return sqlClassOther, reason
		}
		if reason := checkMutatingFunc(stmt); reason != "" {
			return sqlClassOther, reason
		}
		return sqlClassRead, ""
	}

	// Not parseable as a single pure SELECT. EXPLAIN and PRAGMA never
	// serialize via json_serialize_sql at all (confirmed empirically), so
	// they're handled here unconditionally.
	if reExplainAnalyze.MatchString(trimmed) {
		// EXPLAIN ANALYZE actually executes the statement — never a
		// read-tier allowance.
		return sqlClassOther, ""
	}
	if m := reExplain.FindStringSubmatch(trimmed); m != nil {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[1]), ";"))
		if inner != "" {
			// Recursively classify the explained statement itself, so
			// `EXPLAIN SELECT * FROM read_csv(...)` is denied exactly like
			// the bare SELECT would be. inner is a substring of the already
			// -bound trimmed text (stripping a fixed "EXPLAIN " prefix, which
			// never itself contains a "?"), so the same args — in the same
			// order — still line up with inner's "?" occurrences.
			if cls, _ := classifyBound(ctx, userDB, inner, args); cls == sqlClassRead {
				return sqlClassRead, ""
			}
		}
		return sqlClassOther, ""
	}
	if m := rePragmaRead.FindStringSubmatch(trimmed); m != nil {
		if pragmaReadAllowlist[strings.ToLower(m[1])] {
			return sqlClassRead, ""
		}
		return sqlClassOther, ""
	}
	// Safety net for DuckDB version skew: on this build DESCRIBE/SHOW/
	// SUMMARIZE always serialize successfully (handled above via
	// containsShowRef), but if a different DuckDB build ever rejected them
	// instead, they'd still be classified correctly here.
	if cls, reason, handled := classifyDescribeShowSummarize(cat, trimmed); handled {
		return cls, reason
	}

	return classifyFallback(ctx, userDB, cat, trimmed, args)
}

// --- Fallback keyword classifier (DML vs other only — never Read) ------

// dmlKeywords mark row-level data-modification. If any of these appear as a
// token anywhere in the statement (including inside a CTE), the statement is
// at least sqlClassWrite — unless a fullOnlyKeyword is also present, in
// which case it's escalated to sqlClassOther (mixed DML+DDL is not safe to
// wave through as plain DML).
var dmlKeywords = map[string]bool{
	"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true, "REPLACE": true,
}

// fullOnlyKeywords mark DDL/administrative statements that always require
// full sandbox access, wherever in the statement they appear. "SET" is
// deliberately NOT here even though a standalone SET statement (e.g. `SET
// memory_limit='1GB'`) is administrative: SET is also ordinary syntax
// inside UPDATE ("UPDATE t SET x = 1") and MERGE's "WHEN MATCHED THEN
// UPDATE SET" clause. It doesn't need special-casing any more, though:
// classifyFallback below never defaults to Read, so a standalone SET with
// no DML keyword present simply falls through to sqlClassOther already.
var fullOnlyKeywords = map[string]bool{
	"CREATE": true, "DROP": true, "ALTER": true, "ATTACH": true, "DETACH": true,
	"COPY": true, "EXPORT": true, "IMPORT": true, "INSTALL": true, "LOAD": true,
	"GRANT": true, "REVOKE": true, "TRUNCATE": true, "CALL": true,
	"VACUUM": true, "CHECKPOINT": true, "RENAME": true, "EXECUTE": true, "USE": true,
	"BEGIN": true, "COMMIT": true, "ROLLBACK": true, "ANALYZE": true, "PREPARE": true,
}

var tokenRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// reEscapeStringPrefix matches a standalone E'/e' escape-string opener
// anywhere in the raw text — checked against the UNSTRIPPED text, because
// stripCommentsAndStrings's naive quote handling (it doesn't know E'-prefix
// strings are special) already consumes the quote and everything up to its
// own (possibly wrong) idea of where the string ends, so the "E'" substring
// itself doesn't survive stripping to be found afterward.
//
// The (?:^|[^A-Za-z0-9_]) before the e/E is required, not cosmetic: without
// it this matched the last letter of ANY word ending in "e" immediately
// followed by a string's closing quote — e.g. "...VALUES (1, 'Alice',
// 'Austin')" contains "Alice'", whose trailing "e'" is not an escape-string
// opener at all, just an ordinary name that happens to end in "e" butting
// up against its own string's closing quote. That false positive denied
// completely ordinary INSERTs (caught live: a real "INSERT INTO customers
// VALUES (1, 'Alice', 'Austin')" was denied under read-write before this
// fix). Requiring a non-identifier character (or start of string)
// immediately before e/E restricts the match to E/e appearing as its own
// token, which is what an escape-string prefix actually looks like.
var reEscapeStringPrefix = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_])e'`)

// looksObfuscated is the round-3 defence-in-depth item A asks for: rather
// than trying to teach the fallback's lexer to fully understand
// dollar-quoting and E' escapes (on top of the authoritative
// countStatements check above, which already closes the read-bypass this
// class of trick enabled), just refuse to classify anything containing one
// of these constructs as more than Other. A legitimate DML statement that
// happens to need a literal '$' or backslash can still run — from the full
// sandbox.
func looksObfuscated(trimmed string) bool {
	if reEscapeStringPrefix.MatchString(trimmed) {
		return true
	}
	stripped := stripCommentsAndStrings(trimmed)
	return strings.ContainsAny(stripped, "$\\")
}

// writeTierDenyTokens are substrings whose presence anywhere in a DML
// statement's TEXT is suspicious. This is deliberately NOT the allow
// decision any more (round 4 review fix): a text scan over untrusted SQL
// was bypassed just by quoting a function name ("read_csv"(...) — quotes
// aren't in this list) or hiding it behind a comment
// (`FROM /**/ 'file'`) — DuckDB itself doesn't care about either, so
// neither did the query it actually ran. It survives only as an extra,
// cheap deny layer checked BEFORE the real decision
// (checkPlanExternalAccess, DuckDB's own bound plan) — it can still say no
// to something the plan check would have allowed, never the reverse.
var writeTierDenyTokens = []string{
	"read_", "_scan", "glob", "sniff_csv", "parquet_", "query_table", "query(",
	"getenv", "current_setting",
}

var reQuotedTableRef = regexp.MustCompile(`(?i)\b(FROM|JOIN|USING)\s+'`)

// writeStatementSuspiciousText is the cheap, text-only pre-check described
// above — kept as an extra deny layer, never the allow decision (see
// writeTierDenyTokens).
func writeStatementSuspiciousText(trimmed string) bool {
	stripped := strings.ToLower(stripCommentsAndStrings(trimmed))
	for _, tok := range writeTierDenyTokens {
		if strings.Contains(stripped, tok) {
			return true
		}
	}
	if strings.Contains(stripped, "://") {
		return true
	}
	return reQuotedTableRef.MatchString(trimmed)
}

// classifyFallback is reached ONLY for statements DuckDB's real parser has
// already confirmed are a single statement (via countStatements) that is
// not a pure SELECT, and that didn't match the narrow non-SELECT read
// allow-lists above. Its only job here is distinguishing row-level DML
// (sqlClassWrite) from everything else (sqlClassOther) — it deliberately
// has no path that returns sqlClassRead, so even if its naive
// comment/string stripping is fooled by a lexical trick it doesn't
// understand, the worst outcome is treating a statement as more
// restrictive than it needs to be, never granting a read it shouldn't.
// looksObfuscated adds a second, even more conservative layer in front of
// that: any dollar sign, an E'/e' escape-string prefix, or a stray
// backslash anywhere fails the whole statement closed before the keyword
// scan even runs.
//
// The actual DML allow decision (round 4 review fix) is
// checkPlanExternalAccess — DuckDB's own bound physical plan for the
// statement, not a scan over its text — since a text scan alone was proven
// bypassable via quoted function names, comments splitting a keyword from
// its file argument, and unquoted replacement-scan table refs the scan
// never recognized as reaching outside the database at all.
func classifyFallback(ctx context.Context, userDB *sql.DB, cat *catalog, trimmed string, args []any) (sqlClass, string) {
	if looksObfuscated(trimmed) {
		return sqlClassOther, ""
	}

	s := strings.TrimSpace(stripCommentsAndStrings(trimmed))
	if s == "" {
		return sqlClassOther, ""
	}

	matches := tokenRe.FindAllString(s, -1)
	if len(matches) == 0 {
		return sqlClassOther, ""
	}

	hasDML := false
	hasFullOnly := false
	for _, m := range matches {
		tok := strings.ToUpper(m)
		if dmlKeywords[tok] {
			hasDML = true
		}
		if fullOnlyKeywords[tok] {
			hasFullOnly = true
		}
	}
	if hasFullOnly {
		return sqlClassOther, ""
	}
	if !hasDML {
		return sqlClassOther, ""
	}
	if writeStatementSuspiciousText(trimmed) {
		return sqlClassOther, externalAccessDeniedMsg
	}
	if reason := checkPlanExternalAccess(ctx, userDB, cat, trimmed, args); reason != "" {
		return sqlClassOther, reason
	}
	return sqlClassWrite, ""
}

// stripCommentsAndStrings removes SQL line comments (--...), block comments
// (/*...*/), and the contents of single- and double-quoted literals, so
// classifyFallback's keyword scan (and the Write-tier token scan) never
// fire on text that's just data or prose. It does NOT understand
// dollar-quoting ($$...$$/$tag$...$tag$) or E'-style backslash escape
// strings — see looksObfuscated, which refuses to classify anything
// containing either as more than sqlClassOther specifically because this
// function can't be trusted with them.
func stripCommentsAndStrings(sql string) string {
	r := []rune(sql)
	n := len(r)
	var b strings.Builder
	i := 0
	for i < n {
		c := r[i]
		switch {
		case c == '-' && i+1 < n && r[i+1] == '-':
			for i < n && r[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && r[i+1] == '*':
			i += 2
			for i+1 < n && !(r[i] == '*' && r[i+1] == '/') {
				i++
			}
			i += 2
			if i > n {
				i = n
			}
		case c == '\'':
			i++
			for i < n && r[i] != '\'' {
				i++
			}
			i++ // skip closing quote (safe past end: loop condition rechecks)
		case c == '"':
			i++
			for i < n && r[i] != '"' {
				i++
			}
			i++
		default:
			b.WriteRune(c)
			i++
		}
	}
	return b.String()
}

// checkSQLClass gates a run_query statement's classified risk tier against
// the sandbox mode: reads are always allowed, DML needs read-write or full,
// anything else needs full. reason, when non-empty (an external-access
// violation), is surfaced verbatim instead of the generic per-mode message.
func (sc *SandboxConfig) checkSQLClass(class sqlClass, reason string) error {
	switch sc.Mode {
	case ModeReadOnly:
		if class != sqlClassRead {
			if reason != "" {
				return fmt.Errorf("run_query: %s", reason)
			}
			return fmt.Errorf("run_query: only read statements (SELECT, WITH...SELECT, SHOW, DESCRIBE, EXPLAIN, SUMMARIZE, or a read-style PRAGMA) are allowed in read-only mode")
		}
	case ModeReadWrite:
		if class == sqlClassOther {
			if reason != "" {
				return fmt.Errorf("run_query: %s", reason)
			}
			return fmt.Errorf("run_query: this statement requires full sandbox access (DDL, ATTACH/COPY/INSTALL, administrative PRAGMA/SET, and multi-statement SQL are not allowed in read-write mode)")
		}
	case ModeFull:
		// no additional restriction
	default:
		return fmt.Errorf("unknown sandbox mode: %q", sc.Mode)
	}
	return nil
}

// checkSQLClassReadOnly is the render-time gate for a live A2UI component's
// "sql" binding (RunA2UIQuery, #67 step-2 review fix, round 3: the full-mode
// external-access carve-out this function used to have is REMOVED — policy
// decision: render-time bindings are read-only against the file's own
// tables in EVERY mode, full included, full stop). Unlike checkSQLClass —
// which lets ModeFull and, for DML, ModeReadWrite through untouched — this
// enforces sqlClassRead regardless of sc.Mode: a data-bound component
// re-runs every time the page renders, so "a write succeeds because the
// sandbox happens to be read-write/full right now" would fire that write on
// every render, not once when the agent decided to make it. A page's sql
// field stores a question, never an action.
//
// The removed carve-out matched on classifySQL's reason STRING (whether it
// equaled externalAccessDeniedMsg) to decide "this Other was a read that
// merely reached outside the database, tolerate it under full" — but
// classifyFallback (the DML/DDL path a DELETE/INSERT/UPDATE statement with
// suspicious text goes through) ALSO returns externalAccessDeniedMsg as its
// reason for writeStatementSuspiciousText hits, via the exact same string
// constant. A reason string cannot distinguish "a read touching a file" from
// "a write whose text merely happens to contain read_/glob/a quoted table
// ref" — proven live: DELETE FROM victim WHERE EXISTS (SELECT 1 AS read_me),
// INSERT INTO victim SELECT * FROM read_csv('x.csv'), DELETE ... WHERE n IN
// (SELECT n FROM 'x.csv'), and UPDATE ... WHERE n IN (SELECT 1 AS glob) were
// all sqlClassOther-with-externalAccessDeniedMsg and so ALL sailed through
// the carve-out under full mode, each one a real write. There is no reason
// string, and no amount of refining the match, that closes this: only
// removing the carve-out does.
// errReadOnlyBindingDenied is the ONE message checkSQLClassReadOnly ever
// returns for a denial (review fix, round 4): it used to surface
// classifySQL's reason verbatim (e.g. externalAccessDeniedMsg, "external
// file/network access requires the full sandbox"), which is actively false
// for a full-mode user/agent — they ARE in the full sandbox, and the
// binding is still denied, because render-time bindings are read-only in
// EVERY mode, not because they need more access. The classifier's reason is
// still useful for debugging, so callers log it themselves (at debug level,
// not surfaced to the user) rather than it being silently dropped.
var errReadOnlyBindingDenied = errors.New("live bindings are read-only queries over this file's own tables (no writes, no external files)")

func (sc *SandboxConfig) checkSQLClassReadOnly(class sqlClass, _ string) error {
	if class == sqlClassRead {
		return nil
	}
	return errReadOnlyBindingDenied
}

// IsReadOnlyStatement reports whether sql (with the given bound params)
// classifies as a pure read (sqlClassRead) against db — the SAME classifier
// handleRunQuery uses internally, exported so a caller outside this package
// that only has the original run_query request (e.g. cmd/mcp.go's
// debounced-save dirty tracking, review fix for #70: a plain SELECT via
// run_query must never mark a container dirty) can ask "did this run_query
// call actually write anything?" without duplicating classification logic
// or peeking at internal sqlClass values.
func IsReadOnlyStatement(ctx context.Context, db *sql.DB, sqlText string, params map[string]any) bool {
	if db == nil {
		return false
	}
	class, _ := classifySQL(ctx, db, sqlText, params)
	return class == sqlClassRead
}
