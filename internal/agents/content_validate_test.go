package agents

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/notebooks"
	"github.com/SandwichLabs/assay/internal/skills"
)

// contentCase is one content tool with a valid and an invalid request, and a
// count of what it has written so far.
type contentCase struct {
	tool    string
	valid   map[string]any
	invalid map[string]any
	// written reports how much the tool's store holds.
	written func(t *testing.T, fx *artifactFixture) int
	// prepare, when set, seeds the fixture and returns parameters (notebook id,
	// cell id, rev) merged into both requests.
	prepare func(t *testing.T, fx *artifactFixture) map[string]any
}

// requests returns the case's valid and invalid parameters, seeded when the
// case needs a notebook to exist.
func (c contentCase) requests(t *testing.T, fx *artifactFixture) (valid, invalid map[string]any) {
	t.Helper()
	valid, invalid = c.valid, c.invalid
	if c.prepare == nil {
		return valid, invalid
	}
	extra := c.prepare(t, fx)
	merge := func(base map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range extra {
			out[k] = v
		}
		for k, v := range base {
			out[k] = v
		}
		return out
	}
	return merge(valid), merge(invalid)
}

func contentCases() []contentCase {
	return []contentCase{
		{
			tool:    "write_artifact",
			valid:   map[string]any{"name": "sales", "manifest": artifactManifest(rowsQuery(okSQL)), "view": viewMapNoT(artifactView)},
			invalid: map[string]any{"name": "sales", "manifest": artifactManifest(rowsQuery("DROP TABLE t")), "view": viewMapNoT(artifactView)},
			written: func(t *testing.T, fx *artifactFixture) int {
				list, err := fx.store.List(context.Background())
				require.NoError(t, err)
				return len(list)
			},
		},
		{
			tool:    "create_or_update_skill",
			valid:   map[string]any{"name": "revenue-rules", "description": "Revenue rules", "instructions": "Use net."},
			invalid: map[string]any{"name": "Bad Name", "description": "x", "instructions": "y"},
			written: func(t *testing.T, fx *artifactFixture) int {
				list, err := fx.h.access.Skills().List(context.Background())
				require.NoError(t, err)
				return len(list)
			},
		},
		{
			tool:    "add_query_cell",
			valid:   map[string]any{"sql": "SELECT 1", "label": "One"},
			invalid: map[string]any{"sql": "DELETE FROM t"},
			written: notebookCount,
		},
		{
			tool:    "add_markdown_cell",
			valid:   map[string]any{"markdown": "# Notes\n\nhello"},
			invalid: map[string]any{"markdown": "  "},
			written: notebookCount,
		},
		{
			tool:    "update_cell",
			valid:   map[string]any{"sql": "SELECT 2"},
			invalid: map[string]any{"sql": "DELETE FROM t"},
			prepare: seedNotebook(1),
			written: func(t *testing.T, fx *artifactFixture) int {
				return countCells(t, fx, func(c notebooks.Cell) bool { return c.Source == "SELECT 2" })
			},
		},
		{
			tool:    "delete_cell",
			valid:   map[string]any{},
			invalid: map[string]any{"cell_id": "nope"},
			prepare: seedNotebook(1),
			written: func(t *testing.T, fx *artifactFixture) int {
				return 1 - countCells(t, fx, func(notebooks.Cell) bool { return true })
			},
		},
		{
			tool:    "move_cell",
			valid:   map[string]any{"position": 1},
			invalid: map[string]any{"position": 9},
			prepare: seedNotebook(2),
			written: func(t *testing.T, fx *artifactFixture) int {
				nb := onlyNotebook(t, fx)
				if len(nb.Cells) == 2 && nb.Cells[1].ID == "cell_zero" {
					return 1
				}
				return 0
			},
		},
		{
			tool:    "create_dashboard",
			valid:   map[string]any{"page": map[string]any{"title": "T", "components": []any{map[string]any{"id": "a", "type": "heading", "text": "hi"}}}},
			invalid: map[string]any{"page": map[string]any{"title": "T", "components": []any{map[string]any{"id": "a", "type": "nope"}}}},
			written: notebookCount,
		},
		{
			tool:    "save_query",
			valid:   map[string]any{"title": "Named rows", "sql": "SELECT id, name FROM t WHERE id >= 1"},
			invalid: map[string]any{"title": "Named rows", "sql": "DELETE FROM t"},
			written: queryCount,
		},
		{
			tool: "propose_definition",
			valid: map[string]any{"kind": "glossary", "rationale": "Finance uses this word both ways.",
				"definition": map[string]any{"term": "Churn", "text": "Customers who cancelled in the period."}},
			invalid: map[string]any{"kind": "glossary", "rationale": "x",
				"definition": map[string]any{"term": "Churn", "text": "text\n## smuggled\nsecond term"}},
			written: func(t *testing.T, fx *artifactFixture) int {
				list, err := fx.h.access.Proposals().List(context.Background())
				require.NoError(t, err)
				return len(list)
			},
		},
		{
			tool:    "build_query",
			valid:   map[string]any{"name": "Big ids", "table": "t", "conditions": []any{map[string]any{"column": "id", "op": "gt", "value": "1"}}},
			invalid: map[string]any{"name": "Big ids", "table": "t", "conditions": []any{map[string]any{"column": "nope", "op": "gt", "value": "1"}}},
			written: queryCount,
		},
	}
}

