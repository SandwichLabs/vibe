package semantics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/connname"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/sqllex"
)

// MaxMetricCalls is how many metric() calls one text may hold.
const MaxMetricCalls = 20

// ErrParamClash is returned (wrapped in a refusal, so also an ErrInvalid) when
// two metrics in one statement declare the same $name parameter differently.
var ErrParamClash = errors.New("metric parameter clash")

// refusal is an expansion failure: errors.Is(err, ErrInvalid) holds, but its
// text is only the sentence for the person (or agent) who wrote the statement,
// without the "invalid semantics definition:" prefix a stored file's error has.
type refusal struct {
	msg  string
	also error
}

func (e *refusal) Error() string { return e.msg }

func (e *refusal) Is(target error) bool {
	return target == ErrInvalid || (e.also != nil && target == e.also)
}

func refuse(format string, args ...any) error {
	return &refusal{msg: fmt.Sprintf(format, args...)}
}

// Reader is what the expander reads: metric definitions and the saved queries
// they name. NewReader adapts a Store and a queries.Store; tests pass fakes.
type Reader interface {
	Metric(ctx context.Context, name string) (*Metric, error)
	Query(ctx context.Context, id string) (*queries.Query, error)
}

type reader struct {
	sem ReadStore
	q   queries.Store
}

// NewReader reads metrics from sem and saved queries from q. Either may be
// nil; a metric() call then fails with a plain message.
func NewReader(sem ReadStore, q queries.Store) Reader { return &reader{sem: sem, q: q} }

func (s *reader) Metric(ctx context.Context, name string) (*Metric, error) {
	if s.sem == nil {
		return nil, ErrNotFound
	}
	return s.pinnedMetric(ctx, name)
}

func (s *reader) Query(ctx context.Context, id string) (*queries.Query, error) {
	if s.q == nil {
		return nil, queries.ErrNotFound
	}
	return s.pinnedQuery(ctx, id)
}

// Options describe the run the text is for.
type Options struct {
	// Connection is the connection the statement runs on ("" is the workspace
	// default). A metric whose saved query names another connection is refused.
	Connection string
	// DefaultConnection is the workspace's default connection name, so that
	// "" and the default's own name are the same place.
	DefaultConnection string
}

// Use is one metric() call that was expanded.
type Use struct {
	Name    string   `json:"name"`
	Dims    []string `json:"by,omitempty"`
	QueryID string   `json:"query"`
	// Start and End are the call's byte span in the input text.
	Start int `json:"-"`
	End   int `json:"-"`
	// Piece is the parenthesised SELECT the call became: what a person's
	// surface hands to the read-only policy.
	Piece string `json:"-"`
}

// Result is an expanded text.
type Result struct {
	// SQL is the text with every metric() call replaced; equal to the input
	// when Changed is false.
	SQL     string
	Changed bool
	// Params are the declared parameters of the saved queries the calls use,
	// de-duplicated by name; BindParams fills their values.
	Params []queries.Param
	// ParamOwner names, per parameter, the first metric that declares it.
	ParamOwner map[string]string
	Uses       []Use
	// Bodies are the saved-query SQL texts the calls embed, once each. A person's
	// surface checks every one is a single SELECT before it runs the statement.
	Bodies []Body
}

// Body is one saved query's text as embedded, with the first metric that uses it.
type Body struct {
	Metric string
	Query  string
	SQL    string
}

// HasMetricCall is the fast path: whether text could hold a metric() call.
func HasMetricCall(text string) bool {
	return strings.Contains(strings.ToLower(text), "metric")
}

// Expand replaces every metric(...) call in text with a parenthesised SELECT
// over the metric's saved query:
//
//	(SELECT "region", sum(amount) AS "net_revenue" FROM (
//	<saved query SQL>
//	) AS vibe_metric_net_revenue GROUP BY "region")
//
// It lexes with internal/sqllex, the SQL policy's own lexer, so what it
// rewrites is exactly what the policy would have read as a call. Only declared
// names and the metric file's measure reach the output; the call's literals
// never do. The output holds no metric( call, so Expand(Expand(x)) == Expand(x).
//
// Every refusal wraps ErrInvalid.
func Expand(ctx context.Context, text string, src Reader, o Options) (*Result, error) {
	res := &Result{SQL: text, ParamOwner: map[string]string{}}
	if !HasMetricCall(text) {
		return res, nil
	}
	calls, err := findCalls(text)
	if err != nil || len(calls) == 0 {
		return res, err
	}
	if len(calls) > MaxMetricCalls {
		return nil, refuse("metric(): %d metric() calls; at most %d are allowed in one statement", len(calls), MaxMetricCalls)
	}
	if src == nil {
		return nil, refuse("metric(): there are no semantic definitions here")
	}
	x := &expander{ctx: ctx, src: src, o: o, res: res, bodies: map[string]bool{}, params: map[string]paramOwner{}}
	var b strings.Builder
	last := 0
	for _, c := range calls {
		piece, err := x.call(c)
		if err != nil {
			return nil, err
		}
		b.WriteString(text[last:c.start])
		b.WriteString(piece)
		last = c.end
	}
	b.WriteString(text[last:])
	res.SQL = b.String()
	res.Changed = true
	sort.Slice(res.Params, func(i, j int) bool { return res.Params[i].Name < res.Params[j].Name })
	return res, nil
}

