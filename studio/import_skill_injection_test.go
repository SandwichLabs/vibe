package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImportCSV_MaliciousColumnName_CannotWriteFile is the end-to-end
// regression test for the CRITICAL finding: a CSV header crafted to break
// out of a Go `%q`-quoted identifier let ImportCSV -> WriteImportSkill run
// an attacker-chosen COPY ... TO '<path>' statement — an arbitrary-file-write
// primitive triggered just by importing a CSV with a hostile column name.
// DuckDB creates the column name verbatim from the CSV header (ImportCSV
// itself never quotes it), so the exploit fires the moment WriteImportSkill
// (called automatically after a successful import) introspects that column.
//
// The payload mirrors the reviewer's scratchpad/evil.csv and PoC files:
// `a\,"a"") FROM evilcsv; COPY (SELECT 'pwned-by-csv') TO '<path>'; --`.
func TestImportCSV_MaliciousColumnName_CannotWriteFile(t *testing.T) {
	app := newOpenApp(t)

	pwnedPath := filepath.Join(t.TempDir(), "pwned-by-csv.txt")
	evilHeader := `a\,"a"") FROM evilcsv; COPY (SELECT 'pwned-by-csv') TO '` + pwnedPath + `'; --`

	csvPath := filepath.Join(t.TempDir(), "evil.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(evilHeader+"\n1,2\n"), 0o644))

	// The import itself may succeed (DuckDB accepts almost any column name)
	// or fail cleanly — either is acceptable. What must NEVER happen is the
	// embedded COPY statement executing.
	_, _ = app.ImportCSV(csvPath, "evilcsv")

	_, statErr := os.Stat(pwnedPath)
	assert.Error(t, statErr, "the injected COPY ... TO statement must never have run — no file should exist at %s", pwnedPath)
}

// TestImportCSV_QuoteInColumnName_ImportsCleanly proves the fix doesn't just
// block the attack — a legitimate header containing a double quote (a real,
// if unusual, CSV header) must still import and be describable.
func TestImportCSV_QuoteInColumnName_ImportsCleanly(t *testing.T) {
	app := newOpenApp(t)

	csvPath := filepath.Join(t.TempDir(), "quoted.csv")
	// CSV-escape a literal double quote in the header as "" per RFC 4180;
	// DuckDB's sniffer decodes that to a column literally named `a"b`.
	require.NoError(t, os.WriteFile(csvPath, []byte("\"a\"\"b\",plain\nx,y\n"), 0o644))

	res, err := app.ImportCSV(csvPath, "quoted_col")
	require.NoError(t, err)
	assert.Equal(t, 1, res.RowsImported)

	// WriteImportSkill must have run cleanly against the quote-bearing column
	// (this is exercised automatically by ImportCSV; confirm the skill exists
	// and mentions the column without erroring).
	var instructions string
	err = app.container.SQLiteDB().QueryRow(
		`SELECT instructions FROM _vibe_skills WHERE name = 'workspace-quoted_col'`,
	).Scan(&instructions)
	require.NoError(t, err)
	assert.Contains(t, instructions, "## Columns as stored")
}

// TestTableHasRows_MaliciousStoredTableName is the gating-path regression:
// a shared .vibe file can carry an attacker-authored _vibe_skills row whose
// table_name is itself a crafted identifier (not something ImportCSV wrote —
// the attacker just inserted the row directly, as anyone with write access
// to the SQLite envelope can). LoadSkills (called on every chat turn and
// every Skills view load) must not let that name break out of its quoting.
func TestTableHasRows_MaliciousStoredTableName(t *testing.T) {
	app := newOpenApp(t)

	pwnedPath := filepath.Join(t.TempDir(), "pwned-by-gating.txt")
	evil := `x") ; COPY (SELECT 'pwned') TO '` + pwnedPath + `' ; --`

	// The attacker creates a real table with this exact (crafted) name —
	// they authored the .vibe file, so this is within their control.
	quoted := `"` + escapeDoubleQuotes(evil) + `"`
	_, err := app.container.DuckDBDB().Exec(`CREATE TABLE ` + quoted + ` (a INT)`)
	require.NoError(t, err)
	_, err = app.container.DuckDBDB().Exec(`INSERT INTO ` + quoted + ` VALUES (1)`)
	require.NoError(t, err)

	require.NoError(t, agents.SaveUserSkill(app.container.SQLiteDB(), app.container.DuckDBDB(), "evil", "d", "i", evil))

	_, err = app.ListSkills()
	require.NoError(t, err)

	_, statErr := os.Stat(pwnedPath)
	assert.Error(t, statErr, "gating (tableHasRows, run on every list) must never execute embedded SQL from a crafted table name")
}

func escapeDoubleQuotes(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '"' {
			out = append(out, '"', '"')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
