package semantics

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/SandwichLabs/slaw/internal/session"
)

// Join profiling measures how two entities' tables really relate and records
// it in the join's file as a measured: block, so the declared cardinality can
// be compared with the data. It is the person's action only (`slaw semantics
// profile`, the Semantics view's Profile button): no agent tool or MCP method
// calls it, and the queries it plans are run by the caller's read-only runner.
//
// The queries are table scans (a count of each side, orphans both ways, the
// largest number of right rows sharing a left key), so a profile of a large
// table takes as long as a few full scans.

// ProfileQueries are the statements of one profile, by role.
type ProfileQueries struct {
	LeftRows     Statement
	RightRows    Statement
	LeftOrphans  Statement
	RightOrphans Statement
	// MaxFanout is the largest number of right rows sharing a key that some
	// left row has.
	MaxFanout Statement
	// LeftDup is the largest number of left rows sharing one key (the check
	// one_to_many and one_to_one need; not stored in the file).
	LeftDup Statement
}

// Statement is a SQL text and where it runs.
type Statement struct {
	Connection string
	SQL        string
}

// PlanProfile plans the statements that profile j over its two entities. Every
// identifier comes from a validated name (entity table, join columns); there
// are no values. The statements that read both tables run on one connection,
// so if the entities name different connections and neither table is
// catalog-qualified it is refused.
func PlanProfile(j *Join, left, right *Entity) (*ProfileQueries, error) {
	if err := ValidateJoin(j); err != nil {
		return nil, err
	}
	for _, e := range []*Entity{left, right} {
		if e == nil {
			return nil, errors.New("a join needs both entities")
		}
		if e.Error != "" {
			return nil, fmt.Errorf("entity %q cannot be read: %s", e.Name, e.Error)
		}
		if err := ValidateEntity(e); err != nil {
			return nil, err
		}
	}
	both, err := bothSidesConnection(left, right)
	if err != nil {
		return nil, fmt.Errorf("join %q: %w", j.Name, err)
	}
	var lk, rk []string
	for _, p := range j.On {
		lk, rk = append(lk, session.QuoteIdent(p.Left)), append(rk, session.QuoteIdent(p.Right))
	}
	lTable, rTable := QuoteTable(left.Table), QuoteTable(right.Table)
	match := func(l, r string) string {
		parts := make([]string, len(lk))
		for i := range lk {
			parts[i] = fmt.Sprintf("%s.%s = %s.%s", r, rk[i], l, lk[i])
		}
		return strings.Join(parts, " AND ")
	}
	groupBy := func(alias string, cols []string) string {
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = alias + "." + c
		}
		return strings.Join(parts, ", ")
	}
	notNull := func(alias string, cols []string) string {
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = alias + "." + c + " IS NOT NULL"
		}
		return strings.Join(parts, " AND ")
	}
	lt, rt := lTable+" AS slaw_l", rTable+" AS slaw_r"
	return &ProfileQueries{
		LeftRows:  Statement{left.Connection, "SELECT count(*) FROM " + lTable},
		RightRows: Statement{right.Connection, "SELECT count(*) FROM " + rTable},
		LeftOrphans: Statement{both, fmt.Sprintf("SELECT count(*) FROM %s WHERE NOT EXISTS (SELECT 1 FROM %s WHERE %s)",
			lt, rt, match("slaw_l", "slaw_r"))},
		RightOrphans: Statement{both, fmt.Sprintf("SELECT count(*) FROM %s WHERE NOT EXISTS (SELECT 1 FROM %s WHERE %s)",
			rt, lt, match("slaw_l", "slaw_r"))},
		MaxFanout: Statement{both, fmt.Sprintf("SELECT coalesce(max(c), 0) FROM (SELECT count(*) AS c FROM %s WHERE %s AND EXISTS (SELECT 1 FROM %s WHERE %s) GROUP BY %s) AS slaw_g",
			rt, notNull("slaw_r", rk), lt, match("slaw_l", "slaw_r"), groupBy("slaw_r", rk))},
		LeftDup: Statement{left.Connection, fmt.Sprintf("SELECT coalesce(max(c), 0) FROM (SELECT count(*) AS c FROM %s WHERE %s GROUP BY %s) AS slaw_g",
			lt, notNull("slaw_l", lk), groupBy("slaw_l", lk))},
	}, nil
}

