package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
	sassert "github.com/SandwichLabs/vibe/internal/semantics/assert"
)

// grainSource serves one metric and its saved query to the planner.
type grainSource struct {
	m *semantics.Metric
	q *queries.Query
}

func (g grainSource) Metric(_ context.Context, name string) (*semantics.Metric, error) {
	if g.m != nil && g.m.Name == name {
		return g.m, nil
	}
	return nil, semantics.ErrNotFound
}

func (g grainSource) Query(_ context.Context, id string) (*queries.Query, error) {
	if g.q != nil && g.q.ID == id {
		return g.q, nil
	}
	return nil, queries.ErrNotFound
}

// runChecks runs planned checks through the real read-only policy path on a
// real DuckDB, the way `vibe semantics check` does.
func (fx *policyFixture) runChecks(checks []sassert.Check) []sassert.Result {
	fx.t.Helper()
	run := func(ctx context.Context, conn, sql string, params map[string]any) (*engine.ResultSet, error) {
		rs, _, err := fx.h.RunReadOnlySQLIn(ctx, conn, sql, params, 100)
		return rs, err
	}
	return sassert.Run(context.Background(), checks, sassert.Runner{Run: run})
}

func TestAssertionKinds_MetricGrainAndRangeOnRealDuckDB(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	// Two sources stamp the same night; summing them gives 106.1.
	fx.exec(`CREATE TABLE sleep (log_date DATE, source_id VARCHAR, score DOUBLE);
		INSERT INTO sleep VALUES ('2026-09-29','a',70), ('2026-09-30','a',55), ('2026-09-30','b',51.1)`)
	m := &semantics.Metric{Version: 1, Name: "sleep_score", Query: "sleep_q", Measure: "sum(score)", Dimensions: []string{"log_date"},
		Assertions: []semantics.Assertion{{Kind: semantics.AssertUnique}, {Kind: semantics.AssertRange, Max: numPtr(100)}, {Kind: semantics.AssertRange, Min: numPtr(0), Max: numPtr(200)}}}
	q := &queries.Query{ID: "sleep_q", SQL: "SELECT log_date, score FROM sleep;"}
	src := grainSource{m: m, q: q}
	plan := func() []sassert.Check {
		checks, err := sassert.PlanMetric(context.Background(), m, src, time.Now())
		require.NoError(t, err)
		return checks
	}

	res := fx.runChecks(plan())
	require.Len(t, res, 3)
	assert.Equal(t, sassert.StatusFail, res[0].Status, res[0].Error)
	assert.EqualValues(t, 1, res[0].Violations, "one night has two rows")
	assert.Equal(t, sassert.StatusFail, res[1].Status, res[1].Error)
	assert.EqualValues(t, 1, res[1].Violations, "106.1 is over 100")
	assert.Equal(t, sassert.StatusPass, res[2].Status, res[2].Error)

	// The saved query now keeps one row per night.
	q.SQL = "SELECT log_date, max(score) AS score FROM sleep GROUP BY log_date"
	res = fx.runChecks(plan())
	for _, r := range res {
		assert.Equal(t, sassert.StatusPass, r.Status, "%s: %s", r.Check.Kind, r.Error)
	}
}

