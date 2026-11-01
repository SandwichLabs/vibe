package agents

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/session"
	"github.com/SandwichLabs/slaw/internal/storageglob"
)

// #138 follow-up: wildcards on s3, gcs and r2 are expanded before the policy
// looks at a statement. These tests use canned listings (the lister is the
// hub's, tested in internal/storageglob); the end-to-end run with real objects
// is policy_integration_test.go (MinIO).

func globFixture(t *testing.T) *policyFixture {
	t.Helper()
	lake := session.ConnectionInfo{Name: "lake", Type: "s3", Kind: session.StatusOK, Remote: true, Catalogless: true, Scope: "s3://lake/raw/"}
	fx := newPolicyFixture(t, ModeFull, func(c *fixtureCfg) { c.extra = []session.ConnectionInfo{lake} })
	fx.sess.keys = map[string][]string{
		"s3://lake/raw/2024/*.parquet": {"s3://lake/raw/2024/a.parquet", "s3://lake/raw/2024/b.parquet"},
		"s3://lake/raw/**":             {"s3://lake/raw/a.csv", "s3://lake/raw/it's.csv", "s3://lake/raw/x.csv?s3_endpoint=evil.example"},
	}
	return fx
}

func TestAgentExpandsAWildcardInsideAReadyScope(t *testing.T) {
	fx := globFixture(t)
	ctx := context.Background()
	text, ectx, err := fx.h.expand(ctx, `SELECT count(*) FROM read_parquet('s3://lake/raw/2024/*.parquet', hive_partitioning = true)`)
	require.NoError(t, err)
	assert.Equal(t, `SELECT count(*) FROM read_parquet(['s3://lake/raw/2024/a.parquet', 's3://lake/raw/2024/b.parquet'], hive_partitioning = true)`, text)
	rw, ok := storageglob.FromContext(ectx)
	require.True(t, ok, "the engine is told the statement is final")
	assert.True(t, rw.Changed())

	// A name with a "?" is refused for the agent: it reads only plain URLs.
	_, _, err = fx.h.expand(ctx, `SELECT * FROM read_csv('s3://lake/raw/**')`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "? or # in the name")
	assert.NotContains(t, err.Error(), "evil.example")

	// Text with no pattern is untouched and still marked final.
	plain := `SELECT * FROM read_parquet('s3://lake/raw/a.parquet')`
	text, ectx, err = fx.h.expand(ctx, plain)
	require.NoError(t, err)
	assert.Equal(t, plain, text)
	_, ok = storageglob.FromContext(ectx)
	assert.True(t, ok)
}

func TestAgentNeverListsAPatternOutsideItsScopes(t *testing.T) {
	fx := globFixture(t)
	for _, q := range []string{
		`SELECT * FROM read_parquet('s3://lake/other/*.parquet')`,
		`SELECT * FROM read_parquet('s3://lake/*.parquet')`,
		`SELECT * FROM read_parquet('s3://elsewhere/raw/*.parquet')`,
		`SELECT * FROM 's3://lake/raw*'`,
		`SELECT * FROM read_parquet('s3://lake/raw/../x/*.parquet')`,
		`SELECT * FROM read_parquet('s3://lake/raw/a?s3_endpoint=evil.example*')`,
		`SELECT * FROM read_parquet('gcs://lake/raw/*.parquet')`,
	} {
		_, _, err := fx.h.expand(context.Background(), q)
		require.Error(t, err, q)
	}
	assert.Empty(t, fx.sess.listed, "a pattern the policy refuses is never listed")

	// Through the tool: a permission error, not a silent empty read.
	resp := fx.run(`SELECT * FROM read_parquet('s3://lake/other/*.parquet')`)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "outside every storage connection's scope")

	// No ready connection at all: refused as before.
	bare := newPolicyFixture(t, ModeFull)
	_, _, err := bare.h.expand(context.Background(), `SELECT * FROM read_parquet('s3://lake/raw/*.parquet')`)
	require.Error(t, err)
}

func TestAgentGlobErrorsAreNotPermissionErrors(t *testing.T) {
	fx := globFixture(t)
	resp := fx.run(`SELECT * FROM read_parquet('s3://lake/raw/nothing/*.parquet')`)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "no files found")
}

