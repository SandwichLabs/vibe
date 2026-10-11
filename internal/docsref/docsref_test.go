// Package docsref holds the docs-consistency check for the shipped reference
// documentation. It is intentionally test-only: there is no production
// behavior here. The check keeps docs/reference/ honest against the code —
//
//   - every A2UI component type registered in internal/a2ui must be
//     documented in docs/reference/notebooks.md
//   - every bundled starter template registered in internal/templates must
//     be documented in docs/reference/
//
// #69 retired the page system (docs/reference/pages.md, the directive
// registry this test used to scrape out of the page parser, and the
// stale-directive-token check that went with it) in favor of Notebooks/A2UI
// (docs/reference/notebooks.md). #68 removed internal/automation (macros)
// along with docs/reference/macros.md, forms.md, and reports.md, so the
// macro-action and form/report-surface checks that used to live here are
// gone too.
package docsref

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SandwichLabs/vibe/internal/a2ui"
	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoRoot walks up from this test file's package dir to the module root (where
// go.mod lives) so the check can read both source files and docs by absolute path.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "reached filesystem root without finding go.mod")
		dir = parent
	}
}

// readReferenceDocs concatenates every markdown file under docs/reference/ into a
// single lowercased blob. The reference is allowed to be split across multiple
// files (notebooks, import/office-links, templates).
func readReferenceDocs(t *testing.T, root string) string {
	t.Helper()
	refDir := filepath.Join(root, "docs", "reference")
	var b strings.Builder
	err := filepath.WalkDir(refDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	require.NoError(t, err, "docs/reference/ must exist and contain the reference markdown")
	blob := strings.ToLower(b.String())
	require.NotEmpty(t, strings.TrimSpace(blob), "docs/reference/ contained no documentation")
	return blob
}

// TestReferenceDocsDocumentEveryA2UIComponent asserts every component type
// registered in internal/a2ui.Components appears in the reference docs —
// the single source of truth docs/reference/notebooks.md's component table
// must stay in sync with.
func TestReferenceDocsDocumentEveryA2UIComponent(t *testing.T) {
	root := repoRoot(t)
	docs := readReferenceDocs(t, root)
	require.NotEmpty(t, a2ui.Components, "expected the A2UI component registry to be non-empty")
	for name := range a2ui.Components {
		assert.Contains(t, docs, strings.ToLower(name),
			"a2ui component %q is registered in internal/a2ui but undocumented in docs/reference/", name)
	}
}

// TestReferenceDocsDocumentColorEnum asserts every closed A2UI color token
// appears in the reference docs, so the enum can't silently drift from what
// the docs claim is valid.
func TestReferenceDocsDocumentColorEnum(t *testing.T) {
	root := repoRoot(t)
	docs := readReferenceDocs(t, root)
	require.NotEmpty(t, a2ui.ColorTokens)
	for _, c := range a2ui.ColorTokens {
		assert.Contains(t, docs, strings.ToLower(c),
			"a2ui color token %q is registered in internal/a2ui but undocumented in docs/reference/", c)
	}
}

// TestReferenceDocsDocumentEveryTemplate asserts every bundled starter template
// name appears in the reference docs.
func TestReferenceDocsDocumentEveryTemplate(t *testing.T) {
	root := repoRoot(t)
	docs := readReferenceDocs(t, root)
	names := templates.List()
	require.NotEmpty(t, names, "expected bundled starter templates from the P6.6 registry")
	for _, name := range names {
		assert.Contains(t, docs, strings.ToLower(name),
			"starter template %q is registered but undocumented in docs/reference/", name)
	}
}

// TestReferenceDocsCoverRequiredSections asserts the reference covers each
// surface Vibe still ships (#68 dropped forms/reports/macros/switchboard;
// #69 dropped the page system): notebooks, the A2UI component schema, the
// Import Spreadsheet Wizard, Office Links, and starter templates.
func TestReferenceDocsCoverRequiredSections(t *testing.T) {
	root := repoRoot(t)
	docs := readReferenceDocs(t, root)
	required := []string{
		"notebook", // Notebooks
		"a2ui",     // A2UI component schema
		"import",   // Import Spreadsheet Wizard
		"office",   // Office Links
		"template", // starter templates
	}
	for _, kw := range required {
		assert.Contains(t, docs, kw,
			"reference docs are missing coverage of required surface %q", kw)
	}
}

// componentTableRowRe matches one row of docs/reference/notebooks.md's "##
// Component types" table: `| \`Type\` | required | optional |`. Capturing
// group 1 is the backtick-wrapped type name in the first column.
var componentTableRowRe = regexp.MustCompile("(?m)^\\|\\s*`([A-Za-z][A-Za-z0-9]*)`\\s*\\|")

// notebooksDocComponentTypes parses every component type named in
// docs/reference/notebooks.md's component-types table (the first column of
// each `| \`Type\` | ... | ... |` row).
func notebooksDocComponentTypes(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs", "reference", "notebooks.md"))
	require.NoError(t, err, "docs/reference/notebooks.md must exist")

	out := map[string]bool{}
	for _, m := range componentTableRowRe.FindAllStringSubmatch(string(data), -1) {
		out[m[1]] = true
	}
	return out
}

// TestNotebooksDocComponentTableMatchesA2UIRegistry cross-checks
// docs/reference/notebooks.md's "Component types" table against
// internal/a2ui.Components BOTH ways: every registered type must have a
// table row (TestReferenceDocsDocumentEveryA2UIComponent already partly
// covers this via a substring check, but not specifically the table), and
// every table row must name a real registered type — so a typo'd or
// stale/renamed component type in the table (not just a totally missing
// one) is caught too.
func TestNotebooksDocComponentTableMatchesA2UIRegistry(t *testing.T) {
	root := repoRoot(t)
	docTypes := notebooksDocComponentTypes(t, root)
	require.NotEmpty(t, docTypes, "docs/reference/notebooks.md's component-types table must not be empty/unparseable")

	for name := range a2ui.Components {
		assert.True(t, docTypes[name], "a2ui component %q has no row in docs/reference/notebooks.md's component-types table", name)
	}
	for name := range docTypes {
		assert.NotNil(t, a2ui.Components[name], "docs/reference/notebooks.md's component-types table lists %q, which is not (or no longer) a registered a2ui component", name)
	}
}

// fencedCodeRe strips fenced code blocks (```...```) so the stale-directive
// scan below never flags a directive mentioned INSIDE an example/quoted
// fence (docs/reference/import-office.md and notebooks.md both show real
// SQL/JSON snippets in fences that could otherwise coincidentally contain a
// brace pattern).
var fencedCodeRe = regexp.MustCompile("(?s)```.*?```")

// staleDirectiveRe matches the retired page-directive syntax
// `{directive_name: ...}` for every directive token the old page system
// ever recognized (table/chart/metric/value directives, the Access-layer
// form/detail/subform/button directives, and the P3.5/P3.6 layout/input
// directives) — the full token list #69's removal covers, not just the
// handful TestReferenceDocsDoNotReferencePageSystem spot-checks.
var staleDirectiveRe = regexp.MustCompile(`\{(table|chart|metric|value|form|detail|if|row|stack|callout|tabs|dropdown|slider|text_input|button_group|toggle|button|subform)\s*:`)

// TestReferenceDocsHaveNoStaleDirectivesOutsideCode scans every doc under
// docs/reference/, with fenced code blocks stripped, for the retired page
// directive syntax.
func TestReferenceDocsHaveNoStaleDirectivesOutsideCode(t *testing.T) {
	root := repoRoot(t)
	refDir := filepath.Join(root, "docs", "reference")

	err := filepath.WalkDir(refDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		prose := fencedCodeRe.ReplaceAll(data, nil)
		for _, m := range staleDirectiveRe.FindAllString(string(prose), -1) {
			assert.Fail(t, "stale page directive found outside a fenced code block",
				"%s references %q — the page directive system was removed in #69", filepath.Base(path), m)
		}
		return nil
	})
	require.NoError(t, err)
}

