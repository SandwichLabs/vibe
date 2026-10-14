//go:build integration

package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Spike #107 Postgres: secret-based READ_ONLY attach, path column,
// postgres_execute/query under READ_ONLY.
func TestSpikePostgresSecretAttachReadOnly(t *testing.T) {
	pg := startPG(t)
	pg.psql(t, "CREATE TABLE people(id int primary key, name text); INSERT INTO people VALUES (1,'ann')")
	db := openWithPostgres(t, "")
	require.NoError(t, pg.createSecret(db, "pgs", pgPassword))

	_, err := db.Exec("ATTACH '' AS pg (TYPE postgres, SECRET pgs, READ_ONLY)")
	require.NoError(t, err, "secret-based attach with empty connection string")
	v, err := scalar(context.Background(), db, "SELECT name FROM pg.public.people WHERE id=1")
	require.NoError(t, err)
	assert.Equal(t, "ann", v)

	t.Run("duckdb_databases path is empty for secret attach", func(t *testing.T) {
		r, err := rows(db, "SELECT database_name, type, path, readonly FROM duckdb_databases() WHERE database_name='pg'")
		require.NoError(t, err)
		t.Logf("duckdb_databases row: %v", r)
		require.Len(t, r, 1)
		assert.NotContains(t, r[0], pgPassword)
		assert.Equal(t, "pg|postgres||true", r[0])
	})

	t.Run("inline-DSN attach leaks the password into path (non-hardened hub)", func(t *testing.T) {
		db := openWithPostgresOpts(t, "", false)
		_, err := db.Exec("ATTACH 'host=127.0.0.1 port=" + pg.port + " dbname=postgres user=postgres password=" + pgPassword + "' AS pginline (TYPE postgres, READ_ONLY)")
		require.NoError(t, err)
		r, err := rows(db, "SELECT database_name, path FROM duckdb_databases() WHERE database_name='pginline'")
		require.NoError(t, err)
		t.Logf("inline-DSN attach path: %v", r)
		assert.Contains(t, strings.Join(r, ","), pgPassword, "FINDING: inline attach exposes password via duckdb_databases().path")
	})

	t.Run("duckdb_secrets redacts", func(t *testing.T) {
		r, err := rows(db, "SELECT name, type, secret_string FROM duckdb_secrets()")
		require.NoError(t, err)
		t.Logf("duckdb_secrets(): %v", r)
		assert.NotContains(t, strings.Join(r, ","), pgPassword)
		_, err = rows(db, "SELECT name FROM duckdb_secrets(redact=false)")
		t.Logf("duckdb_secrets(redact=false) err=%v", err)
		require.Error(t, err)
	})

	t.Run("native DML/DDL blocked by READ_ONLY", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO pg.public.people VALUES (2,'bob')")
		require.Error(t, err)
		t.Logf("INSERT under READ_ONLY: %v", err)
		_, err = db.Exec("CREATE TABLE pg.public.evil(a int)")
		require.Error(t, err)
	})

	t.Run("postgres_execute under READ_ONLY runs in a READ ONLY txn", func(t *testing.T) {
		_, err := db.Exec("CALL postgres_execute('pg', 'CREATE TABLE bypass_exec(a int)')")
		require.Error(t, err)
		t.Logf("CALL postgres_execute err=%v", err)
		assert.Equal(t, "0", pg.psql(t, "SELECT count(*) FROM information_schema.tables WHERE table_name='bypass_exec'"))
	})

	t.Run("postgres_execute multi-statement escape attempts", func(t *testing.T) {
		attempts := map[string]string{
			"commit-then-ddl":  "COMMIT; CREATE TABLE esc_commit(a int)",
			"set-txn-rw":       "SET TRANSACTION READ WRITE; CREATE TABLE esc_rw(a int)",
			"rollback-and-ddl": "ROLLBACK; CREATE TABLE esc_rollback(a int)",
			"end-begin-rw":     "END; BEGIN READ WRITE; CREATE TABLE esc_end(a int); COMMIT",
			"set-session-rw":   "SET SESSION CHARACTERISTICS AS TRANSACTION READ WRITE; SET default_transaction_read_only = off; CREATE TABLE esc_sess(a int)",
		}
		for name, payload := range attempts {
			_, err := db.Exec("CALL postgres_execute('pg', '" + payload + "')")
			made := pg.psql(t, "SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'esc_%'")
			t.Logf("%-18s err=%v | esc_* tables now in PG: %s", name, err != nil, made)
		}
		made := pg.psql(t, "SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'esc_%'")
		t.Logf("ESCAPE_TABLES_CREATED=%s", made)
		assert.NotEqual(t, "0", made, "FINDING: multi-statement postgres_execute escapes READ_ONLY (>=1 of 5 created a table)")
		pg.psql(t, "DROP TABLE IF EXISTS esc_commit, esc_rw, esc_rollback, esc_end, esc_sess")
	})

	t.Run("postgres_query under READ_ONLY", func(t *testing.T) {
		r, err := rows(db, "SELECT * FROM postgres_query('pg', 'SELECT name FROM people WHERE id=1')")
		require.NoError(t, err)
		t.Logf("postgres_query select rows=%v", r)
		_, err = rows(db, "SELECT * FROM postgres_query('pg', 'WITH x AS (INSERT INTO people VALUES (98, ''m2'') RETURNING id) SELECT * FROM x')")
		t.Logf("postgres_query data-modifying CTE err=%v", err)
		assert.Equal(t, "0", pg.psql(t, "SELECT count(*) FROM people WHERE id=98"))
		// side-effecting function call through a plain SELECT
		_, err = rows(db, "SELECT * FROM postgres_query('pg', 'SELECT pg_sleep(0.01)')")
		t.Logf("postgres_query pg_sleep err=%v", err)
		r, err = rows(db, "SELECT * FROM postgres_query('pg', 'SELECT current_user, inet_server_addr()::text, version()')")
		t.Logf("postgres_query can read server metadata: err=%v rows=%v", err, r)
	})

	t.Run("postgres_scan with inline DSN is a credential passthrough", func(t *testing.T) {
		r, err := rows(db, "SELECT * FROM postgres_scan('host=127.0.0.1 port="+pg.port+" dbname=postgres user=postgres password="+pgPassword+"', 'public', 'people')")
		require.NoError(t, err)
		t.Logf("postgres_scan(inline dsn) rows=%v", r)
	})
}

