package bundle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

const artManifest = `# kept as written, comment and all
version: 1
title: Sales by region
parameters:
  - name: min_id
    type: integer
    default: "0"
queries:
  - name: rows
    sql: SELECT 1 AS id WHERE 1 >= $min_id
`

const artView = `{"title":"Sales","components":[{"id":"t","type":"table","query":"rows"}]}`

func putArtifact(t *testing.T, ws workspace.Workspace, name, manifest, view string) {
	t.Helper()
	require.NoError(t, ws.Artifacts().Put(context.Background(), &artifacts.Artifact{Name: name, ManifestText: []byte(manifest), View: []byte(view)}, artifacts.WithRev("")))
}

func artFiles(t *testing.T, root, name string) (string, string) {
	t.Helper()
	m, err := os.ReadFile(filepath.Join(root, "artifacts", name, "artifact.yaml"))
	require.NoError(t, err)
	v, err := os.ReadFile(filepath.Join(root, "artifacts", name, "view.a2ui.json"))
	require.NoError(t, err)
	return string(m), string(v)
}

func exportArtifacts(t *testing.T, ws workspace.Workspace, names ...string) []byte {
	t.Helper()
	res, err := Export(context.Background(), ws, ExportOptions{Selection: Selection{Artifacts: names}})
	require.NoError(t, err)
	return res.Data
}

func TestArtifact_RoundTripIsByteIdentical(t *testing.T) {
	src, _ := newWS(t)
	putArtifact(t, src, "sales", artManifest, artView)

	res, err := Export(context.Background(), src, ExportOptions{Selection: Selection{Artifacts: []string{"sales"}}})
	require.NoError(t, err)
	require.Len(t, res.Manifest.Contents, 2)
	assert.Equal(t, "artifacts/sales/artifact.yaml", res.Manifest.Contents[0].Path)
	assert.Equal(t, "artifacts/sales/view.a2ui.json", res.Manifest.Contents[1].Path)
	assert.Equal(t, 1, res.Manifest.Counts().Artifacts, "two entries are one artifact")

	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	items := pkg.Items()
	require.Len(t, items, 1)
	assert.Equal(t, "artifact/sales", items[0].Key)
	assert.Equal(t, "Sales by region", items[0].Title)

	dst, _ := newWS(t)
	out, err := pkg.Import(context.Background(), dst, ImportOptions{})
	require.NoError(t, err)
	require.Len(t, out.Outcomes, 1)
	assert.Equal(t, "created", out.Outcomes[0].Result, out.Outcomes[0].Error)

	wantM, wantV := artFiles(t, src.Root(), "sales")
	gotM, gotV := artFiles(t, dst.Root(), "sales")
	assert.Equal(t, wantM, gotM)
	assert.Equal(t, wantV, gotV)
}

func TestArtifact_SelectAllAndContentsOf(t *testing.T) {
	ws, _ := newWS(t)
	putArtifact(t, ws, "sales", artManifest, artView)
	// A folder that is not a usable artifact is listed by the store with an
	// error: not offered, and skipped with a warning by Selection.All.
	require.NoError(t, os.MkdirAll(filepath.Join(ws.Root(), "artifacts", "broken"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws.Root(), "artifacts", "broken", "artifact.yaml"), []byte("nope: 1\n"), 0o644))

	c, err := ContentsOf(context.Background(), ws)
	require.NoError(t, err)
	require.Len(t, c.Artifacts, 1)
	assert.Equal(t, ContentItem{ID: "sales", Title: "Sales by region"}, c.Artifacts[0])

	res, err := Export(context.Background(), ws, ExportOptions{Selection: Selection{All: true}})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Manifest.Counts().Artifacts)
	require.Len(t, res.Manifest.Warnings, 1)
	assert.Contains(t, res.Manifest.Warnings[0], "artifacts/broken was skipped")

	_, err = Export(context.Background(), ws, ExportOptions{Selection: Selection{Artifacts: []string{"broken"}}})
	require.Error(t, err, "naming a broken artifact fails the export")
	_, err = Export(context.Background(), ws, ExportOptions{Selection: Selection{Artifacts: []string{"Bad/Name"}}})
	require.Error(t, err)
}

