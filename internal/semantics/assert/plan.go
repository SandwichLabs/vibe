// Package assert plans and runs the assertions a semantic definition declares:
// an entity's unique, not_null and accepted_values, a metric's non_negative and
// not_null. Planning is pure: it turns a validated definition into SQL texts
// (identifiers come only from validated names, values only as bound
// parameters) and runs nothing. Run executes the plan through an injected
// RunFunc, so this package never imports the agent: the caller hands it the
// agent policy's read-only runner (agents.Handler.RunReadOnlySQLIn in the
// tightest tier), because a definition may come from a cloned repository and a
// check must not be able to do more than read.
//
// Reference: docs/reference/semantics.md.
package assert

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/session"
)

// Status of one check.
const (
	StatusPass  = "pass"
	StatusFail  = "fail"
	StatusError = "error"
)

// KindCompile is the check kind of a compile check: not a declared assertion
// but a bind of the definition's SQL (an entity's table and columns, a
// metric's measure and dimensions against its saved query) that reads no data.
const KindCompile = "compile"

// Subject kinds a check belongs to.
const (
	SubjectEntity = "entity"
	SubjectMetric = "metric"
)

// Check is one planned assertion: a statement that counts violations, and what
// it is about. SQL holds only validated identifiers (and, for a metric, the
// expansion of its saved query); every value travels in Params.
type Check struct {
	// Subject is the entity or metric name and SubjectKind says which.
	SubjectKind string `json:"subject_kind"`
	Subject     string `json:"subject"`
	// Kind is the assertion kind (unique, not_null, accepted_values, non_negative).
	Kind   string `json:"kind"`
	Column string `json:"column,omitempty"`
	// Connection is where the statement runs ("" is the workspace default).
	Connection string         `json:"connection,omitempty"`
	SQL        string         `json:"-"`
	Params     map[string]any `json:"-"`
	// VetOnly marks a compile check: SQL is vetted (bound against the real
	// connection by the read-only policy's EXPLAIN) and never executed, so no
	// row is read. Such a check passes when the vet passes.
	VetOnly bool `json:"-"`
	// PlanError is set when the check could not be planned (a metric whose
	// saved query is gone, say): Run reports it as an error without running
	// anything, and the other checks still run.
	PlanError string `json:"plan_error,omitempty"`
}

// PlanEntity plans an entity's assertions, in the order they are declared. now
// is the clock a freshness window counts back from. It
// re-validates the entity first, so a definition that never went through the
// strict decoder (a test double, a bug) still cannot put an unchecked name
// into SQL.
func PlanEntity(e *semantics.Entity, now time.Time) ([]Check, error) {
	if e == nil {
		return nil, fmt.Errorf("no entity to plan")
	}
	if e.Error != "" {
		return nil, fmt.Errorf("entity %q cannot be read: %s", e.Name, e.Error)
	}
	if err := semantics.ValidateEntity(e); err != nil {
		return nil, err
	}
	out := make([]Check, 0, len(e.Assertions))
	for _, a := range e.Assertions {
		table, col := semantics.QuoteTable(e.Table), ""
		if a.Column != "" {
			col = session.QuoteIdent(a.Column)
		}
		var err error
		c := Check{SubjectKind: SubjectEntity, Subject: e.Name, Kind: a.Kind, Column: a.Column, Connection: e.Connection}
		switch a.Kind {
		case semantics.AssertUnique:
			c.SQL = fmt.Sprintf("SELECT count(%s) - count(DISTINCT %s) FROM %s", col, col, table)
		case semantics.AssertNotNull:
			c.SQL = fmt.Sprintf("SELECT count(*) FROM %s WHERE %s IS NULL", table, col)
		case semantics.AssertAcceptedValues:
			c.SQL, c.Params = acceptedValuesSQL(table, a)
		case semantics.AssertRange:
			c.SQL, c.Params = rangeSQL("SELECT count(*) FROM "+table, col, a)
		case semantics.AssertFreshness:
			c.SQL, c.Params, err = freshnessSQL(table, col, a, now)
			if err != nil {
				return nil, fmt.Errorf("entity %q: %w", e.Name, err)
			}
		case semantics.AssertRowCount:
			c.SQL, c.Params = rowCountSQL(table, a)
		default:
			return nil, fmt.Errorf("entity %q: assertion kind %q does not apply to an entity", e.Name, a.Kind)
		}
		out = append(out, c)
	}
	return out, nil
}

