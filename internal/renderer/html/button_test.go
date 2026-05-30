package html

// Renderer tests for the Access Command-Button control (plan §5, unit P5.5).
//
// Acceptance: "Renderer emits a button carrying the macro hook attribute (no
// inline JS), usable inside {form} and standalone."
//
// The rendered control is a <button> carrying a data-macro-id (and data-action)
// attribute the Vue GUI runtime hooks to invoke RunMacro(target) — there must be
// NO inline JS (no onclick=) so it stays consistent with the other controls.

import (
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderButtonEmitsMacroHook asserts a {button:} renders a <button> element
// with the visible label and a data-macro-id hook (no inline JS).
func TestRenderButtonEmitsMacroHook(t *testing.T) {
	r := newTestRenderer()

	out, err := r.RenderButton(&renderer.ButtonConfig{
		Label:  "Approve",
		Action: "mac_approve",
	})
	require.NoError(t, err)

	assert.Contains(t, out, "<button", "must render a button element")
	assert.Contains(t, out, ">Approve<", "button must show its label")
	assert.Contains(t, out, `data-macro-id="mac_approve"`, "button must carry the macro hook attribute")
	assert.NotContains(t, strings.ToLower(out), "onclick", "button must not use inline JS")
}

// TestRenderButtonEscapesLabel asserts the label is HTML-escaped.
func TestRenderButtonEscapesLabel(t *testing.T) {
	r := newTestRenderer()

	out, err := r.RenderButton(&renderer.ButtonConfig{
		Label:  "<script>",
		Action: "mac_x",
	})
	require.NoError(t, err)

	assert.NotContains(t, out, "<script>", "label must be HTML-escaped")
	assert.Contains(t, out, "&lt;script&gt;")
}
