package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetFlags puts every flag of cmd and its subcommands back to its default,
// so one test's flags never leak into the next on the shared command tree.
func resetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// runVibe runs the root command in-process and returns stdout, stderr and the
// error.
func runVibe(args ...string) (stdout, stderr string, err error) {
	return runVibeIn(nil, args...)
}

func runVibeIn(in io.Reader, args ...string) (stdout, stderr string, err error) {
	var out, errb bytes.Buffer
	resetFlags(rootCmd)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errb)
	if in == nil {
		in = strings.NewReader("")
	}
	rootCmd.SetIn(in)
	rootCmd.SetArgs(args)
	err = rootCmd.Execute()
	return out.String(), errb.String(), err
}

// executeCommand runs the root command and returns stdout and the error.
func executeCommand(args ...string) (string, error) {
	out, _, err := runVibe(args...)
	return out, err
}

// newWS creates a workspace through the CLI in a temp dir (with an isolated
// config dir) and returns its root.
func newWS(t *testing.T, extra ...string) string {
	t.Helper()
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	root := filepath.Join(t.TempDir(), "acme")
	args := append([]string{"workspace", "init", root}, extra...)
	_, _, err := runVibe(args...)
	require.NoError(t, err)
	r, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	return r
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestWorkspaceInit_CreatesWorkspaceAndPrintsNextSteps(t *testing.T) {
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	root := filepath.Join(t.TempDir(), "acme-analytics")
	out, stderr, err := runVibe("workspace", "init", root, "--name", "Acme Analytics")
	require.NoError(t, err, stderr)
	assert.Contains(t, out, `Created workspace "Acme Analytics"`)
	assert.Contains(t, out, "Next steps")
	assert.Contains(t, out, "vibe query")
	assert.Contains(t, out, "vibe import")

	for _, p := range []string{"vibe.yaml", ".gitignore", ".gitattributes"} {
		assert.FileExists(t, filepath.Join(root, p), p)
	}
}

func TestWorkspaceInit_Details(t *testing.T) {
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	root := filepath.Join(t.TempDir(), "w")
	_, _, err := runVibe("workspace", "init", root, "--name", "Named")
	require.NoError(t, err)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	assert.Equal(t, "Named", ws.Name())
	assert.Equal(t, "local", ws.DefaultConnection())
	for _, d := range []string{"notebooks", "queries", "skills", "data"} {
		fi, err := os.Stat(filepath.Join(root, d))
		require.NoError(t, err, d)
		assert.True(t, fi.IsDir(), d)
	}
}

func TestWorkspaceInit_Git(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	root := filepath.Join(t.TempDir(), "g")
	out, _, err := runVibe("workspace", "init", root, "--git")
	require.NoError(t, err)
	assert.Contains(t, out, "Initialized a git repository")
	assert.DirExists(t, filepath.Join(root, ".git"))

	plain := filepath.Join(t.TempDir(), "nogit")
	_, _, err = runVibe("workspace", "init", plain)
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(plain, ".git"))
}

func TestWorkspaceInit_Template(t *testing.T) {
	root := newWS(t, "--template", "order-entry")
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck

	nbs, err := ws.Notebooks().List(context.Background())
	require.NoError(t, err)
	require.Len(t, nbs, 1)
	assert.Equal(t, "Order Entry", nbs[0].Title)
	files, _ := filepath.Glob(filepath.Join(root, "notebooks", "*.vnb"))
	assert.Len(t, files, 1, "the notebook is a .vnb file")

	out, stderr, err := runVibe("query", "--workspace", root, "SELECT count(*) AS n FROM order_items")
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "3")

	// The seeded data is in the workspace's local connection, visible to a
	// fresh session.
	sess, issues, err := workspace.OpenSession(context.Background(), ws, testCmdLogger())
	require.NoError(t, err)
	defer sess.Close() //nolint:errcheck
	require.Empty(t, issues)
	var n int
	require.NoError(t, sess.DB().QueryRow("SELECT count(*) FROM customers").Scan(&n))
	assert.Equal(t, 2, n)
}