// acceptedValuesSQL counts rows whose column, read as text, is not one of the
// listed values. table and the column are already quoted. Values are bound as $v0, $v1, ... and compared as text, so a
// column of any type works and a value never becomes SQL. NULL is not a
// violation (not_null says that).
func acceptedValuesSQL(table string, a semantics.Assertion) (string, map[string]any) {
	params := make(map[string]any, len(a.Values))
	holes := make([]string, len(a.Values))
	for i, v := range a.Values {
		name := "v" + strconv.Itoa(i)
		params[name] = v
		holes[i] = "$" + name
	}
	return fmt.Sprintf("SELECT count(*) FROM %s WHERE CAST(%s AS VARCHAR) NOT IN (%s)", table, session.QuoteIdent(a.Column), strings.Join(holes, ", ")), params
}

// PlanMetric plans a metric's assertions over the metric at its full dimension
// set (every declared dimension), so a negative or missing value in any cell
// of the breakdown is found. The metric is expanded with semantics.Expand
// (the expander every surface shares) and the saved query's parameters take
// their declared defaults through queries.Bind; now is the clock those
// defaults read.
//
// A metric that cannot be expanded is an error for the whole metric; the
// caller reports it and goes on to the next subject.
func PlanMetric(ctx context.Context, m *semantics.Metric, src semantics.Reader, now time.Time) ([]Check, error) {
	if m == nil {
		return nil, fmt.Errorf("no metric to plan")
	}
	if m.Error != "" {
		return nil, fmt.Errorf("metric %q cannot be read: %s", m.Name, m.Error)
	}
	if err := semantics.ValidateMetric(m); err != nil {
		return nil, err
	}
	q, err := src.Query(ctx, m.Query)
	if err != nil {
		return nil, fmt.Errorf("metric %q: its saved query %q cannot be read: %w", m.Name, m.Query, err)
	}
	out := make([]Check, 0, len(m.Assertions))
	for _, a := range m.Assertions {
		c, err := planMetricAssertion(ctx, m, a, q.Connection, src, now)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// planMetricAssertion plans one assertion of a metric. All but unique count the
// cells of the metric at its full dimension set; unique counts the dimension
// sets the saved query has more than one row for (semantics.ExpandGrain).
func planMetricAssertion(ctx context.Context, m *semantics.Metric, a semantics.Assertion, connection string, src semantics.Reader, now time.Time) (Check, error) {
	opts := semantics.Options{Connection: connection, DefaultConnection: connection}
	var res *semantics.Result
	var extra map[string]any
	var err error
	switch a.Kind {
	case semantics.AssertNonNegative, semantics.AssertNotNull, semantics.AssertRange:
		pred := session.QuoteIdent(m.Name) + " < 0"
		switch a.Kind {
		case semantics.AssertNotNull:
			pred = session.QuoteIdent(m.Name) + " IS NULL"
		case semantics.AssertRange:
			pred, extra = rangePredicate(session.QuoteIdent(m.Name), a)
		}
		text := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", semantics.MetricCall(m.Name, m.Dimensions), pred)
		res, err = semantics.Expand(ctx, text, src, opts)
	case semantics.AssertUnique:
		res, err = semantics.ExpandGrain(ctx, m, src, opts)
	default:
		return Check{}, fmt.Errorf("metric %q: assertion kind %q does not apply to a metric", m.Name, a.Kind)
	}
	if err != nil {
		return Check{}, err
	}
	for k := range extra {
		for _, p := range res.Params {
			if p.Name == k {
				return Check{}, fmt.Errorf("metric %q: its saved query declares the parameter $%s, which a range assertion reserves", m.Name, k)
			}
		}
	}
	params, err := res.BindParams(extra, now)
	if err != nil {
		return Check{}, err
	}
	return Check{SubjectKind: SubjectMetric, Subject: m.Name, Kind: a.Kind, Connection: connection, SQL: res.SQL, Params: params}, nil
}

// Parameter names the planner binds itself; a saved query may not declare them
// for a metric's range assertion.
const (
	paramMin       = "vibe_min"
	paramMax       = "vibe_max"
	paramThreshold = "vibe_threshold"
	paramMinRows   = "vibe_min_rows"
)

// rangePredicate is the WHERE predicate a range assertion counts rows for (a
// NULL is not out of range; not_null says that) and the bounds as bound
// parameters. col is already quoted.
func rangePredicate(col string, a semantics.Assertion) (string, map[string]any) {
	var parts []string
	params := map[string]any{}
	if a.Min != nil {
		parts = append(parts, col+" < $"+paramMin)
		params[paramMin] = a.Min.Float()
	}
	if a.Max != nil {
		parts = append(parts, col+" > $"+paramMax)
		params[paramMax] = a.Max.Float()
	}
	return strings.Join(parts, " OR "), params
}

// rangeCastPredicate is rangePredicate for a compile check: each bound is a
// parameter cast to DOUBLE, so the binder rejects a text column the way the data
// check's bound number would. The vet path binds plain values, so the bounds
// travel as float64.
func rangeCastPredicate(col string, a semantics.Assertion) (string, map[string]any) {
	pred, params := rangePredicate(col, a)
	pred = strings.ReplaceAll(pred, "$"+paramMin, "CAST($"+paramMin+" AS DOUBLE)")
	pred = strings.ReplaceAll(pred, "$"+paramMax, "CAST($"+paramMax+" AS DOUBLE)")
	return pred, params
}

// rangeSQL counts the rows of an entity's table whose column is below min or
// above max. prefix is "SELECT count(*) FROM <table>" with the table quoted.
func rangeSQL(prefix, col string, a semantics.Assertion) (string, map[string]any) {
	pred, params := rangePredicate(col, a)
	return prefix + " WHERE " + pred, params
}

// freshnessSQL is 1 when the column's latest value is older than the window
// (or there is no value: an empty table is stale), else 0. The threshold is
// computed here (now minus the window, UTC) and bound, so no interval text
// built from the file reaches SQL. A DATE or a TIMESTAMP column compares
// against it as a timestamp (a date counts from its midnight). The second
// column is a plain-words detail naming the latest value seen.
func freshnessSQL(table, col string, a semantics.Assertion, now time.Time) (string, map[string]any, error) {
	age, err := semantics.MaxAgeDuration(a.MaxAge)
	if err != nil {
		return "", nil, err
	}
	sql := fmt.Sprintf("SELECT CASE WHEN m IS NULL OR m < $%s THEN 1 ELSE 0 END, "+
		"CASE WHEN m IS NULL THEN 'no values' ELSE 'latest ' || CAST(m AS VARCHAR) END "+
		"FROM (SELECT max(%s) AS m FROM %s) AS vibe_fresh", paramThreshold, col, table)
	return sql, map[string]any{paramThreshold: now.UTC().Add(-age).Truncate(time.Microsecond)}, nil
}

// rowCountSQL is 1 when the table has fewer than min_rows rows, else 0; the
// second column says how many it has.
func rowCountSQL(table string, a semantics.Assertion) (string, map[string]any) {
	sql := fmt.Sprintf("SELECT CASE WHEN n < $%s THEN 1 ELSE 0 END, CAST(n AS VARCHAR) || ' rows' FROM (SELECT count(*) AS n FROM %s) AS vibe_rows", paramMinRows, table)
	return sql, map[string]any{paramMinRows: int64(*a.MinRows)}
}

// PlanEntityCompile plans the compile check of an entity: its declared columns
// (the id column, every annotated column and every column an assertion names) selected from its table, vetted
// and not run, so a missing table or column fails with the binder's message.
func PlanEntityCompile(e *semantics.Entity) (Check, error) {
	if e == nil {
		return Check{}, fmt.Errorf("no entity to plan")
	}
	if e.Error != "" {
		return Check{}, fmt.Errorf("entity %q cannot be read: %s", e.Name, e.Error)
	}
	if err := semantics.ValidateEntity(e); err != nil {
		return Check{}, err
	}
	cols := []string{session.QuoteIdent(e.IDColumn)}
	seen := map[string]bool{e.IDColumn: true}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			cols = append(cols, session.QuoteIdent(name))
		}
	}
	for _, c := range e.Columns {
		add(c.Name)
	}
	for _, a := range e.Assertions {
		add(a.Column)
	}
	sql := fmt.Sprintf("SELECT %s FROM %s", strings.Join(cols, ", "), semantics.QuoteTable(e.Table))
	// A range or freshness column is compared with a bound number or timestamp;
	// including the comparison makes a type mismatch (a text column) fail here
	// with the binder's own message instead of in the data check.
	var preds []string
	params := map[string]any{}
	for _, a := range e.Assertions {
		col := session.QuoteIdent(a.Column)
		switch a.Kind {
		case semantics.AssertRange:
			p, ps := rangeCastPredicate(col, a)
			preds = append(preds, "("+p+")")
			for k, v := range ps {
				params[k] = v
			}
		case semantics.AssertFreshness:
			// A string cast to TIMESTAMP: the vet path binds plain values only.
			preds = append(preds, "("+col+" < CAST($"+paramThreshold+" AS TIMESTAMP))")
			params[paramThreshold] = "1970-01-01 00:00:00"
		}
	}
	c := Check{SubjectKind: SubjectEntity, Subject: e.Name, Kind: KindCompile, Connection: e.Connection, SQL: sql, VetOnly: true}
	if len(preds) > 0 {
		c.SQL += " WHERE " + strings.Join(preds, " OR ")
		c.Params = params
	}
	return c, nil
}

