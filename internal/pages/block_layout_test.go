package pages

// TDD RED — Phase 3 / unit P3.5: "Block layout directives: stack, tabs, callout"
// (parity plan §3, Evidence WU-4 generalization).
//
// Today only {row} is a layout block (RowNode). These tests pin the generalized
// block-parsing behavior: {stack}...{/stack}, {tabs}{tab:..}...{/tabs}, and
// {callout: type}...{/callout} must parse — mirroring the {row} sibling-
// collection-until-{/x} mechanism — into a generic BlockNode carrying a Kind,
// its Config, and the collected Children. Unclosed blocks must error clearly,
// matching the existing {/row} behavior.
//
// These are parser-level assertions over the public Parse API; the studio
// integration test (studio/block_layout_render_test.go) covers execution +
// render end-to-end against a real .vibe container + DuckDB.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseStackBlock pins {stack}...{/stack} parsing into a BlockNode whose
// Kind is "stack" and whose Children are the collected sibling nodes.
func TestParseStackBlock(t *testing.T) {
	input := `{stack}

First paragraph inside

{table: results}

{/stack}`

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	blk, ok := page.Nodes[0].(*BlockNode)
	require.True(t, ok, "stack should parse into a *BlockNode")
	assert.Equal(t, "stack", blk.Kind)
	require.Len(t, blk.Children, 2)

	_, ok = blk.Children[0].(*TextNode)
	assert.True(t, ok, "first child should be the prose TextNode")

	dir, ok := blk.Children[1].(*DirectiveNode)
	require.True(t, ok, "second child should be the {table:} directive")
	assert.Equal(t, "table", dir.Type)
}

// TestParseTabsBlock pins {tabs}{tab: ..}...{/tabs} parsing. The {tab:} markers
// segment the collected children into labeled tab sections; each section's
// directives become children that the executor can later resolve.
func TestParseTabsBlock(t *testing.T) {
	input := `{tabs}

{tab: "Overview"}

{table: alpha}

{tab: "Detail"}

{table: beta}

{/tabs}`

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	blk, ok := page.Nodes[0].(*BlockNode)
	require.True(t, ok, "tabs should parse into a *BlockNode")
	assert.Equal(t, "tabs", blk.Kind)

	// The two {tab:} sections become child BlockNodes of Kind "tab" carrying a
	// label, each containing its own directive child.
	var tabs []*BlockNode
	for _, c := range blk.Children {
		if tb, ok := c.(*BlockNode); ok && tb.Kind == "tab" {
			tabs = append(tabs, tb)
		}
	}
	require.Len(t, tabs, 2, "tabs block must contain two tab sections")

	assert.Equal(t, "Overview", tabs[0].Config["tab"])
	require.Len(t, tabs[0].Children, 1)
	d0, ok := tabs[0].Children[0].(*DirectiveNode)
	require.True(t, ok)
	assert.Equal(t, "table", d0.Type)

	assert.Equal(t, "Detail", tabs[1].Config["tab"])
	require.Len(t, tabs[1].Children, 1)
	d1, ok := tabs[1].Children[0].(*DirectiveNode)
	require.True(t, ok)
	assert.Equal(t, "table", d1.Type)
}

// TestParseCalloutBlock pins {callout: type: warning}...{/callout} parsing into
// a BlockNode of Kind "callout" carrying the type in Config and prose Children.
func TestParseCalloutBlock(t *testing.T) {
	input := `{callout: type: warning}

Heads up — this is important.

{/callout}`

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	blk, ok := page.Nodes[0].(*BlockNode)
	require.True(t, ok, "callout should parse into a *BlockNode")
	assert.Equal(t, "callout", blk.Kind)
	assert.Equal(t, "warning", blk.Config["type"])
	require.Len(t, blk.Children, 1)

	txt, ok := blk.Children[0].(*TextNode)
	require.True(t, ok)
	assert.Contains(t, txt.Content, "Heads up")
}

// TestParseUnclosedStack asserts a clear parser error for an unclosed {stack},
// mirroring the existing unclosed {row} / {if} behavior.
func TestParseUnclosedStack(t *testing.T) {
	input := `{stack}

orphan text`

	_, err := Parse(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{/stack}")
}

// TestParseUnclosedCallout asserts a clear parser error for an unclosed callout.
func TestParseUnclosedCallout(t *testing.T) {
	input := `{callout: type: info}

orphan text`

	_, err := Parse(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{/callout}")
}
