package agents

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLexStatementsDollarInIdentifier(t *testing.T) {
	sts := lexStatements("SELECT 1 AS x$a$; CREATE SECRET s (TYPE s3); SELECT $a$")
	require.Len(t, sts, 3)
	assert.Equal(t, "CREATE", sts[1][0].Text)
	assert.Len(t, lexStatements("SELECT 1e5$b$; CREATE SECRET s (TYPE s3); $b$"), 1)
	assert.Len(t, lexStatements("SELECT 1a$b$; CREATE SECRET s (TYPE s3);"), 2)
}

func TestLexStatementsUnderscoreNumberShapes(t *testing.T) {
	for _, shape := range []string{"1._5", "1__0", "1_", "1e_5", "0x_1", "0x1_f", "0b1_0", "1.5_", "1_e5", "1._"} {
		q := "SELECT " + shape + "$a$; CREATE SECRET s (TYPE s3); SELECT 1 AS b$a$"
		assert.Len(t, lexStatements(q), 3, q)
	}
	for _, shape := range []string{"1_000", "1e5_0", ".5_0", "1.5e+5_0", "1.e5", "1_0.5_0", "0_1"} {
		assert.Len(t, lexStatements("SELECT "+shape+"$a$; CREATE SECRET s (TYPE s3); $a$"), 1, shape)
	}
}

// The agent lexer cuts statements where DuckDB does.
func TestLexStatementsAgreeWithDuckDB(t *testing.T) {
	scripts := []string{
		"SELECT 1._5$a$; CREATE TABLE t1(x INT); SELECT 1 AS b$a$;",
		"SELECT 1._5$$; CREATE TABLE t1(x INT); SELECT 1 AS b$$;",
		"SELECT 1__0$a$; CREATE TABLE t1(x INT);",
		"SELECT 1e_5$a$; CREATE TABLE t1(x INT);",
		"SELECT 0x1_f$a$; CREATE TABLE t1(x INT);",
		"SELECT 0b1_0$a$; CREATE TABLE t1(x INT);",
		"SELECT 1_e5$a$; CREATE TABLE t1(x INT);",
		"SELECT 1 AS é$a$; CREATE TABLE t1(x INT);",
	}
	for _, script := range scripts {
		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		_, err = db.Exec(script)
		require.NoError(t, err, script)
		var want int
		require.NoError(t, db.QueryRow("SELECT count(*) FROM duckdb_tables()").Scan(&want))
		_ = db.Close()
		got := 0
		for _, st := range lexStatements(script) {
			if len(st) > 0 && st[0].isWord("CREATE") {
				got++
			}
		}
		assert.Equal(t, want, got, script)
	}
}
