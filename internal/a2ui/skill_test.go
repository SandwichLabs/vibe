package a2ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Every registered type (chat and view-only) is in the skill body, in the
// section for where it is valid.
func TestSkillBody_CoversBothRegistries(t *testing.T) {
	body := SkillBody()
	split := strings.Index(body, "## Data artifact view components")
	if split < 0 {
		t.Fatal("no view section")
	}
	dash, view := body[:split], body[split:]
	for name := range Components {
		assert.Contains(t, dash, "  "+name+" ", name)
		assert.Contains(t, view, "  "+name+" ", name)
	}
	for name := range ViewComponents {
		assert.Contains(t, view, "  "+name+" ", name)
		assert.NotContains(t, dash, "  "+name+" ", name+" is view-only")
	}
	assert.Len(t, ViewTypeNames(), len(ViewComponents))
}