func notebookCount(t *testing.T, fx *artifactFixture) int {
	list, err := fx.h.access.Notebooks().List(context.Background())
	require.NoError(t, err)
	return len(list)
}

func queryCount(t *testing.T, fx *artifactFixture) int {
	list, err := fx.h.access.Queries().List(context.Background())
	require.NoError(t, err)
	return len(list)
}

func viewMapNoT(raw string) map[string]any {
	var v map[string]any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		panic(err)
	}
	return v
}

// Every content tool has a case here, so a new one cannot skip validate-first.
func TestContentTools_AllHaveAValidateFirstCase(t *testing.T) {
	var want, got []string
	for _, td := range ListTools() {
		if td.Category == "content" {
			want = append(want, td.Name)
		}
	}
	for _, c := range contentCases() {
		got = append(got, c.tool)
	}
	for _, td := range ListTools() {
		assert.Equal(t, td.Category == "content", td.DryRunSafe, "DryRunSafe marks exactly the content tools that honour a dry run: %s", td.Name)
	}
	sort.Strings(want)
	sort.Strings(got)
	assert.Equal(t, want, got)
}

// Headless with the gate on: an invalid request gets its own validation error;
// a valid one is told it is valid and ready and needs approval; neither writes.
func TestContentTools_ValidateBeforeTheApprovalGate(t *testing.T) {
	for _, c := range contentCases() {
		t.Run(c.tool, func(t *testing.T) {
			fx := newArtifactFixture(t, ModeReadWrite, withAsk(true))

			valid, invalid := c.requests(t, fx)
			resp := fx.call(c.tool, invalid)
			require.NotNil(t, resp.Error)
			assert.NotEqual(t, ErrApprovalNeeded, resp.Error.Code, "an invalid request is not an approval problem: %s", resp.Error.Message)
			assert.Nil(t, resp.Error.Data)
			assert.Equal(t, 0, c.written(t, fx))

			resp = fx.call(c.tool, valid)
			require.NotNil(t, resp.Error)
			assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
			assert.Equal(t, approvalNeededData(), resp.Error.Data)
			assert.Contains(t, resp.Error.Message, "valid and ready")
			assert.Contains(t, resp.Error.Message, "NOT saved")
			assert.Contains(t, resp.Error.Message, "Settings → Workspace → The agent")
			assert.NotContains(t, resp.Error.Message, "local database")
			assert.Equal(t, 0, c.written(t, fx), "nothing may be written on refusal")
		})
	}
}

// With someone present the card is shown and the write happens, as before.
func TestContentTools_ApprovedWriteStillLands(t *testing.T) {
	for _, c := range contentCases() {
		t.Run(c.tool, func(t *testing.T) {
			fx := newArtifactFixture(t, ModeReadWrite, withAsk(true), withApprover())
			valid, _ := c.requests(t, fx)
			resp := fx.call(c.tool, valid)
			require.Nil(t, resp.Error, errMsg(resp))
			assert.Equal(t, 1, fx.approver.count())
			assert.Equal(t, 1, c.written(t, fx))
		})
	}
}

// The gate off: no validation detour, the write simply runs.
func TestContentTools_GateOffWritesDirectly(t *testing.T) {
	for _, c := range contentCases() {
		t.Run(c.tool, func(t *testing.T) {
			fx := newArtifactFixture(t, ModeReadWrite, withAsk(false))
			valid, _ := c.requests(t, fx)
			resp := fx.call(c.tool, valid)
			require.Nil(t, resp.Error, errMsg(resp))
			assert.Equal(t, 1, c.written(t, fx))
		})
	}
}

// A request that would conflict is reported as a conflict, not as ready.
func TestContentTools_DryRunReportsAConflict(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(false))
	require.Nil(t, fx.call("write_artifact", contentCases()[0].valid).Error)
	require.Nil(t, fx.call("create_or_update_skill", contentCases()[1].valid).Error)

	fx.h.SetFilePolicy(ModeReadWrite, true)
	for _, c := range contentCases()[:2] {
		resp := fx.call(c.tool, c.valid)
		require.NotNil(t, resp.Error, c.tool)
		assert.Equal(t, ErrInvalidParams, resp.Error.Code, c.tool)
		assert.Contains(t, resp.Error.Message, "already exists", c.tool)
		assert.Nil(t, resp.Error.Data, c.tool)
	}
}

// A missing notebook is a validation error, and a dry run never creates one.
func TestContentTools_DryRunNeverCreatesANotebook(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(true))
	resp := fx.call("add_query_cell", map[string]any{"sql": "SELECT 1", "notebook_id": "nope"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Equal(t, 0, notebookCount(t, fx))
}

// The data variant names the real settings path and talks about data, not files.
func TestDataApproval_HeadlessText(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull, withAsk(true))
	resp := fx.call("create_record", map[string]any{"table": "t", "values": map[string]any{"id": 9}})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
	assert.Nil(t, resp.Error.Data)
	assert.Contains(t, resp.Error.Message, "Settings → Workspace → The agent")
	assert.NotContains(t, resp.Error.Message, "Settings → Agent")
	assert.NotContains(t, resp.Error.Message, "valid and ready")
}

