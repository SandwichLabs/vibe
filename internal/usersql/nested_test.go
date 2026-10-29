package usersql

import (
	"database/sql"
	"strings"
	"testing"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkipBlockComment(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int // index just after the comment, in runes
	}{
		{"/* a */x", 7},
		{"/* a /* b */ c */x", 17},
		{"/* a /* b */ x", 14}, // mis-nested: swallows the rest
		{"/* open", 7},
		{"/**/x", 4},
	} {
		assert.Equal(t, c.want, skipBlockComment([]rune(c.in), 0), c.in)
	}
}

func TestCheckNestedCommentsHideOnlyWhatDuckDBHides(t *testing.T) {
	// The inner comment closes, the outer does not: everything after is comment,
	// so there is no statement to refuse; the check must not crash or misread.
	assert.NoError(t, Check("SELECT 1; /* x /* y */ CREATE SECRET s (TYPE http)", false))
	// A nested comment that closes properly must not hide what follows it.
	assert.Error(t, Check("/* x /* y */ z */ CREATE SECRET s (TYPE http)", false))
	// The old non-nesting lexer ended the comment at the first */ and then read
	// "z */ ..." as SQL; nesting keeps the hidden text hidden both ways.
	assert.NoError(t, Check("/* x /* y */ CREATE SECRET s (TYPE http) */ SELECT 1", false))
}

func TestTokensNestedCommentDoesNotHideKeywords(t *testing.T) {
	got := Tokens("/* x /* y */ CREATE SECRET s */ SELECT 1")
	require.Len(t, got, 1)
	assert.Equal(t, Token{TokWord, "SELECT"}, got[0][0])
	st := statements("/* x /* y */ CREATE SECRET s */ SELECT 1")
	require.Len(t, st, 1)
	assert.Equal(t, "SELECT", st[0].words[0])
}

// Differential: statements() cuts these scripts where DuckDB does.
func TestStatementsAgreeWithDuckDB(t *testing.T) {
	scripts := []string{
		"/* x /* y */ ; CREATE TABLE no(x INT); /* z */ */ CREATE TABLE yes(x INT);",
		"/* ; */ CREATE TABLE t1(x INT) /* a /* ; */ ; */; CREATE TABLE t2(x INT);",
		// "$" inside a name never opens a dollar quote; after a number it does.
		"SELECT 1 AS x$a$; CREATE TABLE t1(x INT); SELECT 2 AS y$a$; CREATE TABLE t2(x INT);",
		"SELECT 1 AS é$a$; CREATE TABLE t1(x INT); SELECT 1 AS y$$; CREATE TABLE t2(x INT);",
		"SELECT 1a$b$; CREATE TABLE t1(x INT);",
		"SELECT 1_0 AS a$b$; CREATE TABLE t1(x INT);",
		"SELECT 0x1f$b$; CREATE TABLE t1(x INT);",
		"SELECT 1._5$a$; CREATE TABLE t1(x INT); SELECT 1 AS b$a$;",
		"SELECT 1._5$$; CREATE TABLE t1(x INT); SELECT 1 AS b$$;",
		"SELECT 1__0$a$; CREATE TABLE t1(x INT);",
		"SELECT 1_$a$; CREATE TABLE t1(x INT);",
		"SELECT 1e_5$a$; CREATE TABLE t1(x INT);",
		"SELECT 0x_1$a$; CREATE TABLE t1(x INT);",
		"SELECT 0x1_f$a$; CREATE TABLE t1(x INT);",
		"SELECT 0b1_0$a$; CREATE TABLE t1(x INT);",
		"SELECT 1.5_$a$; CREATE TABLE t1(x INT);",
		"SELECT 1_e5$a$; CREATE TABLE t1(x INT);",
		"SELECT 1._$a$; CREATE TABLE t1(x INT);",
		"CREATE TABLE t1 AS SELECT $t$ $u$; $u$ ; $t$ AS s; CREATE TABLE t2(x INT);",
		"CREATE TABLE t1 AS SELECT 'a;b' AS s; -- ; CREATE TABLE no(x INT);\r\nCREATE TABLE t2(x INT);",
	}
	for _, script := range scripts {
		c, err := duckdb.NewConnector("", nil)
		require.NoError(t, err)
		db := sql.OpenDB(c)
		_, err = db.Exec(script)
		require.NoError(t, err, script)
		var want int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM duckdb_tables()").Scan(&want))
		_ = db.Close()

		n := 0
		for _, st := range statements(script) {
			if len(st.words) > 0 && st.words[0] == "CREATE" {
				n++
			}
		}
		assert.Equal(t, want, n, script)
	}
}