// PlanMetricCompile plans the compile check of a metric: the metric at its full
// dimension set (none for a metric with no dimensions), expanded like any
// metric() call, so the measure and every dimension are bound against the
// saved query's output and a $name parameter takes its declared default.
func PlanMetricCompile(ctx context.Context, m *semantics.Metric, src semantics.Reader, now time.Time) (Check, error) {
	if m == nil {
		return Check{}, fmt.Errorf("no metric to plan")
	}
	if m.Error != "" {
		return Check{}, fmt.Errorf("metric %q cannot be read: %s", m.Name, m.Error)
	}
	if err := semantics.ValidateMetric(m); err != nil {
		return Check{}, err
	}
	q, err := src.Query(ctx, m.Query)
	if err != nil {
		return Check{}, fmt.Errorf("metric %q: its saved query %q cannot be read: %w", m.Name, m.Query, err)
	}
	text := "SELECT * FROM " + semantics.MetricCall(m.Name, m.Dimensions)
	var preds []string
	extra := map[string]any{}
	for _, a := range m.Assertions {
		if a.Kind == semantics.AssertRange {
			p, ps := rangeCastPredicate(session.QuoteIdent(m.Name), a)
			preds = append(preds, "("+p+")")
			for k, v := range ps {
				extra[k] = v
			}
		}
	}
	if len(preds) > 0 {
		text += " WHERE " + strings.Join(preds, " OR ")
	}
	res, err := semantics.Expand(ctx, text, src, semantics.Options{Connection: q.Connection, DefaultConnection: q.Connection})
	if err != nil {
		return Check{}, err
	}
	values := placeholderValues(res.Params)
	if len(extra) > 0 && values == nil {
		values = map[string]any{}
	}
	for k, v := range extra {
		for _, p := range res.Params {
			if p.Name == k {
				return Check{}, fmt.Errorf("metric %q: its saved query declares the parameter $%s, which a range assertion reserves", m.Name, k)
			}
		}
		values[k] = v
	}
	params, err := res.BindParams(values, now)
	if err != nil {
		return Check{}, err
	}
	return Check{SubjectKind: SubjectMetric, Subject: m.Name, Kind: KindCompile, Connection: q.Connection, SQL: res.SQL, Params: params, VetOnly: true}, nil
}

// placeholderValues gives each saved-query parameter that has no default a
// value valid for its type, so a required parameter does not stop a bind that
// only checks names and types. The value is never run: a compile check is
// vetted, not executed.
func placeholderValues(ps []queries.Param) map[string]any {
	var out map[string]any
	for _, p := range ps {
		if p.Default != "" {
			continue
		}
		var v any = ""
		switch p.Type {
		case "number":
			v = float64(0)
		case "integer":
			v = int64(0)
		case "boolean":
			v = false
		case "date":
			v = "2000-01-01"
		}
		if out == nil {
			out = map[string]any{}
		}
		out[p.Name] = v
	}
	return out
}
