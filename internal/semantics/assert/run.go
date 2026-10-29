package assert

import (
	"context"
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

// Run executes the checks in order through run. One check failing to run (an
// error) never stops the others; a canceled context marks the remaining
// checks as errors. The result list has one entry per check.
func Run(ctx context.Context, checks []Check, run RunFunc) []Result {
	out := make([]Result, 0, len(checks))
	for _, c := range checks {
		out = append(out, runOne(ctx, c, run))
	}
	return out
}

// Failure is the result for a subject whose checks could not be planned at
// all (an unreadable file, a missing saved query).
func Failure(subjectKind, subject string, err error) Result {
	return Result{Check: Check{SubjectKind: subjectKind, Subject: subject, Kind: "plan"}, Status: StatusError, Error: err.Error()}
}

func runOne(ctx context.Context, c Check, run RunFunc) Result {
	r := Result{Check: c}
	if c.PlanError != "" {
		r.Status, r.Error = StatusError, c.PlanError
		return r
	}
	if err := ctx.Err(); err != nil {
		r.Status, r.Error = StatusError, err.Error()
		return r
	}
	start := time.Now()
	rs, err := run(ctx, c.Connection, c.SQL, c.Params)
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
