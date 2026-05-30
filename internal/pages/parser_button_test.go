package pages

// Parser tests for the Access Command-Button control (plan §5, unit P5.5).
//
// Acceptance: "{button:} parses into a DirectiveNode with label + action(macro
// target); missing label or target yields a line-numbered parse error."
//
// The {button:} directive is parser/renderer-local: it carries a human label and
// a macro target (the macro id RunMacro invokes at runtime). It is treated like a
// non-source directive — the primary positional value is the label and `action`
// names the macro target — and must be reusable standalone on a page and inside a
// {form} body.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseButtonDirective verifies a standalone {button:} parses into a
// DirectiveNode{Type:"button"} carrying its label + action (macro target).
func TestParseButtonDirective(t *testing.T) {
	input := `{button: label=Approve, action=mac_approve}`

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	dir, ok := page.Nodes[0].(*DirectiveNode)
	require.True(t, ok, "expected a DirectiveNode")
	assert.Equal(t, "button", dir.Type)
	assert.Equal(t, "Approve", dir.Config["label"])
	assert.Equal(t, "mac_approve", dir.Config["action"])
}

// TestParseButtonMissingActionErrors asserts a {button:} that omits its macro
// target yields a line-numbered parse error in the existing directive-error style.
func TestParseButtonMissingActionErrors(t *testing.T) {
	// Place the directive on line 3 so the line number in the error is non-trivial.
	input := "line one\n\n{button: label=Approve}"

	_, err := Parse(input)
	require.Error(t, err, "a {button:} missing its action/macro target must error")
	assert.Contains(t, err.Error(), "line 3", "parse error must carry the directive's line number")
	assert.Contains(t, err.Error(), "button", "parse error should mention the button directive")
}

// TestParseButtonMissingLabelErrors asserts a {button:} that omits its label
// yields a line-numbered parse error.
func TestParseButtonMissingLabelErrors(t *testing.T) {
	input := "{button: action=mac_approve}"

	_, err := Parse(input)
	require.Error(t, err, "a {button:} missing its label must error")
	assert.Contains(t, err.Error(), "line 1", "parse error must carry the directive's line number")
	assert.Contains(t, err.Error(), "button", "parse error should mention the button directive")
}

// TestParseTableDirectiveUnaffectedByButton is a guard ensuring adding the
// {button:} directive does not perturb an existing directive's parsing.
func TestParseTableDirectiveUnaffectedByButton(t *testing.T) {
	input := "{table: revenue_by_month, page_size: 20}"

	page, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, page.Nodes, 1)

	dir, ok := page.Nodes[0].(*DirectiveNode)
	require.True(t, ok)
	assert.Equal(t, "table", dir.Type)
	assert.Equal(t, "revenue_by_month", dir.Config["source"])
}
