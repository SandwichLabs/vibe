package agents

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/semantics"
	"github.com/SandwichLabs/slaw/internal/semantics/proposals"
)

func semanticsFixture(t *testing.T, mode SandboxMode, opts ...fixtureOpt) *policyFixture {
	t.Helper()
	fx := newPolicyFixture(t, mode, opts...)
	fx.exec(`CREATE TABLE sales (region VARCHAR, month INTEGER, amount INTEGER);
		INSERT INTO sales VALUES ('east', 1, 10), ('east', 2, 20), ('west', 1, 5)`)
	putSaved(t, fx, "net_revenue_base", "SELECT region, month, amount FROM sales WHERE amount > $min;\n",
		queries.Param{Name: "min", Type: "number", Default: "2"})
	return fx
}

func metricProposal(name string) map[string]any {
	return map[string]any{
		"kind": "metric", "name": name, "rationale": "Refunds are excluded, per the finance thread.",
		"evidence": []any{"saved query net_revenue_base"},
		"definition": map[string]any{
			"query": "net_revenue_base", "measure": "sum(amount)", "dimensions": []any{"region", "month"}, "title": "Net revenue",
		},
	}
}

func semProposals(t *testing.T, fx *policyFixture) []proposals.Proposal {
	t.Helper()
	list, err := fx.h.access.Proposals().List(context.Background())
	require.NoError(t, err)
	return list
}

// The three tools are in the registry with the tiers the owner chose: the two
// readers are introspect (a read-only agent can use metrics), the proposer is
// content (read-write and full).
func TestDefinitionTools_Categories(t *testing.T) {
	assert.Equal(t, "introspect", GetTool("list_definitions").Category)
	assert.Equal(t, "introspect", GetTool("get_definition").Category)
	assert.Equal(t, "content", GetTool("propose_definition").Category)
	assert.True(t, GetTool("propose_definition").DryRunSafe)
	for _, name := range []string{"list_definitions", "get_definition", "propose_definition"} {
		assert.Empty(t, GetTool(name).Modes, "%s is offered in chat, MCP and task mode alike", name)
	}
}

func TestDefinitionTools_TierGating(t *testing.T) {
	for _, mode := range []SandboxMode{ModeReadOnly, ModeReadWrite, ModeFull} {
		fx := semanticsFixture(t, mode)
		resp := fx.call("list_definitions", map[string]any{})
		require.Nil(t, resp.Error, "%s: %s", mode, errMsg(resp))
		resp = fx.call("get_definition", map[string]any{"kind": "glossary"})
		require.Nil(t, resp.Error, "%s: %s", mode, errMsg(resp))

		resp = fx.call("propose_definition", metricProposal("net_revenue"))
		if mode == ModeReadOnly {
			require.NotNil(t, resp.Error)
			assert.Equal(t, ErrPermission, resp.Error.Code)
			assert.Contains(t, resp.Error.Message, "not allowed in read-only mode")
			assert.Empty(t, semProposals(t, fx))
			continue
		}
		require.Nil(t, resp.Error, "%s: %s", mode, errMsg(resp))
	}
}

func TestProposeDefinition_FilesAProposalAndChangesNothingElse(t *testing.T) {
	fx := semanticsFixture(t, ModeReadWrite)
	resp := fx.call("propose_definition", metricProposal("net_revenue"))
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, true, res["proposed"])
	assert.Equal(t, "open", res["status"])
	assert.Equal(t, false, res["replaces"])
	assert.Regexp(t, `^\d{8}T\d{6}Z-metric-net_revenue-[0-9a-f]{4}$`, res["id"])
	assert.Contains(t, res["note"], "Nothing in semantics/ has changed")

	list := semProposals(t, fx)
	require.Len(t, list, 1)
	assert.Equal(t, res["id"], list[0].ID)
	assert.Equal(t, proposals.ByAgent, list[0].ProposedBy)
	assert.Equal(t, []string{"saved query net_revenue_base"}, list[0].Evidence)
	text, err := list[0].Render()
	require.NoError(t, err)
	assert.Contains(t, text, "measure: sum(amount)")

	ms, err := fx.h.access.Semantics().Metrics(context.Background())
	require.NoError(t, err)
	assert.Empty(t, ms, "nothing in semantics/ changed")

	// list_definitions counts it
	res = mapOf(t, fx.call("list_definitions", map[string]any{}).Result)
	assert.Equal(t, map[string]any{"open": 1}, res["proposals"])
}

