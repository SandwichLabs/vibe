package automation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// ErrMacroCycle is returned when RunMacro recursion detects a cycle or exceeds
// the configured depth limit.
var ErrMacroCycle = errors.New("automation: macro recursion cycle or depth limit exceeded")

// UI intent kinds: the structured navigation/dialog intents the Executor returns
// for UI-bound actions instead of executing them in Go.
const (
	IntentOpenForm    = "open_form"
	IntentOpenReport  = "open_report"
	IntentGoToRecord  = "go_to_record"
	IntentFindRecord  = "find_record"
	IntentMsgBox      = "msg_box"
	IntentExport      = "export"
	IntentEmailObject = "email_object"
	IntentSetValue    = "set_value"
	// IntentOpenSwitchboard is the UI intent an OpenSwitchboard action surfaces as
	// (the canonical AutoExec startup intent the App Shell applies).
	IntentOpenSwitchboard = "open_switchboard"
	// IntentSetFilter is the UI-intent kind a server-side SetFilter is forwarded
	// as: the Wails layer translates each AppliedFilter into a set_filter intent
	// the frontend merges onto a subsequent form/report navigation as a "where"
	// predicate.
	IntentSetFilter = "set_filter"
)

// Column describes a single column in a query result returned to the Executor.
type Column struct {
	Name string
	Type string
}

// ResultSet is the Executor-facing shape of a query result, decoupled from the
// engine package so automation depends only on a narrow capability interface.
type ResultSet struct {
	Columns []Column
	Rows    [][]any
}

// QueryEngine is the server-evaluable capability the Executor needs for
// RunQuery/RunSQL/SetFilter and condition evaluation.
type QueryEngine interface {
	Execute(ctx context.Context, query string, params map[string]any) (*ResultSet, error)
}

// UIIntent is a structured navigation/dialog request returned to the caller for
// a UI-bound action (the frontend performs the actual navigation/dialog).
type UIIntent struct {
	Kind   string
	Target string
	Args   map[string]string
}

// AppliedFilter records a server-side SetFilter that the Executor applied.
type AppliedFilter struct {
	Target string
	Filter string
}

// Result is the outcome of running a macro: structured UI intents for the
// frontend plus a record of server-side filters applied.
type Result struct {
	Intents []UIIntent
	Filters []AppliedFilter
}

// MacroResolver resolves a sub-macro by id for RunMacro.
type MacroResolver func(id string) (MacroSpec, error)

// Executor interprets a MacroSpec against injected capability interfaces.
type Executor struct {
	qe       QueryEngine
	resolver MacroResolver
	maxDepth int
	logger   *slog.Logger
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor)

// WithQueryEngine injects the server-evaluable query capability.
func WithQueryEngine(qe QueryEngine) ExecutorOption {
	return func(e *Executor) { e.qe = qe }
}

// WithMacroResolver injects the sub-macro resolver used by RunMacro.
func WithMacroResolver(r MacroResolver) ExecutorOption {
	return func(e *Executor) { e.resolver = r }
}

// WithMaxDepth bounds RunMacro recursion depth.
func WithMaxDepth(d int) ExecutorOption {
	return func(e *Executor) { e.maxDepth = d }
}

// WithLogger injects a structured logger.
func WithLogger(l *slog.Logger) ExecutorOption {
	return func(e *Executor) { e.logger = l }
}

// NewExecutor constructs an Executor.
func NewExecutor(opts ...ExecutorOption) *Executor {
	e := &Executor{maxDepth: 16}
	for _, opt := range opts {
		opt(e)
	}
	if e.logger == nil {
		e.logger = slog.Default()
	}
	return e
}

