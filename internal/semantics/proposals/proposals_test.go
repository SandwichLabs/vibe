package proposals

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
)

var t0 = time.Date(2026, 10, 29, 15, 30, 12, 0, time.UTC)

func metricDraft() Draft {
	return Draft{
		Kind: semantics.KindMetric, Name: "net_revenue",
		Definition: map[string]any{
			"query": "net_revenue_base", "measure": "sum(amount)", "dimensions": []any{"region", "month"},
			"title": "Net revenue", "assertions": []any{map[string]any{"kind": "non_negative"}},
		},
		Rationale: "Refunds are excluded, per the finance thread.",
		Evidence:  []string{"receipt:abc123"},
	}
}

func defOf(d *Draft) map[string]any {
	m, _ := d.Definition.(map[string]any)
	return m
}

func mustBuild(t *testing.T, d Draft) *Proposal {
	t.Helper()
	p, err := Build(d, t0, ByAgent)
	require.NoError(t, err)
	return p
}

type storeCase struct {
	name string
	new  func(t *testing.T) (Store, semantics.Store)
}

func storeCases() []storeCase {
	return []storeCase{
		{"mock", func(*testing.T) (Store, semantics.Store) { return &Mock{}, &semantics.Mock{} }},
		{"file", func(t *testing.T) (Store, semantics.Store) {
			root := t.TempDir()
			return NewFileStore(root), semantics.NewFileStore(root)
		}},
	}
}

func TestBuild_CanonicalAndShaped(t *testing.T) {
	p := mustBuild(t, metricDraft())
	assert.Regexp(t, `^20261029T153012Z-metric-net_revenue-[0-9a-f]{4}$`, p.ID)
	assert.True(t, ValidID(p.ID))
	assert.Equal(t, StatusOpen, p.Status)
	assert.Equal(t, ByAgent, p.ProposedBy)
	assert.Equal(t, "2026-10-29T15:30:12Z", p.ProposedAt)
	assert.Equal(t, "semantics/metrics/net_revenue.yaml", p.Target())
	text, err := p.Render()
	require.NoError(t, err)
	// the canonical file decodes with the store's own decoder
	m, err := semantics.DecodeMetric("net_revenue", []byte(text))
	require.NoError(t, err)
	assert.Equal(t, "sum(amount)", m.Measure)
	assert.Equal(t, []string{"region", "month"}, m.Dimensions)
	assert.Equal(t, 1, m.Version, "version defaults to 1")

	// the proposal file round trips and renders the same bytes
	data, err := Encode(p)
	require.NoError(t, err)
	back, err := Decode(p.ID, data)
	require.NoError(t, err)
	again, err := back.Render()
	require.NoError(t, err)
	assert.Equal(t, text, again)
}

