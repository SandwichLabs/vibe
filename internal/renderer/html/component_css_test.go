package html

// P3.10: the shared SchoolHouse component CSS must be injected exactly once into
// rendered report output, even when the report spans multiple bands/groups (the
// multi-band / multi-page case the review flagged).

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderReportInjectsSharedComponentCSSOnce asserts the shared component CSS
// marker appears exactly once in a multi-band report's rendered HTML.
func TestRenderReportInjectsSharedComponentCSSOnce(t *testing.T) {
	r := NewRenderer(reportTestLogger())
	tree, spec := sampleBandTree() // two groups (Alice, Bob), each with multiple detail rows

	out, err := r.RenderReport(tree, spec, RenderReportOptions{})
	require.NoError(t, err)

	require.Greater(t, len(tree.Groups), 1, "test must exercise multi-band output")
	assert.Equal(t, 1, strings.Count(out, componentCSSMarker),
		"shared component CSS must be injected exactly once across multi-band output")
}

// TestInjectComponentCSSOnceIsIdempotent asserts the injection helper does not
// duplicate the shared CSS when the output already carries it (the multi-page /
// concatenated-fragment case).
func TestInjectComponentCSSOnceIsIdempotent(t *testing.T) {
	r := NewRenderer(reportTestLogger())
	tree, spec := sampleBandTree()

	page1, err := r.RenderReport(tree, spec, RenderReportOptions{})
	require.NoError(t, err)
	page2, err := r.RenderReport(tree, spec, RenderReportOptions{})
	require.NoError(t, err)

	// Simulate concatenating two rendered "pages" then guarding the whole.
	combined := InjectComponentCSSOnce(page1 + page2)

	assert.Equal(t, 2, strings.Count(combined, componentCSSMarker),
		"each RenderReport already injects once; the guard must not add a third copy")

	// A fragment with no prior CSS gets exactly one injection.
	bare := InjectComponentCSSOnce("<div>no css here</div>")
	assert.Equal(t, 1, strings.Count(bare, componentCSSMarker),
		"bare fragment must receive exactly one shared CSS injection")
}

// TestReportPrintCSSNoLongerDuplicatesScreenStyles guards against regressing to
// the pre-P3.10 state where screen styling lived in both blocks.
func TestReportPrintCSSNoLongerDuplicatesScreenStyles(t *testing.T) {
	assert.NotContains(t, reportPrintCSS, ".vibe-report {",
		"screen styling must live only in SharedComponentCSS, not reportPrintCSS")
	assert.Contains(t, SharedComponentCSS, ".vibe-report {",
		"shared CSS owns the report screen styling")
}
