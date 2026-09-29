package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCmdLogger (defined in mcp_test.go) provides a *slog.Logger for tests
// that build a container directly (not through executeCommand/
// rootCmd.Execute, which is what initializes the cmd package's own global
// `logger` via cobra.OnInitialize) — using it avoids depending on test
// execution order/another test having already run to populate that global.

// mockReadPassword returns a function that simulates terminal passphrase input.
func mockReadPassword(pass string) func(fd int) ([]byte, error) {
	return func(fd int) ([]byte, error) {
		return []byte(pass), nil
	}
}

// executeCommand runs the root command with the given args and returns stdout output and error.
// It resets flag values between invocations to prevent leakage.
func executeCommand(args ...string) (string, error) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs(args)

	// Reset flag values to defaults (without clearing flag definitions).
	initCmd.Flags().Set("title", "")            //nolint:errcheck
	initCmd.Flags().Set("description", "")      //nolint:errcheck
	initCmd.Flags().Set("from", "")             //nolint:errcheck
	initCmd.Flags().Set("import", "")           //nolint:errcheck
	infoCmd.Flags().Set("json", "false")        //nolint:errcheck
	importCmd.Flags().Set("file", "")           //nolint:errcheck
	importCmd.Flags().Set("csv", "")            //nolint:errcheck
	importCmd.Flags().Set("table", "")          //nolint:errcheck
	importCmd.Flags().Set("sheet", "")          //nolint:errcheck
	importCmd.Flags().Set("no-header", "false") //nolint:errcheck
	importCmd.Flags().Set("mode", "")           //nolint:errcheck
	importCmd.Flags().Set("dry-run", "false")   //nolint:errcheck
	importCmd.Flags().Set("key", "")            //nolint:errcheck
	// resolveImportKey distinguishes "the user passed --key" from "they
	// didn't" via Flags().Changed, which the Set() call just above would
	// otherwise permanently pin to true on this shared *cobra.Command —
	// reset it directly so each test starts from "not passed".
	importCmd.Flags().Lookup("key").Changed = false

	err := rootCmd.Execute()
	return buf.String(), err
}

func TestExportCommand_RequiresTable(t *testing.T) {
	output, err := executeCommand("export", "test.vibe")
	_ = output
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--table flag is required")
}

func TestImportCommand_RequiresFlags(t *testing.T) {
	_, err := executeCommand("import", "test.vibe")
	assert.Error(t, err)
}

func TestInitCommand_CreatesFile(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.vibe")

	output, err := executeCommand("init", filePath)
	require.NoError(t, err)

	_, statErr := os.Stat(filePath)
	require.NoError(t, statErr)

	assert.Contains(t, output, "Created")
	assert.Contains(t, output, "test")
}

func TestInitCommand_WithTitle(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "titled.vibe")

	output, err := executeCommand("init", filePath, "--title", "My App")
	require.NoError(t, err)

	assert.Contains(t, output, `"My App"`)
}

func TestInitCommand_WithDescription(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "described.vibe")

	output, err := executeCommand("init", filePath, "--description", "A test container")
	require.NoError(t, err)

	assert.Contains(t, output, "Created")
}

func TestInitCommand_AddsVibeExtension(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "noext")

	_, err := executeCommand("init", filePath)
	require.NoError(t, err)

	_, statErr := os.Stat(filePath + ".vibe")
	require.NoError(t, statErr)
}

func TestInitCommand_FromTemplate(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	// Create a template first.
	templatePath := filepath.Join(dir, "template.vibe")
	_, err := executeCommand("init", templatePath, "--title", "Template")
	require.NoError(t, err)

	// Create from template.
	newPath := filepath.Join(dir, "clone.vibe")
	output, err := executeCommand("init", newPath, "--from", templatePath, "--title", "Clone")
	require.NoError(t, err)

	assert.Contains(t, output, "from template")
	assert.Contains(t, output, "Created")

	_, statErr := os.Stat(newPath)
	require.NoError(t, statErr)
}

func TestInitCommand_ImportCSV(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	csvPath := filepath.Join(dir, "data.csv")
	err := os.WriteFile(csvPath, []byte("name,age\nAlice,30\nBob,25\n"), 0644)
	require.NoError(t, err)

	filePath := filepath.Join(dir, "imported.vibe")
	output, err := executeCommand("init", filePath, "--import", csvPath)
	require.NoError(t, err)

	assert.Contains(t, output, "Imported 2 rows")
	assert.Contains(t, output, "Created")
}

