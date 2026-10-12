package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/importer"
	"github.com/SandwichLabs/vibe/internal/skills"
)

// TestImportFile_MaliciousColumnName_CannotWriteFile is the end-to-end
// regression test for the CRITICAL finding: a CSV header crafted to break
// out of a Go `%q`-quoted identifier let the old ImportCSV -> WriteImportSkill
// path run an attacker-chosen COPY ... TO '<path>' statement — an
// arbitrary-file-write primitive triggered just by importing a CSV with a
// hostile column name. The importer now additionally normalizes every
// column name to a safe identifier (NormalizeName) before it ever reaches
// CREATE TABLE, on top of WriteImportSkill's own session.QuoteIdent
// quoting — either alone is enough to block this, and this test proves
// both together still do.
//
// The payload mirrors the reviewer's malicious CSV proof of concept:
// `a\,"a"") FROM evilcsv; COPY (SELECT 'pwned-by-csv') TO '<path>'; --`.
func TestImportFile_MaliciousColumnName_CannotWriteFile(t *testing.T) {
	app := newOpenApp(t)

	pwnedPath := filepath.Join(t.TempDir(), "pwned-by-csv.txt")
	evilHeader := `a\,"a"") FROM evilcsv; COPY (SELECT 'pwned-by-csv') TO '` + pwnedPath + `'; --`

	csvPath := filepath.Join(t.TempDir(), "evil.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(evilHeader+"\n1,2\n"), 0o644))

	// The import itself may succeed (every header normalizes to some valid
	// identifier) or fail cleanly — either is acceptable. What must NEVER
	// happen is the embedded COPY statement executing.
	_, _ = app.ImportFile(importer.Options{
		Source: importer.Source{Path: csvPath, HasHeader: true}, Table: "evilcsv", Key: importer.KeyNone,
	})

	_, statErr := os.Stat(pwnedPath)
	assert.Error(t, statErr, "the injected COPY ... TO statement must never have run — no file should exist at %s", pwnedPath)
}

// TestImportFile_QuoteInColumnName_ImportsCleanly proves the fix doesn't just
// block the attack — a legitimate header containing a double quote (a real,
// if unusual, CSV header) must still import and be describable, normalized
// to a safe identifier.
func TestImportFile_QuoteInColumnName_ImportsCleanly(t *testing.T) {
	app := newOpenApp(t)

	csvPath := filepath.Join(t.TempDir(), "quoted.csv")
	// CSV-escape a literal double quote in the header as "" per RFC 4180;
	// DuckDB's sniffer decodes that to a column literally named `a"b`.
	require.NoError(t, os.WriteFile(csvPath, []byte("\"a\"\"b\",plain\nx,y\n"), 0o644))

	sum, err := app.ImportFile(importer.Options{
		Source: importer.Source{Path: csvPath, HasHeader: true}, Table: "quoted_col", Key: importer.KeyNone,
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)
	assert.Equal(t, 1, sum.Rows)

	// WriteImportSkill must have run cleanly against the quote-bearing
	// (now normalized) column (this is exercised automatically by
	// ImportFile; confirm the skill exists and describes the table without
	// erroring).
	stored, err := app.ws.Skills().Get(context.Background(), "workspace-quoted-col")
	require.NoError(t, err)
	assert.Contains(t, stored.Instructions, "## Columns as stored")
}

// TestTableHasRows_MaliciousStoredTableName is the gating-path regression:
// a shared .vibe file can carry an attacker-authored _vibe_skills row whose
// table_name is itself a crafted identifier (not something the importer
// wrote — the attacker just inserted the row directly, as anyone with write
// access to the SQLite envelope can). LoadSkills (called on every chat turn
// and every Skills view load) must not let that name break out of its
// quoting.
func TestTableHasRows_MaliciousStoredTableName(t *testing.T) {
	app := newOpenApp(t)

	pwnedPath := filepath.Join(t.TempDir(), "pwned-by-gating.txt")
	evil := `x") ; COPY (SELECT 'pwned') TO '` + pwnedPath + `' ; --`

	// The attacker creates a real table with this exact (crafted) name —
	// they authored the .vibe file, so this is within their control.
	quoted := `"` + escapeDoubleQuotes(evil) + `"`
	_, err := app.sess.DB().Exec(`CREATE TABLE ` + quoted + ` (a INT)`)
	require.NoError(t, err)
	_, err = app.sess.DB().Exec(`INSERT INTO ` + quoted + ` VALUES (1)`)
	require.NoError(t, err)

	_, err = skills.SaveUserSkillAt(app.ws.Skills(), app.sess, "evil", "d", "i", evil, "")
	require.NoError(t, err)

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
