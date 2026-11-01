package assert

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/semantics"
)

// RunFunc runs one planned statement and returns its result. In the product it
// is the agent policy's read-only runner (see workspace.ReadOnlyRunFunc), so a
// definition from a cloned repository is held to what a cloned artifact query
// is.
type RunFunc = semantics.QueryFunc

// Result is one check's outcome.
type Result struct {
	Check Check `json:"check"`
	// Status is pass, fail (Violations rows broke the assertion) or error (the
	// check could not run: a connection down, a refusal by the policy, a
	// failed plan). An error says nothing about the data.
	Status     string        `json:"status"`
	Violations int64         `json:"violations"`
	Duration   time.Duration `json:"duration_ns"`
	Error      string        `json:"error,omitempty"`
	// Receipt is reserved for the receipts epic: the id of the receipt a run
	// will record. It is empty until then.
	Receipt string `json:"receipt,omitempty"`
}

// VetFunc vets one planned statement without running it: nil means it binds
// and the read-only policy lets it through. In the product it is the agent
// policy's agents.Handler.VetReadOnlySQLIn (see workspace.ReadOnlyVetFunc). An
// error that reports DefinitionProblem() true is the statement or the policy
// refusing it; ConnectFailure() true is a connection that could not be reached;
// any other is a failure of the check itself.
type VetFunc func(ctx context.Context, connection, sql string, params map[string]any) error

// Runner is what Run executes checks with: Run for a counting check, Vet for a
// compile check.
type Runner struct {
	Run RunFunc
	Vet VetFunc
}

// notRunCompile is the Error of a check skipped because its subject's compile
// check failed: one binder message, not one per assertion.
const notRunCompile = "not run: the definition does not compile"

// Run executes the checks in order through r. One check failing to run (an
// error) never stops the others; a canceled context marks the remaining
// checks as errors. The result list has one entry per check. When a subject's
// compile check fails, its later checks are errors that say so instead of
// repeating the binder's message.
func Run(ctx context.Context, checks []Check, r Runner) []Result {
	out := make([]Result, 0, len(checks))
	broken := map[[2]string]bool{}
	for _, c := range checks {
		key := [2]string{c.SubjectKind, c.Subject}
		if broken[key] && !c.VetOnly {
			out = append(out, Result{Check: c, Status: StatusError, Error: notRunCompile})
			continue
		}
		res := runOne(ctx, c, r)
		if c.VetOnly && c.Kind == KindCompile && res.Status == StatusFail {
			broken[key] = true
		}
		out = append(out, res)
	}
	return out
}

// Failure is the result for a subject whose checks could not be planned at
// all (an unreadable file, a missing saved query).
func Failure(subjectKind, subject string, err error) Result {
	return Result{Check: Check{SubjectKind: subjectKind, Subject: subject, Kind: "plan"}, Status: StatusError, Error: err.Error()}
}

func runOne(ctx context.Context, c Check, rn Runner) Result {
	r := Result{Check: c}
	if c.PlanError != "" {
		r.Status, r.Error = StatusError, c.PlanError
		return r
	}
	if err := ctx.Err(); err != nil {
		r.Status, r.Error = StatusError, err.Error()
		return r
	}
	if c.VetOnly {
		return vetOne(ctx, c, rn.Vet)
	}
	start := time.Now()
	rs, err := rn.Run(ctx, c.Connection, c.SQL, c.Params)
	r.Duration = time.Since(start)
	if err != nil {
		r.Status, r.Error = StatusError, err.Error()
		return r
	}
	n, err := violationCount(rs)
	if err != nil {
		r.Status, r.Error = StatusError, err.Error()
		return r
	}
	r.Violations = n
	r.Status = StatusPass
	if n > 0 {
		r.Status = StatusFail
	}
	return r
}

// vetOne runs a compile check. Only a statement the binder or the policy
// refuses (an error reporting DefinitionProblem() true) is a fail: fix the
// definition. Everything else is an error, which says nothing about the files:
// a connection that cannot be reached, a canceled or timed-out vet, an I/O
// failure while binding.
func vetOne(ctx context.Context, c Check, vet VetFunc) Result {
	r := Result{Check: c}
	if vet == nil {
		r.Status, r.Error = StatusError, "no vet is available to check that the definition compiles"
		return r
	}
	start := time.Now()
	err := vet(ctx, c.Connection, c.SQL, c.Params)
	r.Duration = time.Since(start)
	var def interface{ DefinitionProblem() bool }
	var cf interface{ ConnectFailure() bool }
	switch {
	case err == nil:
		r.Status = StatusPass
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		(errors.As(err, &cf) && cf.ConnectFailure()):
		r.Status, r.Error = StatusError, err.Error()
	case errors.As(err, &def) && def.DefinitionProblem():
		r.Status, r.Error = StatusFail, err.Error()
	default:
		r.Status, r.Error = StatusError, err.Error()
	}
	return r
}

// violationCount reads the one number a counting statement returns.
func violationCount(rs *engine.ResultSet) (int64, error) {
	if rs == nil {
		return 0, fmt.Errorf("the check returned no result")
	}
	v := rs.Value(0, 0)
	n, ok := AsCount(v)
	if !ok || n < 0 {
		return 0, fmt.Errorf("the check returned %v where a count was expected", v)
	}
	return n, nil
}

// AsCount reads a database integer of any width.
func AsCount(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int32:
		return int64(x), true
	case int:
		return int64(x), true
	case uint64:
		if x > math.MaxInt64 {
			return 0, false
		}
		return int64(x), true
	case uint32:
		return int64(x), true
	case float64:
		if x == math.Trunc(x) && x >= 0 && x < 1e15 {
			return int64(x), true
		}
	}
	return 0, false
}

// Row is a Result flattened for a table or a wire: what the CLI's --json and
// Studio's Semantics view show.
type Row struct {
	SubjectKind string `json:"subject_kind"`
	Subject     string `json:"subject"`
	Assertion   string `json:"assertion"`
	Column      string `json:"column,omitempty"`
	Status      string `json:"status"`
	Violations  int64  `json:"violations"`
	DurationMS  int64  `json:"duration_ms"`
	Error       string `json:"error,omitempty"`
	Receipt     string `json:"receipt,omitempty"`
}

// Rows flattens results; the slice is never nil.
func Rows(rs []Result) []Row {
	out := make([]Row, len(rs))
	for i, r := range rs {
		out[i] = Row{SubjectKind: r.Check.SubjectKind, Subject: r.Check.Subject, Assertion: r.Check.Kind, Column: r.Check.Column,
			Status: r.Status, Violations: r.Violations, DurationMS: r.Duration.Milliseconds(), Error: r.Error, Receipt: r.Receipt}
	}
	return out
}
