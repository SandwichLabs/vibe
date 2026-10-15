//go:build integration

package connections_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// rawLake builds an honest DuckLake catalog ("duckdb" or "sqlite") with a plain
// DuckDB connection and exercises it so that every kind of catalog object
// exists: inlined data (small inserts), parquet (a big insert, a flush), a
// delete (inlined and file), a schema change, snapshots, a second schema, a
// view and a macro defined in the lake, and merged files. It returns the
// catalog file and the connection (still open, lake detached).
func rawLake(t *testing.T, kind string) (string, *sql.Conn) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	exec := func(s string) {
		t.Helper()
		_, err := conn.ExecContext(ctx, s)
		require.NoError(t, err, s)
	}
	exec("SET extension_directory = " + session.QuoteLiteral(extDir(t)))
	for _, e := range []string{"ducklake", "sqlite"} {
		exec("INSTALL " + e)
		exec("LOAD " + e)
	}
	dir := t.TempDir()
	cat := filepath.Join(dir, "catalog.db")
	target := "ducklake:" + cat
	if kind == "sqlite" {
		target = "ducklake:sqlite:" + cat
	}
	exec("ATTACH " + session.QuoteLiteral(target) + " AS l (DATA_PATH " + session.QuoteLiteral(filepath.Join(dir, "data")+"/") + ")")
	for _, s := range []string{
		"CREATE TABLE l.people (id INTEGER, name VARCHAR, ts TIMESTAMP DEFAULT now(), n VARCHAR DEFAULT 'x')",
		"INSERT INTO l.people (id, name) VALUES (1, 'ada'), (2, 'bob')",
		"DELETE FROM l.people WHERE id = 1",
		"INSERT INTO l.people (id, name) SELECT i, 'x' FROM range(100000) t(i)",
		"DELETE FROM l.people WHERE id = 7",
		"ALTER TABLE l.people ADD COLUMN age INTEGER",
		"CREATE VIEW l.v AS SELECT 1 AS a",
		"CREATE MACRO l.m(x) AS x + 1",
		"CREATE SCHEMA l.other",
		"CREATE TABLE l.other.t (a INTEGER, d DATE)",
		"ALTER TABLE l.other.t SET PARTITIONED BY (bucket(4, a), year(d))",
		"CREATE TABLE l.sorted (a INTEGER)",
		"ALTER TABLE l.sorted SET SORTED BY (a DESC)",
		"INSERT INTO l.other.t VALUES (1, DATE '2024-01-02')",
		"CALL ducklake_flush_inlined_data('l')",
		"INSERT INTO l.people (id, name, age) VALUES (-1, 'inlined', 3)",
		"CALL ducklake_merge_adjacent_files('l')",
		"DETACH l",
	} {
		exec(s)
	}
	return cat, conn
}

// catalogObjects lists every object in a catalog file (the table names; other
// kinds as "kind:name") with the same queries the vetting uses.
func catalogObjects(t *testing.T, conn *sql.Conn, cat, kind string) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	if kind == "sqlite" {
		rows, err := conn.QueryContext(ctx, "SELECT type, name FROM sqlite_scan("+session.QuoteLiteral(cat)+", 'sqlite_master')")
		require.NoError(t, err)
		defer rows.Close() //nolint:errcheck
		for rows.Next() {
			var k, n string
			require.NoError(t, rows.Scan(&k, &n))
			out = append(out, k+":"+n)
		}
		sort.Strings(out)
		return out
	}
	_, err := conn.ExecContext(ctx, "ATTACH "+session.QuoteLiteral(cat)+" AS probe (READ_ONLY)")
	require.NoError(t, err)
	defer conn.ExecContext(ctx, "DETACH probe") //nolint:errcheck
	rows, err := conn.QueryContext(ctx, `
		SELECT 'table', table_name FROM duckdb_tables() WHERE database_name = 'probe'
		UNION ALL SELECT 'view', view_name FROM duckdb_views() WHERE database_name = 'probe'
		UNION ALL SELECT 'function', function_name FROM duckdb_functions() WHERE database_name = 'probe'
		UNION ALL SELECT 'sequence', sequence_name FROM duckdb_sequences() WHERE database_name = 'probe'
		UNION ALL SELECT 'index', index_name FROM duckdb_indexes() WHERE database_name = 'probe'
		UNION ALL SELECT 'type', type_name FROM duckdb_types() WHERE database_name = 'probe' AND NOT internal
		UNION ALL SELECT 'schema', schema_name FROM duckdb_schemas() WHERE database_name = 'probe' AND schema_name <> 'main'`)
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var k, n string
		require.NoError(t, rows.Scan(&k, &n))
		out = append(out, k+":"+n)
	}
	sort.Strings(out)
	return out
}