// bothSidesConnection is the connection a statement over both tables runs on.
func bothSidesConnection(left, right *Entity) (string, error) {
	lq := strings.Count(left.Table, ".") == 2
	rq := strings.Count(right.Table, ".") == 2
	switch {
	case left.Connection == right.Connection:
		return left.Connection, nil
	case lq:
		return right.Connection, nil
	case rq:
		return left.Connection, nil
	}
	return "", fmt.Errorf("entities %q and %q name different connections (%q and %q) and neither table is catalog-qualified (catalog.schema.table); profiling reads both tables in one statement", left.Name, right.Name, left.Connection, right.Connection)
}

// Profile is the result of a profiling run.
type Profile struct {
	Join     string   `json:"join"`
	Declared string   `json:"declared"`
	Measured Measured `json:"measured"`
	// LeftMaxDup is the largest number of left rows sharing a key. It is
	// reported and used for the verdict but not stored in the file.
	LeftMaxDup Count `json:"left_max_dup"`
	// Warnings say where the measure contradicts the declared cardinality.
	Warnings []string `json:"warnings"`
	// Rev is the join file's revision after the write.
	Rev string `json:"rev"`
}

// ProfileJoin profiles one join through run and writes the result into the
// join's file as measured:. It reads the file once, up front, and writes with
// that revision: a file that changed while the (possibly long) profile ran is
// ErrConflict and nothing is written, never overwritten. The rest of the file,
// comments included, is kept as written. A file that cannot be read, or an
// entity that is missing, is refused before any query runs.
func ProfileJoin(ctx context.Context, st Store, name string, run QueryFunc, now time.Time) (*Profile, error) {
	src, err := st.Source(ctx, KindJoin, name)
	if err != nil {
		return nil, fmt.Errorf("failed to read join %q: %w", name, err)
	}
	j, err := DecodeJoin(name, []byte(src.Text))
	if err != nil {
		return nil, fmt.Errorf("join %q cannot be profiled: %w", name, err)
	}
	left, err := st.Entity(ctx, j.Left)
	if err != nil {
		return nil, fmt.Errorf("join %q: its left entity %q: %w", name, j.Left, err)
	}
	right, err := st.Entity(ctx, j.Right)
	if err != nil {
		return nil, fmt.Errorf("join %q: its right entity %q: %w", name, j.Right, err)
	}
	plan, err := PlanProfile(j, left, right)
	if err != nil {
		return nil, err
	}
	// A file the block cannot be written into fails now, not after the scans.
	if _, err := withMeasured(j, src.Text, Measured{At: now.UTC().Format(time.RFC3339), Verdict: VerdictConsistent}); err != nil {
		return nil, err
	}
	var n struct{ lr, rr, lo, ro, fo, ld int64 }
	for _, step := range []struct {
		what string
		s    Statement
		into *int64
	}{
		{"left row count", plan.LeftRows, &n.lr}, {"right row count", plan.RightRows, &n.rr},
		{"left orphans", plan.LeftOrphans, &n.lo}, {"right orphans", plan.RightOrphans, &n.ro},
		{"largest fan-out", plan.MaxFanout, &n.fo}, {"largest left duplicate", plan.LeftDup, &n.ld},
	} {
		if *step.into, err = count(ctx, run, step.s); err != nil {
			return nil, fmt.Errorf("profiling join %q (%s): %w", name, step.what, err)
		}
	}
	m := Measured{At: now.UTC().Format(time.RFC3339), LeftRows: Count(n.lr), RightRows: Count(n.rr),
		MaxFanout: Count(n.fo), LeftOrphans: Count(n.lo), RightOrphans: Count(n.ro)}
	verdict, warnings := judge(j.Cardinality, &m, Count(n.ld))
	m.Verdict = verdict
	text, err := withMeasured(j, src.Text, m)
	if err != nil {
		return nil, err
	}
	rev, err := st.PutSource(ctx, KindJoin, name, []byte(text), WithRev(src.Rev))
	if err != nil {
		return nil, fmt.Errorf("failed to record the profile of join %q (nothing was written): %w", name, err)
	}
	return &Profile{Join: name, Declared: j.Cardinality, Measured: m, LeftMaxDup: Count(n.ld), Warnings: warnings, Rev: rev}, nil
}