func TestAssertionKinds_EntityKindsOnRealDuckDB(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	fx.exec(`CREATE TABLE logs_d (log_date DATE, n INTEGER);
		CREATE TABLE logs_ts (seen_at TIMESTAMP, n INTEGER);
		CREATE TABLE logs_tz (seen_at TIMESTAMPTZ, n INTEGER);
		CREATE TABLE empty_t (seen_at DATE, n INTEGER);
		INSERT INTO logs_d VALUES (DATE '2026-09-07', 1), (DATE '2026-09-30', 2);
		INSERT INTO logs_ts VALUES (TIMESTAMP '2026-09-30 00:00:00', 2);
		INSERT INTO logs_tz VALUES (TIMESTAMPTZ '2026-09-30 00:00:00+00', 2)`)
	now := time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC) // the data ends 30 days before
	ent := func(table, col string, a ...semantics.Assertion) *semantics.Entity {
		return &semantics.Entity{Version: 1, Name: "e", Grain: "one row per day", Table: table, IDColumn: col, Assertions: a}
	}
	fresh := func(age string) semantics.Assertion {
		return semantics.Assertion{Kind: semantics.AssertFreshness, Column: "x", MaxAge: age}
	}
	run := func(e *semantics.Entity) []sassert.Result {
		checks, err := sassert.PlanEntity(e, now)
		require.NoError(t, err)
		return fx.runChecks(checks)
	}
	for _, tc := range []struct{ table, col string }{{"logs_d", "log_date"}, {"logs_ts", "seen_at"}, {"logs_tz", "seen_at"}} {
		a7, a60 := fresh("7d"), fresh("60d")
		a7.Column, a60.Column = tc.col, tc.col
		res := run(ent(tc.table, tc.col, a7, a60))
		require.Len(t, res, 2, tc.table)
		assert.Equal(t, sassert.StatusFail, res[0].Status, "%s: %s", tc.table, res[0].Error)
		assert.EqualValues(t, 1, res[0].Violations, tc.table)
		// A TIMESTAMPTZ prints in the session time zone, so only the month is stable.
		assert.Contains(t, res[0].Detail, "latest 2026-09-", tc.table)
		assert.Equal(t, sassert.StatusPass, res[1].Status, "%s: %s", tc.table, res[1].Error)
	}
	// Hours: the latest value is exactly 30 days old.
	res := run(ent("logs_ts", "seen_at", semantics.Assertion{Kind: semantics.AssertFreshness, Column: "seen_at", MaxAge: "721h"}, semantics.Assertion{Kind: semantics.AssertFreshness, Column: "seen_at", MaxAge: "719h"}))
	assert.Equal(t, sassert.StatusPass, res[0].Status, res[0].Error)
	assert.Equal(t, sassert.StatusFail, res[1].Status, res[1].Error)

	// An empty table is stale and has too few rows.
	res = run(ent("empty_t", "seen_at",
		semantics.Assertion{Kind: semantics.AssertFreshness, Column: "seen_at", MaxAge: "7d"},
		semantics.Assertion{Kind: semantics.AssertRowCount, MinRows: countPtr(1)}))
	assert.Equal(t, sassert.StatusFail, res[0].Status, res[0].Error)
	assert.Equal(t, "no values", res[0].Detail)
	assert.Equal(t, sassert.StatusFail, res[1].Status, res[1].Error)
	assert.Equal(t, "0 rows", res[1].Detail)

	// row_count and an entity range.
	res = run(ent("logs_d", "log_date",
		semantics.Assertion{Kind: semantics.AssertRowCount, MinRows: countPtr(2)},
		semantics.Assertion{Kind: semantics.AssertRowCount, MinRows: countPtr(3)},
		semantics.Assertion{Kind: semantics.AssertRange, Column: "n", Min: numPtr(1.5)},
		semantics.Assertion{Kind: semantics.AssertRange, Column: "n", Max: numPtr(2)}))
	assert.Equal(t, sassert.StatusPass, res[0].Status, res[0].Error)
	assert.Equal(t, sassert.StatusFail, res[1].Status, res[1].Error)
	assert.Equal(t, "2 rows", res[1].Detail)
	assert.Equal(t, sassert.StatusFail, res[2].Status, res[2].Error)
	assert.EqualValues(t, 1, res[2].Violations)
	assert.Equal(t, sassert.StatusPass, res[3].Status, res[3].Error)
}

func numPtr(f float64) *semantics.Number { n := semantics.Number(f); return &n }
func countPtr(c int64) *semantics.Count  { n := semantics.Count(c); return &n }