// A tier refusal says which tier is needed and where to change it.
func TestTierRefusal_IsActionable(t *testing.T) {
	cases := []struct {
		tool, mode, need string
		params           map[string]any
	}{
		{"write_artifact", "read-only", `read-write tier ("Change rows")`, contentCases()[0].valid},
		{"create_record", "read-only", `read-write tier ("Change rows")`, map[string]any{"table": "t", "values": map[string]any{"id": 1}}},
		{"create_table", "read-write", `full tier ("Full control")`, map[string]any{"name": "x", "columns": []any{}}},
	}
	for _, c := range cases {
		fx := newArtifactFixture(t, SandboxMode(c.mode), withAsk(true))
		resp := fx.call(c.tool, c.params)
		require.NotNil(t, resp.Error, c.tool)
		assert.Equal(t, ErrPermission, resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "not allowed in "+c.mode+" mode")
		assert.Contains(t, resp.Error.Message, c.need)
		assert.Contains(t, resp.Error.Message, "Settings → Workspace → The agent")
		assert.Contains(t, resp.Error.Message, "Nothing was changed")
	}
}

// A content tool that is not marked dry-run-safe is never dispatched ahead of
// the gate: it gets the plain approval refusal, and nothing is written.
func TestContentTool_NotMarkedDryRunSafeIsRefusedUnvalidated(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(true))
	td := GetTool("create_dashboard")
	td.DryRunSafe = false
	t.Cleanup(func() { td.DryRunSafe = true })
	resp := fx.call("create_dashboard", contentCaseFor("create_dashboard").valid)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
	assert.Nil(t, resp.Error.Data)
	assert.NotContains(t, resp.Error.Message, "valid and ready")
	assert.Equal(t, 0, notebookCount(t, fx))
}

// A dry run that "succeeds" without the marker is an internal error, never ready.
func TestValidateOnly_UnmarkedSuccessIsAnInternalError(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(true))
	// list_artifacts ignores the dry run and returns a plain result.
	resp := fx.h.validateOnly(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "list_artifacts", Params: map[string]any{}})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInternal, resp.Error.Code)
}

// A stored skill that cannot be read is reported, not called ready.
func TestSkillDryRun_ReadErrorIsNotReady(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(true))
	acc, ok := fx.h.access.(*policyAccess)
	require.True(t, ok)
	acc.mockAccess.sks = brokenSkills{}
	resp := fx.call("create_or_update_skill", contentCases()[1].valid)
	require.NotNil(t, resp.Error)
	assert.NotEqual(t, ErrApprovalNeeded, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "cannot be read")
}

type brokenSkills struct{ skills.Store }

func (brokenSkills) Get(context.Context, string) (*skills.Stored, error) {
	return nil, errors.New("disk on fire")
}

// seedNotebook makes a prepare func: a notebook "nb" on a file store (so it has
// a rev) holding n sql cells, the first "cell_zero" and the second "cell_one".
func seedNotebook(n int) func(t *testing.T, fx *artifactFixture) map[string]any {
	return func(t *testing.T, fx *artifactFixture) map[string]any {
		t.Helper()
		store := notebooks.NewFileStore(filepath.Join(fx.dir, "ws"))
		acc, ok := fx.h.access.(*policyAccess)
		require.True(t, ok)
		acc.mockAccess.nbs = store
		nb := notebooks.Empty("nb")
		names := []string{"cell_zero", "cell_one", "cell_two"}
		for i := 0; i < n; i++ {
			cell, err := notebooks.NewCell(notebooks.CellSQL)
			require.NoError(t, err)
			cell.ID, cell.Source = names[i], "SELECT 1"
			nb.Cells = append(nb.Cells, cell)
		}
		_, _, err := store.Insert(context.Background(), nb)
		require.NoError(t, err)
		got := onlyNotebook(t, fx)
		return map[string]any{"notebook_id": got.ID, "cell_id": "cell_zero", "rev": got.Rev}
	}
}

func onlyNotebook(t *testing.T, fx *artifactFixture) notebooks.Notebook {
	t.Helper()
	store := fx.h.access.Notebooks()
	list, err := store.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	nb, _, err := store.Get(context.Background(), list[0].ID)
	require.NoError(t, err)
	return nb
}

func countCells(t *testing.T, fx *artifactFixture, match func(notebooks.Cell) bool) int {
	t.Helper()
	n := 0
	for _, c := range onlyNotebook(t, fx).Cells {
		if match(c) {
			n++
		}
	}
	return n
}

// contentCaseFor finds a case by tool name.
func contentCaseFor(tool string) contentCase {
	for _, c := range contentCases() {
		if c.tool == tool {
			return c
		}
	}
	panic("no content case for " + tool)
}
