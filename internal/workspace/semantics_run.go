package workspace

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/connname"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/semantics/assert"
)

// Running a definition: assertions, join profiling and a metric's "Try it".
// These are the person's actions (`vibe semantics check|profile`, the
// Semantics view); no agent tool or MCP method reaches them. Every statement
// they plan runs through the caller's ReadOnlyRunner, which is the agent
// policy's read-only path in the tightest tier (hub.NewAgentHandler with
// read-only, or Studio's read-only handler): the definitions may come from a
// cloned repository, so a hostile table name or metric body is refused there,
// not trusted here.

// ReadOnlyRunner is agents.Handler.RunReadOnlySQLIn (or Studio's wrapper of
// it). The workspace package cannot import agents, so each surface hands it in.
type ReadOnlyRunner interface {
	RunReadOnlySQLIn(ctx context.Context, connection, sql string, params map[string]any, maxRows int) (*engine.ResultSet, bool, error)
}

// Caps and limits of a run.
const (
	// CheckStatementTimeout bounds one assertion or profiling statement.
	CheckStatementTimeout = 2 * time.Minute
	// countRowCap is the rows a counting statement may return: one.
	countRowCap = 10
)

// ReadOnlyRunFunc adapts a ReadOnlyRunner to the QueryFunc assertions and
// profiling take, with CheckStatementTimeout per statement.
func ReadOnlyRunFunc(r ReadOnlyRunner) semantics.QueryFunc {
	return func(ctx context.Context, connection, sql string, params map[string]any) (*engine.ResultSet, error) {
		ctx, cancel := context.WithTimeout(ctx, CheckStatementTimeout)
		defer cancel()
		rs, _, err := r.RunReadOnlySQLIn(ctx, connection, sql, params, countRowCap)
		return rs, err
	}
}

// RunAssertions plans and runs the declared assertions of one entity or metric
// (kind "entity" or "metric" with a name), of every entity or metric (a kind
// with no name), or of everything (neither). A subject whose checks cannot be
// planned (an unreadable file, a missing saved query) is one error result and
// the others still run.
func RunAssertions(ctx context.Context, ws Workspace, kind, name string, run semantics.QueryFunc, now time.Time) ([]assert.Result, error) {
	plans, err := planAssertions(ctx, ws, kind, name, now)
	if err != nil {
		return nil, err
	}
	out := []assert.Result{}
	for _, p := range plans {
		if p.err != nil {
			out = append(out, assert.Failure(p.kind, p.name, p.err))
			continue
		}
		out = append(out, assert.Run(ctx, p.checks, assert.RunFunc(run))...)
	}
	return out, nil
}

type subjectPlan struct {
	kind, name string
	checks     []assert.Check
	err        error
}

func planAssertions(ctx context.Context, ws Workspace, kind, name string, now time.Time) ([]subjectPlan, error) {
	if name != "" && kind == "" {
		return nil, fmt.Errorf("give the kind of %q: entity or metric", name)
	}
	if kind != "" && kind != assert.SubjectEntity && kind != assert.SubjectMetric {
		return nil, fmt.Errorf("unknown kind %q (want entity or metric)", kind)
	}
	st := ws.Semantics()
	var out []subjectPlan
	if kind == "" || kind == assert.SubjectEntity {
		list, err := entitiesNamed(ctx, st, name)
		if err != nil {
			return nil, err
		}
		for i := range list {
			p := subjectPlan{kind: assert.SubjectEntity, name: list[i].Name}
			p.checks, p.err = assert.PlanEntity(&list[i])
			if p.err != nil || len(p.checks) > 0 || name != "" {
				out = append(out, p)
			}
		}
	}
	if kind == "" || kind == assert.SubjectMetric {
		list, err := metricsNamed(ctx, st, name)
		if err != nil {
			return nil, err
		}
		src := semantics.NewReader(st, ws.Queries())
		for i := range list {
			p := subjectPlan{kind: assert.SubjectMetric, name: list[i].Name}
			p.checks, p.err = assert.PlanMetric(ctx, &list[i], src, now)
			if p.err != nil || len(p.checks) > 0 || name != "" {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func entitiesNamed(ctx context.Context, st semantics.ReadStore, name string) ([]semantics.Entity, error) {
	list, err := st.Entities(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}
	if name == "" {
		return list, nil
	}
	for _, e := range list {
		if e.Name == name {
			return []semantics.Entity{e}, nil
		}
	}
	return nil, fmt.Errorf("there is no entity named %q (`vibe semantics list` shows them)", name)
}

func metricsNamed(ctx context.Context, st semantics.ReadStore, name string) ([]semantics.Metric, error) {
	list, err := st.Metrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list metrics: %w", err)
	}
	if name == "" {
		return list, nil
	}
	for _, m := range list {
		if m.Name == name {
			return []semantics.Metric{m}, nil
		}
	}
	return nil, fmt.Errorf("there is no metric named %q (`vibe semantics list` shows them)", name)
}

// ProfileJoin profiles one join and records the measure in its file (see
// semantics.ProfileJoin): a file that changed while the profile ran is a
// semantics.ErrConflict and nothing is written.
func ProfileJoin(ctx context.Context, ws Workspace, name string, run semantics.QueryFunc, now time.Time) (*semantics.Profile, error) {
	return semantics.ProfileJoin(ctx, ws.Semantics(), name, run, now)
}

// MetricQuery is the statement behind a metric's "Try it": the metric broken
// down by dims (a subset of its dimensions; none is the scalar), ordered by
// them, and the connection it runs on (its saved query's). The caller runs it
// through the read-only policy, which expands the call like any other.
func MetricQuery(ctx context.Context, ws Workspace, name string, dims []string) (sql, connection string, err error) {
	if !connname.Valid(name) {
		return "", "", fmt.Errorf("there is no metric named %q", name)
	}
	for _, d := range dims {
		if !connname.Valid(d) {
			return "", "", fmt.Errorf("%q is not a dimension name", d)
		}
	}
	m, err := ws.Semantics().Metric(ctx, name)
	if err != nil {
		return "", "", fmt.Errorf("failed to read metric %q: %w", name, err)
	}
	q, err := ws.Queries().Get(ctx, m.Query)
	if err != nil {
		return "", "", fmt.Errorf("metric %q: its saved query %q cannot be read: %w", name, m.Query, err)
	}
	sql = "SELECT * FROM " + semantics.MetricCall(name, dims)
	if len(dims) > 0 {
		order := make([]string, len(dims))
		for i := range dims {
			order[i] = strconv.Itoa(i + 1)
		}
		sql += " ORDER BY " + strings.Join(order, ", ")
	}
	return sql, q.Connection, nil
}