func TestWorkspaceInit_UnknownTemplateListsTemplates(t *testing.T) {
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	root := filepath.Join(t.TempDir(), "x")
	_, _, err := runVibe("workspace", "init", root, "--template", "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "order-entry")
	assert.NoFileExists(t, filepath.Join(root, "vibe.yaml"), "nothing is created for an unknown template")
}

func TestWorkspaceInit_RefusesExistingWorkspaceAndVibeFiles(t *testing.T) {
	root := newWS(t)
	_, _, err := runVibe("workspace", "init", root)
	require.Error(t, err)
	assert.ErrorIs(t, err, workspace.ErrAlreadyWorkspace)

	for _, args := range [][]string{{"workspace", "init", "old.vibe"}, {"init", "old.vibe"}} {
		_, _, err = runVibe(args...)
		require.Error(t, err, "%v", args)
		assert.Contains(t, err.Error(), "no longer opened")
		assert.Contains(t, err.Error(), "vibe workspace init")
	}
}

func TestInit_IsAnAliasOfWorkspaceInit(t *testing.T) {
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	root := filepath.Join(t.TempDir(), "alias")
	out, _, err := runVibe("init", root, "--name", "Aliased")
	require.NoError(t, err)
	assert.Contains(t, out, `Created workspace "Aliased"`)
	assert.FileExists(t, filepath.Join(root, "vibe.yaml"))
}

func TestWorkspaceInfo_ShowsEverything(t *testing.T) {
	root := newWS(t, "--template", "contacts")
	writeFile(t, filepath.Join(root, "queries"), "q1.sql", "SELECT 1\n")

	out, stderr, err := runVibe("workspace", "info", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "Root:        "+root)
	assert.Contains(t, out, "acme")
	assert.Regexp(t, `local\s+ok\s+\[duckdb, shared, read-write, default\]\s+\d+ table\(s\)`, out)
	assert.Contains(t, out, "Notebooks:   1")
	assert.Contains(t, out, "Queries:     1")
	assert.Contains(t, out, "Skills:      0")
	assert.Contains(t, out, "Agent:       sandbox read-write, ask before writes on")
	assert.Contains(t, out, "Hub owner:   none")
	assert.Regexp(t, `DuckDB:\s+v\d+\.\d+`, out)

	js, _, err := runVibe("workspace", "info", "--workspace", root, "--json")
	require.NoError(t, err)
	var rep infoReport
	require.NoError(t, json.Unmarshal([]byte(js), &rep))
	assert.Equal(t, root, rep.Root)
	assert.Regexp(t, `^v\d+\.\d+`, rep.DuckDB)
	assert.NotNil(t, rep.Extensions)
	require.Len(t, rep.Connections, 1)
	assert.Equal(t, "ok", rep.Connections[0].Status)
	require.NotNil(t, rep.Connections[0].Tables)
	assert.Equal(t, 2, *rep.Connections[0].Tables)
	assert.Equal(t, 1, rep.Notebooks)
}

func TestWorkspaceInfo_FreshWorkspaceDoesNotCreateDataFile(t *testing.T) {
	root := newWS(t)
	out, _, err := runVibe("workspace", "info", "--workspace", root)
	require.NoError(t, err)
	assert.Contains(t, out, "no data file yet")
	assert.NoFileExists(t, filepath.Join(root, "data", "local.duckdb"), "info must not create anything")
}

func TestWorkspaceInfo_NamesTheHubHolder(t *testing.T) {
	root := newWS(t)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	hold, err := hub.AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck

	out, _, err := runVibe("workspace", "info", "--workspace", root)
	require.NoError(t, err)
	assert.Contains(t, out, "Hub owner:   Vibe Studio (pid ")
}

func TestWorkspaceInfo_FindsWorkspaceFromSubfolder(t *testing.T) {
	root := newWS(t)
	t.Chdir(filepath.Join(root, "notebooks"))
	out, _, err := runVibe("workspace", "info")
	require.NoError(t, err)
	assert.Contains(t, out, root)
}

func TestWorkspaceInfo_NoWorkspace(t *testing.T) {
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	_, _, err := runVibe("workspace", "info", "--workspace", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Vibe workspace found")
}

func TestQuery_Formats(t *testing.T) {
	root := newWS(t)
	q := func(args ...string) string {
		t.Helper()
		out, stderr, err := runVibe(append([]string{"query", "--workspace", root}, args...)...)
		require.NoError(t, err, stderr)
		return out
	}
	table := q("SELECT 1 AS a, 'x' AS b")
	assert.Contains(t, table, "a  b")
	assert.Contains(t, table, "1  x")
	assert.Contains(t, table, "(1 row)")

	assert.Equal(t, "a,b\n1,x\n", q("--format", "csv", "SELECT 1 AS a, 'x' AS b"))

	js := q("--format", "json", "SELECT 1 AS a, 'x' AS b UNION ALL SELECT 2, NULL")
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(js), &rows), js)
	require.Len(t, rows, 2)
	assert.Equal(t, "x", rows[0]["b"])
	assert.Nil(t, rows[1]["b"])
	assert.Less(t, strings.Index(js, `"a"`), strings.Index(js, `"b"`), "column order is preserved")
}