// The allow-list drift test. An honest catalog, after every operation DuckLake
// has, holds exactly DuckLake's fixed metadata tables plus its per-table
// inlined-data and inlined-delete tables, and nothing else: its views and macros
// are rows (ducklake_view, ducklake_macro), not catalog objects. When an
// upgrade of the ducklake extension adds, renames or drops a table this fails,
// and so does every user's attach if it slipped through, so the list must be
// updated deliberately (after reading what the new table is for).
func TestDuckLakeCatalogObjectsAreTheAllowList(t *testing.T) {
	for _, kind := range []string{"duckdb", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			cat, conn := rawLake(t, kind)
			objs := catalogObjects(t, conn, cat, kind)
			var fixed []string
			inlinedData, inlinedDelete := 0, 0
			for _, o := range objs {
				k, name, _ := strings.Cut(o, ":")
				if k == "index" && strings.HasPrefix(name, "sqlite_autoindex_") {
					continue // SQLite's implicit PRIMARY KEY / UNIQUE indexes
				}
				require.Equal(t, "table", k, "a catalog holds only tables, found %s", o)
				switch {
				case strings.HasPrefix(name, "ducklake_inlined_data_") && name != "ducklake_inlined_data_tables":
					inlinedData++
					assert.True(t, connections.DuckLakeCatalogTable(name), name)
				case strings.HasPrefix(name, "ducklake_inlined_delete_"):
					inlinedDelete++
					assert.True(t, connections.DuckLakeCatalogTable(name), name)
				default:
					fixed = append(fixed, name)
				}
			}
			sort.Strings(fixed)
			assert.Equal(t, connections.DuckLakeMetadataTables(), fixed,
				"the ducklake extension's table set changed: review the new tables, then update ducklakeMetadataTables")
			assert.Positive(t, inlinedData, "the exercise must create inlined-data tables")
			assert.Positive(t, inlinedDelete, "the exercise must create an inlined-delete table")
			// The lake's own view and macro are rows.
			ctx := context.Background()
			_, err := conn.ExecContext(ctx, "ATTACH "+session.QuoteLiteral(map[string]string{"duckdb": cat, "sqlite": ""}[kind])+" AS rows_probe (READ_ONLY)")
			if kind == "duckdb" {
				require.NoError(t, err)
				defer conn.ExecContext(ctx, "DETACH rows_probe") //nolint:errcheck
				for _, tbl := range []string{"ducklake_view", "ducklake_macro"} {
					var n int
					require.NoError(t, conn.QueryRowContext(ctx, "SELECT count(*) FROM rows_probe.main."+tbl).Scan(&n))
					assert.Positive(t, n, tbl)
				}
			}
			if kind == "duckdb" {
				// No defaults, generated columns or constraints beyond PRIMARY KEY / NOT NULL.
				var n int
				require.NoError(t, conn.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_columns() WHERE database_name = 'rows_probe' AND column_default IS NOT NULL").Scan(&n))
				assert.Zero(t, n, "an honest catalog table has no default or generated column")
				require.NoError(t, conn.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_constraints() WHERE database_name = 'rows_probe' AND constraint_type NOT IN ('PRIMARY KEY', 'NOT NULL')").Scan(&n))
				assert.Zero(t, n, "an honest catalog table has no CHECK, UNIQUE or FOREIGN KEY")
			}
			// And the vetting accepts it.
			require.NoError(t, connections.VetDuckLakeCatalog(ctx, conn, cat, kind))
		})
	}
}

// tamperCatalog runs statements against a catalog file with plain DuckDB (a
// DuckDB file directly; a SQLite file through the sqlite extension as "c").
func tamperCatalog(t *testing.T, conn *sql.Conn, cat, kind string, stmts ...string) {
	t.Helper()
	ctx := context.Background()
	att := "ATTACH " + session.QuoteLiteral(cat) + " AS c"
	if kind == "sqlite" {
		att += " (TYPE sqlite)"
	}
	_, err := conn.ExecContext(ctx, att)
	require.NoError(t, err)
	for _, s := range stmts {
		_, err := conn.ExecContext(ctx, s)
		require.NoError(t, err, s)
	}
	_, err = conn.ExecContext(ctx, "DETACH c")
	require.NoError(t, err)
}

// realDF is how a view in the catalog reads the renamed table: a DuckDB view may
// name its own database, a SQLite one reaches the table through sqlite_scan.
func realDF(kind string) string {
	if kind == "sqlite" {
		return "sqlite_scan('%CAT%', 'real_df')"
	}
	return "c.real_df"
}

