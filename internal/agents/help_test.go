package agents

import (
	"regexp"
	"strings"
	"testing"

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
	// And nothing task-only (preview_query/write_cell/need_more_info) leaks in.
	for _, name := range []string{"preview_query", "write_cell", "need_more_info"} {
		assert.NotContains(t, entry.Content, "**"+name+"**", "mcp-tools topic must not advertise task-only tool %q", name)
	}
}