// call is one parsed metric(...) call.
type call struct {
	start, end int
	name       string
	dims       []string
}

// findCalls lexes text and returns its metric(...) calls in order. A bare word
// METRIC directly followed by "(" is a call; a qualified name (x.metric(...))
// or a quoted "metric"(...) is left alone, and fails later like any unknown
// function.
func findCalls(text string) ([]call, error) {
	var out []call
	for _, toks := range sqllex.Statements(text) {
		for i := 0; i+1 < len(toks); i++ {
			if !isCallAt(toks, i) {
				continue
			}
			closeIdx := sqllex.MatchParen(toks, i+1)
			if closeIdx < 0 {
				return nil, refuse("metric(): the call is missing its closing parenthesis")
			}
			c, err := parseArgs(toks[i+2 : closeIdx])
			if err != nil {
				return nil, err
			}
			c.start, c.end = toks[i].Start, toks[closeIdx].End
			out = append(out, c)
			i = closeIdx
		}
	}
	return out, nil
}

// isCallAt reports whether toks[i] starts a metric( call.
func isCallAt(toks []sqllex.Tok, i int) bool {
	if !toks[i].IsWord("METRIC") || !toks[i+1].IsParen("(") {
		return false
	}
	return i == 0 || !toks[i-1].IsPunct(".")
}

const grammarHint = "write metric('name') or metric('name', by => 'dimension') or metric('name', by => ['dim1', 'dim2'])"

func parseArgs(args []sqllex.Tok) (call, error) {
	var c call
	if len(args) == 0 || args[0].Kind != sqllex.String || !args[0].Plain {
		return c, refuse("metric(): the metric name must be a plain 'quoted' literal")
	}
	c.name = args[0].Text
	rest := args[1:]
	if len(rest) == 0 {
		return c, nil
	}
	label := fmt.Sprintf("metric(%s)", quoteForMessage(c.name))
	// , by => <value>
	if len(rest) < 5 || !rest[0].IsPunct(",") || !rest[1].IsWord("BY") ||
		!rest[2].IsPunct("=") || !rest[3].IsPunct(">") || rest[2].End != rest[3].Start {
		return c, refuse("%s: only by => is supported (no where, no other arguments); %s", label, grammarHint)
	}
	dims, err := parseDims(label, rest[4:])
	if err != nil {
		return c, err
	}
	c.dims = dims
	return c, nil
}

func parseDims(label string, v []sqllex.Tok) ([]string, error) {
	plain := func(t sqllex.Tok) bool { return t.Kind == sqllex.String && t.Plain }
	if len(v) == 1 && plain(v[0]) {
		return []string{v[0].Text}, nil
	}
	if len(v) < 3 || !v[0].IsPunct("[") || !v[len(v)-1].IsPunct("]") {
		return nil, refuse("%s: each dimension must be a plain 'quoted' literal; %s", label, grammarHint)
	}
	var dims []string
	for i, t := range v[1 : len(v)-1] {
		switch {
		case i%2 == 0 && plain(t):
			dims = append(dims, t.Text)
		case i%2 == 1 && t.IsPunct(","):
		default:
			return nil, refuse("%s: each dimension must be a plain 'quoted' literal; %s", label, grammarHint)
		}
	}
	if len(dims) == 0 || len(v[1:len(v)-1])%2 == 0 {
		return nil, refuse("%s: by => [...] needs one or more 'quoted' dimensions; %s", label, grammarHint)
	}
	return dims, nil
}

// quoteForMessage prints a caller's literal inside an error: bounded and
// escaped, since it is the one place a call's text is echoed.
func quoteForMessage(s string) string {
	if r := []rune(s); len(r) > 64 {
		s = string(r[:64]) + "..."
	}
	return "'" + strings.NewReplacer("'", "''", "\n", " ", "\r", " ").Replace(s) + "'"
}