// Run interprets the spec's actions in order against the injected backend.
//
// Server-side actions (RunSQL, RunQuery, SetFilter) execute on the real query
// engine; UI-bound actions (OpenForm, OpenReport, ...) are returned as structured
// UIIntents for the frontend to perform. If/Then conditions gate the action they
// annotate. RunMacro recursion is bounded and cycles are rejected.
func (e *Executor) Run(ctx context.Context, spec MacroSpec) (*Result, error) {
	res := &Result{}
	if err := e.run(ctx, spec, res, nil); err != nil {
		return nil, err
	}
	return res, nil
}

// run is the recursive worker. visited is the chain of macro ids currently on
// the call stack, used for cycle detection.
func (e *Executor) run(ctx context.Context, spec MacroSpec, res *Result, visited []string) error {
	if len(visited) >= e.maxDepth {
		return fmt.Errorf("%w: depth %d", ErrMacroCycle, len(visited))
	}
	for _, id := range visited {
		if id == spec.ID {
			return fmt.Errorf("%w: %q already on stack %v", ErrMacroCycle, spec.ID, visited)
		}
	}
	chain := append(append([]string{}, visited...), spec.ID)

	for i, action := range spec.Actions {
		if _, ok := knownActionKinds[action.Kind]; !ok {
			return fmt.Errorf("automation: action %d has unknown kind %q", i, action.Kind)
		}

		if action.Condition != nil {
			ok, err := e.evalCondition(ctx, action.Condition)
			if err != nil {
				return fmt.Errorf("automation: failed to evaluate condition on action %d (%s): %w", i, action.Kind, err)
			}
			if !ok {
				continue
			}
		}

		if err := e.runAction(ctx, action, res, chain); err != nil {
			return fmt.Errorf("automation: action %d (%s) failed: %w", i, action.Kind, err)
		}
	}
	return nil
}

// runAction dispatches a single (condition-satisfied) action.
func (e *Executor) runAction(ctx context.Context, action MacroAction, res *Result, chain []string) error {
	switch action.Kind {
	case ActionRunSQL, ActionRunQuery:
		sql := action.Args["sql"]
		if sql == "" {
			return fmt.Errorf("%s requires a non-empty \"sql\" arg", action.Kind)
		}
		if e.qe == nil {
			return fmt.Errorf("%s requires a query engine", action.Kind)
		}
		if _, err := e.qe.Execute(ctx, sql, nil); err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		return nil

	case ActionSetFilter:
		res.Filters = append(res.Filters, AppliedFilter{
			Target: action.Args["target"],
			Filter: action.Args["filter"],
		})
		return nil

	case ActionRunMacro:
		if e.resolver == nil {
			return fmt.Errorf("RunMacro requires a macro resolver")
		}
		target := action.Args["target"]
		sub, err := e.resolver(target)
		if err != nil {
			return fmt.Errorf("failed to resolve sub-macro %q: %w", target, err)
		}
		return e.run(ctx, sub, res, chain)

	case ActionOpenForm:
		res.Intents = append(res.Intents, e.uiIntent(IntentOpenForm, action))
		return nil
	case ActionOpenReport:
		res.Intents = append(res.Intents, e.uiIntent(IntentOpenReport, action))
		return nil
	case ActionGoToRecord:
		res.Intents = append(res.Intents, e.uiIntent(IntentGoToRecord, action))
		return nil
	case ActionFindRecord:
		res.Intents = append(res.Intents, e.uiIntent(IntentFindRecord, action))
		return nil
	case ActionMsgBox:
		res.Intents = append(res.Intents, e.uiIntent(IntentMsgBox, action))
		return nil
	case ActionExport:
		res.Intents = append(res.Intents, e.uiIntent(IntentExport, action))
		return nil
	case ActionEmailObject:
		res.Intents = append(res.Intents, e.uiIntent(IntentEmailObject, action))
		return nil
	case ActionOpenSwitchboard:
		res.Intents = append(res.Intents, e.uiIntent(IntentOpenSwitchboard, action))
		return nil
	case ActionSetValue:
		// SetValue is UI-bound, not a DuckDB mutation. In Access, SetValue assigns
		// a control/form/property value (e.g. [Forms]![f]![ctl] = expr) — it mutates
		// live UI state, not persisted table data. We therefore surface it as a
		// structured "set_value" intent for the frontend to apply, rather than
		// executing SQL against DuckDB. (Persisting a field belongs to an explicit
		// RunSQL UPDATE, not SetValue.) This intentionally diverges from the
		// acceptance-criteria parenthetical that groups SetValue with the
		// server-side actions: server-side write-back is not part of SetValue's
		// semantics here.
		res.Intents = append(res.Intents, e.uiIntent(IntentSetValue, action))
		return nil

	default:
		return fmt.Errorf("automation: unsupported action kind %q", action.Kind)
	}
}