// attackCases are the crafted catalogs. %SECRET% is a file outside the lake.
// Each is applied to a honest lake that has inlined data (the table
// ducklake_inlined_data_<n>_<m> exists), so the same list serves both catalog
// kinds.
func attackCases(inlined, kind string) map[string][]string {
	view := func(name string) string {
		return "CREATE VIEW c." + name + " AS SELECT 0 AS row_id, 0 AS begin_snapshot, NULL AS end_snapshot, 7 AS id, content AS name FROM read_text('%SECRET%')"
	}
	return map[string][]string{
		"view replaces the inlined data table": {"DROP TABLE c." + inlined, view(inlined)},
		"view over ducklake_data_file":         {"ALTER TABLE c.ducklake_data_file RENAME TO real_df", "CREATE VIEW c.ducklake_data_file AS SELECT * FROM " + realDF(kind)},
		"view over a known name, no data":      {"DROP TABLE c.ducklake_table_stats", "CREATE VIEW c.ducklake_table_stats AS SELECT 1 AS table_id, 0 AS record_count, 0 AS next_row_id, 0 AS file_size_bytes"},
		"extra unknown table":                  {"CREATE TABLE c.notes (a INTEGER)"},
		"table with an inlined-looking suffix": {"CREATE TABLE c.ducklake_inlined_data_1_2_extra (a INTEGER)"},
		"extra view":                           {"CREATE VIEW c.harmless AS SELECT 1 AS a"},
		// Rows that DuckLake turns into SQL.
		"inlined registry splices a file read":    {"UPDATE c.ducklake_inlined_data_tables SET table_name = '" + inlined + " UNION ALL SELECT 0, 0, NULL, 7, content FROM read_text(''%SECRET%'') --'"},
		"inlined registry names no table":         {"UPDATE c.ducklake_inlined_data_tables SET table_name = 'ducklake_inlined_data_99_99'"},
		"inlined registry names a metadata table": {"UPDATE c.ducklake_inlined_data_tables SET table_name = 'ducklake_metadata'"},
		"partition transform":                     {"INSERT INTO c.ducklake_partition_column VALUES (1, 1, 0, 1, 'identity(x) FROM read_text(''%SECRET%'') --')"},
	}
}

func TestDuckLakeRefusesViewsAndExtraObjectsInTheCatalog(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"duckdb", "sqlite"} {
		cat, conn := rawLake(t, kind)
		var inlined string
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT name FROM (SELECT unnest(string_split(?, ',')) AS name) WHERE name LIKE 'table:ducklake_inlined_data_%\\_%' ESCAPE '\\' LIMIT 1",
			strings.Join(catalogObjects(t, conn, cat, kind), ",")).Scan(&inlined))
		inlined = strings.TrimPrefix(inlined, "table:")
		cases := attackCases(inlined, kind)
		if kind == "duckdb" {
			cases["macro"] = []string{"CREATE MACRO c.evil(x) AS x"}
			cases["table macro"] = []string{"CREATE MACRO c.evil_t() AS TABLE SELECT 1"}
			cases["sequence"] = []string{"CREATE SEQUENCE c.sq"}
			cases["index"] = []string{"CREATE INDEX ix ON c.ducklake_metadata(key)"}
			cases["enum type"] = []string{"CREATE TYPE c.ty AS ENUM ('a')"}
			for n, col := range map[string]string{
				"generated column": "value VARCHAR GENERATED ALWAYS AS (current_setting('home_directory'))",
				"column default":   "value VARCHAR DEFAULT 'x'",
				"check constraint": "value VARCHAR CHECK (value <> 'z')",
				"unique":           "value VARCHAR UNIQUE",
			} {
				cases[n] = []string{"DROP TABLE c.ducklake_tag", "CREATE TABLE c.ducklake_tag (object_id BIGINT, begin_snapshot BIGINT, end_snapshot BIGINT, key VARCHAR, " + col + ")"}
			}
			cases["extra schema"] = []string{"CREATE SCHEMA c.s2", "CREATE TABLE c.s2.ducklake_metadata (a INTEGER)"}
		}
		for name, stmts := range cases {
			t.Run(kind+"/"+name, func(t *testing.T) {
				secretDir := t.TempDir()
				secretFile := filepath.Join(secretDir, "secret.txt")
				require.NoError(t, os.WriteFile(secretFile, []byte("TOP-SECRET"), 0o600))
				// A fresh honest lake per case: the tampering is on a copy.
				cat2, conn2 := rawLake(t, kind)
				sub := make([]string, len(stmts))
				for i, s := range stmts {
					sub[i] = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "%SECRET%", secretFile), "%CAT%", cat2), inlined, catalogInlined(t, conn2, cat2, kind))
				}
				tamperCatalog(t, conn2, cat2, kind, sub...)

				err := connections.VetDuckLakeCatalog(ctx, conn2, cat2, kind)
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "TOP-SECRET")
				assert.Contains(t, err.Error(), "refusing")
				var n int
				require.NoError(t, conn2.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_databases() WHERE database_name LIKE '__vibe_vet_%'").Scan(&n))
				assert.Zero(t, n, "the scratch attachment is always detached")
			})
		}
	}
}