func TestAgentLeavesNonPathLiteralsAlone(t *testing.T) {
	fx := globFixture(t)
	for _, q := range []string{`SELECT 's3://lake/raw/*' AS p`, `SELECT * FROM t WHERE name = 's3://lake/raw/*.csv'`} {
		text, _, err := fx.h.expand(context.Background(), q)
		require.NoError(t, err)
		assert.Equal(t, q, text)
		assert.Equal(t, sqlClassRead, fx.classify(q).Class, q)
	}
	assert.Empty(t, fx.sess.listed)
}

func TestPolicyAcceptsAListOfVettedPaths(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	for _, q := range []string{
		"SELECT count(*) FROM read_csv(['%DATA%/x.csv', '%DATA%/y.csv'])",
		"SELECT count(*) FROM read_csv(['%DATA%/x.csv', '%DATA%/y.csv'], header = true)",
		"SELECT count(*) FROM glob(['%DATA%/x.csv'])",
		"WITH a AS (SELECT * FROM read_csv(['%DATA%/x.csv'])) SELECT * FROM a",
	} {
		v := fx.classify(fx.expand(q))
		assert.Equal(t, sqlClassRead, v.Class, "%s: %s", q, v.Reason)
	}
	// The token path (INSERT, CTAS): the same rule.
	fx.exec(`CREATE TABLE sink (a BIGINT, b BIGINT)`)
	for _, q := range []string{
		"INSERT INTO sink SELECT * FROM read_csv(['%DATA%/x.csv', '%DATA%/y.csv'])",
		"CREATE TABLE made AS SELECT * FROM read_csv(['%DATA%/x.csv', '%DATA%/y.csv'])",
	} {
		v := fx.classify(fx.expand(q))
		assert.NotEqual(t, sqlClassOther, v.Class, "%s: %s", q, v.Reason)
	}
	for _, q := range []string{
		"INSERT INTO sink SELECT * FROM read_csv(['%DATA%/x.csv', '%OUT%/secret.csv'])",
		"INSERT INTO sink SELECT * FROM read_csv(['%DATA%/x.csv', 'y.csv'])",
		"INSERT INTO sink SELECT * FROM read_csv(['%DATA%/x.csv' || '.csv'])",
		"INSERT INTO sink SELECT * FROM read_csv(['%DATA%/x.csv',])",
		"INSERT INTO sink SELECT * FROM read_csv([])",
		"INSERT INTO sink SELECT * FROM read_csv(['%DATA%/x.csv'], '%DATA%/y.csv')",
	} {
		assert.Equal(t, sqlClassOther, fx.classify(fx.expand(q)).Class, q)
	}
}

func TestPolicyRefusesAWildcardThatWasNotExpanded(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull, func(c *fixtureCfg) {
		c.extra = []session.ConnectionInfo{{Name: "lake", Type: "s3", Kind: session.StatusOK, Remote: true, Catalogless: true, Scope: "s3://lake/raw/"}}
	})
	// DELETE ... USING is not a place a pattern is expanded: it would be read as
	// a literal key, so it is refused, not run.
	v := fx.classify(`DELETE FROM t USING read_csv('s3://lake/raw/*.csv') AS u WHERE t.id = u.a`)
	assert.Equal(t, sqlClassOther, v.Class)
	assert.Contains(t, v.Reason, "was not expanded")
}