func TestProposeDefinition_ApprovalCardSaysNothingChangesUntilAccepted(t *testing.T) {
	fx := semanticsFixture(t, ModeReadWrite, withAsk(true), withApprover())
	resp := fx.call("propose_definition", metricProposal("net_revenue"))
	require.Nil(t, resp.Error, errMsg(resp))
	require.Equal(t, 1, fx.approver.count())
	card := fx.approver.asked[0]
	assert.Equal(t, "propose_definition", card.Tool)
	assert.Contains(t, card.Summary, "goes to your proposals inbox; nothing in semantics/ changes until you accept it")
	assert.Contains(t, card.Summary, `Propose metric "net_revenue"`, "a new definition is not 'a change to'")
	assert.NotContains(t, card.Summary, "a change to")
	assert.Equal(t, "sum(amount)", card.SQL)
	assert.False(t, card.Destructive)

	// An existing definition is "a change to"
	m, _ := semantics.DecodeMetric("net_revenue", []byte("version: 1\nname: net_revenue\nquery: net_revenue_base\nmeasure: sum(a)\n"))
	require.NoError(t, writableSemantics(fx).PutMetric(context.Background(), m))
	p := metricProposal("net_revenue")
	p["replaces_rev"] = m.Rev
	require.Nil(t, fx.call("propose_definition", p).Error)
	assert.Contains(t, fx.approver.asked[1].Summary, `Propose a change to metric "net_revenue"`)

	// A declined card files nothing.
	fx2 := semanticsFixture(t, ModeReadWrite, withAsk(true), withApprover())
	fx2.approver.decision = ErrApprovalDeclined
	resp = fx2.call("propose_definition", metricProposal("net_revenue"))
	require.NotNil(t, resp.Error)
	assert.Empty(t, semProposals(t, fx2))
}

func TestProposeDefinition_HeadlessIsValidatedThenNeedsApproval(t *testing.T) {
	fx := semanticsFixture(t, ModeReadWrite, withAsk(true))
	resp := fx.call("propose_definition", metricProposal("net_revenue"))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "valid and ready")
	assert.Contains(t, resp.Error.Message, "nothing in semantics/ changes until the person accepts it")
	assert.Empty(t, semProposals(t, fx))

	bad := metricProposal("net_revenue")
	badDef, _ := bad["definition"].(map[string]any)
	badDef["measure"] = "sum(amount) FROM secrets"
	resp = fx.call("propose_definition", bad)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
}

func TestProposeDefinition_Refusals(t *testing.T) {
	mut := func(f func(p map[string]any)) map[string]any {
		p := metricProposal("net_revenue")
		f(p)
		return p
	}
	def := func(p map[string]any) map[string]any { d, _ := p["definition"].(map[string]any); return d }
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"measure rule", mut(func(p map[string]any) { def(p)["measure"] = "sum(a) FROM secrets" }), "measure may not use FROM"},
		{"denied function", mut(func(p map[string]any) { def(p)["measure"] = "sum(getenv('HOME'))" }), "getenv"},
		{"sleep_ms", mut(func(p map[string]any) { def(p)["measure"] = "sum(sleep_ms(5))" }), "sleep_ms"},
		{"reserved word", mut(func(p map[string]any) { def(p)["dimensions"] = []any{"order"} }), "reserved SQL keyword"},
		{"unknown field", mut(func(p map[string]any) { def(p)["colour"] = "red" }), "colour"},
		{"bad kind", mut(func(p map[string]any) { p["kind"] = "skill" }), "kind must be"},
		{"bad name", mut(func(p map[string]any) { p["name"] = "Net Revenue" }), "name"},
		{"no rationale", mut(func(p map[string]any) { p["rationale"] = " " }), "rationale is required"},
		{"missing query", mut(func(p map[string]any) { def(p)["query"] = "nope" }), `saved query "nope" does not exist`},
		{"definition not object", mut(func(p map[string]any) { p["definition"] = "sum(a)" }), "definition must be an object"},
		{"evidence not strings", mut(func(p map[string]any) { p["evidence"] = []any{1} }), "evidence must be an array of strings"},
		{"id from the agent is ignored", mut(func(p map[string]any) { p["id"] = "x" }), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := semanticsFixture(t, ModeReadWrite)
			resp := fx.call("propose_definition", c.params)
			if c.want == "" {
				require.Nil(t, resp.Error, errMsg(resp))
				assert.NotEqual(t, "x", mapOf(t, resp.Result)["id"])
				return
			}
			require.NotNil(t, resp.Error)
			assert.Equal(t, ErrInvalidParams, resp.Error.Code)
			assert.Contains(t, resp.Error.Message, c.want)
			assert.NotContains(t, resp.Error.Message, fx.dir, "no path of the workspace reaches the agent")
			assert.Empty(t, semProposals(t, fx))
		})
	}
}

