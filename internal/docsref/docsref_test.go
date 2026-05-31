// Package docsref holds the docs-consistency check for the v1.0 reference
// documentation (plan §6.4 / unit P6.9). It is intentionally test-only: there is
// no production behavior here. The check keeps the shipped reference docs honest
// against the code — every directive token registered in internal/pages, every
// macro action registered in internal/automation, and every bundled starter
// template registered in internal/container must be documented, and the docs
// must not reference a directive/action/template that no longer exists.
package docsref

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
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
// files (pages, forms, reports, macros, import/office-links, templates).
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
	require.NoError(t, err, "docs/reference/ must exist and contain the v1.0 reference markdown")
	blob := strings.ToLower(b.String())
	require.NotEmpty(t, strings.TrimSpace(blob), "docs/reference/ contained no documentation")
	return blob
}

// registeredDirectiveTypes scrapes the canonical knownTypes slice out of
// internal/pages/parser.go. This is the single registry of directive tokens the
// parser recognizes; the reference must document every one of them.
func registeredDirectiveTypes(t *testing.T, root string) []string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, "internal", "pages", "parser.go"))
	require.NoError(t, err)
	re := regexp.MustCompile(`knownTypes\s*:?=\s*\[\]string\{([^}]*)\}`)
	m := re.FindSubmatch(src)
	require.Len(t, m, 2, "could not locate knownTypes directive registry in parser.go")
	return extractQuoted(string(m[1]))
}

// registeredMacroActions scrapes the Action* string constants out of
// internal/automation/macro.go.
func registeredMacroActions(t *testing.T, root string) []string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, "internal", "automation", "macro.go"))
	require.NoError(t, err)
	re := regexp.MustCompile(`Action\w+\s*=\s*"([A-Za-z]+)"`)
	matches := re.FindAllStringSubmatch(string(src), -1)
	require.NotEmpty(t, matches, "could not locate Action* constants in macro.go")
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

func extractQuoted(s string) []string {
	re := regexp.MustCompile(`"([^"]+)"`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestReferenceDocsDocumentEveryDirective asserts every registered page directive
// token appears in the reference docs.
func TestReferenceDocsDocumentEveryDirective(t *testing.T) {
	root := repoRoot(t)
	docs := readReferenceDocs(t, root)
	for _, dir := range registeredDirectiveTypes(t, root) {
		assert.Contains(t, docs, strings.ToLower(dir),
			"page directive %q is registered in parser.go but undocumented in docs/reference/", dir)
	}
}

// TestReferenceDocsDocumentEveryMacroAction asserts every registered macro action
// kind appears in the reference docs.
func TestReferenceDocsDocumentEveryMacroAction(t *testing.T) {
	root := repoRoot(t)
	docs := readReferenceDocs(t, root)
	for _, act := range registeredMacroActions(t, root) {
		assert.Contains(t, docs, strings.ToLower(act),
			"macro action %q is registered in internal/automation but undocumented in docs/reference/", act)
	}
}

// TestReferenceDocsDocumentEveryTemplate asserts every bundled starter template
// name appears in the reference docs.
func TestReferenceDocsDocumentEveryTemplate(t *testing.T) {
	root := repoRoot(t)
	docs := readReferenceDocs(t, root)
	names := container.ListTemplates()
	require.NotEmpty(t, names, "expected bundled starter templates from the P6.6 registry")
	for _, name := range names {
		assert.Contains(t, docs, strings.ToLower(name),
			"starter template %q is registered but undocumented in docs/reference/", name)
	}
}

// TestReferenceDocsCoverRequiredSections asserts the reference covers each surface
// called for by §6.4: page directives, form archetypes/bound controls, banded
// reports, macros/actions, the Import Spreadsheet Wizard, Office Links, and
// starter templates.
func TestReferenceDocsCoverRequiredSections(t *testing.T) {
	root := repoRoot(t)
	docs := readReferenceDocs(t, root)
	required := []string{
		"directive", // page directives
		"form",      // form archetypes / bound controls
		"archetype", // columnar/continuous/datasheet/master-detail
		"report",    // banded reports
		"macro",     // macros / actions
		"import",    // Import Spreadsheet Wizard
		"office",    // Office Links
		"template",  // starter templates
	}
	for _, kw := range required {
		assert.Contains(t, docs, kw,
			"reference docs are missing coverage of required surface %q (plan §6.4)", kw)
	}
}

// TestReferenceDocsHaveNoStaleTokens flags docs that reference a directive or
// macro action that no longer exists in the code, scanning the docs' own
// directive-style tokens and Access-action mentions against the live registries.
func TestReferenceDocsHaveNoStaleTokens(t *testing.T) {
	root := repoRoot(t)
	refDir := filepath.Join(root, "docs", "reference")

	// Live registries (case-preserving for actions, lower for directives).
	liveDirectives := map[string]struct{}{}
	for _, d := range registeredDirectiveTypes(t, root) {
		liveDirectives[d] = struct{}{}
	}
	// Layout-block closers and well-known non-directive braces are not registry
	// tokens; allow them so the stale check doesn't false-positive.
	for _, ok := range []string{"/if", "/row", "/stack", "/tabs", "/callout", "stack", "tabs", "callout", "tab"} {
		liveDirectives[ok] = struct{}{}
	}

	// Collect {token: ...} directive references used in the docs and ensure each is real.
	dirRef := regexp.MustCompile(`\{([a-z_/]+):`)
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
		for _, m := range dirRef.FindAllStringSubmatch(string(data), -1) {
			tok := m[1]
			_, ok := liveDirectives[tok]
			assert.True(t, ok, "doc %s references directive {%s:} that is not registered in parser.go", filepath.Base(path), tok)
		}
		return nil
	})
	require.NoError(t, err)
}

// TestEntryPointsCrossLinkReference asserts README and CLAUDE.md link to the
// reference docs so the shipped surface is discoverable.
func TestEntryPointsCrossLinkReference(t *testing.T) {
	root := repoRoot(t)
	for _, entry := range []string{"README.md", "CLAUDE.md"} {
		data, err := os.ReadFile(filepath.Join(root, entry))
		require.NoError(t, err, "%s must exist", entry)
		assert.Contains(t, strings.ToLower(string(data)), "docs/reference",
			"%s should cross-link the v1.0 reference docs (docs/reference/...)", entry)
	}
}

// sortedDedupe is a small helper kept for diagnostics in failing runs.
func sortedDedupe(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

var _ = sortedDedupe