func TestInitCommand_FromAndImportMutuallyExclusive(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.vibe")

	_, err := executeCommand("init", filePath, "--from", "template.vibe", "--import", "data.csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestInfoCommand_ShowsMetadata(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	// Create a file first.
	filePath := filepath.Join(dir, "info-test.vibe")
	_, err := executeCommand("init", filePath, "--title", "Info Test")
	require.NoError(t, err)

	// Run info.
	output, err := executeCommand("info", filePath)
	require.NoError(t, err)

	assert.Contains(t, output, "File:")
	assert.Contains(t, output, "Size:")
	assert.Contains(t, output, "Info Test")
	assert.Contains(t, output, "Notebooks:")
	assert.Contains(t, output, "Skills:")
	assert.Contains(t, output, "Tables:")
	assert.NotContains(t, output, "Legacy pages", "a freshly-created file has no legacy pages to report")
}

func TestInfoCommand_JSON(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	// Create a file first.
	filePath := filepath.Join(dir, "json-test.vibe")
	_, err := executeCommand("init", filePath, "--title", "JSON Test")
	require.NoError(t, err)

	// Run info --json.
	output, err := executeCommand("info", filePath, "--json")
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	assert.Equal(t, "JSON Test", result["title"])
	assert.NotNil(t, result["size"])
	assert.NotNil(t, result["notebooks"])
	assert.NotNil(t, result["skills"])
	assert.NotNil(t, result["tables"])
	assert.Nil(t, result["legacy_pages_unmigrated"], "a freshly-created file has no legacy pages to report")
}

// TestInfoCommand_ReportsUnmigratedLegacyPages asserts `vibe info` surfaces
// legacy pages as "legacy pages (unmigrated): N" — info opens the file
// read-only, so MigrateOnOpen never runs the migration itself (see its doc
// comment), and this count is exactly what tells the user there's legacy
// content waiting to be picked up by a normal (read-write) open.
func TestInfoCommand_ReportsUnmigratedLegacyPages(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "legacy-test.vibe")

	c, err := container.Create(filePath, container.WithPassphrase("testpass123"), container.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	_, err = c.SQLiteDB().Exec(
		`INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES ('p1', 'Old Page', '# Hi', 'markdown', 0, '', '')`,
	)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	output, err := executeCommand("info", filePath)
	require.NoError(t, err)
	assert.Contains(t, output, "Legacy pages (unmigrated): 1")

	jsonOutput, err := executeCommand("info", filePath, "--json")
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOutput), &result))
	assert.Equal(t, float64(1), result["legacy_pages_unmigrated"])
}

// TestInfoCommand_CountsDuckDBTables is a regression guard: `vibe info` used
// to count "Tables" from sqlite_master, but user tables live in DuckDB (the
// _vibe_duckdb blob) — sqlite_master only ever holds the internal _vibe_*
// tables, so the count was always 0 regardless of how much real data the
// file had. Must use the same information_schema.tables query GetDashboard
// (studio/app.go) uses.
func TestInfoCommand_CountsDuckDBTables(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "tables-test.vibe")

	c, err := container.Create(filePath, container.WithPassphrase("testpass123"), container.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	_, err = c.DuckDBDB().Exec(`CREATE TABLE widgets (id INTEGER); CREATE TABLE gadgets (id INTEGER);`)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	output, err := executeCommand("info", filePath)
	require.NoError(t, err)
	assert.Contains(t, output, "Tables:    2")

	jsonOutput, err := executeCommand("info", filePath, "--json")
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOutput), &result))
	assert.Equal(t, float64(2), result["tables"])
}

// TestDumpCommand_IncludesNotebooksAndSkills asserts `vibe dump` covers the
// current content surfaces (#66/#62) — not just the old schema+pages+queries
// triad.
func TestDumpCommand_IncludesNotebooksAndSkills(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "dump-test.vibe")

	c, err := container.Create(filePath, container.WithPassphrase("testpass123"), container.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	_, err = c.SQLiteDB().Exec(
		`INSERT INTO _vibe_notebooks (id, title, cells, created_at, updated_at) VALUES ('nb1', 'My Notebook', '[]', '', '')`,
	)
	require.NoError(t, err)
	_, err = c.SQLiteDB().Exec(
		`INSERT INTO _vibe_skills (name, description, instructions, table_name, source, created_at, updated_at) VALUES ('my-skill', 'A test skill', 'do the thing', '', 'user', '', '')`,
	)
	require.NoError(t, err)
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	output, err := executeCommand("dump", filePath)
	require.NoError(t, err)
	assert.Contains(t, output, "-- Notebooks")
	assert.Contains(t, output, "My Notebook")
	assert.Contains(t, output, "-- Skills")
	assert.Contains(t, output, "my-skill")
	assert.Contains(t, output, "A test skill")
}

// --- #82: `vibe import` on the shared internal/importer, DuckDB-side ---