type paramOwner struct {
	metric string
	param  queries.Param
}

type expander struct {
	ctx    context.Context
	src    Reader
	o      Options
	res    *Result
	bodies map[string]bool
	params map[string]paramOwner
}

func (x *expander) call(c call) (string, error) {
	label := fmt.Sprintf("metric(%s)", quoteForMessage(c.name))
	m, err := x.loadMetric(c.name, label)
	if err != nil {
		return "", err
	}
	dims, err := pickDimensions(m, c.dims, label)
	if err != nil {
		return "", err
	}
	q, err := x.loadQuery(m, label)
	if err != nil {
		return "", err
	}
	if err := x.checkConnection(m, q); err != nil {
		return "", err
	}
	body, err := checkBody(q, label)
	if err != nil {
		return "", err
	}
	if err := x.addParams(m, q, body); err != nil {
		return "", err
	}
	if !x.bodies[q.ID] {
		x.bodies[q.ID] = true
		x.res.Bodies = append(x.res.Bodies, Body{Metric: m.Name, Query: q.ID, SQL: body})
	}
	piece := render(m, dims, body)
	x.res.Uses = append(x.res.Uses, Use{Name: m.Name, Dims: dims, QueryID: q.ID, Start: c.start, End: c.end, Piece: piece})
	return piece, nil
}

func (x *expander) loadMetric(name, label string) (*Metric, error) {
	if !connname.Valid(name) {
		return nil, refuse("%s: there is no metric named %s (metrics live in semantics/metrics/; `vibe semantics list` shows them)", label, quoteForMessage(name))
	}
	m, err := x.src.Metric(x.ctx, name)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, refuse("%s: there is no metric named %s (metrics live in semantics/metrics/; `vibe semantics list` shows them)", label, quoteForMessage(name))
	case err != nil:
		return nil, refuse("%s: the metric cannot be read: %s", label, ReadableError(err))
	case m == nil || m.Error != "":
		return nil, refuse("%s: the metric cannot be read", label)
	}
	return m, nil
}

func pickDimensions(m *Metric, asked []string, label string) ([]string, error) {
	out := make([]string, 0, len(asked))
	seen := map[string]bool{}
	for _, a := range asked {
		var found string
		for _, d := range m.Dimensions {
			if strings.EqualFold(d, a) {
				found = d
				break
			}
		}
		if found == "" {
			have := "none"
			if len(m.Dimensions) > 0 {
				have = strings.Join(m.Dimensions, ", ")
			}
			return nil, refuse("%s: %s is not a dimension of %s (dimensions: %s)", label, quoteForMessage(a), m.Name, have)
		}
		if seen[found] {
			return nil, refuse("%s: dimension %q is listed twice", label, found)
		}
		seen[found] = true
		out = append(out, found)
	}
	return out, nil
}

func (x *expander) loadQuery(m *Metric, label string) (*queries.Query, error) {
	q, err := x.src.Query(x.ctx, m.Query)
	switch {
	case errors.Is(err, queries.ErrNotFound):
		return nil, refuse("%s: its saved query %q does not exist (queries/%s.sql)", label, m.Query, m.Query)
	case err != nil:
		return nil, refuse("%s: its saved query %q cannot be read: %s", label, m.Query, err)
	case q == nil:
		return nil, refuse("%s: its saved query %q does not exist", label, m.Query)
	case q.Error != "":
		return nil, refuse("%s: its saved query %q cannot be read: %s", label, m.Query, q.Error)
	}
	if q.ID == "" {
		q.ID = m.Query
	}
	return q, nil
}

// checkConnection refuses a metric run somewhere its saved query does not
// belong: the same unqualified table name means another table on another
// connection.
func (x *expander) checkConnection(m *Metric, q *queries.Query) error {
	eff := func(c string) string {
		if c == "" {
			return x.o.DefaultConnection
		}
		return c
	}
	want, have := eff(q.Connection), eff(x.o.Connection)
	if strings.EqualFold(want, have) {
		return nil
	}
	if want == "" {
		return refuse("metric '%s' is defined on the workspace's default connection; run this statement there", m.Name)
	}
	return refuse("metric '%s' is defined on connection '%s'; run this statement on %s", m.Name, want, want)
}

