package pages

// TDD RED — Phase 1 / unit P1.6: "{subform} directive + master-detail link"
// (parity plan §1.5/§1.6).
//
// The {subform} directive is the Access master-detail control: an FK-linked
// child datasheet embedded inside a parent form, auto-filtered to the current
// master record. It carries a parent-key/child-key link (child_key->parent_key)
// plus the child table/source binding.
//
// This test asserts ONLY the parse-time contract: the parser must register
// "subform" as a known directive type and expose its link + child binding on
// the resulting DirectiveNode.Config. No rendering or execution here.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubformDirectiveParses(t *testing.T) {
	// An order header form with an FK-linked line-item subform.
	// link=order_id->id means: child.order_id = master.id.
	src := "" +
		"{form: table=orders, archetype=master_detail}\n\n" +
		"{subform: child=order_items, link=order_id->id, title=Line Items}\n"

	page, err := Parse(src)
	require.NoError(t, err, "page with a {subform} directive must parse")
	require.NotNil(t, page)

	// Find the subform directive node.
	var sub *DirectiveNode
	for _, n := range page.Nodes {
		if d, ok := n.(*DirectiveNode); ok && d.Type == "subform" {
			sub = d
			break
		}
	}
	require.NotNil(t, sub, "parser must recognize {subform} as a directive (registered in knownTypes), not fold it into text")

	// The child table binding must be carried through.
	assert.Equal(t, "order_items", sub.Config["child"],
		"subform must carry the child table/source binding")

	// The master-detail FK link must be carried through verbatim so the
	// renderer can build the WHERE child_key = <master parent_key> filter.
	assert.Equal(t, "order_id->id", sub.Config["link"],
		"subform must carry the child_key->parent_key link")

	assert.Equal(t, "Line Items", sub.Config["title"],
		"subform must carry the optional title")
}