// Hardened hub (allow_persistent_secrets=false) rejects secret-less / unknown-secret attach.
func TestSpikePostgresHardenedRejectsInlineAttach(t *testing.T) {
	pg := startPG(t)
	db := openWithPostgres(t, "")
	_, err := db.Exec("ATTACH 'host=127.0.0.1 port=" + pg.port + " dbname=postgres user=postgres password=" + pgPassword + "' AS inl (TYPE postgres, READ_ONLY)")
	t.Logf("inline attach on hardened hub: %v", err)
	require.Error(t, err)
	_, err = db.Exec("ATTACH '' AS nos (TYPE postgres, SECRET nosuch, READ_ONLY)")
	t.Logf("unknown secret on hardened hub: %v", err)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local_file", "misleading error text: the session must translate it")
}

// Does a wrong password put credentials into error text? Not on DuckDB 1.5.6's postgres extension
// (it was echoed as password='...' on 1.4.1); the redactor is kept as defence in depth.
func TestSpikePostgresWrongPasswordErrorText(t *testing.T) {
	pg := startPG(t)
	db := openWithPostgres(t, "")
	const wrong = "WRONGpw_needle_5521"
	require.NoError(t, pg.createSecret(db, "pgbad", wrong))
	_, err := db.Exec("ATTACH '' AS bad (TYPE postgres, SECRET pgbad, READ_ONLY)")
	require.Error(t, err)
	t.Logf("secret attach wrong password: %v", err)
	assert.NotContains(t, err.Error(), wrong, "FINDING (postgres ext on DuckDB 1.5.6): the password is no longer echoed; the redactor stays as defence in depth")

	_, err = db.Exec("CREATE OR REPLACE TEMPORARY SECRET pgdown (TYPE postgres, HOST '127.0.0.1', PORT 1, DATABASE 'postgres', USER 'u', PASSWORD '" + wrong + "')")
	require.NoError(t, err)
	_, err = db.Exec("ATTACH '' AS down (TYPE postgres, SECRET pgdown, READ_ONLY)")
	require.Error(t, err)
	t.Logf("unreachable: %v", err)
	assert.NotContains(t, err.Error(), wrong, "not echoed when the server is unreachable either")

	// Rotated password: attach error again carries the (old) secret.
	require.NoError(t, pg.createSecret(db, "pgok", pgPassword))
	pg.psql(t, "ALTER USER postgres PASSWORD 'rotated_pw_9'")
	_, err = db.Exec("ATTACH '' AS ok2 (TYPE postgres, SECRET pgok, READ_ONLY)")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), pgPassword)
}