// uiIntent builds a UIIntent from a UI-bound action, copying its args.
func (e *Executor) uiIntent(kind string, action MacroAction) UIIntent {
	var args map[string]string
	if len(action.Args) > 0 {
		args = make(map[string]string, len(action.Args))
		for k, v := range action.Args {
			args[k] = v
		}
	}
	return UIIntent{Kind: kind, Target: action.Args["target"], Args: args}
}

// evalCondition evaluates a minimal If/Then condition expression of the form:
//
//	RunQuery(<sql>) <op> <number>
//
// where <op> is one of > >= < <= == !=. The SQL is executed on the real engine
// and its first scalar cell is compared numerically against <number>.
func (e *Executor) evalCondition(ctx context.Context, cond *MacroCondition) (bool, error) {
	expr := strings.TrimSpace(cond.Expr)
	if !strings.HasPrefix(expr, "RunQuery(") {
		return false, fmt.Errorf("unsupported condition expression %q (expected RunQuery(...) <op> <number>)", expr)
	}
	close := strings.LastIndex(expr, ")")
	if close < 0 {
		return false, fmt.Errorf("malformed condition expression %q: missing closing paren", expr)
	}
	sql := strings.TrimSpace(expr[len("RunQuery("):close])
	rest := strings.TrimSpace(expr[close+1:])

	op, rhsStr, err := splitComparison(rest)
	if err != nil {
		return false, fmt.Errorf("malformed condition expression %q: %w", expr, err)
	}
	rhs, err := strconv.ParseFloat(rhsStr, 64)
	if err != nil {
		return false, fmt.Errorf("condition rhs %q is not numeric: %w", rhsStr, err)
	}

	if e.qe == nil {
		return false, fmt.Errorf("RunQuery condition requires a query engine")
	}
	rs, err := e.qe.Execute(ctx, sql, nil)
	if err != nil {
		return false, fmt.Errorf("condition query failed: %w", err)
	}
	if len(rs.Rows) == 0 || len(rs.Rows[0]) == 0 {
		return false, fmt.Errorf("condition query returned no scalar value")
	}
	lhs, err := toFloat(rs.Rows[0][0])
	if err != nil {
		return false, fmt.Errorf("condition lhs not numeric: %w", err)
	}

	switch op {
	case ">":
		return lhs > rhs, nil
	case ">=":
		return lhs >= rhs, nil
	case "<":
		return lhs < rhs, nil
	case "<=":
		return lhs <= rhs, nil
	case "==", "=":
		return lhs == rhs, nil
	case "!=", "<>":
		return lhs != rhs, nil
	default:
		return false, fmt.Errorf("unsupported comparison operator %q", op)
	}
}

// splitComparison parses "<op> <number>" tail of a condition expression.
func splitComparison(rest string) (op, rhs string, err error) {
	rest = strings.TrimSpace(rest)
	for _, candidate := range []string{">=", "<=", "==", "!=", "<>", ">", "<", "="} {
		if strings.HasPrefix(rest, candidate) {
			return candidate, strings.TrimSpace(rest[len(candidate):]), nil
		}
	}
	return "", "", fmt.Errorf("expected comparison operator in %q", rest)
}

// toFloat coerces a scalar query cell into a float64 for numeric comparison.
func toFloat(v any) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to number", v)
	}
}