func TestQuery_WritesLandInTheDefaultConnection(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runVibe("query", "--workspace", root, "CREATE TABLE t AS SELECT 1 AS id")
	require.NoError(t, err, stderr)
	assert.FileExists(t, filepath.Join(root, "data", "local.duckdb"))
	out, _, err := runVibe("query", "--workspace", root, "--format", "csv", "SELECT * FROM local.main.t")
	require.NoError(t, err)
	assert.Equal(t, "id\n1\n", out)
}

func TestQuery_Limit(t *testing.T) {
	root := newWS(t)
	out, stderr, err := runVibe("query", "--workspace", root, "--limit", "3", "--format", "csv", "SELECT * FROM range(10)")
	require.NoError(t, err)
	assert.Equal(t, 4, strings.Count(out, "\n"), "header plus 3 rows")
	assert.Contains(t, stderr, "showing the first 3 of 10 rows")

	out, stderr, err = runVibe("query", "--workspace", root, "--limit", "0", "--format", "csv", "SELECT * FROM range(10)")
	require.NoError(t, err)
	assert.Equal(t, 11, strings.Count(out, "\n"))
	assert.NotContains(t, stderr, "showing the first")
}

func TestQuery_StatementFromStdin(t *testing.T) {
	root := newWS(t)
	out, _, err := runVibeIn(strings.NewReader("SELECT 7 AS seven"), "query", "--workspace", root, "--format", "csv", "-")
	require.NoError(t, err)
	assert.Equal(t, "seven\n7\n", out)
}

func TestQuery_RefusesWhatVibeManagesForYou(t *testing.T) {
	root := newWS(t)
	for _, s := range []string{"ATTACH 'x.duckdb' AS x", "SET threads = 1", "CREATE SECRET s (TYPE s3)", "SELECT 1; INSTALL httpfs"} {
		_, _, err := runVibe("query", "--workspace", root, s)
		require.Error(t, err, s)
		assert.Contains(t, err.Error(), "not available here", s)
	}
	assert.NoFileExists(t, filepath.Join(root, "data", "local.duckdb"), "a refusal opens nothing for writing")

	// A SQL string that merely mentions a .vibe file is just SQL.
	_, _, err := runVibe("query", "--workspace", root, "SELECT * FROM s.vibe")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "no longer opened")
}

func TestQuery_ReadOnlyRefusesCopyTo(t *testing.T) {
	root := seededWS(t)
	dst := filepath.Join(t.TempDir(), "o.csv")
	_, _, err := runVibe("query", "--workspace", root, "--read-only", "COPY people TO '"+dst+"'")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--read-only")
	assert.NoFileExists(t, dst)
}