func TestArtifact_OpenRefusesWholeBundle(t *testing.T) {
	good := func() map[string][]byte {
		return map[string][]byte{
			"artifacts/sales/artifact.yaml":  []byte(artManifest),
			"artifacts/sales/view.a2ui.json": []byte(artView),
		}
	}
	cases := []struct {
		name   string
		mutate func(map[string][]byte)
	}{
		{"parent traversal", func(e map[string][]byte) { e["artifacts/../x"] = []byte("x") }},
		{"traversal inside an artifact", func(e map[string][]byte) { e["artifacts/sales/../../evil/artifact.yaml"] = []byte("x") }},
		{"nested path", func(e map[string][]byte) { e["artifacts/sales/sub/artifact.yaml"] = []byte(artManifest) }},
		{"unknown file in the folder", func(e map[string][]byte) { e["artifacts/sales/notes.txt"] = []byte("hi") }},
		{"view.html before level 2", func(e map[string][]byte) { e["artifacts/sales/view.html"] = []byte("<script>alert(1)</script>") }},
		{"only the manifest", func(e map[string][]byte) { delete(e, "artifacts/sales/view.a2ui.json") }},
		{"only the view", func(e map[string][]byte) { delete(e, "artifacts/sales/artifact.yaml") }},
		{"manifest fails Decode", func(e map[string][]byte) {
			e["artifacts/sales/artifact.yaml"] = []byte(strings.Replace(artManifest, "title:", "surprise: 1\ntitle:", 1))
		}},
		{"manifest with an alias", func(e map[string][]byte) {
			e["artifacts/sales/artifact.yaml"] = []byte(strings.Replace(artManifest, "version: 1", "version: &v 1", 1))
		}},
		{"view fails ValidateView", func(e map[string][]byte) {
			e["artifacts/sales/view.a2ui.json"] = []byte(`{"title":"x","components":[{"id":"t","type":"table","query":"nope"}]}`)
		}},
		{"view is not JSON", func(e map[string][]byte) { e["artifacts/sales/view.a2ui.json"] = []byte("{") }},
		{"upper-case name", func(e map[string][]byte) {
			e["artifacts/Sales/artifact.yaml"] = []byte(artManifest)
			e["artifacts/Sales/view.a2ui.json"] = []byte(artView)
		}},
		{"a name that is a device name", func(e map[string][]byte) {
			e["artifacts/con/artifact.yaml"] = []byte(artManifest)
			e["artifacts/con/view.a2ui.json"] = []byte(artView)
		}},
		{"artifact file at the top", func(e map[string][]byte) { e["artifacts/artifact.yaml"] = []byte(artManifest) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entries := good()
			c.mutate(entries)
			_, err := Open(build(t, entries, nil), "")
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrCorrupt)
		})
	}

	t.Run("the good bundle opens", func(t *testing.T) {
		_, err := Open(build(t, good(), nil), "")
		require.NoError(t, err)
	})
	t.Run("the manifest lists a lone half", func(t *testing.T) {
		// Listed and present in the archive, but its sibling is not.
		entries := map[string][]byte{"artifacts/sales/artifact.yaml": []byte(artManifest)}
		_, err := Open(build(t, entries, nil), "")
		assert.ErrorIs(t, err, ErrCorrupt)
	})
}