func writeImportCSV(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// TestImportCommand_CreatesTableWithPK proves a plain `vibe import` creates
// the table with a primary key (the suggested customer_id column, not an
// unkeyed table) and that `vibe info` sees it as a live DuckDB table.
func TestImportCommand_CreatesTableWithPK(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "customers.vibe")
	_, err := executeCommand("init", filePath)
	require.NoError(t, err)

	csvPath := writeImportCSV(t, dir, "customers.csv", "customer_id,name\n1,Ann\n2,Bob\n")
	output, err := executeCommand("import", filePath, "--file", csvPath)
	require.NoError(t, err, output)
	assert.Contains(t, output, "customers")

	info, err := executeCommand("info", filePath, "--json")
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(info), &result))
	assert.Equal(t, float64(1), result["tables"])

	c, err := container.Open(filePath, container.WithPassphrase("testpass123"), container.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer c.Close()
	var pkCount int
	require.NoError(t, c.DuckDBDB().QueryRow(
		`SELECT COUNT(*) FROM duckdb_constraints() WHERE table_name='customers' AND constraint_type='PRIMARY KEY'`,
	).Scan(&pkCount))
	assert.Equal(t, 1, pkCount, "the suggested customer_id key must have been used")
}

// TestImportCommand_RerunWithoutModeErrors proves a second `vibe import`
// into an existing table without --mode is refused rather than guessing a
// destructive default.
func TestImportCommand_RerunWithoutModeErrors(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "customers.vibe")
	_, err := executeCommand("init", filePath)
	require.NoError(t, err)

	csvPath := writeImportCSV(t, dir, "customers.csv", "customer_id,name\n1,Ann\n")
	_, err = executeCommand("import", filePath, "--file", csvPath)
	require.NoError(t, err)

	_, err = executeCommand("import", filePath, "--file", csvPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customers")
}

// TestImportCommand_UpsertReportsUnchanged proves --mode upsert on an
// unchanged re-import reports "0 new, 0 changed, N unchanged" (re-import
// idempotence).
func TestImportCommand_UpsertReportsUnchanged(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "customers.vibe")
	_, err := executeCommand("init", filePath)
	require.NoError(t, err)

	lines := "customer_id,name\n"
	for i := 1; i <= 40; i++ {
		lines += fmt.Sprintf("%d,Customer %d\n", i, i)
	}
	csvPath := writeImportCSV(t, dir, "customers.csv", lines)
	_, err = executeCommand("import", filePath, "--file", csvPath)
	require.NoError(t, err)

	output, err := executeCommand("import", filePath, "--file", csvPath, "--mode", "upsert")
	require.NoError(t, err)
	assert.Contains(t, output, "0 new, 0 changed, 40 unchanged")
}

// TestImportCommand_DryRunWritesNothing proves --dry-run leaves the table
// absent (and, by extension, the .vibe file unsaved).
func TestImportCommand_DryRunWritesNothing(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "customers.vibe")
	_, err := executeCommand("init", filePath)
	require.NoError(t, err)

	csvPath := writeImportCSV(t, dir, "customers.csv", "customer_id,name\n1,Ann\n")
	output, err := executeCommand("import", filePath, "--file", csvPath, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "Would import", "a dry run must never claim past tense (Imported/Added/Replaced) — nothing was written")

	c, err := container.Open(filePath, container.WithPassphrase("testpass123"), container.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer c.Close()
	var n int
	require.NoError(t, c.DuckDBDB().QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_name='customers'").Scan(&n))
	assert.Equal(t, 0, n)
}

// TestImportCommand_DryRunUpsertSaysWouldImport proves an upsert dry run
// against an existing table also uses future tense, with the same counts
// the real run would produce.
func TestImportCommand_DryRunUpsertSaysWouldImport(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "customers.vibe")
	_, err := executeCommand("init", filePath)
	require.NoError(t, err)

	csvPath := writeImportCSV(t, dir, "customers.csv", "customer_id,name\n1,Ann\n")
	_, err = executeCommand("import", filePath, "--file", csvPath)
	require.NoError(t, err)

	output, err := executeCommand("import", filePath, "--file", csvPath, "--mode", "upsert", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "Would import 0 new, update 0, leave 1 unchanged")
}

// TestInitCommand_ImportLandsInDuckDB proves `init --import` (the #72 import
// half) writes to the DuckDB engine, not the legacy SQLite side.
func TestInitCommand_ImportLandsInDuckDB(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	csvPath := writeImportCSV(t, dir, "data.csv", "name,age\nAlice,30\nBob,25\n")
	filePath := filepath.Join(dir, "imported.vibe")
	_, err := executeCommand("init", filePath, "--import", csvPath)
	require.NoError(t, err)

	c, err := container.Open(filePath, container.WithPassphrase("testpass123"), container.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer c.Close()
	var n int
	require.NoError(t, c.DuckDBDB().QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_name='data'").Scan(&n))
	assert.Equal(t, 1, n)
}