// catalogInlined returns the name of an inlined-data table in the catalog.
func catalogInlined(t *testing.T, conn *sql.Conn, cat, kind string) string {
	t.Helper()
	for _, o := range catalogObjects(t, conn, cat, kind) {
		name := strings.TrimPrefix(o, "table:")
		if strings.HasPrefix(name, "ducklake_inlined_data_") && name != "ducklake_inlined_data_tables" {
			return name
		}
	}
	t.Fatal("no inlined data table")
	return ""
}

// tamperFile applies stmts to a catalog file with a plain DuckDB (the sqlite
// extension reads a SQLite file).
func tamperFile(t *testing.T, cat, kind string, stmts []string) {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	defer db.Close() //nolint:errcheck
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck
	for _, s := range []string{"SET extension_directory = " + session.QuoteLiteral(extDir(t)), "INSTALL sqlite", "LOAD sqlite"} {
		_, err := conn.ExecContext(context.Background(), s)
		require.NoError(t, err, s)
	}
	tamperCatalog(t, conn, cat, kind, stmts...)
}

// The reviewer's repro, end to end, for both catalog kinds: a view in the place
// of the inlined data table over read_text() of a file outside the workspace.
// The read-only agent must not get the file through the lake, the connection is
// refused (also by Test), and an honest lake with inlined data still attaches
// and reads.
func TestDuckLakeCrafted(t *testing.T) {
	ctx := context.Background()
	secretFile := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(secretFile, []byte("TOP-SECRET"), 0o600))

	for _, kind := range []string{"duckdb", "sqlite"} {
		cases := map[string]string{
			"view over inlined data":               "view replaces the inlined data table",
			"view over ducklake_data_file":         "view over ducklake_data_file",
			"inlined registry splices a file read": "inlined registry splices a file read",
			"extra unknown table":                  "extra unknown table",
		}
		for name, key := range cases {
			t.Run(kind+"/"+name, func(t *testing.T) {
				craftedLake(t, ctx, kind, key, secretFile)
			})
		}
	}
}

