package crud

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeCastType_RealDuckDBTypes(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TYPE mood AS ENUM ('sad', 'it''s ok', 'a;b')`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE ty (
		a DECIMAL(10,2), b VARCHAR, c TIMESTAMP WITH TIME ZONE, d INTEGER[], e STRUCT(x_y INTEGER, "b c" VARCHAR),
		f MAP(VARCHAR, INTEGER), g mood, h UNION(num INTEGER, str VARCHAR), i INTEGER[3], j TIMESTAMP_NS, k STRUCT(a INTEGER)[])`)
	require.NoError(t, err)
	rows, err := db.Query(`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'ty'`)
	require.NoError(t, err)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name, typ string
		require.NoError(t, rows.Scan(&name, &typ))
		assert.True(t, SafeCastType(typ), "%s: %s", name, typ)
		var got any
		require.NoError(t, db.QueryRow(`SELECT CAST(NULL AS `+typ+`)`).Scan(&got), typ)
		n++
	}
	assert.Equal(t, 11, n)
}

func TestSafeCastType_Hostile(t *testing.T) {
	for _, bad := range []string{"", "  ", "123", "INTEGER); DROP TABLE t; --", "INT) FROM secret WHERE (1", "VARCHAR(", "INT;",
		"INT -- x", "INT /* x */", "ENUM('a)", `STRUCT("a)`, "INT, VARCHAR", "INT + 1", "IN\nT", "ENUM('a\x00b')", "INT.x", "ENUM(E'\\' ', '1')) OR TRUE --')", "ENUM(N'a')", "ENUM(B'a')"} {
		assert.False(t, SafeCastType(bad), "%q", bad)
	}
	assert.True(t, SafeCastType(`ENUM('a)', 'b''); DROP TABLE t; --')`), "quoted content may hold punctuation")
}

func TestCastType_HostileCatalogTypeFallsBack(t *testing.T) {
	m := map[string]string{"a": "INTEGER) , (SELECT 1", "b": "DECIMAL(10,2)", "c": ""}
	assert.Equal(t, "VARCHAR", castType(m, "a"))
	assert.Equal(t, "DECIMAL(10,2)", castType(m, "b"))
	assert.Equal(t, "VARCHAR", castType(m, "c"))
	assert.Equal(t, "VARCHAR", castType(m, "zzz"))
}

func TestCheckAgentColumnType(t *testing.T) {
	for _, ok := range []string{"VARCHAR(64)", "DECIMAL(10,2)", "INTEGER PRIMARY KEY", "timestamp", "INTEGER REFERENCES customers(id)",
		"STRUCT(a INT, b VARCHAR(8))", "INTEGER NOT NULL", "INTEGER DEFAULT 0", "INTEGER DEFAULT -5 NOT NULL", "DOUBLE DEFAULT 1.5e3",
		"VARCHAR DEFAULT 'abc'", "VARCHAR DEFAULT 'check as'", "VARCHAR DEFAULT 'it''s'", "VARCHAR DEFAULT 'a;b -- c' NOT NULL", "VARCHAR DEFAULT NULL", "BOOLEAN default TRUE",
		"TIMESTAMP DEFAULT CURRENT_TIMESTAMP", "DATE DEFAULT CURRENT_DATE", "INTEGER[]", "VARCHAR UNIQUE"} {
		assert.NoError(t, CheckAgentColumnType(ok), ok)
	}
	for _, bad := range []string{"", "123", "INT;", "INT--", `"x"`, "INT) AS SELECT 1 FROM t WHERE (1", "VARCHAR(64",
		"VARCHAR DEFAULT chr(39)||chr(120)", "VARCHAR DEFAULT concat('a','b')", "VARCHAR DEFAULT repeat('x',3)",
		"INTEGER DEFAULT 1+1", "INTEGER DEFAULT 1 - 1", "INTEGER DEFAULT (1)", "INTEGER DEFAULT nextval('s')", "INTEGER DEFAULT",
		"VARCHAR DEFAULT 'a' 'b'", "VARCHAR DEFAULT E'x'", "VARCHAR DEFAULT 'unterminated", "INTEGER DEFAULT 1 2",
		"INTEGER CHECK (x > 0)", "INTEGER check(x>0)", "INTEGER GENERATED ALWAYS AS (1)", "INTEGER AS (1+1)",
		"INTEGER DEFAULT 0 CHECK (x > 0)", "INTEGER DEFAULT getenv('HOME')", "VARCHAR DEFAULT 'x'::INT", "INTEGER DEFAULT 1 -- c",
		"INTEGER, ALTER v TYPE VARCHAR USING concat(v, 'x')", "INTEGER,"} {
		assert.Error(t, CheckAgentColumnType(bad), "%q", bad)
	}
	err := CheckAgentColumnType("INTEGER CHECK (x > 0)")
	assert.ErrorContains(t, err, "CHECK is not allowed")
	assert.ErrorContains(t, err, "allowed:")
}

func TestDeleteRowTx_EscapeStringTypeFallsBack(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE d (id INTEGER PRIMARY KEY); INSERT INTO d VALUES (1),(2),(3)`)
	require.NoError(t, err)
	hostile := "ENUM(E'\\' ', '1')) OR TRUE --')"
	require.False(t, SafeCastType(hostile))
	res, err := DeleteRowTx(db, map[string]string{"id": hostile}, "d", "id", "1")
	require.NoError(t, err)
	assert.EqualValues(t, 1, res.Affected)
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM d`).Scan(&n))
	assert.Equal(t, 2, n)
}
