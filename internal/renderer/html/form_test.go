package html

// Renderer tests for the columnar record-navigation bar (plan §1.5, unit P1.6).
//
// Acceptance: "Columnar archetype renders a record-nav bar
// (first/prev/next/last/new/save/delete/Find)".

import (
	"io"
	"log/slog"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRenderer() *Renderer {
	return NewRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestColumnarFormEmitsRecordNavBar asserts the columnar archetype emits the
// full Access-style record-navigation bar: first/prev/next/last/new/save/delete
// plus a Find Record control.
func TestColumnarFormEmitsRecordNavBar(t *testing.T) {
	r := newTestRenderer()
	cfg := &renderer.FormConfig{
		Table:     "customers",
		Archetype: "columnar",
		Fields: []renderer.FormField{
			{Name: "name", Label: "Name", Type: "text"},
		},
	}

	out, err := r.RenderForm(cfg)
	require.NoError(t, err)

	// The nav bar container is present.
	assert.Contains(t, out, `class="vibe-record-nav"`, "columnar form must render a record-nav bar")

	// Each record-navigation + CRUD control is present, keyed by its stable
	// data-nav action so App-Mode wiring is unambiguous.
	for _, action := range []string{"first", "prev", "next", "last", "new", "save", "delete"} {
		assert.Contains(t, out, `data-nav="`+action+`"`,
			"record-nav bar must include the %q control", action)
	}

	// Find Record control.
	assert.Contains(t, out, `data-nav="find"`, "record-nav bar must include a Find Record input")
	assert.Contains(t, out, "Find Record", "record-nav bar must label the Find Record control")
}

// TestColumnarFormViaBoundControls asserts the record-nav bar is also emitted by
// the bound-control form renderer (RenderFormWithOptions), not just the plain
// RenderForm path.
func TestColumnarFormViaBoundControls(t *testing.T) {
	r := newTestRenderer()
	cfg := &renderer.FormConfig{
		Table:     "customers",
		Archetype: "columnar",
		Fields: []renderer.FormField{
			{Name: "name", Label: "Name", ControlType: "text"},
		},
	}

	out, err := r.RenderFormWithOptions(t.Context(), cfg, nil)
	require.NoError(t, err)

	assert.Contains(t, out, `class="vibe-record-nav"`)
	for _, action := range []string{"first", "prev", "next", "last", "new", "save", "delete", "find"} {
		assert.Contains(t, out, `data-nav="`+action+`"`)
	}
}

// TestNonColumnarFormHasPlainSubmit asserts non-columnar archetypes keep the
// plain submit footer (no record-nav bar).
func TestNonColumnarFormHasPlainSubmit(t *testing.T) {
	r := newTestRenderer()
	cfg := &renderer.FormConfig{
		Table:     "customers",
		Archetype: "datasheet",
		Fields:    []renderer.FormField{{Name: "name", Type: "text"}},
	}

	out, err := r.RenderForm(cfg)
	require.NoError(t, err)

	assert.NotContains(t, out, `class="vibe-record-nav"`, "non-columnar form must not render a record-nav bar")
	assert.Contains(t, out, `>Submit</button>`, "non-columnar form keeps the plain submit button")
}