// A "$" continues an identifier: x$a$ is one name, so the ";" after it ends the
// statement, as DuckDB sees it. Treating it as a dollar quote hid statements
// from the gates.
func TestDollarInIdentifierDoesNotHideStatements(t *testing.T) {
	for _, stmt := range []string{
		"CREATE SECRET s (TYPE s3, KEY_ID 'k', SECRET 'v')",
		"ATTACH ':memory:' AS evil",
		"INSTALL httpfs",
		"LOAD httpfs",
		"SET enable_external_access = true",
	} {
		for _, prefix := range []string{"SELECT 1 AS x$a$; ", "SELECT 1 AS é$a$; ", "SELECT 1 AS _x1$$; ", "SELECT 1 AS x$ ; "} {
			q := prefix + stmt + "; SELECT $a$"
			assert.Error(t, Check(q, false), q)
			if strings.HasPrefix(stmt, "CREATE SECRET") || strings.HasPrefix(stmt, "INSTALL") || strings.HasPrefix(stmt, "LOAD") {
				assert.Error(t, CheckManaged(q), q) // the editor allows ATTACH and SET by design
			}
		}
	}
	// The same text after a number is a real dollar quote, which hides the rest.
	assert.NoError(t, Check("SELECT 1$a$; ATTACH 'x' AS y; $a$", false))
	got := Tokens("SELECT 1 AS x$a$; ATTACH 'x' AS y")
	require.Len(t, got, 2)
	assert.Equal(t, Token{TokWord, "ATTACH"}, got[1][0])
}

// After a numeric literal a "$" does open a dollar quote (DuckDB rejects the
// statement, but the lexers must agree about where the quote runs).
func TestDollarAfterNumberOpensAQuote(t *testing.T) {
	for _, q := range []string{"SELECT 1e5$b$; ATTACH 'x' AS y; $b$", "SELECT 1.5$b$; ATTACH 'x' AS y; $b$", "SELECT 1$b$; ATTACH 'x' AS y; $b$"} {
		assert.Len(t, statements(q), 1, q)
		assert.NoError(t, Check(q, false), q)
	}
	// ...but not after a letter that follows the number.
	assert.Error(t, Check("SELECT 1a$b$; ATTACH 'x' AS y; SELECT $b$", false))
	assert.Error(t, Check("SELECT 1e$b$; ATTACH 'x' AS y; SELECT $b$", false))
}

// "_" is part of a number only between two digits: 1._5 is "1." then the name
// _5, so the "$a$" after it is inside a name and never opens a quote.
func TestUnderscoreNumberShapes(t *testing.T) {
	for _, shape := range []string{"1._5", "1__0", "1_", "1e_5", "0x_1", "0x1_f", "0b1_0", "1.5_", "1_e5", "1._"} {
		q := "SELECT " + shape + "$a$; CREATE SECRET s (TYPE s3, KEY_ID 'k', SECRET 'v'); SELECT 1 AS b$a$"
		assert.Error(t, Check(q, false), q)
		assert.Error(t, CheckManaged(q), q)
	}
	// Real numbers followed by "$a$" open a dollar quote that hides the rest.
	for _, shape := range []string{"1_000", "1e5_0", ".5_0", "1.5e+5_0", "1.e5", "1_0.5_0", "0_1"} {
		assert.NoError(t, Check("SELECT "+shape+"$a$; ATTACH 'x' AS y; $a$", false), shape)
	}
}