func TestArtifact_ImportConflicts(t *testing.T) {
	src, _ := newWS(t)
	putArtifact(t, src, "sales", artManifest, artView)
	data := exportArtifacts(t, src, "sales")
	ctx := context.Background()

	open := func(t *testing.T) *Package {
		pkg, err := Open(data, "")
		require.NoError(t, err)
		return pkg
	}
	mine := strings.Replace(artManifest, "Sales by region", "My own title", 1)
	newDst := func(t *testing.T) workspace.Workspace {
		dst, _ := newWS(t)
		putArtifact(t, dst, "sales", mine, artView)
		return dst
	}

	t.Run("rename keeps both", func(t *testing.T) {
		dst := newDst(t)
		pkg := open(t)
		require.True(t, pkg.Preview(ctx, dst).Items[0].Conflict)
		res, err := pkg.Import(ctx, dst, ImportOptions{})
		require.NoError(t, err)
		assert.Equal(t, "renamed", res.Outcomes[0].Result, res.Outcomes[0].Error)
		assert.Equal(t, "sales-2", res.Outcomes[0].FinalID)
		m, _ := artFiles(t, dst.Root(), "sales")
		assert.Equal(t, mine, m)
		m2, _ := artFiles(t, dst.Root(), "sales-2")
		assert.Equal(t, artManifest, m2)
	})
	t.Run("skip", func(t *testing.T) {
		dst := newDst(t)
		res, err := open(t).Import(ctx, dst, ImportOptions{OnConflict: ActionSkip})
		require.NoError(t, err)
		assert.Equal(t, "skipped", res.Outcomes[0].Result)
		m, _ := artFiles(t, dst.Root(), "sales")
		assert.Equal(t, mine, m)
	})
	t.Run("overwrite replaces the version the preview saw", func(t *testing.T) {
		dst := newDst(t)
		pkg := open(t)
		pkg.Preview(ctx, dst)
		res, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionOverwrite})
		require.NoError(t, err)
		assert.Equal(t, "overwritten", res.Outcomes[0].Result, res.Outcomes[0].Error)
		m, _ := artFiles(t, dst.Root(), "sales")
		assert.Equal(t, artManifest, m)
	})
	t.Run("overwrite is a conflict when the artifact changed after the preview", func(t *testing.T) {
		dst := newDst(t)
		pkg := open(t)
		pkg.Preview(ctx, dst)
		changed := strings.Replace(mine, "My own title", "Changed since", 1)
		require.NoError(t, os.WriteFile(filepath.Join(dst.Root(), "artifacts", "sales", "artifact.yaml"), []byte(changed), 0o644))
		res, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionOverwrite})
		require.NoError(t, err)
		assert.Equal(t, "failed", res.Outcomes[0].Result)
		assert.NotEmpty(t, res.Outcomes[0].Error)
		m, _ := artFiles(t, dst.Root(), "sales")
		assert.Equal(t, changed, m, "never clobbered")
	})
	t.Run("dry run writes nothing", func(t *testing.T) {
		dst, _ := newWS(t)
		res, err := open(t).Import(ctx, dst, ImportOptions{DryRun: true})
		require.NoError(t, err)
		assert.Equal(t, "created", res.Outcomes[0].Result)
		_, statErr := os.Stat(filepath.Join(dst.Root(), "artifacts", "sales"))
		assert.True(t, os.IsNotExist(statErr))
	})
}