func TestQuery_ConnectionFlag(t *testing.T) {
	root := newWS(t)
	archive := filepath.Join(root, "data", "archive.duckdb")
	db, err := sql.Open("duckdb", archive)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE old AS SELECT 5 AS n")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	addConnection(t, root, workspace.ConnectionDef{Name: "archive", Type: "duckdb", Path: "data/archive.duckdb"})

	out, stderr, err := runVibe("query", "--workspace", root, "-c", "archive", "--format", "csv", "SELECT * FROM old")
	require.NoError(t, err, stderr)
	assert.Equal(t, "n\n5\n", out)

	// Writing to a read-only connection says why and how to change it.
	_, _, err = runVibe("query", "--workspace", root, "-c", "archive", "INSERT INTO old VALUES (6)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read_only: false")

	_, _, err = runVibe("query", "--workspace", root, "-c", "missing", "SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown connection "missing"`)
	assert.Contains(t, err.Error(), "archive")
}

// addConnection appends a shared connection to the workspace's vibe.yaml.
func addConnection(t *testing.T, root string, def workspace.ConnectionDef) {
	t.Helper()
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	cfg := ws.Config()
	cfg.Connections = append(cfg.Connections, def)
	require.NoError(t, ws.SaveConfig(cfg))
}

func TestQuery_WhenAnotherProcessOwnsTheHub(t *testing.T) {
	root := newWS(t)
	_, _, err := runVibe("query", "--workspace", root, "CREATE TABLE t AS SELECT 1 AS id")
	require.NoError(t, err)

	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	hold, err := hub.AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck

	_, _, err = runVibe("query", "--workspace", root, "SELECT * FROM t")
	require.Error(t, err)
	assert.ErrorIs(t, err, hub.ErrHubHeld)
	assert.Contains(t, err.Error(), "Vibe Studio (pid ")
	assert.Contains(t, err.Error(), "--read-only")

	// Nothing in this process has the DuckDB file open (only the lock is
	// held), so a read-only reader gets through.
	out, stderr, err := runVibe("query", "--workspace", root, "--read-only", "--format", "csv", "SELECT * FROM t")
	require.NoError(t, err, stderr)
	assert.Equal(t, "id\n1\n", out)

	_, _, err = runVibe("query", "--workspace", root, "--read-only", "INSERT INTO t VALUES (2)")
	require.Error(t, err, "a read-only attach cannot write")
}

func TestQuery_ReadOnlyOnAFreshWorkspaceSaysThereIsNoData(t *testing.T) {
	root := newWS(t)
	_, _, err := runVibe("query", "--workspace", root, "--read-only", "SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no data file yet")
	assert.NoFileExists(t, filepath.Join(root, "data", "local.duckdb"))
}

func TestQuery_RefusesVibeFilesAndBadFlags(t *testing.T) {
	root := newWS(t)
	_, _, err := runVibe("query", "--workspace", "old.vibe", "SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer opened")

	_, _, err = runVibe("query", "--workspace", root, "--format", "xml", "SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestImport_CreatesTableWithPK(t *testing.T) {
	root := newWS(t)
	csvPath := writeFile(t, t.TempDir(), "customers.csv", "customer_id,name\n1,Ann\n2,Bob\n")

	out, stderr, err := runVibe("import", csvPath, "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "Imported 2 row(s)")
	assert.Contains(t, out, "customers")

	q, _, err := runVibe("query", "--workspace", root, "--format", "csv", "SELECT name FROM customers ORDER BY customer_id")
	require.NoError(t, err)
	assert.Equal(t, "name\nAnn\nBob\n", q)

	q, _, err = runVibe("query", "--workspace", root, "--format", "csv",
		`SELECT count(*) FROM duckdb_constraints() WHERE table_name='customers' AND constraint_type='PRIMARY KEY'`)
	require.NoError(t, err)
	assert.Contains(t, q, "1", "the suggested customer_id key is used")

	// The import also refreshed the workspace-<table> skill, as Studio's does.
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	sk, err := ws.Skills().List(context.Background())
	require.NoError(t, err)
	require.Len(t, sk, 1)
	assert.Contains(t, sk[0].Name, "customers")
}

// TestImport_LandsInTheWorkspaceNotAnEnvelope is the #72 regression: the CLI
// used to write into the SQLite envelope, where nothing else could see it.
// An imported table must be visible to `vibe query` and to a fresh
// workspace.OpenSession (what Studio and `mcp serve` use), and lives in the
// default connection's DuckDB file.
func TestImport_LandsInTheWorkspaceNotAnEnvelope(t *testing.T) {
	root := newWS(t)
	csvPath := writeFile(t, t.TempDir(), "sales.csv", "id,amount\n1,10.5\n2,20\n")
	_, stderr, err := runVibe("import", csvPath, "--into", "sales", "--workspace", root)
	require.NoError(t, err, stderr)

	out, _, err := runVibe("query", "--workspace", root, "--format", "csv", "SELECT sum(amount) AS total FROM sales")
	require.NoError(t, err)
	assert.Contains(t, out, "30.5")

	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	sess, issues, err := workspace.OpenSession(context.Background(), ws, testCmdLogger())
	require.NoError(t, err)
	require.Empty(t, issues)
	var n int
	require.NoError(t, sess.DB().QueryRow("SELECT count(*) FROM local.main.sales").Scan(&n))
	assert.Equal(t, 2, n)
	require.NoError(t, sess.Close())

	// And in the DuckDB file itself, opened on its own.
	db, err := sql.Open("duckdb", filepath.Join(root, "data", "local.duckdb")+"?access_mode=read_only")
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	require.NoError(t, db.QueryRow("SELECT count(*) FROM sales").Scan(&n))
	assert.Equal(t, 2, n)
	assert.Empty(t, globVibeFiles(t, root), "no .vibe envelope is ever written")
}

func globVibeFiles(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	require.NoError(t, filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.EqualFold(filepath.Ext(p), ".vibe") {
			found = append(found, p)
		}
		return nil
	}))
	return found
}

func TestImport_IntoQualifiedConnection(t *testing.T) {
	root := newWS(t)
	other := filepath.Join(root, "data", "other.duckdb")
	db, err := sql.Open("duckdb", other)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	rw := false
	addConnection(t, root, workspace.ConnectionDef{Name: "other", Type: "duckdb", Path: "data/other.duckdb", ReadOnly: &rw})

	csvPath := writeFile(t, t.TempDir(), "items.csv", "id,label\n1,a\n")
	out, stderr, err := runVibe("import", csvPath, "--into", "other.items", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "other.main.items")

	q, _, err := runVibe("query", "--workspace", root, "-c", "other", "--format", "csv", "SELECT label FROM items")
	require.NoError(t, err)
	assert.Equal(t, "label\na\n", q)
	_, _, err = runVibe("query", "--workspace", root, "SELECT * FROM items")
	require.Error(t, err, "the default connection has no such table")
}

func TestImport_RefusesReadOnlyConnection(t *testing.T) {
	root := newWS(t)
	archive := filepath.Join(root, "data", "archive.duckdb")
	db, err := sql.Open("duckdb", archive)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	addConnection(t, root, workspace.ConnectionDef{Name: "archive", Type: "duckdb", Path: "data/archive.duckdb"})

	csvPath := writeFile(t, t.TempDir(), "items.csv", "id\n1\n")
	_, _, err = runVibe("import", csvPath, "--into", "archive.items", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only")
	assert.Contains(t, err.Error(), "read_only: false")
}

func TestImport_RerunNeedsAMode_UpsertAndAppend(t *testing.T) {
	root := newWS(t)
	dir := t.TempDir()
	csvPath := writeFile(t, dir, "customers.csv", "customer_id,name\n1,Ann\n")
	_, _, err := runVibe("import", csvPath, "--workspace", root)
	require.NoError(t, err)

	out, _, err := runVibe("import", csvPath, "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, out, "--mode")

	csv2 := writeFile(t, dir, "customers.csv", "customer_id,name\n1,Ann\n2,Bob\n")
	out, _, err = runVibe("import", csv2, "--workspace", root, "--mode", "upsert")
	require.NoError(t, err)
	assert.Contains(t, out, "1 new, 0 changed, 1 unchanged")

	csv3 := writeFile(t, dir, "customers.csv", "customer_id,name\n3,Cy\n")
	out, _, err = runVibe("import", csv3, "--workspace", root, "--mode", "append")
	require.NoError(t, err)
	assert.Contains(t, out, "Added 1 row(s)")

	_, _, err = runVibe("import", csv3, "--workspace", root, "--mode", "bogus")
	require.Error(t, err)
}

func TestImport_DryRunWritesNothing(t *testing.T) {
	root := newWS(t)
	csvPath := writeFile(t, t.TempDir(), "customers.csv", "customer_id,name\n1,Ann\n")
	out, _, err := runVibe("import", csvPath, "--workspace", root, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "Would import 1 row(s)")

	_, _, err = runVibe("query", "--workspace", root, "SELECT * FROM customers")
	require.Error(t, err, "a dry run must leave no table behind")

	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	sk, _ := ws.Skills().List(context.Background())
	assert.Empty(t, sk, "no skill from a dry run")
}

func TestImport_UnsupportedFileTypeAndVibeFile(t *testing.T) {
	root := newWS(t)
	p := writeFile(t, t.TempDir(), "x.parquet", "not parquet")
	_, _, err := runVibe("import", p, "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file type")

	// A .vibe argument is a bundle now; an older container is refused plainly.
	old := writeFile(t, t.TempDir(), "old.vibe", "SQLite format 3\x00 and then some data")
	_, _, err = runVibe("import", old, "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "older .vibe file that holds data")
}

func TestImport_WhenAnotherProcessOwnsTheHub(t *testing.T) {
	root := newWS(t)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	hold, err := hub.AcquireHub(ws, "vibe mcp serve")
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck

	csvPath := writeFile(t, t.TempDir(), "c.csv", "id\n1\n")
	_, _, err = runVibe("import", csvPath, "--workspace", root)
	require.Error(t, err)
	assert.ErrorIs(t, err, hub.ErrHubHeld)
	assert.Contains(t, err.Error(), "vibe mcp serve (pid ")
}

func seededWS(t *testing.T) string {
	t.Helper()
	root := newWS(t)
	csvPath := writeFile(t, t.TempDir(), "people.csv", "id,name\n1,Ann\n2,\"Bob, Jr\"\n")
	_, stderr, err := runVibe("import", csvPath, "--workspace", root)
	require.NoError(t, err, stderr)
	return root
}

func TestExport_FilesByExtension(t *testing.T) {
	root := seededWS(t)
	dst := t.TempDir()

	csvOut := filepath.Join(dst, "people.csv")
	out, stderr, err := runVibe("export", "people", "-o", csvOut, "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "Exported to "+csvOut)
	b, _ := os.ReadFile(csvOut)
	assert.Contains(t, string(b), `"Bob, Jr"`, "a comma in a value is quoted")

	jsonOut := filepath.Join(dst, "people.json")
	_, _, err = runVibe("export", "local.people", "-o", jsonOut, "--workspace", root)
	require.NoError(t, err)
	b, _ = os.ReadFile(jsonOut)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(b, &rows), string(b))
	assert.Len(t, rows, 2)

	pq := filepath.Join(dst, "people.parquet")
	_, _, err = runVibe("export", "--sql", "SELECT name FROM people WHERE id = 1", "-o", pq, "--workspace", root)
	require.NoError(t, err)
	fi, err := os.Stat(pq)
	require.NoError(t, err)
	assert.Greater(t, fi.Size(), int64(0))
	got, _, err := runVibe("query", "--workspace", root, "--format", "csv", "SELECT * FROM read_parquet('"+pq+"')")
	require.NoError(t, err)
	assert.Equal(t, "name\nAnn\n", got)
}

func TestExport_StdoutAndRefusals(t *testing.T) {
	root := seededWS(t)
	out, _, err := runVibe("export", "people", "--workspace", root)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "id,name\n"), out)

	out, _, err = runVibe("export", "people", "--format", "json", "--workspace", root)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "["), out)

	dst := filepath.Join(t.TempDir(), "p.csv")
	require.NoError(t, os.WriteFile(dst, []byte("keep me"), 0o644))
	_, _, err = runVibe("export", "people", "-o", dst, "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")
	b, _ := os.ReadFile(dst)
	assert.Equal(t, "keep me", string(b), "an existing file is not touched without --force")
	_, _, err = runVibe("export", "people", "-o", dst, "--force", "--workspace", root)
	require.NoError(t, err)
	b, _ = os.ReadFile(dst)
	assert.Contains(t, string(b), "Ann")

	for _, args := range [][]string{
		{"export", "--workspace", root},
		{"export", "people", "--sql", "SELECT 1", "--workspace", root},
		{"export", "people", "-o", "x.txt", "--workspace", root},
		{"export", "./old.vibe", "--workspace", root},
		{"export", "--sql", "ATTACH 'x' AS y", "-o", filepath.Join(t.TempDir(), "a.csv"), "--workspace", root},
		{"export", "nosuchtable", "-o", filepath.Join(t.TempDir(), "n.csv"), "--workspace", root},
	} {
		_, _, err = runVibe(args...)
		assert.Error(t, err, "%v", args)
	}
}