func count(ctx context.Context, run QueryFunc, s Statement) (int64, error) {
	rs, err := run(ctx, s.Connection, s.SQL, nil)
	if err != nil {
		return 0, err
	}
	if rs == nil {
		return 0, errors.New("no result")
	}
	switch v := rs.Value(0, 0).(type) {
	case int64:
		return nonNeg(v)
	case int32:
		return nonNeg(int64(v))
	case int:
		return nonNeg(int64(v))
	case uint64:
		if v <= 1<<62 {
			return int64(v), nil
		}
	case float64:
		if v >= 0 && v < 1e15 && v == float64(int64(v)) {
			return int64(v), nil
		}
	}
	return 0, fmt.Errorf("expected a count, got %v", rs.Value(0, 0))
}

func nonNeg(v int64) (int64, error) {
	if v < 0 {
		return 0, fmt.Errorf("expected a count, got %d", v)
	}
	return v, nil
}

// judge derives the verdict and the warnings. Fan-out is what multiplies rows
// in a join: a many_to_one or one_to_one join wants at most one right row per
// left key, a one_to_many or one_to_one wants at most one left row per key.
// Orphans matter on the side that must find a partner: the left rows of a
// many_to_one (an inner join drops them), the right rows of a one_to_many,
// either side of a one_to_one. A many_to_many join is not held to either.
func judge(card string, m *Measured, leftDup Count) (string, []string) {
	var fan, orph []string
	rightOne := card == ManyToOne || card == OneToOne
	leftOne := card == OneToMany || card == OneToOne
	if rightOne && m.MaxFanout > 1 {
		fan = append(fan, fmt.Sprintf("declared %s, but a left key matches up to %d right rows: joining repeats left rows, so sums over the left side are inflated", card, m.MaxFanout))
	}
	if leftOne && leftDup > 1 {
		fan = append(fan, fmt.Sprintf("declared %s, but up to %d left rows share one key: joining repeats right rows, so sums over the right side are inflated", card, leftDup))
	}
	if rightOne && m.LeftOrphans > 0 {
		orph = append(orph, fmt.Sprintf("%d left rows have no match on the right (an inner join drops them)", m.LeftOrphans))
	}
	if leftOne && m.RightOrphans > 0 {
		orph = append(orph, fmt.Sprintf("%d right rows have no match on the left (an inner join drops them)", m.RightOrphans))
	}
	switch {
	case len(fan) > 0:
		return VerdictFanOut, append(fan, orph...)
	case len(orph) > 0:
		return VerdictOrphans, orph
	}
	return VerdictConsistent, nil
}

// withMeasured returns text with its measured: block replaced by m (or added),
// every other line kept as written. The result is decoded again and must read
// back as the same join with only the measure changed; anything else is
// refused rather than written.
func withMeasured(j *Join, text string, m Measured) (string, error) {
	block := fmt.Sprintf("measured:\n  at: %s\n  left_rows: %d\n  right_rows: %d\n  max_fanout: %d\n  left_orphans: %d\n  right_orphans: %d\n  verdict: %s\n",
		m.At, m.LeftRows, m.RightRows, m.MaxFanout, m.LeftOrphans, m.RightOrphans, m.Verdict)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
		block = strings.ReplaceAll(block, "\n", eol)
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "measured:") {
			start = i
			break
		}
	}
	var out string
	if start < 0 {
		if len(lines) > 0 && !strings.HasSuffix(lines[len(lines)-1], "\n") {
			lines[len(lines)-1] += eol
		}
		out = strings.Join(lines, "") + block
	} else {
		end := start + 1
		last := start
		for ; end < len(lines); end++ {
			l := lines[end]
			if strings.TrimSpace(l) == "" {
				continue
			}
			if l[0] != ' ' && l[0] != '\t' {
				break
			}
			last = end
		}
		out = strings.Join(lines[:start], "") + block + strings.Join(lines[last+1:], "")
	}
	back, err := DecodeJoin(j.Name, []byte(out))
	if err != nil {
		return "", fmt.Errorf("the measured block could not be added to the join file: %w", err)
	}
	if back.Measured == nil || *back.Measured != m {
		return "", errors.New("the measured block could not be added to the join file: it did not read back as written")
	}
	a, b := *j, *back
	a.Measured, b.Measured, a.Meta, b.Meta = nil, nil, Meta{}, Meta{}
	if !reflect.DeepEqual(a, b) {
		return "", errors.New("the measured block could not be added to the join file without changing something else")
	}
	return out, nil
}