func TestArtifact_ExportValidatesBothFilesPresent(t *testing.T) {
	ws, _ := newWS(t)
	putArtifact(t, ws, "good", artManifest, artView)
	dir := filepath.Join(ws.Root(), "artifacts", "bad")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte("version: 1\ntitle: Bad\nsurprise: 1\nqueries: []\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "view.a2ui.json"), []byte(artView), 0o644))

	_, err := Export(context.Background(), ws, ExportOptions{Selection: Selection{Artifacts: []string{"bad"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact/bad")
	assert.NotContains(t, err.Error(), "surprise", "decoder detail stays out")

	res, err := Export(context.Background(), ws, ExportOptions{Selection: Selection{All: true}})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Manifest.Counts().Artifacts)
	require.Len(t, res.Manifest.Warnings, 1)
	assert.Contains(t, res.Manifest.Warnings[0], "artifacts/bad was skipped")
	assert.NotContains(t, res.Manifest.Warnings[0], "damaged")
	assert.NotContains(t, res.Manifest.Warnings[0], "surprise")
}

func TestArtifactParts_AddRefusesUnknownNamesAndFilledSlots(t *testing.T) {
	ap := artifactParts{}
	m := Entry{Path: "artifacts/a/artifact.yaml", Kind: KindArtifact, ID: "a"}
	v := Entry{Path: "artifacts/a/view.a2ui.json", Kind: KindArtifact, ID: "a"}
	require.NoError(t, ap.add(m, []byte("m")))
	require.NoError(t, ap.add(v, []byte("v")))
	assert.ErrorIs(t, ap.add(m, []byte("m2")), ErrCorrupt)
	assert.ErrorIs(t, ap.add(v, []byte("v2")), ErrCorrupt)
	assert.ErrorIs(t, artifactParts{}.add(Entry{Path: "artifacts/a/view.html", Kind: KindArtifact, ID: "a"}, nil), ErrCorrupt)
}

// A symbolic link where the store folder should be makes every candidate name
// unreadable: Keep both must fail the item, not spin looking for a free name.
func TestImport_SymlinkedStoreFolderFailsTheItemInsteadOfHanging(t *testing.T) {
	src, _ := newWS(t)
	putArtifact(t, src, "sales", artManifest, artView)
	seedQ := &queries.Query{Title: "Top", SQL: "SELECT 1"}
	require.NoError(t, src.Queries().Create(context.Background(), seedQ))
	data, err := Export(context.Background(), src, ExportOptions{Selection: Selection{All: true}})
	require.NoError(t, err)

	dst, _ := newWS(t)
	outside := t.TempDir()
	for _, d := range []string{"artifacts", "queries"} {
		require.NoError(t, os.RemoveAll(filepath.Join(dst.Root(), d)))
		require.NoError(t, os.Symlink(outside, filepath.Join(dst.Root(), d)))
	}
	pkg, err := Open(data.Data, "")
	require.NoError(t, err)

	done := make(chan Result, 1)
	go func() {
		res, _ := pkg.Import(context.Background(), dst, ImportOptions{OnConflict: ActionRename})
		done <- res
	}()
	select {
	case res := <-done:
		require.NotEmpty(t, res.Outcomes)
		for _, o := range res.Outcomes {
			if o.Kind == KindArtifact || o.Kind == KindQuery {
				assert.Equal(t, "failed", o.Result, o.Key)
				assert.NotEmpty(t, o.Error)
			}
		}
	case <-time.After(20 * time.Second):
		t.Fatal("import hung on a symlinked store folder")
	}
	entries, _ := os.ReadDir(outside)
	assert.Empty(t, entries, "nothing was written through the link")
}

const scriptManifest = "version: 1\ntitle: Chart\nview: script\nqueries:\n  - name: q\n    sql: SELECT 1\n"
const scriptSource = "function draw(ctx) { return '<svg xmlns=\"http://www.w3.org/2000/svg\"/>'; }\n"

func TestArtifact_ScriptViewRoundTripsAndCarriesNoDecision(t *testing.T) {
	src, _ := newWS(t)
	require.NoError(t, src.Artifacts().Put(context.Background(),
		&artifacts.Artifact{Name: "chart", ManifestText: []byte(scriptManifest), View: []byte(scriptSource)}, artifacts.WithRev("")))
	st, err := src.ArtifactViewState("chart")
	require.NoError(t, err)
	require.NoError(t, src.AllowArtifactView("chart", st.Hash))

	res, err := Export(context.Background(), src, ExportOptions{Selection: Selection{Artifacts: []string{"chart"}}})
	require.NoError(t, err)
	require.Len(t, res.Manifest.Contents, 2)
	assert.Equal(t, "artifacts/chart/artifact.yaml", res.Manifest.Contents[0].Path)
	assert.Equal(t, "artifacts/chart/view.js", res.Manifest.Contents[1].Path)
	assert.NotContains(t, string(res.Data), "views.json")

	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	dst, _ := newWS(t)
	out, err := pkg.Import(context.Background(), dst, ImportOptions{})
	require.NoError(t, err)
	require.Len(t, out.Outcomes, 1)
	assert.Equal(t, "created", out.Outcomes[0].Result, out.Outcomes[0].Error)

	js, err := os.ReadFile(filepath.Join(dst.Root(), "artifacts", "chart", "view.js"))
	require.NoError(t, err)
	assert.Equal(t, scriptSource, string(js))
	got, err := dst.ArtifactViewState("chart")
	require.NoError(t, err)
	assert.Equal(t, "undecided", got.State, "an allow never travels in a bundle: it shows as source on the other side")
	assert.Equal(t, st.Hash, got.Hash)
}

func TestArtifact_ScriptViewFileMustMatchTheManifest(t *testing.T) {
	cases := map[string]map[string][]byte{
		"script manifest with a2ui view": {
			"artifacts/chart/artifact.yaml":  []byte(scriptManifest),
			"artifacts/chart/view.a2ui.json": []byte(artView),
		},
		"a2ui manifest with view.js": {
			"artifacts/sales/artifact.yaml": []byte(artManifest),
			"artifacts/sales/view.js":       []byte(scriptSource),
		},
		"both view files": {
			"artifacts/chart/artifact.yaml":  []byte(scriptManifest),
			"artifacts/chart/view.js":        []byte(scriptSource),
			"artifacts/chart/view.a2ui.json": []byte(artView),
		},
		"empty view.js": {
			"artifacts/chart/artifact.yaml": []byte(scriptManifest),
			"artifacts/chart/view.js":       []byte(" \n"),
		},
		"view.js over the cap": {
			"artifacts/chart/artifact.yaml": []byte(scriptManifest),
			"artifacts/chart/view.js":       []byte(strings.Repeat("a", artifacts.MaxScriptBytes+1)),
		},
		"script manifest with no view": {
			"artifacts/chart/artifact.yaml": []byte(scriptManifest),
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Open(build(t, entries, nil), "")
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrCorrupt)
		})
	}
}

const pageManifestText = `version: 1
title: Regional page
view: html
parameters:
  - name: min_id
    type: integer
    default: "0"
queries:
  - name: rows
    sql: SELECT 1 AS id WHERE 1 >= $min_id
`

const pageTemplate = `<!doctype html><html><head><link rel="stylesheet" href="assets/style.css"></head><body>{{ range (query "rows").Rows }}<p>{{ index . 0 }}</p>{{ end }}</body></html>`

func putPage(t *testing.T, ws workspace.Workspace, name string, extra ...artifacts.PageFile) {
	t.Helper()
	files := append([]artifacts.PageFile{
		{Path: "index.html.tmpl", Data: []byte(pageTemplate)},
		{Path: "assets/style.css", Data: []byte("p { color: red; }")},
		{Path: "assets/logo.png", Data: []byte("\x89PNG\r\n\x1a\n\x00")},
	}, extra...)
	require.NoError(t, ws.Artifacts().Put(context.Background(), &artifacts.Artifact{Name: name, ManifestText: []byte(pageManifestText), Files: files}, artifacts.WithRev("")))
}

// A page travels as its manifest plus one entry per html/ file, byte for byte,
// and arrives undecided: a bundle never carries an allow.
func TestArtifact_HTMLPageRoundTripsAndCarriesNoDecision(t *testing.T) {
	src, _ := newWS(t)
	putPage(t, src, "page")
	srcState, err := src.ArtifactViewState("page")
	require.NoError(t, err)
	require.NoError(t, src.AllowArtifactView("page", srcState.Hash))

	res, err := Export(context.Background(), src, ExportOptions{Selection: Selection{Artifacts: []string{"page"}}})
	require.NoError(t, err)
	var paths []string
	for _, e := range res.Manifest.Contents {
		paths = append(paths, e.Path)
	}
	assert.ElementsMatch(t, []string{
		"artifacts/page/artifact.yaml", "artifacts/page/html/index.html.tmpl",
		"artifacts/page/html/assets/style.css", "artifacts/page/html/assets/logo.png",
	}, paths)
	assert.Equal(t, 1, res.Manifest.Counts().Artifacts, "a page is one artifact however many entries")

	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	require.Len(t, pkg.Items(), 1)
	assert.Equal(t, "Regional page", pkg.Items()[0].Title)

	dst, _ := newWS(t)
	out, err := pkg.Import(context.Background(), dst, ImportOptions{})
	require.NoError(t, err)
	require.Len(t, out.Outcomes, 1)
	assert.Equal(t, "created", out.Outcomes[0].Result, out.Outcomes[0].Error)
	for _, f := range []string{"artifact.yaml", "html/index.html.tmpl", "html/assets/style.css", "html/assets/logo.png"} {
		want, err := os.ReadFile(filepath.Join(src.Root(), "artifacts", "page", filepath.FromSlash(f)))
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(dst.Root(), "artifacts", "page", filepath.FromSlash(f)))
		require.NoError(t, err)
		assert.Equal(t, want, got, f)
	}
	st, err := dst.ArtifactViewState("page")
	require.NoError(t, err)
	assert.Equal(t, "undecided", st.State, "an allow never travels")
	assert.Equal(t, srcState.Hash, st.Hash, "the same files hash the same")
	assert.Equal(t, "static", st.PageKind)
}

// Everything is checked while the bundle opens, before any store is written.
func TestArtifact_HTMLPageOpenRefusesWholeBundle(t *testing.T) {
	good := func() map[string][]byte {
		return map[string][]byte{
			"artifacts/page/artifact.yaml":         []byte(pageManifestText),
			"artifacts/page/html/index.html.tmpl":  []byte(pageTemplate),
			"artifacts/page/html/assets/style.css": []byte("p{}"),
		}
	}
	cases := map[string]func(map[string][]byte){
		"a page with a view file too": func(e map[string][]byte) { e["artifacts/page/view.a2ui.json"] = []byte(artView) },
		"a page with no manifest":     func(e map[string][]byte) { delete(e, "artifacts/page/artifact.yaml") },
		"a page with no entry":        func(e map[string][]byte) { delete(e, "artifacts/page/html/index.html.tmpl") },
		"an a2ui manifest with html/": func(e map[string][]byte) {
			e["artifacts/page/artifact.yaml"] = []byte(strings.Replace(pageManifestText, "view: html\n", "", 1))
		},
		"a path escape":        func(e map[string][]byte) { e["artifacts/page/html/../../x/artifact.yaml"] = []byte("x") },
		"a folder in html/":    func(e map[string][]byte) { e["artifacts/page/html/sub/a.tmpl"] = []byte("x") },
		"a deep folder":        func(e map[string][]byte) { e["artifacts/page/html/assets/a/b/c.css"] = []byte("x") },
		"a dotfile":            func(e map[string][]byte) { e["artifacts/page/html/assets/.env"] = []byte("x") },
		"a reserved folder":    func(e map[string][]byte) { e["artifacts/page/html/vibe-lib/x.js"] = []byte("x") },
		"an unknown extension": func(e map[string][]byte) { e["artifacts/page/html/assets/a.exe"] = []byte("x") },
		"a stylesheet at top":  func(e map[string][]byte) { e["artifacts/page/html/style.css"] = []byte("x") },
		"a template in assets": func(e map[string][]byte) { e["artifacts/page/html/assets/a.tmpl"] = []byte("x") },
		"an outside link": func(e map[string][]byte) {
			e["artifacts/page/html/index.html.tmpl"] = []byte(`<a href="https://example.com/">x</a>`)
		},
		"a preconnect": func(e map[string][]byte) {
			e["artifacts/page/html/index.html.tmpl"] = []byte(`<link rel="preconnect" href="https://example.com">`)
		},
		"an unknown function": func(e map[string][]byte) { e["artifacts/page/html/index.html.tmpl"] = []byte(`{{ safeHTML "x" }}`) },
		"a query the manifest lacks": func(e map[string][]byte) {
			e["artifacts/page/html/index.html.tmpl"] = []byte(`{{ query "nope" }}`)
		},
		"a file over the cap": func(e map[string][]byte) {
			e["artifacts/page/html/assets/big.css"] = []byte(strings.Repeat("a", artifacts.MaxPageFileBytes+1))
		},
		"a capability on a static page": func(e map[string][]byte) {
			e["artifacts/page/artifact.yaml"] = []byte(strings.Replace(pageManifestText, "view: html\n", "view: html\nhtml:\n  capabilities:\n    state: {}\n", 1))
		},
		"a case-folded duplicate": func(e map[string][]byte) { e["artifacts/page/html/assets/Style.css"] = []byte("x") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			entries := good()
			mutate(entries)
			_, err := Open(build(t, entries, nil), "")
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrCorrupt)
		})
	}
	t.Run("the good page opens", func(t *testing.T) {
		_, err := Open(build(t, good(), nil), "")
		require.NoError(t, err)
	})
}