func TestBuild_RefusesWhatTheKindsOwnDecoderRefuses(t *testing.T) {
	cases := map[string]func(d *Draft){
		"unknown key":        func(d *Draft) { defOf(d)["colour"] = "red" },
		"measure rule":       func(d *Draft) { defOf(d)["measure"] = "sum(a) FROM secrets" },
		"denied function":    func(d *Draft) { defOf(d)["measure"] = "sum(getenv('HOME'))" },
		"reserved name":      func(d *Draft) { d.Name = "select"; defOf(d)["name"] = "select" },
		"reserved dimension": func(d *Draft) { defOf(d)["dimensions"] = []any{"order"} },
		"name mismatch":      func(d *Draft) { defOf(d)["name"] = "other" },
		"bad name":           func(d *Draft) { d.Name = "Net Revenue" },
		"no name":            func(d *Draft) { d.Name = "" },
		"bad query id":       func(d *Draft) { defOf(d)["query"] = "../x" },
		"bad format":         func(d *Draft) { defOf(d)["format"] = "euro" },
		"no rationale":       func(d *Draft) { d.Rationale = "  " },
		"long rationale":     func(d *Draft) { d.Rationale = strings.Repeat("x", MaxRationale+1) },
		"hidden rationale":   func(d *Draft) { d.Rationale = "ok‮" },
		"many evidence":      func(d *Draft) { d.Evidence = make([]string, MaxEvidence+1) },
		"bad kind":           func(d *Draft) { d.Kind = "skill" },
		"not an object":      func(d *Draft) { d.Definition = "sum(a)" },
		"control in value":   func(d *Draft) { defOf(d)["description"] = "a\u0007b" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := metricDraft()
			mutate(&d)
			_, err := Build(d, t0, ByAgent)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
	// evidence entries must be non-empty text
	d := metricDraft()
	d.Evidence = []string{""}
	_, err := Build(d, t0, ByAgent)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestBuild_EntityJoinGlossary(t *testing.T) {
	e := mustBuild(t, Draft{Kind: semantics.KindEntity, Name: "customer", Rationale: "r", Definition: map[string]any{
		"grain": "one row per customer", "table": "prod.public.customers", "id_column": "customer_id",
		"columns": []any{map[string]any{"name": "email", "pii": true}},
	}})
	assert.Equal(t, "semantics/entities/customer.yaml", e.Target())

	j := mustBuild(t, Draft{Kind: semantics.KindJoin, Name: "orders_customers", Rationale: "r", Definition: map[string]any{
		"left": "orders", "right": "customer", "cardinality": "many_to_one",
		"on": []any{map[string]any{"left": "customer_id", "right": "customer_id"}},
	}})
	assert.Equal(t, "semantics/joins/orders_customers.yaml", j.Target())

	// measured is written only by a profiling run
	_, err := Build(Draft{Kind: semantics.KindJoin, Name: "orders_customers", Rationale: "r", Definition: map[string]any{
		"left": "orders", "right": "customer", "cardinality": "many_to_one",
		"on":       []any{map[string]any{"left": "customer_id", "right": "customer_id"}},
		"measured": map[string]any{"at": "2026-10-29T15:30:12Z", "left_rows": 1, "right_rows": 1, "max_fanout": 1, "left_orphans": 0, "right_orphans": 0, "verdict": "consistent"},
	}}, t0, ByAgent)
	assert.ErrorIs(t, err, ErrInvalid)
	assert.Contains(t, err.Error(), "profiling run")

	g := mustBuild(t, Draft{Kind: semantics.KindGlossary, Name: "ignored", Rationale: "r", Definition: map[string]any{"term": "Net revenue", "text": "Gross less refunds."}})
	assert.Equal(t, "net_revenue", g.Name)
	assert.Equal(t, "semantics/glossary.md", g.Target())
	text, err := g.Render()
	require.NoError(t, err)
	assert.Equal(t, "## Net revenue\n\nGross less refunds.\n", text)

	for name, def := range map[string]any{
		"extra key": map[string]any{"term": "a", "text": "b", "x": 1},
		"heading":   map[string]any{"term": "a", "text": "b\n## c\nd"},
		"no text":   map[string]any{"term": "a"},
		"not map":   "a",
	} {
		_, err := Build(Draft{Kind: semantics.KindGlossary, Rationale: "r", Definition: def}, t0, ByAgent)
		assert.ErrorIs(t, err, ErrInvalid, name)
	}
}

func TestDecode_Strict(t *testing.T) {
	p := mustBuild(t, metricDraft())
	good, err := Encode(p)
	require.NoError(t, err)
	s := string(good)
	bad := map[string]string{
		"unknown key":    s + "extra: 1\n",
		"anchor":         strings.Replace(s, "rationale:", "rationale: &a", 1),
		"wrong id":       strings.Replace(s, p.ID, "20261029T153012Z-metric-net_revenue-0000", 1),
		"status":         strings.Replace(s, "status: open", "status: maybe", 1),
		"accepted no at": strings.Replace(s, "status: open", "status: accepted", 1),
		"open with at":   strings.Replace(s, "decided_at: \"\"", "decided_at: 2026-10-29T15:30:12Z", 1),
		"by":             strings.Replace(s, "proposed_by: agent", "proposed_by: robot", 1),
		"edited measure": strings.Replace(s, "sum(amount)", "sum(amount) FROM x", 1),
		"edited key":     strings.Replace(s, "query: net_revenue_base", "query: net_revenue_base\n  colour: red", 1),
		"oversize":       s + "#" + strings.Repeat("x", MaxBytes),
		"two docs":       s + "---\nversion: 1\n",
	}
	for name, doc := range bad {
		_, err := Decode(p.ID, []byte(doc))
		assert.ErrorIs(t, err, ErrInvalid, name)
	}
}

func TestIDsAreValidatedAndNeverPaths(t *testing.T) {
	for _, id := range []string{"", "../../etc/passwd", "x", "20261029T153012Z-metric-net_revenue-7f3a/../x", "20261029T153012Z-skill-a-0000",
		"20261029T153012Z-metric-A-0000", "20261029T153012Z-metric-a-00000", "20261029T153012Z-metric-a-zzzz", "20261029T153012Z-metric-a-0000.yaml"} {
		assert.False(t, ValidID(id), id)
	}
	for _, c := range storeCases() {
		t.Run(c.name, func(t *testing.T) {
			props, sem := c.new(t)
			_, err := props.Get(context.Background(), "../../../etc/passwd")
			assert.ErrorIs(t, err, ErrInvalid)
			_, err = Accept(context.Background(), props, sem, "../x", AcceptOptions{ShownRev: "x"})
			assert.ErrorIs(t, err, ErrInvalid)
			assert.ErrorIs(t, Reject(context.Background(), props, "../x"), ErrInvalid)
			_, err = Show(context.Background(), props, sem, "20261029T153012Z-metric-a-0000")
			assert.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func TestStore_CreateListDecide(t *testing.T) {
	ctx := context.Background()
	for _, c := range storeCases() {
		t.Run(c.name, func(t *testing.T) {
			props, _ := c.new(t)
			list, err := props.List(ctx)
			require.NoError(t, err)
			assert.NotNil(t, list)
			assert.Empty(t, list)

			a := mustBuild(t, metricDraft())
			require.NoError(t, props.Create(ctx, a))
			assert.Error(t, props.Create(ctx, a), "an id is never reused")
			b := mustBuild(t, Draft{Kind: semantics.KindGlossary, Rationale: "r", Definition: map[string]any{"term": "ARR", "text": "x"}})
			require.NoError(t, props.Create(ctx, b))

			n, err := props.OpenCount(ctx)
			require.NoError(t, err)
			assert.Equal(t, 2, n)

			require.NoError(t, Reject(ctx, props, a.ID))
			n, _ = props.OpenCount(ctx)
			assert.Equal(t, 1, n)
			got, err := props.Get(ctx, a.ID)
			require.NoError(t, err)
			assert.Equal(t, StatusRejected, got.Status)
			assert.NotEmpty(t, got.DecidedAt)
			assert.ErrorIs(t, Reject(ctx, props, a.ID), ErrNotOpen)
			assert.ErrorIs(t, props.Decide(ctx, b.ID, "maybe", ""), ErrInvalid)
		})
	}
}

// seed files n proposals under made-up ids straight into the mock, decided or
// open, without paying for a listing per Create.
func seed(t *testing.T, props *Mock, n int, status string) {
	t.Helper()
	d := metricDraft()
	d.Name = "m"
	base := mustBuild(t, d)
	for i := 0; i < n; i++ {
		p := *base
		p.ID = "20260101T000000Z-metric-m-" + fourHex(i)
		if status != StatusOpen {
			p.Status, p.DecidedAt = status, "2026-01-02T00:00:00Z"
		}
		data, err := Encode(&p)
		require.NoError(t, err)
		props.SetFile(p.ID, data)
	}
}

func fourHex(i int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[(i>>12)&15], digits[(i>>8)&15], digits[(i>>4)&15], digits[i&15]})
}

func TestStore_CapOfOpenProposals(t *testing.T) {
	ctx := context.Background()
	props := &Mock{}
	seed(t, props, MaxOpen, StatusOpen)
	err := props.Create(ctx, mustBuild(t, metricDraft()))
	assert.ErrorIs(t, err, ErrTooMany)
	assert.Contains(t, err.Error(), "accept or reject some first")
}

func TestStore_PrunesOnlyDecidedHistory(t *testing.T) {
	ctx := context.Background()
	props := &Mock{}
	seed(t, props, MaxFiles, StatusRejected)
	open := mustBuild(t, metricDraft())
	require.NoError(t, props.Create(ctx, open))
	list, err := props.List(ctx)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(list), PruneTo+1)
	_, err = props.Get(ctx, open.ID)
	assert.NoError(t, err, "an open proposal is never pruned")
	n, _ := props.OpenCount(ctx)
	assert.Equal(t, 1, n)
}

func TestList_UnreadableFileIsListedWithItsError(t *testing.T) {
	ctx := context.Background()
	props := &Mock{}
	props.SetFile("20261029T153012Z-metric-net_revenue-7f3a", []byte("version: 1\nnope: [\n"))
	props.SetFile("not-an-id", []byte("x"))
	list, err := props.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	for _, p := range list {
		assert.NotEmpty(t, p.Error)
		assert.False(t, p.Open())
	}
	n, _ := props.OpenCount(ctx)
	assert.Zero(t, n)
}

func TestFileStore_LivesInLocalDirAndNeverUnderSemantics(t *testing.T) {
	root := t.TempDir()
	props := NewFileStore(root)
	p := mustBuild(t, metricDraft())
	require.NoError(t, props.Create(context.Background(), p))
	_, err := os.Stat(filepath.Join(root, ".vibe-local", "proposals", p.ID+".yaml"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "semantics"))
	assert.True(t, os.IsNotExist(err), "proposing writes nothing under semantics/")
}

func TestAccept_NewMetricWritesExactBytes(t *testing.T) {
	ctx := context.Background()
	for _, c := range storeCases() {
		t.Run(c.name, func(t *testing.T) {
			props, sem := c.new(t)
			p := mustBuild(t, metricDraft())
			require.NoError(t, props.Create(ctx, p))

			pv, err := Show(ctx, props, sem, p.ID)
			require.NoError(t, err)
			assert.False(t, pv.Exists)
			assert.False(t, pv.Stale)
			assert.Empty(t, pv.CurrentText)

			res, err := acceptSeen(t, props, sem, p.ID)
			require.NoError(t, err)
			src, err := sem.Source(ctx, semantics.KindMetric, "net_revenue")
			require.NoError(t, err)
			assert.Equal(t, res.ProposedText, src.Text, "the exact bytes shown are the bytes written")
			_, err = sem.Metric(ctx, "net_revenue")
			require.NoError(t, err)
			got, _ := props.Get(ctx, p.ID)
			assert.Equal(t, StatusAccepted, got.Status)

			_, err = acceptSeen(t, props, sem, p.ID)
			assert.ErrorIs(t, err, ErrNotOpen)
		})
	}
}

func TestAccept_StaleRevConflictsUnlessOverwrite(t *testing.T) {
	ctx := context.Background()
	for _, c := range storeCases() {
		t.Run(c.name, func(t *testing.T) {
			props, sem := c.new(t)
			// an existing metric the proposal replaces
			orig := &semantics.Metric{Name: "net_revenue", Query: "q", Measure: "sum(a)"}
			require.NoError(t, sem.PutMetric(ctx, orig))
			d := metricDraft()
			d.ReplacesRev = orig.Rev
			p := mustBuild(t, d)
			require.NoError(t, props.Create(ctx, p))

			// the person edits the file after the proposal was made
			edited := &semantics.Metric{Name: "net_revenue", Query: "q", Measure: "sum(b)", Rev: orig.Rev}
			require.NoError(t, sem.PutMetric(ctx, edited))

			pv, err := Show(ctx, props, sem, p.ID)
			require.NoError(t, err)
			assert.True(t, pv.Stale)
			assert.Contains(t, pv.CurrentText, "sum(b)")

			_, err = acceptSeen(t, props, sem, p.ID)
			var stale *StaleError
			require.ErrorAs(t, err, &stale)
			assert.ErrorIs(t, err, ErrConflict)
			assert.ErrorIs(t, err, semantics.ErrConflict)
			still, _ := sem.Metric(ctx, "net_revenue")
			assert.Equal(t, "sum(b)", still.Measure, "nothing was written")
			got, _ := props.Get(ctx, p.ID)
			assert.Equal(t, StatusOpen, got.Status)

			_, err = acceptAnyway(t, props, sem, p.ID)
			require.NoError(t, err)
			now, _ := sem.Metric(ctx, "net_revenue")
			assert.Equal(t, "sum(amount)", now.Measure)
		})
	}
}

func TestAccept_CreatedOrDeletedSinceIsStale(t *testing.T) {
	ctx := context.Background()
	props, sem := (&Mock{}), &semantics.Mock{}
	// proposed as new; the file appeared meanwhile
	p := mustBuild(t, metricDraft())
	require.NoError(t, props.Create(ctx, p))
	require.NoError(t, sem.PutMetric(ctx, &semantics.Metric{Name: "net_revenue", Query: "q", Measure: "sum(a)"}))
	_, err := acceptSeen(t, props, sem, p.ID)
	var stale *StaleError
	require.ErrorAs(t, err, &stale)
	assert.Equal(t, "was created", stale.Reason)

	// proposed against a revision; the file was deleted meanwhile
	cur, _ := sem.Metric(ctx, "net_revenue")
	d := metricDraft()
	d.ReplacesRev = cur.Rev
	q := mustBuild(t, d)
	require.NoError(t, props.Create(ctx, q))
	require.NoError(t, sem.DeleteMetric(ctx, "net_revenue"))
	_, err = acceptSeen(t, props, sem, q.ID)
	require.ErrorAs(t, err, &stale)
	assert.Equal(t, "was deleted", stale.Reason)
	_, err = acceptAnyway(t, props, sem, q.ID)
	require.NoError(t, err)
}

func TestAccept_GlossaryAppendsAndReplaces(t *testing.T) {
	ctx := context.Background()
	for _, c := range storeCases() {
		t.Run(c.name, func(t *testing.T) {
			props, sem := c.new(t)
			add := func(term, text, rev string) *Proposal {
				p := mustBuild(t, Draft{Kind: semantics.KindGlossary, Rationale: "r", ReplacesRev: rev,
					Definition: map[string]any{"term": term, "text": text}})
				require.NoError(t, props.Create(ctx, p))
				return p
			}
			p1 := add("Churn", "Customers who left.", "")
			_, err := acceptSeen(t, props, sem, p1.ID)
			require.NoError(t, err)
			g, _ := sem.Glossary(ctx)
			assert.Equal(t, "## Churn\n\nCustomers who left.\n", g.Text)

			p2 := add("ARR", "Annual recurring revenue.", "")
			_, err = acceptSeen(t, props, sem, p2.ID)
			require.NoError(t, err)
			g, _ = sem.Glossary(ctx)
			assert.Equal(t, []string{"Churn", "ARR"}, g.Terms)

			p3 := add("churn", "Left within 30 days.", g.Rev)
			_, err = acceptSeen(t, props, sem, p3.ID)
			require.NoError(t, err)
			g, _ = sem.Glossary(ctx)
			assert.Equal(t, "## churn\n\nLeft within 30 days.\n\n## ARR\n\nAnnual recurring revenue.\n", g.Text)

			// a stale glossary revision conflicts, and overwrite still keeps the other terms
			p4 := add("ARR", "Annual recurring revenue, net.", "stale")
			_, err = acceptSeen(t, props, sem, p4.ID)
			assert.ErrorIs(t, err, ErrConflict)
			_, err = acceptAnyway(t, props, sem, p4.ID)
			require.NoError(t, err)
			g, _ = sem.Glossary(ctx)
			assert.Equal(t, []string{"churn", "ARR"}, g.Terms)
			assert.Contains(t, g.Text, "Annual recurring revenue, net.")
		})
	}
}

func TestAccept_HandEditedProposalCannotProduceAnInvalidFile(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	p := mustBuild(t, metricDraft())
	data, err := Encode(p)
	require.NoError(t, err)
	props.SetFile(p.ID, []byte(strings.Replace(string(data), "sum(amount)", "sum(amount) FROM secrets", 1)))
	_, err = Accept(ctx, props, sem, p.ID, AcceptOptions{ShownRev: "x"})
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = sem.Source(ctx, semantics.KindMetric, "net_revenue")
	assert.ErrorIs(t, err, semantics.ErrNotFound, "nothing was written")
}

func TestShow_GlossaryIsDiffedAgainstTheCurrentFile(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	sem.SetFile("glossary.md", []byte("# Terms\n\n## A\n\nold\n"))
	p := mustBuild(t, Draft{Kind: semantics.KindGlossary, Rationale: "r", Definition: map[string]any{"term": "B", "text": "new"}})
	require.NoError(t, props.Create(ctx, p))
	pv, err := Show(ctx, props, sem, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "# Terms\n\n## A\n\nold\n", pv.CurrentText)
	assert.Equal(t, "# Terms\n\n## A\n\nold\n\n## B\n\nnew\n", pv.ProposedText)
	assert.False(t, pv.Stale, "a new term is not stale because the glossary has other terms")
}

func TestDiff(t *testing.T) {
	d := Diff("a\nb\nc\n", "a\nB\nc\nd\n")
	assert.Equal(t, []DiffLine{{' ', "a"}, {'-', "b"}, {'+', "B"}, {' ', "c"}, {'+', "d"}}, d)
	assert.Equal(t, "", Unified(Diff("same\n", "same\n"), 3))
	assert.Equal(t, "+ x\n+ y\n", Unified(Diff("", "x\ny\n"), 3))
	long := strings.Repeat("l\n", 30)
	u := Unified(Diff(long+"old\n"+long, long+"new\n"+long), 1)
	assert.Equal(t, "...\n  l\n- old\n+ new\n  l\n", u)
	// too large to compare: still correct, only blunt
	big := strings.Repeat("x\n", 2100)
	d = Diff(big, big+"y\n")
	assert.Equal(t, DiffLine{'+', "y"}, d[len(d)-1])
}

func acceptAnyway(t *testing.T, props Store, sem semantics.Store, id string) (*Preview, error) {
	t.Helper()
	pv, err := Show(context.Background(), props, sem, id)
	require.NoError(t, err)
	return Accept(context.Background(), props, sem, id, AcceptOptions{Overwrite: true, ShownRev: pv.ShownRev})
}

func TestAccept_NewGlossaryTermsDoNotGoStaleOnEachOther(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	sem.SetFile("glossary.md", []byte("## A\n\nold\n"))
	add := func(term string) *Proposal {
		p := mustBuild(t, Draft{Kind: semantics.KindGlossary, Rationale: "r", ReplacesRev: "",
			Definition: map[string]any{"term": term, "text": "x"}})
		require.NoError(t, props.Create(ctx, p))
		return p
	}
	p1, p2 := add("B"), add("C") // both made against the same glossary revision
	_, err := acceptSeen(t, props, sem, p1.ID)
	require.NoError(t, err)
	_, err = acceptSeen(t, props, sem, p2.ID)
	require.NoError(t, err, "no --overwrite needed: the glossary changed, not term C")
	g, _ := sem.Glossary(ctx)
	assert.Equal(t, []string{"A", "B", "C"}, g.Terms)

	// but a term that now exists is stale
	p3 := add("D")
	p4 := add("d")
	_, err = acceptSeen(t, props, sem, p3.ID)
	require.NoError(t, err)
	_, err = acceptSeen(t, props, sem, p4.ID)
	assert.ErrorIs(t, err, ErrConflict)
}

func TestShow_RefusesAGlossaryWithAnUnclosedFence(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	sem.SetFile("glossary.md", []byte("## A\n\n```\ncode never closed\n"))
	p := mustBuild(t, Draft{Kind: semantics.KindGlossary, Rationale: "r", Definition: map[string]any{"term": "B", "text": "x"}})
	require.NoError(t, props.Create(ctx, p))
	_, err := Show(ctx, props, sem, p.ID)
	require.ErrorIs(t, err, ErrUnclosedFence)
	assert.Contains(t, err.Error(), "glossary.md has an unclosed ``` fence; fix it first")
	_, err = Accept(ctx, props, sem, p.ID, AcceptOptions{ShownRev: "x"})
	assert.ErrorIs(t, err, ErrUnclosedFence)
	g, _ := sem.Glossary(ctx)
	assert.Equal(t, "## A\n\n```\ncode never closed\n", g.Text)
}

// A crash between writing the file and marking the proposal leaves the target
// reading as proposed: accepting again only marks it.
func TestAccept_AlreadyWrittenOnlyMarksAccepted(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	p := mustBuild(t, metricDraft())
	require.NoError(t, props.Create(ctx, p))
	pv, err := Show(ctx, props, sem, p.ID)
	require.NoError(t, err)
	_, err = sem.PutSource(ctx, semantics.KindMetric, "net_revenue", []byte(pv.ProposedText))
	require.NoError(t, err) // the write landed, the mark did not
	before, _ := sem.Source(ctx, semantics.KindMetric, "net_revenue")

	_, err = acceptSeen(t, props, sem, p.ID)
	require.NoError(t, err, "not a conflict: it already reads as proposed")
	after, _ := sem.Source(ctx, semantics.KindMetric, "net_revenue")
	assert.Equal(t, before.Rev, after.Rev, "not written again")
	got, _ := props.Get(ctx, p.ID)
	assert.Equal(t, StatusAccepted, got.Status)
}

func TestAccept_PinsWhatThePersonSaw(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	orig := &semantics.Metric{Name: "net_revenue", Query: "q", Measure: "sum(a)"}
	require.NoError(t, sem.PutMetric(ctx, orig))
	d := metricDraft()
	d.ReplacesRev = orig.Rev
	p := mustBuild(t, d)
	require.NoError(t, props.Create(ctx, p))
	pv, err := Show(ctx, props, sem, p.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, pv.ShownRev)

	// overwrite without the pin is refused outright
	_, err = Accept(ctx, props, sem, p.ID, AcceptOptions{Overwrite: true})
	assert.ErrorIs(t, err, ErrInvalid)

	// the file moves after the person looked: refused even with overwrite
	edited := &semantics.Metric{Name: "net_revenue", Query: "q", Measure: "sum(b)", Meta: semantics.Meta{Rev: orig.Rev}}
	require.NoError(t, sem.PutMetric(ctx, edited))
	_, err = Accept(ctx, props, sem, p.ID, AcceptOptions{Overwrite: true, ShownRev: pv.ShownRev})
	var again *ChangedAgainError
	require.ErrorAs(t, err, &again)
	assert.ErrorIs(t, err, ErrConflict)
	cur, _ := sem.Metric(ctx, "net_revenue")
	assert.Equal(t, "sum(b)", cur.Measure)

	// looking again gives a new pin that works
	pv2, err := Show(ctx, props, sem, p.ID)
	require.NoError(t, err)
	assert.NotEqual(t, pv.ShownRev, pv2.ShownRev)
	_, err = Accept(ctx, props, sem, p.ID, AcceptOptions{Overwrite: true, ShownRev: pv2.ShownRev})
	require.NoError(t, err)
}

type queryLookup map[string]bool

func (q queryLookup) Get(_ context.Context, id string) (*queries.Query, error) {
	if q[id] {
		return &queries.Query{ID: id}, nil
	}
	return nil, queries.ErrNotFound
}

func TestShow_Warnings(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}

	// a metric whose saved query is gone
	p := mustBuild(t, metricDraft())
	require.NoError(t, props.Create(ctx, p))
	pv, err := Show(ctx, props, sem, p.ID, WithQueries(queryLookup{}))
	require.NoError(t, err)
	require.Len(t, pv.Warnings, 1)
	assert.Contains(t, pv.Warnings[0], `saved query "net_revenue_base" no longer exists`)
	pv, _ = Show(ctx, props, sem, p.ID, WithQueries(queryLookup{"net_revenue_base": true}))
	assert.Empty(t, pv.Warnings)

	// a hand-written file with comments is replaced
	sem.SetFile("metrics/net_revenue.yaml", []byte("# mine\nversion: 1\nname: net_revenue\nquery: q\nmeasure: sum(a)\n"))
	cur, _ := sem.Source(ctx, semantics.KindMetric, "net_revenue")
	d := metricDraft()
	d.ReplacesRev = cur.Rev
	p2 := mustBuild(t, d)
	require.NoError(t, props.Create(ctx, p2))
	pv, err = Show(ctx, props, sem, p2.ID)
	require.NoError(t, err)
	require.Len(t, pv.Warnings, 1)
	assert.Contains(t, pv.Warnings[0], "comments will be lost")

	// a measured join profile is dropped
	sem.SetFile("joins/orders_customers.yaml", []byte("version: 1\nname: orders_customers\nleft: orders\nright: customers\non: [{left: a, right: a}]\ncardinality: many_to_one\nmeasured: {at: 2026-10-29T15:30:12Z, left_rows: 1, right_rows: 1, max_fanout: 1, left_orphans: 0, right_orphans: 0, verdict: consistent}\n"))
	jc, _ := sem.Source(ctx, semantics.KindJoin, "orders_customers")
	jp := mustBuild(t, Draft{Kind: semantics.KindJoin, Name: "orders_customers", Rationale: "r", ReplacesRev: jc.Rev, Definition: map[string]any{
		"left": "orders", "right": "customers", "cardinality": "one_to_many", "on": []any{map[string]any{"left": "a", "right": "a"}}}})
	require.NoError(t, props.Create(ctx, jp))
	pv, err = Show(ctx, props, sem, jp.ID)
	require.NoError(t, err)
	require.Len(t, pv.Warnings, 1)
	assert.Contains(t, pv.Warnings[0], "measured block")
}

func acceptSeen(t *testing.T, props Store, sem semantics.Store, id string) (*Preview, error) {
	t.Helper()
	pv, err := Show(context.Background(), props, sem, id)
	require.NoError(t, err)
	return Accept(context.Background(), props, sem, id, AcceptOptions{ShownRev: pv.ShownRev})
}

func TestAccept_PinIsMandatoryAndCoversTheProposalFile(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	p := mustBuild(t, metricDraft())
	require.NoError(t, props.Create(ctx, p))
	pv, err := Show(ctx, props, sem, p.ID)
	require.NoError(t, err)

	_, err = Accept(ctx, props, sem, p.ID, AcceptOptions{})
	require.ErrorIs(t, err, ErrInvalid)
	assert.Contains(t, err.Error(), "needs the rev of the version you looked at")

	// the proposal is edited on disk after it was shown (sum -> max)
	data, _ := Encode(p)
	props.SetFile(p.ID, []byte(strings.Replace(string(data), "sum(amount)", "max(amount)", 1)))
	_, err = Accept(ctx, props, sem, p.ID, AcceptOptions{ShownRev: pv.ShownRev})
	var again *ChangedAgainError
	require.ErrorAs(t, err, &again)
	_, err = sem.Source(ctx, semantics.KindMetric, "net_revenue")
	assert.ErrorIs(t, err, semantics.ErrNotFound, "the unseen edit was not written")

	// a wrong pin never ends as accepted, even when the file already reads as proposed
	pv2, err := Show(ctx, props, sem, p.ID)
	require.NoError(t, err)
	_, err = sem.PutSource(ctx, semantics.KindMetric, "net_revenue", []byte(pv2.ProposedText))
	require.NoError(t, err)
	_, err = Accept(ctx, props, sem, p.ID, AcceptOptions{ShownRev: "wrong"})
	require.ErrorAs(t, err, &again)
	got, _ := props.Get(ctx, p.ID)
	assert.Equal(t, StatusOpen, got.Status)
}

func TestAccept_DecideComparesTheProposalFile(t *testing.T) {
	ctx := context.Background()
	props := &Mock{}
	p := mustBuild(t, metricDraft())
	require.NoError(t, props.Create(ctx, p))
	got, _ := props.Get(ctx, p.ID)
	assert.ErrorIs(t, props.Decide(ctx, p.ID, StatusRejected, "stale-rev"), ErrConflict)
	assert.NoError(t, props.Decide(ctx, p.ID, StatusRejected, got.Rev))
}

func TestAccept_GlossaryReplacementOfAGoneTermIsStale(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	sem.SetFile("glossary.md", []byte("## A\n\nold\n\n## B\n\nb\n"))
	g, _ := sem.Glossary(ctx)
	p := mustBuild(t, Draft{Kind: semantics.KindGlossary, Rationale: "r", ReplacesRev: g.Rev, Definition: map[string]any{"term": "A", "text": "new"}})
	require.NoError(t, props.Create(ctx, p))
	require.NoError(t, sem.PutGlossary(ctx, &semantics.Glossary{Text: "## B\n\nb\n", Meta: semantics.Meta{Rev: g.Rev}}))
	_, err := acceptSeen(t, props, sem, p.ID)
	var stale *StaleError
	require.ErrorAs(t, err, &stale)
	assert.Contains(t, err.Error(), "the term was deleted since this was proposed")
}

func TestAccept_EmptyPinIsAPlainSentence(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	p := mustBuild(t, metricDraft())
	data, err := Encode(p)
	require.NoError(t, err)
	props.SetFile(p.ID, data)
	_, err = Accept(ctx, props, sem, p.ID, AcceptOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalid, "callers can still tell it from a conflict")
	assert.Equal(t, "accepting needs the rev of the version you looked at (shown_rev from Show)", err.Error())
}