// checkBody holds the saved query's text to the shape a derived table needs and
// returns it ready to embed: exactly one statement that begins like a read, its
// trailing ';' and comments cut off, otherwise byte for byte. It also refuses a
// nested metric() and a $name the query does not use correctly (the same
// shape rules a bound query follows).
func checkBody(q *queries.Query, label string) (string, error) {
	notOne := func() (string, error) {
		return "", refuse("%s: saved query %q is not one read statement (one SELECT, WITH, FROM or VALUES)", label, q.ID)
	}
	stmts := sqllex.Statements(q.SQL)
	if len(stmts) != 1 {
		return notOne()
	}
	toks := stmts[0]
	switch {
	case toks[0].IsWord("SELECT"), toks[0].IsWord("WITH"), toks[0].IsWord("FROM"), toks[0].IsWord("VALUES"):
	default:
		return notOne()
	}
	for i, t := range toks {
		if t.Kind == sqllex.String && !t.Closed || t.Kind == sqllex.Ident && !t.Closed {
			return notOne()
		}
		if i+1 < len(toks) && isCallAt(toks, i) {
			return "", refuse("%s: its saved query %q uses metric(), which a metric's own query may not do", label, q.ID)
		}
	}
	body := q.SQL[:toks[len(toks)-1].End]
	// The wrapped body must stay one parenthesised group: an unbalanced paren
	// or an unclosed comment inside it would swallow the rest of the statement.
	wrapped := sqllex.Statements("(\n" + body + "\n)")
	if len(wrapped) != 1 || sqllex.MatchParen(wrapped[0], 0) != len(wrapped[0])-1 {
		return notOne()
	}
	if err := queries.CheckParamShape(body); err != nil {
		return "", refuse("%s: saved query %q: %v", label, q.ID, err)
	}
	return body, nil
}

// addParams records the saved query's declared parameters, refusing a $name it
// uses without declaring, and a name two metrics declare differently.
func (x *expander) addParams(m *Metric, q *queries.Query, body string) error {
	declared := map[string]queries.Param{}
	for _, p := range q.Parameters {
		declared[p.Name] = p
	}
	for _, n := range engine.ParamNames(body) {
		if _, ok := declared[n]; !ok {
			return refuse("metric(%s): its saved query %q uses $%s, which it does not declare as a parameter", quoteForMessage(m.Name), q.ID, n)
		}
	}
	for _, p := range q.Parameters {
		prev, ok := x.params[p.Name]
		if !ok {
			x.params[p.Name] = paramOwner{metric: m.Name, param: p}
			x.res.ParamOwner[p.Name] = m.Name
			x.res.Params = append(x.res.Params, p)
			continue
		}
		if prev.param != p {
			return &refusal{also: ErrParamClash, msg: fmt.Sprintf("metrics %s and %s both declare the parameter $%s, differently (%s default %q, and %s default %q); give them the same declaration",
				prev.metric, m.Name, p.Name, prev.param.Type, prev.param.Default, p.Type, p.Default)}
		}
	}
	return nil
}

func render(m *Metric, dims []string, body string) string {
	quoted := make([]string, len(dims))
	for i, d := range dims {
		quoted[i] = session.QuoteIdent(d)
	}
	var b strings.Builder
	b.WriteString("(SELECT ")
	for _, d := range quoted {
		b.WriteString(d)
		b.WriteString(", ")
	}
	b.WriteString(m.Measure)
	b.WriteString(" AS ")
	b.WriteString(session.QuoteIdent(m.Name))
	b.WriteString(" FROM (\n")
	b.WriteString(body)
	b.WriteString("\n) AS vibe_metric_")
	b.WriteString(m.Name)
	if len(quoted) > 0 {
		b.WriteString(" GROUP BY ")
		b.WriteString(strings.Join(quoted, ", "))
	}
	b.WriteString(")")
	return b.String()
}

// BindParams returns values plus the declared defaults of every metric
// parameter the expanded text uses. A value the caller supplied wins (and is
// coerced to the declared type); a name the metrics do not declare passes
// through untouched. It binds through queries.Bind, the one binder every
// surface shares, so a value only ever becomes a bound parameter.
func (r *Result) BindParams(values map[string]any, now time.Time) (map[string]any, error) {
	if r == nil || len(r.Params) == 0 {
		return values, nil
	}
	declared := map[string]queries.Param{}
	for _, p := range r.Params {
		declared[p.Name] = p
	}
	var used []queries.Param
	for _, n := range engine.ParamNames(r.SQL) {
		if p, ok := declared[n]; ok {
			used = append(used, p)
		}
	}
	if len(used) == 0 {
		return values, nil
	}
	synthetic := &queries.Query{Parameters: used}
	names := make([]string, len(used))
	for i, p := range used {
		names[i] = "$" + p.Name
	}
	synthetic.SQL = "SELECT " + strings.Join(names, ", ")
	supplied := map[string]any{}
	for _, p := range used {
		if v, ok := values[p.Name]; ok {
			supplied[p.Name] = v
		}
	}
	bound, err := queries.Bind(synthetic, supplied, queries.BindOptions{Now: now})
	if err != nil {
		return nil, refuse("metric parameters: %v", err)
	}
	out := make(map[string]any, len(values)+len(bound))
	for k, v := range values {
		out[k] = v
	}
	for k, v := range bound {
		out[k] = v
	}
	return out, nil
}