func TestArtifact_HTMLPageConflictsAndOverwrite(t *testing.T) {
	src, _ := newWS(t)
	putPage(t, src, "page")
	data := exportArtifacts(t, src, "page")
	ctx := context.Background()
	open := func(t *testing.T) *Package {
		pkg, err := Open(data, "")
		require.NoError(t, err)
		return pkg
	}
	newDst := func(t *testing.T) workspace.Workspace {
		dst, _ := newWS(t)
		putPage(t, dst, "page", artifacts.PageFile{Path: "extra.js", Data: []byte("1")})
		return dst
	}
	extra := func(dst workspace.Workspace) string {
		return filepath.Join(dst.Root(), "artifacts", "page", "html", "extra.js")
	}

	t.Run("rename keeps both", func(t *testing.T) {
		dst := newDst(t)
		pkg := open(t)
		require.True(t, pkg.Preview(ctx, dst).Items[0].Conflict)
		res, err := pkg.Import(ctx, dst, ImportOptions{})
		require.NoError(t, err)
		assert.Equal(t, "renamed", res.Outcomes[0].Result, res.Outcomes[0].Error)
		assert.FileExists(t, extra(dst))
		assert.FileExists(t, filepath.Join(dst.Root(), "artifacts", "page-2", "html", "assets", "style.css"))
	})
	t.Run("overwrite replaces the whole page", func(t *testing.T) {
		dst := newDst(t)
		pkg := open(t)
		pkg.Preview(ctx, dst)
		res, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionOverwrite})
		require.NoError(t, err)
		assert.Equal(t, "overwritten", res.Outcomes[0].Result, res.Outcomes[0].Error)
		assert.NoFileExists(t, extra(dst), "a file the bundle does not hold is removed with the page it belonged to")
	})
	t.Run("overwrite refuses a page that changed since the preview", func(t *testing.T) {
		dst := newDst(t)
		pkg := open(t)
		pkg.Preview(ctx, dst)
		require.NoError(t, os.WriteFile(extra(dst), []byte("2"), 0o644))
		res, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionOverwrite})
		require.NoError(t, err)
		assert.Equal(t, "failed", res.Outcomes[0].Result)
		b, _ := os.ReadFile(extra(dst))
		assert.Equal(t, "2", string(b))
	})
}

func TestArtifact_PageStateIsNeverBundled(t *testing.T) {
	ws, _ := newWS(t)
	putArtifact(t, ws, "sales", artManifest, artView)
	dir := filepath.Join(ws.Root(), ".vibe-local", "artifacts", "sales")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"version":1,"values":{"leak":"QUERYRESULT"}}`), 0o644))

	res, err := Export(context.Background(), ws, ExportOptions{Selection: Selection{All: true}})
	require.NoError(t, err)
	for _, e := range res.Manifest.Contents {
		assert.NotContains(t, e.Path, "state", e.Path)
		assert.NotContains(t, e.Path, ".vibe-local", e.Path)
	}
	assert.NotContains(t, string(res.Data), "QUERYRESULT")
}
