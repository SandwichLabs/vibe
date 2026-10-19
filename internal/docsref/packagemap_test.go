package docsref

import (
	"flag"
	"fmt"
	"go/doc"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the generated package map in CLAUDE.md")

const (
	mapStart = "<!-- package-map:start -->"
	mapEnd   = "<!-- package-map:end -->"

	// maxSynopsis keeps each map line to one idea; detail belongs in the
	// comment's later paragraphs, which `go doc` shows.
	maxSynopsis = 300
)

// skipDirs are never walked for Go packages: generated, vendored or
// non-Go trees.
var skipDirs = map[string]bool{
	"node_modules": true,
	"testdata":     true,
	"vendor":       true,
	"frontend":     true, // studio/frontend: Vue, plus generated wailsjs/go
}

// pkgEntry is one Go package as the map shows it.
type pkgEntry struct {
	dir      string // slash path relative to the repo root, "." for the root
	synopsis string // first sentence of the package comment, name prefix dropped
}

// TestPackageMapIsCurrent fails when CLAUDE.md's package map differs from the
// one generated from the Go package comments, or when a package has no
// comment, more than one, or a synopsis too long to scan. Run
// `task docs` to regenerate the map.
func TestPackageMapIsCurrent(t *testing.T) {
	root := repoRoot(t)
	entries := collectPackages(t, root)
	want := renderPackageMap(entries)

	path := filepath.Join(root, "CLAUDE.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	before, got, after, ok := splitPackageMap(string(raw))
	require.True(t, ok, "CLAUDE.md needs a %s ... %s block", mapStart, mapEnd)

	if *update {
		require.False(t, t.Failed(), "fix the package comments above before regenerating")
		require.NoError(t, os.WriteFile(path, []byte(before+want+after), 0o644))
		return
	}
	if got != want {
		t.Errorf("CLAUDE.md's package map is stale: run `task docs`\n%s", lineDiff(got, want))
	}
}

// lineDiff lists the map lines only one side has, so a stale map names the
// packages that changed instead of printing the whole block twice.
func lineDiff(got, want string) string {
	in := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, l := range strings.Split(s, "\n") {
			m[l] = true
		}
		return m
	}
	g, w := in(got), in(want)
	var b strings.Builder
	for _, l := range strings.Split(got, "\n") {
		if !w[l] {
			b.WriteString("  - " + l + "\n")
		}
	}
	for _, l := range strings.Split(want, "\n") {
		if !g[l] {
			b.WriteString("  + " + l + "\n")
		}
	}
	return b.String()
}

// collectPackages reads the package comment of every Go package in the repo.
func collectPackages(t *testing.T, root string) []pkgEntry {
	t.Helper()
	dirs := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			dirs[filepath.Dir(path)] = true
		}
		return nil
	})
	require.NoError(t, err)

	var out []pkgEntry
	for dir := range dirs {
		rel, err := filepath.Rel(root, dir)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)
		synopsis, problem := packageSynopsis(dir)
		if problem != "" {
			t.Errorf("%s: %s", rel, problem)
			continue
		}
		out = append(out, pkgEntry{dir: rel, synopsis: synopsis})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
	return out
}

// packageSynopsis returns the first sentence of the package's one doc
// comment, or why it cannot. A test-only package (internal/docsref) may keep
// its comment in a _test.go file.
func packageSynopsis(dir string) (synopsis, problem string) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return "", err.Error()
	}
	var name string
	docs, testDocs := map[string]string{}, map[string]string{} // file -> comment
	for _, f := range files {
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.PackageClauseOnly|parser.ParseComments)
		if err != nil {
			return "", err.Error()
		}
		if strings.HasSuffix(af.Name.Name, "_test") {
			continue // an external test package documents nothing
		}
		name = af.Name.Name
		if af.Doc == nil {
			continue
		}
		if strings.HasSuffix(f, "_test.go") {
			testDocs[filepath.Base(f)] = af.Doc.Text()
		} else {
			docs[filepath.Base(f)] = af.Doc.Text()
		}
	}
	use := docs
	if len(use) == 0 {
		use = testDocs
	}
	if len(use) == 0 {
		return "", "no package comment: add one (doc.go, or the file named for the package)"
	}
	if len(use) > 1 {
		names := make([]string, 0, len(use))
		for f := range use {
			names = append(names, f)
		}
		sort.Strings(names)
		return "", fmt.Sprintf("more than one package comment (%s): keep one, detach the others with a blank line", strings.Join(names, ", "))
	}
	var text string
	for _, c := range use {
		text = c
	}
	if name == "main" {
		name = filepath.Base(dir)
	}
	s := synopsisPrefix(name).ReplaceAllString(new(doc.Package).Synopsis(text), "")
	if len(s) > maxSynopsis {
		return "", fmt.Sprintf("first sentence of the package comment is %d characters (max %d): move detail to a later paragraph", len(s), maxSynopsis)
	}
	return s, ""
}

// synopsisPrefix matches the conventional "Package x " / "Command x " opening
// (and a following "is "), which the map's path column already says.
func synopsisPrefix(name string) *regexp.Regexp {
	return regexp.MustCompile(`^(?:Package|Command) ` + regexp.QuoteMeta(name) + ` (?:is )?`)
}

func renderPackageMap(entries []pkgEntry) string {
	var b strings.Builder
	b.WriteString(mapStart + "\n")
	for _, e := range entries {
		label := "`" + e.dir + "/`"
		if e.dir == "." {
			label = "`legal.go` (module root)"
		}
		fmt.Fprintf(&b, "- %s — %s\n", label, e.synopsis)
	}
	b.WriteString(mapEnd)
	return b.String()
}

// splitPackageMap cuts doc around the generated block (markers included).
func splitPackageMap(doc string) (before, block, after string, ok bool) {
	i := strings.Index(doc, mapStart)
	j := strings.Index(doc, mapEnd)
	if i < 0 || j < i {
		return "", "", "", false
	}
	j += len(mapEnd)
	return doc[:i], doc[i:j], doc[j:], true
}