// A reader path written as ['a', 'b'] is a call to list_value, which a user
// macro can replace: the policy vets the literals, DuckDB would read whatever
// the macro returns. The list form is refused once such a macro exists, and
// the agent cannot create one.
func TestPolicyRefusesTheListFormWhenListValueIsAMacro(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	q := fx.expand("SELECT * FROM read_text(['%DATA%/x.csv'])")
	assert.Equal(t, sqlClassRead, fx.classify(q).Class, "before the macro exists")

	// Made by the person (or a pulled query), bypassing the agent's policy.
	fx.exec(`CREATE MACRO list_value(a) AS string_split(chr(47) || 'etc' || chr(47) || 'hostname', ',')`)
	for _, ddl := range []string{
		"SELECT * FROM read_text(['%DATA%/x.csv'])",
		"INSERT INTO t2 SELECT length(content) FROM read_text(['%DATA%/x.csv'])",
		"CREATE TABLE zz AS SELECT * FROM read_text(['%DATA%/x.csv'])",
	} {
		assert.Equal(t, sqlClassOther, fx.classify(fx.expand(ddl)).Class, ddl)
	}
	resp := fx.run(q)
	require.NotNil(t, resp.Error)
	_, _, err := fx.h.RunReadOnlySQL(context.Background(), q, nil, 10)
	require.Error(t, err)
	// A single literal never goes through list_value.
	assert.Equal(t, sqlClassRead, fx.classify(fx.expand("SELECT * FROM read_text('%DATA%/x.csv')")).Class)
}

func TestPolicyRefusesAMacroNamedLikeABuiltin(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	for _, q := range []string{
		"CREATE MACRO list_value(a) AS a",
		"CREATE OR REPLACE MACRO list_pack(a) AS a",
		"CREATE MACRO read_text(p) AS TABLE SELECT 1 AS x",
		"CREATE TEMP MACRO read_parquet(p) AS TABLE SELECT 1 AS x",
		`CREATE MACRO "glob"(p) AS TABLE SELECT 1 AS x`,
		"CREATE FUNCTION list_value(a) AS a",
		"CREATE MACRO main.upper(a) AS a",
	} {
		v := fx.classify(q)
		assert.Equal(t, sqlClassOther, v.Class, q)
		assert.Contains(t, v.Reason, "cannot be named", q)
	}
	assert.NotEqual(t, sqlClassOther, fx.classify("CREATE MACRO my_double(a) AS a * 2").Class)
}

// A person-made table macro that replaces a reader's name would run instead of
// the reader whose literal path was vetted.
func TestPolicyRefusesAReaderShadowedByAMacro(t *testing.T) {
	fx := newPolicyFixture(t, ModeFull)
	fx.exec(`CREATE MACRO read_text(p) AS TABLE SELECT 'shadow' AS content`)
	q := fx.expand("SELECT * FROM read_text('%DATA%/x.csv')")
	v := fx.classify(q)
	assert.Equal(t, sqlClassOther, v.Class)
	assert.Contains(t, v.Reason, "shadowed")
	assert.Equal(t, sqlClassOther, fx.classify(fx.expand("INSERT INTO t2 SELECT length(content) FROM read_text('%DATA%/x.csv')")).Class)
}

// A TEMP macro lives on one pooled connection, where the policy's function
// lookup may not be looking. It must not outlive the run that made it, or a
// vetted read_text(['<data>/x.csv']) could land on a connection where
// list_value reads another file.
func TestTempMacroDoesNotSurviveItsRunOnAPooledConnection(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	ctx := context.Background()
	// Poison as many pooled connections as the pool will hand out, through the
	// session (the person's editor path), one at a time and concurrently.
	for i := 0; i < 8; i++ {
		_, err := fx.sess.Session.Exec(ctx, `CREATE OR REPLACE TEMP MACRO list_value(a) AS string_split(chr(47) || 'etc' || chr(47) || 'hostname', ',')`)
		if err != nil {
			t.Fatal(err)
		}
	}
	q := fx.expand("SELECT content FROM read_text(['%DATA%/x.csv'])")
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	var mu sync.Mutex
	leaks := 0
	for i := 0; i < 120; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rs, _, err := fx.h.RunReadOnlySQL(ctx, q, nil, 10)
			if err != nil || rs == nil {
				return
			}
			for _, row := range rs.Rows {
				if s, _ := row[0].(string); !strings.HasPrefix(s, "a,b") {
					mu.Lock()
					leaks++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	assert.Zero(t, leaks, "nothing but data/x.csv is ever returned")
	// TEMP tables are unaffected.
	_, err := fx.sess.Session.Exec(ctx, `CREATE TEMP TABLE keepme AS SELECT 1 AS a`)
	require.NoError(t, err)
}
