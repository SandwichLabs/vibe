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

	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/session"
)

// Status of one check.
const (
	StatusPass  = "pass"
	StatusFail  = "fail"
	StatusError = "error"
)

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
	// PlanError is set when the check could not be planned (a metric whose
	// saved query is gone, say): Run reports it as an error without running
	// anything, and the other checks still run.
	PlanError string `json:"plan_error,omitempty"`
}

// PlanEntity plans an entity's assertions, in the order they are declared. It
// re-validates the entity first, so a definition that never went through the
// strict decoder (a test double, a bug) still cannot put an unchecked name
// into SQL.
func PlanEntity(e *semantics.Entity) ([]Check, error) {
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
		table, col := semantics.QuoteTable(e.Table), session.QuoteIdent(a.Column)
		c := Check{SubjectKind: SubjectEntity, Subject: e.Name, Kind: a.Kind, Column: a.Column, Connection: e.Connection}
		switch a.Kind {
		case semantics.AssertUnique:
			c.SQL = fmt.Sprintf("SELECT count(%s) - count(DISTINCT %s) FROM %s", col, col, table)
		case semantics.AssertNotNull:
			c.SQL = fmt.Sprintf("SELECT count(*) FROM %s WHERE %s IS NULL", table, col)
		case semantics.AssertAcceptedValues:
			c.SQL, c.Params = acceptedValuesSQL(table, a)
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
		var pred string
		switch a.Kind {
		case semantics.AssertNonNegative:
			pred = session.QuoteIdent(m.Name) + " < 0"
		case semantics.AssertNotNull:
			pred = session.QuoteIdent(m.Name) + " IS NULL"
		default:
			return nil, fmt.Errorf("metric %q: assertion kind %q does not apply to a metric", m.Name, a.Kind)
		}
		text := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", semantics.MetricCall(m.Name, m.Dimensions), pred)
		res, err := semantics.Expand(ctx, text, src, semantics.Options{Connection: q.Connection, DefaultConnection: q.Connection})
		if err != nil {
			return nil, err
		}
		params, err := res.BindParams(nil, now)
		if err != nil {
			return nil, err
		}
		out = append(out, Check{SubjectKind: SubjectMetric, Subject: m.Name, Kind: a.Kind, Connection: q.Connection, SQL: res.SQL, Params: params})
	}
	return out, nil
}
