package agents

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/SandwichLabs/assay/internal/a2ui"
	"github.com/SandwichLabs/assay/internal/notebooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchHelpEmptyQuery(t *testing.T) {
	results := SearchHelp("")
	assert.Equal(t, len(HelpEntries), len(results))
}

func TestSearchHelpByKeyword(t *testing.T) {
	results := SearchHelp("a2ui component dashboard")
	require.NotEmpty(t, results)
	assert.Equal(t, "a2ui", results[0].Topic)
}

func TestSearchHelpNotebooks(t *testing.T) {
	results := SearchHelp("notebook cell sql markdown")
	require.NotEmpty(t, results)
	assert.Equal(t, "notebooks", results[0].Topic)
}

func TestSearchHelpSkills(t *testing.T) {
	results := SearchHelp("skill domain context")
	require.NotEmpty(t, results)
	assert.Equal(t, "skills", results[0].Topic)
}

func TestSearchHelpNoMatch(t *testing.T) {
	results := SearchHelp("xyznonexistent")
	assert.Empty(t, results)
}

func TestSearchHelpMCPTools(t *testing.T) {
	results := SearchHelp("mcp server tool")
	require.NotEmpty(t, results)
	assert.Equal(t, "mcp-tools", results[0].Topic)
}

// TestHelpEntries_NoRetiredPageSystemTopics guards against the #69-retired
// page-system topics (directives, Go templates, frontmatter, page types,
// SQL blocks) reappearing.
func TestHelpEntries_NoRetiredPageSystemTopics(t *testing.T) {
	retired := map[string]bool{
		"sql-blocks": true, "directives": true, "directive-table": true,
		"directive-chart": true, "directive-metric": true, "directive-form": true,
		"directive-detail": true, "go-templates": true, "frontmatter": true,
		"page-types": true,
	}
	for _, e := range HelpEntries {
		assert.False(t, retired[e.Topic], "topic %q documents the retired page system and must not exist", e.Topic)
		assert.NotContains(t, e.Content, "_vibe_pages", "topic %q must not document the retired _vibe_pages table", e.Topic)
	}
}

// mcpToolMentionRe matches the **tool_name** bold-convention buildMCPToolsEntry
// and every hand-written HelpEntries topic above use when naming an actual
// registered tool — see help.go's doc comment on buildMCPToolsEntry.
var mcpToolMentionRe = regexp.MustCompile(`\*\*([a-z][a-z0-9_]*)\*\*`)

// TestHelpTopics_ToolMentionsAreRegistered asserts every **bold_snake_case**
// token across every help topic names a tool that actually exists in the
// live registry — this is what would have caught the old mcp-tools topic
// silently advertising list_pages/get_page/create_page/update_page/
// delete_page long after #69 removed them.
func TestHelpTopics_ToolMentionsAreRegistered(t *testing.T) {
	for _, e := range HelpEntries {
		for _, m := range mcpToolMentionRe.FindAllStringSubmatch(e.Content, -1) {
			name := m[1]
			if !strings.Contains(name, "_") {
				// Single-word bold tokens (e.g. **table**, **stat** component
				// type names) aren't tool mentions by convention — every real
				// tool name in this registry is snake_case with an underscore.
				continue
			}
			assert.NotNil(t, GetTool(name), "topic %q bolds %q as if it were a tool, but no such tool is registered", e.Topic, name)
		}
	}
}

// TestBuildMCPToolsEntry_ListsOnlyRegisteredMCPTools cross-checks the
// generated topic directly against ListToolsForMode(ModeMCP), so a future
// registry change is caught here even if the bold-token heuristic above
// were ever loosened.
func TestBuildMCPToolsEntry_ListsOnlyRegisteredMCPTools(t *testing.T) {
	entry := buildMCPToolsEntry()
	for _, td := range ListToolsForMode(ModeMCP) {
		assert.Contains(t, entry.Content, "**"+td.Name+"**", "mcp-tools topic is missing registered tool %q", td.Name)
	}
	// And nothing task-only (write_cell/need_more_info) leaks in.
	for _, name := range []string{"write_cell", "need_more_info"} {
		assert.NotContains(t, entry.Content, "**"+name+"**", "mcp-tools topic must not advertise task-only tool %q", name)
	}
}

// The generated lists in the notebooks and a2ui topics must name every entry
// of the registries they are built from.
func TestHelpTopics_ListsMatchRegistries(t *testing.T) {
	byTopic := map[string]string{}
	for _, e := range HelpEntries {
		byTopic[e.Topic] = e.Content
	}
	for _, v := range notebooks.Views {
		assert.Contains(t, byTopic["notebooks"], "`"+v+"`", "notebooks topic must list view %q", v)
	}
	for name := range a2ui.Components {
		assert.Contains(t, byTopic["a2ui"], "`"+name+"`", "a2ui topic must list component %q", name)
	}
	for _, c := range a2ui.ColorTokens {
		assert.Contains(t, byTopic["a2ui"], "`"+c+"`", "a2ui topic must list colour %q", c)
	}
}

func TestHelpTopics_NoStaleWording(t *testing.T) {
	for _, e := range HelpEntries {
		assert.NotContains(t, e.Content, "Start MCP Server", e.Topic)
		assert.NotContains(t, strings.ToLower(e.Content), "no directives", e.Topic)
		assert.NotEmpty(t, e.Summary, "topic %q needs a one-line summary for the index", e.Topic)
	}
	for _, td := range Tools {
		assert.NotContains(t, strings.ToLower(td.Description), "directive", td.Name)
		for _, p := range td.Params {
			assert.NotContains(t, strings.ToLower(p.Desc), "directive", td.Name+"."+p.Name)
		}
	}
}

func TestSearchHelpArtifacts(t *testing.T) {
	results := SearchHelp("pivot drill")
	require.NotEmpty(t, results)
	assert.Equal(t, "artifacts", results[0].Topic)
	assert.Contains(t, results[0].Content, "data-artifacts")
	assert.Contains(t, results[0].Content, "docs/reference/artifacts.md")
}

func TestHandleHelpSearch_EmptyQueryIsCompactIndex(t *testing.T) {
	h, _, _ := newSkillsTestHandler(t, ModeReadOnly)
	resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: "help_search", Params: map[string]any{"query": ""}})
	require.Nil(t, resp.Error)
	raw, err := json.Marshal(resp.Result)
	require.NoError(t, err)
	assert.Less(t, len(raw), 2500, "the index must be compact")
	assert.NotContains(t, string(raw), `"content"`)
	assert.Contains(t, string(raw), `"topic":"artifacts"`)
	assert.Contains(t, string(raw), `"summary"`)
}

func TestNewResponse_NilListsBecomeEmptyArrays(t *testing.T) {
	resp := NewResponse(1, map[string]any{
		"rows":     [][]any(nil),
		"warnings": []string(nil),
		"queries":  []map[string]any(nil),
		"nested":   map[string]any{"cols": []string(nil)},
	})
	raw, err := json.Marshal(resp.Result)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "null")
	assert.Contains(t, string(raw), `"rows":[]`)
	assert.Contains(t, string(raw), `"nested":{"cols":[]}`)
}

func TestSearchHelp_ExactTopicNameReturnsOnlyThatTopic(t *testing.T) {
	for _, e := range HelpEntries {
		for _, q := range []string{e.Topic, " " + strings.ToUpper(e.Topic) + " "} {
			got := SearchHelp(q)
			require.Len(t, got, 1, q)
			assert.Equal(t, e.Topic, got[0].Topic)
		}
	}
}
