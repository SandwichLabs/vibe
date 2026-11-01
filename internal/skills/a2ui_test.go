package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/a2ui"
)

// The a2ui skill is generated from the registries: every component type,
// view-only type and color appears, so a new one fails here until it does.
func TestA2UISkill_MatchesRegistry(t *testing.T) {
	var found *Skill
	for _, s := range Builtins() {
		if s.Name == "a2ui" {
			found = &s
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, a2ui.SkillBody(), found.Instructions)
	assert.Contains(t, found.Instructions, a2ui.PromptBlock())
	for _, n := range a2ui.TypeNames() {
		assert.Contains(t, found.Instructions, n)
	}
	require.NotEmpty(t, a2ui.ViewTypeNames())
	for _, n := range a2ui.ViewTypeNames() {
		assert.Contains(t, found.Instructions, "  "+n+" ", "view type %s", n)
	}
	for _, c := range a2ui.ColorTokens {
		assert.Contains(t, found.Instructions, `"`+c+`"`)
	}
}