func craftedLake(t *testing.T, ctx context.Context, kind, caseKey, secretFile string) {
	ws, _ := memWS(t)
	opts := map[string]string{"data_path": "data/lake_files"}
	if kind == "sqlite" {
		opts["catalog"] = "sqlite"
	}
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "data/lake.ducklake", ReadOnly: workspaceBool(false), Options: opts}))
	fresh := extDir(t)
	open := func() session.Session {
		sess, _, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(fresh)))
		require.NoError(t, err)
		return sess
	}
	sess := open()
	require.NoError(t, sess.InstallExtension(ctx, "ducklake"))
	if kind == "sqlite" {
		require.NoError(t, sess.InstallExtension(ctx, "sqlite"))
	}
	sess.Preflight(ctx, "lake")
	require.NoError(t, sess.Connect(ctx, "lake"))
	// Small inserts stay inlined in the catalog.
	_, err := sess.DB().Exec("CREATE TABLE lake.people (id INTEGER, name VARCHAR); INSERT INTO lake.people VALUES (1,'ada'),(2,'bob')")
	require.NoError(t, err)
	require.NoError(t, sess.Close())
	catFile := filepath.Join(ws.Root(), "data", "lake.ducklake")

	// An honest lake with inlined data attaches and reads, through the agent.
	readThroughAgent := func(sess session.Session, q string) *agents.Response {
		h := agents.NewHandler(workspace.NewAgentAccess(ws, sess), engine.NewManager(sess),
			&agents.SandboxConfig{Mode: agents.ModeReadOnly, MaxQueryRows: 100, RequireApproval: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		return h.Handle(ctx, &agents.Request{JSONRPC: "2.0", ID: "1", Method: "run_query", Params: map[string]any{"sql": q}})
	}
	sess = open()
	sess.Preflight(ctx, "lake")
	resp := readThroughAgent(sess, "SELECT name FROM lake.people ORDER BY id")
	require.Nil(t, resp.Error, fmt.Sprint(resp.Error))
	assert.Contains(t, fmt.Sprint(resp.Result), "ada")
	// The catalog's internals are denied to the agent whatever its kind.
	resp = readThroughAgent(sess, "SELECT * FROM __ducklake_metadata_lake.main.ducklake_data_file")
	require.NotNil(t, resp.Error)
	resp = readThroughAgent(sess, "SELECT * FROM sqlite_scan('"+catFile+"', 'ducklake_metadata')")
	require.NotNil(t, resp.Error, "sqlite_scan is not on the allow-list")
	require.NoError(t, sess.Close())

	// Tamper a catalog file with the same statements the unit-level cases use.
	var inlined string
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	pc, err := db.Conn(ctx)
	require.NoError(t, err)
	for _, s := range []string{"SET extension_directory = " + session.QuoteLiteral(fresh), "INSTALL sqlite", "LOAD sqlite"} {
		_, err := pc.ExecContext(ctx, s)
		require.NoError(t, err, s)
	}
	inlined = catalogInlined(t, pc, catFile, kind)
	stmts := attackCases(inlined, kind)[caseKey]
	require.NotEmpty(t, stmts)
	for i, s := range stmts {
		stmts[i] = strings.ReplaceAll(strings.ReplaceAll(s, "%SECRET%", secretFile), "%CAT%", catFile)
	}
	_ = pc.Close()
	_ = db.Close()
	tamperFile(t, catFile, kind, stmts)

	sess = open()
	defer sess.Close() //nolint:errcheck
	sess.Preflight(ctx, "lake")
	resp = readThroughAgent(sess, "SELECT * FROM lake.people")
	require.NotNil(t, resp.Error, "the crafted lake must not be readable")
	assert.NotContains(t, fmt.Sprint(resp), "TOP-SECRET")
	st := connStatus(sess, "lake")
	assert.Equal(t, session.StatusError, st.Kind)
	assert.Contains(t, st.Message, "refusing to read it")
	assert.NotContains(t, st.Message, "TOP-SECRET")
	var n int
	require.NoError(t, sess.DB().QueryRow("SELECT count(*) FROM duckdb_databases() WHERE database_name IN ('lake', '__ducklake_metadata_lake') OR database_name LIKE '__vibe_vet_%'").Scan(&n))
	assert.Zero(t, n)

	res := connections.Test(ctx, connections.Def{Name: "lake", Type: "ducklake", Path: catFile,
		Options: map[string]string{"data_path": filepath.Join(ws.Root(), "data", "lake_files"), "catalog": kindOpt(kind)}}, secrets.Credential{}, connections.WithTestExtensionDir(fresh))
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "refusing to read it")
	assert.NotContains(t, res.Error, "TOP-SECRET")
}

func kindOpt(kind string) string {
	if kind == "sqlite" {
		return "sqlite"
	}
	return ""
}

// A view or macro stored in the lake is SQL by design and is bound when used,
// never at attach. The attach succeeds, and the agent's plan check refuses a
// read of a file outside the workspace through it.
func TestDuckLakeViewAndMacroBodiesAreCheckedWhenUsed(t *testing.T) {
	ctx := context.Background()
	secretFile := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(secretFile, []byte("TOP-SECRET"), 0o600))
	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "data/lake.ducklake", ReadOnly: workspaceBool(false), Options: map[string]string{"data_path": "data/lake_files"}}))
	sess, _, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	defer sess.Close()
	require.NoError(t, sess.InstallExtension(ctx, "ducklake"))
	sess.Preflight(ctx, "lake")
	require.NoError(t, sess.Connect(ctx, "lake"))
	_, err = sess.DB().Exec("CREATE VIEW lake.v AS SELECT content AS a FROM read_text('" + secretFile + "'); CREATE MACRO lake.m() AS (SELECT content FROM read_text('" + secretFile + "'))")
	require.NoError(t, err)
	h := agents.NewHandler(workspace.NewAgentAccess(ws, sess), engine.NewManager(sess),
		&agents.SandboxConfig{Mode: agents.ModeReadOnly, MaxQueryRows: 100, RequireApproval: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, q := range []string{"SELECT * FROM lake.v", "SELECT lake.m()"} {
		resp := h.Handle(ctx, &agents.Request{JSONRPC: "2.0", ID: "1", Method: "run_query", Params: map[string]any{"sql": q}})
		require.NotNil(t, resp.Error, q)
		assert.NotContains(t, fmt.Sprint(resp), "TOP-SECRET", q)
	}
}