// Cost of catalog introspection and SUMMARIZE/DESCRIBE against remote PG with
// ~500 tables; which are remote round trips (server statement log evidence).
func TestSpikePostgresIntrospectionCost(t *testing.T) {
	pg := startPG(t)
	pg.psql(t, `DO $$ BEGIN FOR i IN 1..500 LOOP
	  EXECUTE format('CREATE TABLE t%s (id int primary key, name text, amt numeric, created timestamp)', i);
	END LOOP; END $$;
	CREATE TABLE big AS SELECT g AS id, md5(g::text) AS name, g*1.5 AS amt, now() AS created FROM generate_series(1,200000) g`)
	db := openWithPostgres(t, "")
	require.NoError(t, pg.createSecret(db, "pgs", pgPassword))

	stmts := func() int {
		return strings.Count(pg.logs(t), "LOG:  statement:") + strings.Count(pg.logs(t), "LOG:  execute")
	}
	timeq := func(label, q string) {
		b := stmts()
		t0 := time.Now()
		r, err := rows(db, q)
		require.NoError(t, err, label)
		t.Logf("%-52s %8v rows=%-5d pg_statements=%d", label, time.Since(t0).Round(time.Millisecond), len(r), stmts()-b)
	}
	b := stmts()
	t0 := time.Now()
	_, err := db.Exec("ATTACH '' AS pg (TYPE postgres, SECRET pgs, READ_ONLY)")
	require.NoError(t, err)
	t.Logf("ATTACH: %v pg_statements=%d", time.Since(t0), stmts()-b)

	for _, pass := range []string{"cold", "warm"} {
		timeq(pass+" duckdb_tables() filtered", "SELECT table_name FROM duckdb_tables() WHERE database_name='pg'")
		timeq(pass+" information_schema.tables filtered", "SELECT table_name FROM information_schema.tables WHERE table_catalog='pg'")
		timeq(pass+" information_schema.columns filtered", "SELECT table_name, column_name FROM information_schema.columns WHERE table_catalog='pg'")
		timeq(pass+" duckdb_columns() filtered", "SELECT table_name, column_name FROM duckdb_columns() WHERE database_name='pg'")
	}
	timeq("SHOW ALL TABLES", "SHOW ALL TABLES")
	timeq("DESCRIBE pg.public.big", "DESCRIBE pg.public.big")
	timeq("EXPLAIN SELECT * FROM pg.public.big", "EXPLAIN SELECT * FROM pg.public.big")
	timeq("SELECT * FROM pg.public.big LIMIT 10", "SELECT * FROM pg.public.big LIMIT 10")
	timeq("SELECT count(*) FROM pg.public.big", "SELECT count(*) FROM pg.public.big")
	timeq("SUMMARIZE pg.public.big (200k rows)", "SUMMARIZE pg.public.big")

	// Which statements did SUMMARIZE send? (full-table COPY = O(table) transfer)
	lg := pg.logs(t)
	i := strings.LastIndex(lg, "COPY")
	require.True(t, i >= 0)
	end := i + 200
	if end > len(lg) {
		end = len(lg)
	}
	t.Logf("last COPY sent to PG: %s", strings.ReplaceAll(lg[i:end], "\n", " "))
}

// Does an attached Postgres catalog see tables created after ATTACH?
// (Decides whether the schema cache needs an explicit invalidate.)
func TestSpikePostgresCatalogStaleness(t *testing.T) {
	pg := startPG(t)
	pg.psql(t, "CREATE TABLE before_attach(a int)")
	db := openWithPostgres(t, "")
	require.NoError(t, pg.createSecret(db, "pgs", pgPassword))
	_, err := db.Exec("ATTACH '' AS pg (TYPE postgres, SECRET pgs, READ_ONLY)")
	require.NoError(t, err)
	n1, _ := scalar(t.Context(), db, "SELECT count(*) FROM duckdb_tables() WHERE database_name='pg' AND table_name IN ('before_attach','after_attach')")
	pg.psql(t, "CREATE TABLE after_attach(a int)")
	n2, _ := scalar(t.Context(), db, "SELECT count(*) FROM duckdb_tables() WHERE database_name='pg' AND table_name IN ('before_attach','after_attach')")
	_, qerr := scalar(t.Context(), db, "SELECT count(*) FROM pg.public.after_attach")
	t.Logf("tables visible before=%s after external CREATE=%s; direct query of new table err=%v", n1, n2, qerr)
	_, cerr := db.Exec("CALL pg_clear_cache()")
	n3, _ := scalar(t.Context(), db, "SELECT count(*) FROM duckdb_tables() WHERE database_name='pg' AND table_name IN ('before_attach','after_attach')")
	t.Logf("after CALL pg_clear_cache() (err=%v): %s", cerr, n3)
	assert.Equal(t, "2", n3)
}