func TestProposeDefinition_NewAssertionKinds(t *testing.T) {
	prop := func(assertions []any) map[string]any {
		p := metricProposal("net_revenue")
		def, _ := p["definition"].(map[string]any)
		def["assertions"] = assertions
		return p
	}
	fx := semanticsFixture(t, ModeReadWrite)
	resp := fx.call("propose_definition", prop([]any{
		map[string]any{"kind": "unique"},
		map[string]any{"kind": "range", "min": 0, "max": 100.5},
	}))
	require.Nil(t, resp.Error, errMsg(resp))
	require.Len(t, semProposals(t, fx), 1)

	for name, a := range map[string]map[string]any{
		"quoted bound":        {"kind": "range", "max": "100"},
		"no bound":            {"kind": "range"},
		"min above max":       {"kind": "range", "min": 5, "max": 1},
		"exponent bound":      {"kind": "range", "max": 1e21},
		"max_age on a metric": {"kind": "unique", "max_age": "7d"},
		"freshness on metric": {"kind": "freshness"},
	} {
		fx := semanticsFixture(t, ModeReadWrite)
		resp := fx.call("propose_definition", prop([]any{a}))
		require.NotNil(t, resp.Error, name)
		assert.Equal(t, ErrInvalidParams, resp.Error.Code, name)
		assert.Empty(t, semProposals(t, fx), name)
	}

	// An entity takes the new kinds with their own fields.
	fx = semanticsFixture(t, ModeReadWrite)
	resp = fx.call("propose_definition", map[string]any{
		"kind": "entity", "name": "log", "rationale": "freshness", "evidence": []any{"x"},
		"definition": map[string]any{"grain": "one row per day", "table": "logs", "id_column": "log_date", "assertions": []any{
			map[string]any{"kind": "freshness", "column": "log_date", "max_age": "7d"},
			map[string]any{"kind": "row_count", "min_rows": 1},
		}},
	})
	require.Nil(t, resp.Error, errMsg(resp))
	resp = fx.call("propose_definition", map[string]any{
		"kind": "entity", "name": "log", "rationale": "freshness", "evidence": []any{"x"},
		"definition": map[string]any{"grain": "one row per day", "table": "logs", "id_column": "log_date", "assertions": []any{
			map[string]any{"kind": "freshness", "column": "log_date", "max_age": 7},
		}},
	})
	require.NotNil(t, resp.Error)
}

func TestAssertionKinds_TypeMismatchFailsAtCompile(t *testing.T) {
	fx := newPolicyFixture(t, ModeReadOnly)
	fx.exec(`CREATE TABLE typed (id INTEGER, label VARCHAR, n INTEGER); INSERT INTO typed VALUES (1, 'a', 2)`)
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	e := &semantics.Entity{Version: 1, Name: "e", Grain: "one row", Table: "typed", IDColumn: "id", Assertions: []semantics.Assertion{
		{Kind: semantics.AssertRange, Column: "label", Max: numPtr(5)},
		{Kind: semantics.AssertFreshness, Column: "n", MaxAge: "7d"},
	}}
	compile, err := sassert.PlanEntityCompile(e)
	require.NoError(t, err)
	rest, err := sassert.PlanEntity(e, now)
	require.NoError(t, err)
	vet := func(ctx context.Context, conn, sql string, params map[string]any) error {
		return fx.h.VetReadOnlySQLIn(ctx, conn, sql, params)
	}
	res := sassert.Run(context.Background(), append([]sassert.Check{compile}, rest...), sassert.Runner{Vet: vet, Run: func(ctx context.Context, conn, sql string, params map[string]any) (*engine.ResultSet, error) {
		rs, _, err := fx.h.RunReadOnlySQLIn(ctx, conn, sql, params, 100)
		return rs, err
	}})
	require.Len(t, res, 3)
	assert.Equal(t, sassert.StatusFail, res[0].Status, res[0].Error)
	assert.Contains(t, res[0].Error, "VARCHAR")
	assert.Contains(t, res[1].Error, "not run")
	assert.Contains(t, res[2].Error, "not run")

	// A numeric column with a range and a date column with freshness still compile.
	e.Assertions = []semantics.Assertion{{Kind: semantics.AssertRange, Column: "n", Min: numPtr(0)}}
	compile, err = sassert.PlanEntityCompile(e)
	require.NoError(t, err)
	res = sassert.Run(context.Background(), []sassert.Check{compile}, sassert.Runner{Vet: vet})
	assert.Equal(t, sassert.StatusPass, res[0].Status, res[0].Error)
}
