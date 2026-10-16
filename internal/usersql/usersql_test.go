package usersql

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheck_Refuses(t *testing.T) {
	for _, s := range []string{
		"ATTACH 'x.duckdb' AS x",
		"  -- hi\n detach local",
		"INSTALL httpfs",
		"LOAD httpfs",
		"SET enable_external_access = true",
		"CREATE SECRET s (TYPE s3, KEY_ID 'a')",
		"CREATE OR REPLACE PERSISTENT SECRET s (TYPE s3)",
		"DROP SECRET s",
		"SELECT 1; ATTACH 'x' AS y",
		"/* c */ SET threads = 1",
		"SELECT $$;$$; ATTACH 'x' AS y",
		"SELECT $t$ ' $t$; ATTACH 'x' AS y",
		`SELECT E'a\'b'; ATTACH 'x' AS y`,
	} {
		assert.Error(t, Check(s, false), s)
	}
}

func TestCheck_AllowsLookalikes(t *testing.T) {
	for _, s := range []string{
		"SELECT 'ATTACH x; SET y' AS s",
		`SELECT "set" FROM t`,
		"SELECT 1 -- ; ATTACH",
		"CREATE TABLE secret_agents (id INT)",
		"SELECT * FROM load_events",
		"INSERT INTO t VALUES ('; LOAD x')",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT $$ ; ATTACH 'x' $$",
		"SELECT $tag$ ; SET x $tag$",
		`SELECT E'it\'s; SET x'`,
		"SELECT $1 FROM t",
		"COPY t TO 'out.csv'",
	} {
		assert.NoError(t, Check(s, false), s)
	}
}

func TestCheck_ReadOnlyRefusesFileWrites(t *testing.T) {
	for _, s := range []string{"COPY t TO 'out.csv'", "COPY (SELECT 1) TO 'o.parquet' (FORMAT parquet)", "EXPORT DATABASE 'dir'"} {
		assert.Error(t, Check(s, true), s)
	}
	assert.NoError(t, Check("COPY t FROM 'in.csv'", true))
	assert.NoError(t, Check("SELECT 'to' AS x", true))
}

func TestRequireSelect(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck
	ctx := context.Background()
	for _, s := range []string{"SELECT 1", "WITH x AS (SELECT 1 AS a) SELECT * FROM x", "FROM range(3)", "VALUES (1),(2)", "SELECT 1;"} {
		assert.NoError(t, RequireSelect(ctx, db, s), s)
	}
	for _, s := range []string{
		"DROP TABLE t",
		"INSERT INTO t VALUES (1)",
		"SELECT 1; DROP TABLE t",
		"SELECT 1) TO 'x.csv'; DROP TABLE t; COPY (SELECT 1",
		"PRAGMA version",
		"not sql at all",
	} {
		assert.Error(t, RequireSelect(ctx, db, s), s)
	}
}

func TestCheckManagedRefusesOnlyLoadAndSecretDDL(t *testing.T) {
	for _, bad := range []string{
		"LOAD httpfs", "install httpfs", "DROP TEMPORARY SECRET \"vibe-deny-s3\"",
		"select 1; drop secret if exists x", "CREATE OR REPLACE SECRET s (TYPE s3)", "-- c\nLOAD aws", "FORCE INSTALL httpfs", "force install httpfs FROM core", "UPDATE EXTENSIONS", "update extensions (httpfs)",
	} {
		assert.Error(t, CheckManaged(bad), bad)
	}
	for _, ok := range []string{
		"SELECT 'LOAD x'", "ATTACH 'a.duckdb' AS a", "SET threads=1", "COPY t TO 'f.csv'",
		"DROP TABLE secrets", "UPDATE t SET x = 1", "CREATE TABLE t AS SELECT 1",
	} {
		assert.NoError(t, CheckManaged(ok), ok)
	}
}