func TestProposeDefinition_ReplacesNeedsTheRevTheAgentRead(t *testing.T) {
	fx := semanticsFixture(t, ModeReadWrite)
	m, err := semantics.DecodeMetric("net_revenue", []byte("version: 1\nname: net_revenue\nquery: net_revenue_base\nmeasure: sum(amount)\n"))
	require.NoError(t, err)
	require.NoError(t, writableSemantics(fx).PutMetric(context.Background(), m))

	// no rev: it would replace something the agent never read
	resp := fx.call("propose_definition", metricProposal("net_revenue"))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "already exists")
	assert.Contains(t, resp.Error.Message, "replaces_rev")

	// a stale rev
	p := metricProposal("net_revenue")
	p["replaces_rev"] = "stale"
	resp = fx.call("propose_definition", p)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "changed since the rev you sent")

	// the rev get_definition returned
	got := mapOf(t, fx.call("get_definition", map[string]any{"kind": "metric", "name": "net_revenue"}).Result)
	p["replaces_rev"] = got["rev"]
	resp = fx.call("propose_definition", p)
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Equal(t, true, mapOf(t, resp.Result)["replaces"])

	// the same proposal twice is refused with the id of the first
	resp = fx.call("propose_definition", p)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "already waiting")

	// a rev for something that does not exist
	q := metricProposal("other_metric")
	q["replaces_rev"] = "abc"
	resp = fx.call("propose_definition", q)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "there is no metric named")
}

func TestProposeDefinition_Glossary(t *testing.T) {
	fx := semanticsFixture(t, ModeFull)
	p := map[string]any{"kind": "glossary", "rationale": "r", "definition": map[string]any{"term": "Churn", "text": "Customers who left."}}
	resp := fx.call("propose_definition", p)
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Equal(t, "churn", mapOf(t, resp.Result)["name"])

	// an existing term needs the glossary's rev
	sem := writableSemantics(fx)
	require.NoError(t, sem.PutGlossary(context.Background(), &semantics.Glossary{Text: "## Churn\n\nold\n"}))
	resp = fx.call("propose_definition", map[string]any{"kind": "glossary", "rationale": "r2", "definition": map[string]any{"term": "churn", "text": "new"}})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "already defines")
	got := mapOf(t, fx.call("get_definition", map[string]any{"kind": "glossary", "name": "Churn"}).Result)
	assert.Equal(t, "old", got["text"])
	resp = fx.call("propose_definition", map[string]any{"kind": "glossary", "rationale": "r2", "replaces_rev": got["rev"], "definition": map[string]any{"term": "churn", "text": "new"}})
	require.Nil(t, resp.Error, errMsg(resp))

	// a new term replaces nothing: Slaw clears replaces_rev
	resp = fx.call("propose_definition", map[string]any{"kind": "glossary", "rationale": "r3", "definition": map[string]any{"term": "ARR", "text": "x"}})
	require.Nil(t, resp.Error, errMsg(resp))
	var arr *proposals.Proposal
	for _, p := range semProposals(t, fx) {
		if p.Name == "arr" {
			arr = &p
		}
	}
	require.NotNil(t, arr)
	assert.Empty(t, arr.ReplacesRev, "a new term replaces nothing")
}

