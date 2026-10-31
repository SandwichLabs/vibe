package main

import (
	"context"
	"testing"

	"github.com/SandwichLabs/vibe/internal/semantics"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRegistry_BeginCancelEnd(t *testing.T) {
	var r runRegistry
	ctx, end := r.begin(context.Background(), "k")
	assert.Equal(t, 1, r.size())

	r.cancel("other")
	r.cancel("unknown")
	assert.NoError(t, ctx.Err(), "cancelling another key must not touch this run")

	// A second run under the same key replaces the first, which is cancelled.
	ctx2, end2 := r.begin(context.Background(), "k")
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.NoError(t, ctx2.Err())
	end() // the replaced run ending must not drop the new entry
	assert.Equal(t, 1, r.size())

	r.cancel("k")
	assert.ErrorIs(t, ctx2.Err(), context.Canceled)
	end2()
	assert.Equal(t, 0, r.size())
	r.cancel("k") // finished: no-op
	r.cancelAll()
}

func TestCancelRun_UnknownKeyIsNoOp(t *testing.T) {
	app := newOpenApp(t)
	assert.NotPanics(t, func() { app.CancelRun("query:nope"); app.CancelRun("") })
	assert.Equal(t, 0, app.runs.size())
}

const longQuery = `SELECT count(*) FROM range(100000000000) a, range(1000) b`

func TestRunQuery_CancelStopsLongQuery(t *testing.T) {
	app := newOpenApp(t)
	done := make(chan error, 1)
	go func() {
		_, err := app.RunQuery(RunQueryRequest{SQL: longQuery, TabID: "t1"})
		done <- err
	}()
	require.Eventually(t, func() bool { return app.runs.size() == 1 }, 5*time.Second, 5*time.Millisecond)
	time.Sleep(300 * time.Millisecond) // let DuckDB start executing
	start := time.Now()
	app.CancelRun(queryRunKey("t1"))
	select {
	case err := <-done:
		took := time.Since(start)
		t.Logf("cancel latency: %s", took)
		require.ErrorIs(t, err, errRunCancelled)
		assert.Less(t, took, 2*time.Second)
	case <-time.After(10 * time.Second):
		t.Fatal("the cancelled query did not return")
	}
	assert.Equal(t, 0, app.runs.size(), "the registry is empty after the run")

	// The connection is usable afterwards.
	res, err := app.RunQuery(RunQueryRequest{SQL: "SELECT 1", TabID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, 1, res.RowCount)
}

func TestSemanticsRuns_CancelReturnsCancelled(t *testing.T) {
	app := appWithDefinitions(t)
	// Make the metric's source slow: a view over a huge cross join.
	_, err := app.sess.DB().Exec(`DROP TABLE sales; CREATE VIEW sales AS SELECT 'east' AS region, 1 AS month, a.range::INTEGER AS amount FROM range(100000000000) a, range(1000) b`)
	require.NoError(t, err)

	// The join's entities: make orders slow too, for the profile run below.
	_, err = app.sess.DB().Exec(`DROP TABLE orders; CREATE VIEW orders AS SELECT a.range::INTEGER AS order_id, 1 AS customer_id FROM range(100000000000) a, range(1000) b`)
	require.NoError(t, err)
	before, err := app.ws.Semantics().Source(context.Background(), semantics.KindJoin, "orders_customers")
	require.NoError(t, err)

	runs := map[string]func() error{
		runKeyProfile:    func() error { _, err := app.ProfileJoin("orders_customers"); return err },
		runKeyMetric:     func() error { _, err := app.RunMetric("net_revenue", nil); return err },
		runKeyAssertions: func() error { _, err := app.RunAssertions("metric", "net_revenue"); return err },
	}
	for key, run := range runs {
		done := make(chan error, 1)
		go func() { done <- run() }()
		require.Eventually(t, func() bool { return app.runs.size() == 1 }, 5*time.Second, 5*time.Millisecond, key)
		time.Sleep(300 * time.Millisecond)
		start := time.Now()
		app.CancelRun(key)
		select {
		case err := <-done:
			t.Logf("%s cancel latency: %s", key, time.Since(start))
			require.ErrorIs(t, err, errRunCancelled, key)
			assert.Less(t, time.Since(start), 2*time.Second, key)
		case <-time.After(10 * time.Second):
			t.Fatalf("the cancelled %s run did not return", key)
		}
		assert.Equal(t, 0, app.runs.size(), key)
	}
	after, err := app.ws.Semantics().Source(context.Background(), semantics.KindJoin, "orders_customers")
	require.NoError(t, err)
	assert.Equal(t, before.Text, after.Text, "a stopped profile writes nothing")
	assert.Equal(t, before.Rev, after.Rev)
}
