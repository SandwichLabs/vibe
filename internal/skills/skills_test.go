package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter_Valid(t *testing.T) {
	name, description, body, ok := ParseFrontmatter("---\nname: foo-bar\ndescription: does a thing\n---\n# Foo\n\nBody text.\n")
	require.True(t, ok)
	assert.Equal(t, "foo-bar", name)
	assert.Equal(t, "does a thing", description)
	assert.Equal(t, "# Foo\n\nBody text.", body)
}

func TestParseFrontmatter_CRLF(t *testing.T) {
	raw := "---\r\nname: crlf-skill\r\ndescription: uses windows line endings\r\n---\r\nBody.\r\n"
	name, description, body, ok := ParseFrontmatter(raw)
	require.True(t, ok)
	assert.Equal(t, "crlf-skill", name)
	assert.Equal(t, "uses windows line endings", description)
	assert.Equal(t, "Body.", body)
}

func TestParseFrontmatter_MissingName(t *testing.T) {
	_, _, _, ok := ParseFrontmatter("---\ndescription: no name here\n---\nBody\n")
	assert.False(t, ok)
}

func TestParseFrontmatter_MissingDescription(t *testing.T) {
	_, _, _, ok := ParseFrontmatter("---\nname: no-desc\n---\nBody\n")
	assert.False(t, ok)
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	_, _, _, ok := ParseFrontmatter("# Just markdown\n\nNo frontmatter at all.\n")
	assert.False(t, ok)
}

func TestParseFrontmatter_UnterminatedFrontmatter(t *testing.T) {
	_, _, _, ok := ParseFrontmatter("---\nname: broken\ndescription: never closed\nBody\n")
	assert.False(t, ok)
}

func TestParseFrontmatter_AppendsNothingWithoutAddendum(t *testing.T) {
	// Sanity: parsing alone never appends an addendum section — that's
	// Builtins()'s job, driven by a sibling file's presence.
	_, _, body, ok := ParseFrontmatter("---\nname: x\ndescription: y\n---\nBody only.\n")
	require.True(t, ok)
	assert.NotContains(t, body, "Addendum")
}

func TestBuiltins_ShipsExpectedSkills(t *testing.T) {
	all := Builtins()
	require.NotEmpty(t, all)

	names := make(map[string]Skill, len(all))
	for _, s := range all {
		names[s.Name] = s
	}

	for _, want := range []string{"data-profiling", "trustworthy-numbers", "duckdb-sql"} {
		s, ok := names[want]
		require.True(t, ok, "expected built-in skill %q", want)
		assert.NotEmpty(t, s.Description)
		assert.Less(t, len(s.Description), 300, "description should be a one-liner")
		assert.NotEmpty(t, s.Instructions)
		assert.True(t, NamePattern.MatchString(s.Name))
	}
}

func TestBuiltins_SortedByName(t *testing.T) {
	all := Builtins()
	for i := 1; i < len(all); i++ {
		assert.LessOrEqual(t, all[i-1].Name, all[i].Name)
	}
}

func TestBuiltins_OnlyTaskNotebookCellsIsTaskPrefixed(t *testing.T) {
	// task-notebook-cells (#66 part B) is the first built-in to use the
	// task- prefix, intentionally: it's task mode's own workflow skill, and
	// must be invisible to chat's system prompt / read_skill / list_skills
	// (see internal/skills.ChatVisibleSkills). Every other built-in staying
	// off the prefix is still worth asserting so a future addition has to
	// consciously opt in.
	for _, s := range Builtins() {
		if s.Name == "task-notebook-cells" {
			continue
		}
		assert.False(t, strings.HasPrefix(s.Name, TaskPrefix), "built-in %q should not use the task- prefix", s.Name)
	}
}

func TestBuiltins_TaskNotebookCells(t *testing.T) {
	all := Builtins()
	var found *Skill
	for i := range all {
		if all[i].Name == "task-notebook-cells" {
			found = &all[i]
		}
	}
	require.NotNil(t, found, "expected built-in skill \"task-notebook-cells\"")
	assert.True(t, strings.HasPrefix(found.Name, TaskPrefix))
	assert.NotEmpty(t, found.Description)
	assert.Contains(t, found.Instructions, "write_cell")
	assert.Contains(t, found.Instructions, "preview_query")
}

func TestNamePattern(t *testing.T) {
	valid := []string{"a", "data-profiling", "workspace-sales", "task-notebook-cells", "z9-9"}
	for _, v := range valid {
		assert.True(t, NamePattern.MatchString(v), "expected %q to be valid", v)
	}
	invalid := []string{"", "Data-Profiling", "-leading-dash", "has space", "has_underscore", strings.Repeat("a", 65)}
	for _, v := range invalid {
		assert.False(t, NamePattern.MatchString(v), "expected %q to be invalid", v)
	}
}
