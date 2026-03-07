package pages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandTemplatesVarsInText(t *testing.T) {
	executed := &ExecutedPage{
		Variables: map[string]any{
			"name":   "Acme Corp",
			"region": "US-East",
		},
		Nodes: []ExecutedNode{
			{Node: &TextNode{Content: "Report for {{.Vars.name}} in {{.Vars.region}}"}},
		},
	}

	err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	textNode, ok := executed.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	assert.Equal(t, "Report for Acme Corp in US-East", textNode.Content)
}

func TestExpandTemplatesPreservesNonTextNodes(t *testing.T) {
	executed := &ExecutedPage{
		Variables: map[string]any{"x": "test"},
		Nodes: []ExecutedNode{
			{Node: &SQLBlockNode{Name: "data", SQL: "SELECT 1", Line: 1}},
			{Node: &TextNode{Content: "Value: {{.Vars.x}}"}},
		},
	}

	err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	// SQL node unchanged.
	_, ok := executed.Nodes[0].Node.(*SQLBlockNode)
	assert.True(t, ok)

	// Text node expanded.
	textNode, ok := executed.Nodes[1].Node.(*TextNode)
	require.True(t, ok)
	assert.Equal(t, "Value: test", textNode.Content)
}

func TestExpandTemplatesNoVars(t *testing.T) {
	executed := &ExecutedPage{
		Variables: map[string]any{},
		Nodes: []ExecutedNode{
			{Node: &TextNode{Content: "Plain text with no templates"}},
		},
	}

	err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	textNode, ok := executed.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	assert.Equal(t, "Plain text with no templates", textNode.Content)
}

func TestExpandTemplatesNilVars(t *testing.T) {
	executed := &ExecutedPage{
		Nodes: []ExecutedNode{
			{Node: &TextNode{Content: "No vars here"}},
		},
	}

	err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	textNode, ok := executed.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	assert.Equal(t, "No vars here", textNode.Content)
}

func TestExpandTemplatesInvalidTemplate(t *testing.T) {
	executed := &ExecutedPage{
		Variables: map[string]any{"x": "test"},
		Nodes: []ExecutedNode{
			{Node: &TextNode{Content: "Bad template {{.Vars.x"}},
		},
	}

	err := ExpandTemplates(executed, nil)
	require.NoError(t, err)

	// Invalid template should leave content as-is.
	textNode, ok := executed.Nodes[0].Node.(*TextNode)
	require.True(t, ok)
	assert.Equal(t, "Bad template {{.Vars.x", textNode.Content)
}