func TestProposeDefinition_RefusesPastTheCap(t *testing.T) {
	fx := semanticsFixture(t, ModeReadWrite)
	access, _ := fx.h.access.(*policyAccess)
	props, ok := access.Proposals().(*proposals.Mock)
	require.True(t, ok)
	for i := 0; i < proposals.MaxOpen; i++ {
		p, err := proposals.Build(proposals.Draft{Kind: semantics.KindGlossary, Rationale: "r",
			Definition: map[string]any{"term": "t" + strings.Repeat("x", i%7) + string(rune('a'+i%26)), "text": "x"}}, fixedNow(), proposals.ByAgent)
		require.NoError(t, err)
		p.ID = p.ID[:len(p.ID)-4] + hex4(i)
		require.NoError(t, props.Create(context.Background(), p))
	}
	for _, dry := range []bool{true, false} {
		_ = dry
		resp := fx.call("propose_definition", metricProposal("net_revenue"))
		require.NotNil(t, resp.Error)
		assert.Contains(t, resp.Error.Message, "accept or reject some first")
	}
	res := mapOf(t, fx.call("list_definitions", map[string]any{}).Result)
	assert.Equal(t, map[string]any{"open": proposals.MaxOpen}, res["proposals"])
}

func TestGetDefinition_MetricIncludesSavedQueryParameters(t *testing.T) {
	fx := semanticsFixture(t, ModeReadOnly)
	m, err := semantics.DecodeMetric("net_revenue", []byte("version: 1\nname: net_revenue\nquery: net_revenue_base\nmeasure: sum(amount)\ndimensions: [region]\n"))
	require.NoError(t, err)
	require.NoError(t, writableSemantics(fx).PutMetric(context.Background(), m))

	res := mapOf(t, fx.call("get_definition", map[string]any{"kind": "metric", "name": "net_revenue"}).Result)
	assert.Equal(t, "metric", res["kind"])
	assert.NotEmpty(t, res["rev"])
	def := mapOf(t, res["definition"])
	assert.Equal(t, "sum(amount)", def["measure"])
	assert.NotContains(t, def, "rev")
	params, ok := res["parameters"].([]queries.Param)
	require.True(t, ok)
	require.Len(t, params, 1)
	assert.Equal(t, "min", params[0].Name)
	assert.Equal(t, "2", params[0].Default)

	// a metric over a missing saved query says so
	m2, _ := semantics.DecodeMetric("orphan", []byte("version: 1\nname: orphan\nquery: gone\nmeasure: count(*)\n"))
	require.NoError(t, writableSemantics(fx).PutMetric(context.Background(), m2))
	res = mapOf(t, fx.call("get_definition", map[string]any{"kind": "metric", "name": "orphan"}).Result)
	assert.Contains(t, res["query_error"], "does not exist")

	// errors
	resp := fx.call("get_definition", map[string]any{"kind": "metric", "name": "nope"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "there is no metric named")
	resp = fx.call("get_definition", map[string]any{"kind": "skill", "name": "x"})
	require.NotNil(t, resp.Error)
	resp = fx.call("get_definition", map[string]any{"kind": "metric"})
	require.NotNil(t, resp.Error)
}

var absPath = regexp.MustCompile(`(/home|/tmp|/var|/Users)/`)

func TestDefinitionTools_UnreadableFilesAreReportedWithoutPaths(t *testing.T) {
	fx := semanticsFixture(t, ModeReadOnly)
	sem := writableSemantics(fx)
	sem.SetFile("metrics/broken.yaml", []byte("version: 1\nname: broken\nquery: q\nmeasure: sum(a) FROM t\n"))
	sem.SetFile("entities/odd.yaml", []byte("version: 1\nname: odd\n: [\n"))

	res := mapOf(t, fx.call("list_definitions", map[string]any{}).Result)
	metrics, ok := res["metrics"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, metrics, 1)
	assert.Equal(t, "broken", metrics[0]["name"])
	assert.Contains(t, metrics[0]["error"], "measure may not use FROM")
	ents, _ := res["entities"].([]map[string]any)
	require.Len(t, ents, 1)
	assert.NotEmpty(t, ents[0]["error"])

	resp := fx.call("get_definition", map[string]any{"kind": "metric", "name": "broken"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "the metric cannot be read: semantics/metrics/broken.yaml: measure may not use FROM")
	assert.NotContains(t, resp.Error.Message, "failed to get")
	assert.NotContains(t, resp.Error.Message, "invalid semantics definition")
	for _, s := range []string{resp.Error.Message, errMsg(fx.call("get_definition", map[string]any{"kind": "entity", "name": "odd"}))} {
		assert.False(t, absPath.MatchString(s), s)
	}
}

// --- nothing an agent can call decides or writes -----------------------------

// guardSemantics is a file-backed semantics store that fails the test on any write.
type guardSemantics struct {
	*semantics.FileStore
	t *testing.T
}

func (g guardSemantics) fail(what string) error {
	g.t.Errorf("a tool called semantics.Store.%s", what)
	return nil
}
func (g guardSemantics) PutEntity(context.Context, *semantics.Entity, ...semantics.WriteOption) error {
	return g.fail("PutEntity")
}
func (g guardSemantics) PutMetric(context.Context, *semantics.Metric, ...semantics.WriteOption) error {
	return g.fail("PutMetric")
}
func (g guardSemantics) PutJoin(context.Context, *semantics.Join, ...semantics.WriteOption) error {
	return g.fail("PutJoin")
}
func (g guardSemantics) PutGlossary(context.Context, *semantics.Glossary, ...semantics.WriteOption) error {
	return g.fail("PutGlossary")
}
func (g guardSemantics) PutSource(context.Context, semantics.Kind, string, []byte, ...semantics.WriteOption) (string, error) {
	return "", g.fail("PutSource")
}
func (g guardSemantics) DeleteEntity(context.Context, string, ...semantics.WriteOption) error {
	return g.fail("DeleteEntity")
}
func (g guardSemantics) DeleteMetric(context.Context, string, ...semantics.WriteOption) error {
	return g.fail("DeleteMetric")
}
func (g guardSemantics) DeleteJoin(context.Context, string, ...semantics.WriteOption) error {
	return g.fail("DeleteJoin")
}

// guardProposals is a file-backed inbox that fails the test on any decision.
type guardProposals struct {
	*proposals.FileStore
	t *testing.T
}

func (g guardProposals) Decide(context.Context, string, string, string) error {
	g.t.Error("a tool decided a proposal")
	return nil
}

func (g guardProposals) Get(context.Context, string) (*proposals.Proposal, error) {
	g.t.Error("a tool read a proposal through the store, which only a person's surface does")
	return nil, proposals.ErrNotFound
}

type guardAccess struct {
	WorkspaceAccess
	sem   guardSemantics
	props guardProposals
}

func (g *guardAccess) Semantics() semantics.ReadStore { return g.sem }
func (g *guardAccess) Proposals() proposals.Proposer  { return g.props }
func (g *guardAccess) DataDir() string                { return "" }
func (g *guardAccess) ProtectedPaths() []string       { return nil }

// snapshotTree reads every file under dir (relative paths) so a test can see
// exactly what a call changed on disk.
func snapshotTree(t *testing.T, root, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	base := filepath.Join(root, filepath.FromSlash(dir))
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		require.NoError(t, rerr)
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	return out
}

// Every tool in the registry, at every tier and in task mode, with an approver
// that says yes: none reaches a write method of the semantics store or decides
// a proposal, and the files on disk prove it: semantics/ is byte for byte what
// it was, and the only change under .slaw-local/proposals/ is the new open
// proposal that propose_definition filed. The handler's access is also typed
// so it cannot (ReadStore and Proposer have no such methods).
func TestNoToolWritesSemantics(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []SandboxMode{ModeReadOnly, ModeReadWrite, ModeFull} {
		for _, handlerMode := range []string{"", ModeTask} {
			fx := semanticsFixture(t, mode, withAsk(true), withApprover())
			root := t.TempDir()
			base := fx.h.access
			realSem := semantics.NewFileStore(root)
			_, err := realSem.PutSource(ctx, semantics.KindMetric, "net_revenue", []byte("version: 1\nname: net_revenue\nquery: net_revenue_base\nmeasure: sum(amount)\n"))
			require.NoError(t, err)
			_, err = realSem.PutSource(ctx, semantics.KindEntity, "customer", []byte("version: 1\nname: customer\ngrain: one row per customer\ntable: customers\nid_column: id\n"))
			require.NoError(t, err)
			require.NoError(t, realSem.PutGlossary(ctx, &semantics.Glossary{Text: "## Churn\n\nLeft.\n"}))
			props := proposals.NewFileStore(root)
			seedProp, err := proposals.Build(proposals.Draft{Kind: semantics.KindGlossary, Rationale: "r",
				Definition: map[string]any{"term": "ARR", "text": "x"}}, fixedNow(), proposals.ByPerson)
			require.NoError(t, err)
			require.NoError(t, props.Create(ctx, seedProp))
			fx.h.access = &guardAccess{WorkspaceAccess: base, sem: guardSemantics{FileStore: realSem, t: t}, props: guardProposals{FileStore: props, t: t}}
			fx.h.SetMode(handlerMode)

			semBefore := snapshotTree(t, root, "semantics")
			propBefore := snapshotTree(t, root, ".slaw-local/proposals")
			require.NotEmpty(t, semBefore)

			for _, td := range Tools {
				params := plausibleParams(td)
				switch td.Name {
				case "propose_definition":
					params = metricProposal("another_metric")
				case "get_definition":
					params = map[string]any{"kind": "metric", "name": "net_revenue"}
				}
				resp := fx.h.Handle(ctx, &Request{JSONRPC: "2.0", ID: "1", Method: td.Name, Params: params})
				require.NotNil(t, resp, td.Name)
				switch {
				case handlerMode != "":
				case td.Name == "list_definitions", td.Name == "get_definition":
					assert.Nil(t, resp.Error, "%s must work at %s: %s", td.Name, mode, errMsg(resp))
				}
			}

			assert.Equal(t, semBefore, snapshotTree(t, root, "semantics"), "%s/%q: semantics/ changed", mode, handlerMode)
			propAfter := snapshotTree(t, root, ".slaw-local/proposals")
			for path, text := range propBefore {
				assert.Equal(t, text, propAfter[path], "an existing proposal changed: %s", path)
			}
			added := len(propAfter) - len(propBefore)
			if mode != ModeReadOnly && handlerMode == "" {
				assert.Equal(t, 1, added, "propose_definition filed exactly one proposal")
			} else {
				assert.Zero(t, added, "%s/%q filed nothing", mode, handlerMode)
			}
			for path, text := range propAfter {
				if _, was := propBefore[path]; !was {
					assert.Contains(t, text, "status: open", path)
				}
			}
		}
	}
}

// plausibleParams fills a tool's required parameters with values of the right
// type, enough to get past parameter validation.
func plausibleParams(td ToolDef) map[string]any {
	out := map[string]any{}
	for _, p := range td.Params {
		if !p.Required {
			continue
		}
		switch p.Type {
		case "integer", "number":
			out[p.Name] = 1
		case "boolean":
			out[p.Name] = true
		case "object":
			out[p.Name] = map[string]any{}
		case "array":
			out[p.Name] = []any{}
		default:
			out[p.Name] = "metric"
		}
	}
	return out
}

// The agent surface cannot name the write half of either store: a source scan
// of every package that builds tools, so a later change that threads a full
// semantics.Store or proposals.Store into a handler fails here and not in review.
func TestAgentSurfaceNeverNamesTheWriteHalf(t *testing.T) {
	forbidden := regexp.MustCompile(`\b(PutEntity|PutMetric|PutJoin|PutGlossary|PutSource|DeleteEntity|DeleteMetric|DeleteJoin|Decide)\(|\.\(\s*interface\s*\{|semantics\.Store\b|proposals\.Store\b|proposals\.(Accept|Reject|Show)\b|semantics\.NewFileStore|proposals\.NewFileStore`)
	for _, dir := range []string{".", "../mcp", "../llm", "../hub"} {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)
			for i, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				assert.False(t, forbidden.MatchString(line), "%s/%s:%d reaches a semantics or proposals write: %s", dir, name, i+1, strings.TrimSpace(line))
			}
		}
	}
}

func hex4(i int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[(i>>12)&15], digits[(i>>8)&15], digits[(i>>4)&15], digits[i&15]})
}

func fixedNow() time.Time { return time.Date(2026, time.October, 29, 15, 30, 12, 0, time.UTC) }

func TestProposeDefinition_GlossaryWithAnUnclosedFenceIsRefused(t *testing.T) {
	fx := semanticsFixture(t, ModeReadWrite)
	require.NoError(t, writableSemantics(fx).PutGlossary(context.Background(), &semantics.Glossary{Text: "## A\n\n```\nnever closed\n"}))
	resp := fx.call("propose_definition", map[string]any{"kind": "glossary", "rationale": "r", "definition": map[string]any{"term": "B", "text": "x"}})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "glossary.md has an unclosed ``` fence; fix it first")
	assert.Empty(t, semProposals(t, fx))
}