// UsesJSON is the receipts seam: what a run reports as metrics_used.
func (r *Result) UsesJSON() []map[string]any {
	out := make([]map[string]any, 0, len(r.Uses))
	for _, u := range r.Uses {
		by := u.Dims
		if by == nil {
			by = []string{}
		}
		out = append(out, map[string]any{"name": u.Name, "by": by, "query": u.QueryID})
	}
	return out
}

// CheckOuterParams refuses a $name in the statement the person or agent wrote
// (input, before expansion) that only a metric declares and that no caller
// value supplies: it would silently take the metric's default. A value the
// caller did send for it is fine. declared are parameters the caller's own
// context declares (a saved query open in the editor); one that shares a name
// with a metric's but not its type is a clash.
func (r *Result) CheckOuterParams(input string, supplied map[string]any, declared []queries.Param) error {
	if r == nil || len(r.Params) == 0 {
		return nil
	}
	mine := map[string]queries.Param{}
	for _, p := range r.Params {
		mine[p.Name] = p
	}
	for _, d := range declared {
		if p, ok := mine[d.Name]; ok && p.Type != d.Type {
			return refuse("parameter $%s is declared as %s by the open query and as %s by metric %s's saved query; rename one of them", d.Name, d.Type, p.Type, r.ParamOwner[d.Name])
		}
	}
	for _, n := range engine.ParamNames(input) {
		if _, owned := mine[n]; !owned {
			continue
		}
		if _, ok := supplied[n]; ok {
			continue
		}
		return refuse("$%s belongs to metric %s's saved query; it can't be used outside metric()", n, r.ParamOwner[n])
	}
	return nil
}

// ExpandGrain builds the statement behind a metric's unique assertion: the
// number of dimension sets the metric's saved query holds more than one row
// for. It is the only other place a saved query's text is written into SQL
// besides Expand, and it goes through the same steps (the query is loaded, held
// to one read statement and cut like a metric expansion, its $name parameters
// declared), so there is one set of rules for embedding a saved query:
//
//	SELECT count(*) FROM (SELECT 1 FROM (
//	<saved query SQL>
//	) AS vibe_grain_<metric> GROUP BY "d1", ..., "dn" HAVING count(*) > 1) AS vibe_grain_dups
//
// The metric needs at least one dimension. The Result carries the declared
// parameters for BindParams like an expanded text.
func ExpandGrain(ctx context.Context, m *Metric, src Reader, o Options) (*Result, error) {
	label := fmt.Sprintf("metric(%s)", quoteForMessage(m.Name))
	if len(m.Dimensions) == 0 {
		return nil, refuse("%s: unique needs at least one dimension", label)
	}
	if src == nil {
		return nil, refuse("%s: there are no semantic definitions here", label)
	}
	res := &Result{ParamOwner: map[string]string{}}
	x := &expander{ctx: ctx, src: src, o: o, res: res, bodies: map[string]bool{}, params: map[string]paramOwner{}}
	q, err := x.loadQuery(m, label)
	if err != nil {
		return nil, err
	}
	if err := x.checkConnection(m, q); err != nil {
		return nil, err
	}
	body, err := checkBody(q, label)
	if err != nil {
		return nil, err
	}
	if err := x.addParams(m, q, body); err != nil {
		return nil, err
	}
	res.Bodies = []Body{{Metric: m.Name, Query: q.ID, SQL: body}}
	quoted := make([]string, len(m.Dimensions))
	for i, d := range m.Dimensions {
		quoted[i] = session.QuoteIdent(d)
	}
	res.SQL = "SELECT count(*) FROM (SELECT 1 FROM (\n" + body + "\n) AS vibe_grain_" + m.Name +
		" GROUP BY " + strings.Join(quoted, ", ") + " HAVING count(*) > 1) AS vibe_grain_dups"
	res.Changed = true
	sort.Slice(res.Params, func(i, j int) bool { return res.Params[i].Name < res.Params[j].Name })
	return res, nil
}