// TestReferenceDocsDoNotReferencePageSystem guards against the retired page
// directive syntax and its render entry point creeping back into the docs
// now that docs/reference/pages.md is gone. It does not forbid mentioning
// retired tool names in prose (notebooks.md explains "there is no
// create_page/list_pages tool" on purpose, documenting the absence).
func TestReferenceDocsDoNotReferencePageSystem(t *testing.T) {
	root := repoRoot(t)
	docs := readReferenceDocs(t, root)
	for _, stale := range []string{"renderpagecontent", "{table:}", "{chart:}", "{metric:}"} {
		assert.NotContains(t, docs, stale,
			"docs/reference/ references the retired page system (%q) — #69 removed it", stale)
	}
}

// TestEntryPointsCrossLinkReference asserts README and CLAUDE.md link to the
// reference docs so the shipped surface is discoverable.
func TestEntryPointsCrossLinkReference(t *testing.T) {
	root := repoRoot(t)
	for _, entry := range []string{"README.md", "CLAUDE.md"} {
		data, err := os.ReadFile(filepath.Join(root, entry))
		require.NoError(t, err, "%s must exist", entry)
		assert.Contains(t, strings.ToLower(string(data)), "docs/reference",
			"%s should cross-link the reference docs (docs/reference/...)", entry)
	}
}

// TestReferenceDocsDocumentNotebookV2 asserts notebook format v2 (#108) is
// documented: the schema version, the cell-level `connection`, and every
// A2UI component that accepts a `connection` binding field.
func TestReferenceDocsDocumentNotebookV2(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "reference", "notebooks.md"))
	require.NoError(t, err)
	doc := string(data)
	assert.Contains(t, doc, "schemaVersion: 2", "notebooks.md must state the current format version")
	assert.Equal(t, 2, notebooks.SchemaVersion, "update notebooks.md's format-version section when the schema version changes")
	assert.Contains(t, doc, "`connection`")

	// Every component row that lists `connection` in the table must be one the
	// validator really accepts it on, and vice versa.
	rowRE := regexp.MustCompile("(?m)^\\| `([A-Za-z]+)` \\|.*$")
	documented := map[string]bool{}
	for _, m := range rowRE.FindAllStringSubmatch(doc, -1) {
		if strings.Contains(m[0], "`connection`") {
			documented[m[1]] = true
		}
	}
	for _, name := range a2ui.DataBound {
		assert.True(t, documented[name], "data-bound component %q accepts `connection` but notebooks.md's table does not list it", name)
	}
	assert.Len(t, documented, len(a2ui.DataBound), "only the data-bound components take `connection`")
}