func TestExport_ReadOnlyDoesNotNeedTheHub(t *testing.T) {
	root := seededWS(t)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	hold, err := hub.AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck

	_, _, err = runVibe("export", "people", "--workspace", root)
	require.ErrorIs(t, err, hub.ErrHubHeld)
	out, stderr, err := runVibe("export", "people", "--read-only", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "Ann")
}

func TestRemovedContainerCommandsPointToWorkspaces(t *testing.T) {
	for _, name := range []string{"info", "verify", "compact", "rekey", "dump", "passphrase"} {
		_, _, err := runVibe(name, "old.vibe")
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "no longer opened", name)
		assert.Contains(t, err.Error(), "vibe workspace init", name)
	}
}

// slowKeys feeds keystrokes one at a time with a pause, so the TUI renders a
// frame between them (its renderer is frame-throttled).
func slowKeys(keys ...string) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		time.Sleep(300 * time.Millisecond)
		for _, k := range keys {
			_, _ = io.WriteString(pw, k)
			time.Sleep(120 * time.Millisecond)
		}
		_ = pw.Close()
	}()
	return pr
}

func TestStudio_RefusesVibeFiles(t *testing.T) {
	_, _, err := runVibe("studio", "old.vibe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer opened")
}

// TestStudioTUI_RunsOnTheWorkspace drives the real TUI program on a temp
// workspace through a scripted input: tab to the notebooks view, then quit.
func TestStudioTUI_RunsOnTheWorkspace(t *testing.T) {
	root := newWS(t, "--template", "contacts")
	// \t switches to Tables, \t again to Notebooks; ctrl+c quits.
	out, stderr, err := runVibeIn(slowKeys("\t", "\t", "\x03"), "studio", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "VIBE STUDIO")
	assert.Contains(t, out, "acme")
	assert.Contains(t, out, "Contacts", "the starter notebook is listed")
	assert.NotContains(t, out, "[read-only]")

	// The TUI released the hub on exit.
	_, err = hub.ReadHolder(root)
	assert.ErrorIs(t, err, hub.ErrNotHeld)
}

func TestStudioTUI_ReadOnlyMarksTheStatusBarAndSkipsTheLock(t *testing.T) {
	root := seededWS(t)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	hold, err := hub.AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck

	_, _, err = runVibeIn(strings.NewReader("\x03"), "studio", "--tui", root)
	require.ErrorIs(t, err, hub.ErrHubHeld, "stdin is not a terminal, so there is no prompt, just advice")

	out, stderr, err := runVibeIn(slowKeys("\t", "\x03"), "studio", "--read-only", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "[read-only]")
	assert.Contains(t, out, "people", "the table browser lists the default connection's tables")
}

func TestStudio_GuiAcceptsAFolder(t *testing.T) {
	root := newWS(t)
	t.Setenv("PATH", t.TempDir()) // no vibe-studio to exec: we only check the folder is accepted
	_, _, err := runVibe("studio", "--gui", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vibe-studio binary not found", "a folder argument is no longer refused")
	assert.NotContains(t, err.Error(), "#111")
}

func TestStudio_GuiPassesAVibeBundleToTheApp(t *testing.T) {
	// The desktop app opens its Import dialog for a .vibe; only the terminal UI
	// (which has no import flow) refuses one.
	t.Setenv("PATH", t.TempDir()) // no vibe-studio to exec (not even one in ~/go/bin)
	_, _, err := runVibe("studio", "--gui", filepath.Join(t.TempDir(), "x.vibe"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vibe-studio binary not found")
	assert.NotErrorIs(t, err, errLegacyFileArg)

	_, _, err = runVibe("studio", filepath.Join(t.TempDir(), "x.vibe"))
	assert.ErrorIs(t, err, errLegacyFileArg)
}

func TestStudio_TuiFlagStillAccepted(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runVibeIn(slowKeys("\x03"), "studio", "--tui", root)
	require.NoError(t, err, stderr)
}

func TestStudioTUI_REPLRefusesAttach(t *testing.T) {
	root := newWS(t)
	// Type the statement one key at a time, then Enter, then quit.
	keys := []string{}
	for _, r := range "ATTACH 'x.duckdb' AS x" {
		keys = append(keys, string(r))
	}
	keys = append(keys, "\r", "\x03")
	out, stderr, err := runVibeIn(slowKeys(keys...), "studio", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "not available here")
}

func TestIsVibeTableArg(t *testing.T) {
	dir := t.TempDir()
	real := writeFile(t, dir, "old.vibe", "x")
	assert.True(t, isVibeTableArg(real), "an existing file")
	assert.True(t, isVibeTableArg("./old.vibe"))
	assert.True(t, isVibeTableArg("dir/old.vibe"))
	assert.False(t, isVibeTableArg("s.vibe"), "schema.table shape, no such file")
	assert.False(t, isVibeTableArg("SELECT * FROM s.vibe"))
	assert.False(t, isVibeTableArg(`"my.vibe"`))
	assert.False(t, isVibeTableArg("orders"))
	assert.True(t, isVibeFile("old.vibe"), "a path argument")
}

func TestExport_TableNamedLikeAVibeFileIsATable(t *testing.T) {
	root := newWS(t)
	_, _, err := runVibe("export", "s.vibe", "--workspace", root)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "no longer opened")
}

func TestExport_SQLMustBeOneSelect(t *testing.T) {
	root := seededWS(t)
	dst := filepath.Join(t.TempDir(), "o.csv")
	for _, q := range []string{
		"DROP TABLE people",
		"SELECT 1) TO '/tmp/should-not-exist.csv'; DROP TABLE people; COPY (SELECT 1",
		"SELECT 1; DROP TABLE people",
		"INSERT INTO people VALUES (9, 'x')",
	} {
		_, _, err := runVibe("export", "--sql", q, "-o", dst, "--workspace", root)
		require.Error(t, err, q)
		_, _, err = runVibe("export", "--sql", q, "--workspace", root)
		require.Error(t, err, "also without -o: %s", q)
	}
	assert.NoFileExists(t, dst)
	out, _, err := runVibe("query", "--workspace", root, "--format", "csv", "SELECT count(*) FROM people")
	require.NoError(t, err)
	assert.Equal(t, "count_star()\n2\n", out, "the table survived")
}

func TestExport_DanglingSymlinkNeedsForce(t *testing.T) {
	root := seededWS(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.csv")
	link := filepath.Join(dir, "out.csv")
	require.NoError(t, os.Symlink(target, link))
	_, _, err := runVibe("export", "people", "-o", link, "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")
	assert.NoFileExists(t, target)
}

func TestExportAndImport_ReportADownConnectionBeforeAMissingTable(t *testing.T) {
	root := newWS(t)
	addConnection(t, root, workspace.ConnectionDef{Name: "gone", Type: "duckdb", Path: "data/gone.duckdb"})
	csvPath := writeFile(t, t.TempDir(), "t.csv", "id\n1\n")
	_, _, err := runVibe("export", "gone.t", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection gone is not available")
	_, _, err = runVibe("import", csvPath, "--into", "gone.t", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection gone is not available")
}

// A second process that really holds the DuckDB file: the advice must be
// accurate, not a dead end.
func TestHeldWorkspace_AdviceIsAccurate(t *testing.T) {
	root, cfg := newCmdWorkspace(t)
	_, _, err := runVibe("query", "--workspace", root, "CREATE TABLE t AS SELECT 1 AS id")
	require.NoError(t, err)

	_, in, out, stderr := vibeProc(t, cfg, "mcp", "serve", "--workspace", root)
	_, err = io.WriteString(in, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`+"\n")
	require.NoError(t, err)
	require.True(t, out.Scan(), stderr.String())

	// A command with a --read-only flag suggests it and says what it reaches.
	_, _, err = runVibe("query", "--workspace", root, "SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--read-only")
	assert.Contains(t, err.Error(), "other than local DuckDB files")

	// import has no such flag, so it must not suggest one.
	csvPath := writeFile(t, t.TempDir(), "c.csv", "id\n1\n")
	_, _, err = runVibe("import", csvPath, "--workspace", root)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "--read-only")
	assert.Contains(t, err.Error(), "close it first")

	// --read-only against the held local file: plain words, not DuckDB's error.
	_, _, err = runVibe("query", "--workspace", root, "--read-only", "SELECT * FROM t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has open for writing")
	assert.NotContains(t, err.Error(), "Conflicting lock")
	assert.Contains(t, err.Error(), "vibe mcp serve")

	js, _, err := runVibe("workspace", "info", "--workspace", root, "--json")
	require.NoError(t, err)
	var rep infoReport
	require.NoError(t, json.Unmarshal([]byte(js), &rep))
	require.NotEmpty(t, rep.Connections)
	assert.Equal(t, "locked-by", rep.Connections[0].Status)
	assert.NotContains(t, rep.Connections[0].Message, "Conflicting lock")

	// The TUI refuses to open on a locked default database instead of showing
	// an empty screen.
	_, _, err = runVibeIn(slowKeys("\x03"), "studio", "--read-only", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has open for writing")
}

func TestGuiTarget(t *testing.T) {
	root, err := filepath.EvalSymlinks(newWS(t))
	require.NoError(t, err)
	sub := filepath.Join(root, "notebooks")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	plain := t.TempDir()

	assert.Equal(t, root, guiTarget(sub), "a folder inside a workspace opens its root")
	assert.Equal(t, root, guiTarget(root))
	assert.Equal(t, plain, guiTarget(plain), "a plain folder passes through")
	b := filepath.Join(plain, "x.vibe")
	assert.Equal(t, b, guiTarget(b), "a bundle passes through")

	t.Chdir(sub)
	assert.Equal(t, root, guiTarget(""), "no dir uses the workspace around cwd")
	t.Chdir(plain)
	assert.Equal(t, "", guiTarget(""), "outside any workspace: launcher")
}

// workspace info and conn list use the same status words as Studio's rail
// (session.StatusKind): a connection that could not even be registered reads
// "error" in both, never "unavailable".
func TestConnStatusWords_InfoAndListAgree(t *testing.T) {
	root := newWS(t)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "broken", Type: "sqlite", Path: "${NO_SUCH_VIBE_VAR}/x.sqlite"}))
	require.NoError(t, ws.Close())

	js, _, err := runVibe("workspace", "info", "--workspace", root, "--json")
	require.NoError(t, err)
	var rep infoReport
	require.NoError(t, json.Unmarshal([]byte(js), &rep))
	infoStatus := map[string]string{}
	for _, c := range rep.Connections {
		infoStatus[c.Name] = c.Status
	}

	lj, _, err := runVibe("conn", "list", "--json", "--workspace", root)
	require.NoError(t, err)
	var rows []connRow
	require.NoError(t, json.Unmarshal([]byte(lj), &rows))
	require.NotEmpty(t, rows)
	for _, r := range rows {
		assert.Equal(t, infoStatus[r.Name], r.Status, r.Name)
		switch session.StatusKind(r.Status) {
		case session.StatusOK, session.StatusNotConnected, session.StatusLockedBy, session.StatusError, session.StatusNeedsCredentials, session.StatusExtensionMissing:
		default:
			t.Errorf("%s: %q is not one of the status words Studio uses", r.Name, r.Status)
		}
	}
	assert.Equal(t, "error", infoStatus["broken"])
	assert.NotEqual(t, "unavailable", infoStatus["broken"])
}
