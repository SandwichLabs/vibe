package artifacts

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodScript = "function draw(ctx) { return '<svg xmlns=\"http://www.w3.org/2000/svg\"/>'; }\n"

func newScriptArtifact(t *testing.T, name string) *Artifact {
	t.Helper()
	a := newArtifact(t, name)
	a.Manifest.View = ViewScript
	a.View = []byte(goodScript)
	return a
}

func mustHash(t *testing.T, m []byte, files []ViewPart) string {
	t.Helper()
	h, err := ViewHash(m, files)
	require.NoError(t, err)
	return h
}

func mustHashTag(t *testing.T, tag string, m []byte, files []ViewPart) string {
	t.Helper()
	h, err := hashWithTag(tag, m, files)
	require.NoError(t, err)
	return h
}

func TestViewHash_RefusesDuplicatePaths(t *testing.T) {
	_, err := ViewHash([]byte("m"), []ViewPart{{Path: "a", Data: []byte("1")}, {Path: "a", Data: []byte("2")}})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestViewHash_IsVersionTaggedAndDeterministic(t *testing.T) {
	m := []byte("version: 1\n")
	files := []ViewPart{{Path: ScriptFile, Data: []byte("a")}}
	h := mustHash(t, m, files)
	assert.Equal(t, h, mustHash(t, m, files))
	assert.Regexp(t, `^[0-9a-f]{64}$`, h)

	// The tag is part of the stream: the same canonical bytes under another tag
	// hash differently, so a later encoding can never collide with this one.
	assert.NotEqual(t, h, mustHashTag(t, "slaw-view-hash/2", m, files))
	assert.Equal(t, h, mustHashTag(t, hashTag, m, files))
}

func TestViewHash_BytesCannotMoveAcrossABoundary(t *testing.T) {
	// A <script> in a trailing YAML comment at allow time, moved to the start of
	// view.js later, is a different hash.
	tail := "# <script>x</script>\n"
	before := mustHash(t, []byte("version: 1\n"+tail), []ViewPart{{Path: ScriptFile, Data: []byte("draw()")}})
	after := mustHash(t, []byte("version: 1\n"), []ViewPart{{Path: ScriptFile, Data: []byte(tail + "draw()")}})
	assert.NotEqual(t, before, after)

	// Plain concatenation would collide; the length prefixes keep these apart.
	a := mustHash(t, []byte("ab"), []ViewPart{{Path: "p", Data: []byte("c")}})
	b := mustHash(t, []byte("a"), []ViewPart{{Path: "p", Data: []byte("bc")}})
	assert.NotEqual(t, a, b)
	// A byte moved between a file's path and its content too.
	c := mustHash(t, []byte("m"), []ViewPart{{Path: "pq", Data: []byte("r")}})
	d := mustHash(t, []byte("m"), []ViewPart{{Path: "p", Data: []byte("qr")}})
	assert.NotEqual(t, c, d)
}

func TestViewHash_FileSetIsOrderIndependentButNamesCount(t *testing.T) {
	m := []byte("m")
	x := ViewPart{Path: "html/a.js", Data: []byte("1")}
	y := ViewPart{Path: "html/b.js", Data: []byte("2")}
	assert.Equal(t, mustHash(t, m, []ViewPart{x, y}), mustHash(t, m, []ViewPart{y, x}), "sorted by path")
	renamed := ViewPart{Path: "html/c.js", Data: []byte("1")}
	assert.NotEqual(t, mustHash(t, m, []ViewPart{x, y}), mustHash(t, m, []ViewPart{renamed, y}))
	assert.NotEqual(t, mustHash(t, m, []ViewPart{x, y}), mustHash(t, m, []ViewPart{x}), "a removed file")
	assert.NotEqual(t, mustHash(t, m, nil), mustHash(t, m, []ViewPart{{Path: "p", Data: nil}}), "an empty file is not no file")
	// The input slice is left in its order.
	in := []ViewPart{y, x}
	mustHash(t, m, in)
	assert.Equal(t, "html/b.js", in[0].Path)
}

func TestValidateView_Script(t *testing.T) {
	m := &Manifest{View: ViewScript}
	require.NoError(t, ValidateView(m, []byte(goodScript)))
	for name, src := range map[string]string{
		"empty":      "",
		"blank":      "  \n\t",
		"nul":        "draw()\x00",
		"not utf-8":  "draw(\xff\xfe)",
		"over a MiB": strings.Repeat("a", MaxScriptBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateView(m, []byte(src))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
	require.NoError(t, ValidateView(m, []byte(strings.Repeat("a", MaxScriptBytes))), "exactly the cap is fine")
	_, err := RenderView(m, []byte(goodScript))
	assert.ErrorIs(t, err, ErrInvalid, "a script view has no A2UI page")
}

func TestManifest_ViewKindIsClosed(t *testing.T) {
	a := newArtifact(t, "x")
	a.Manifest.View = "pdf"
	_, err := Encode(&a.Manifest)
	require.ErrorIs(t, err, ErrInvalid)
	a.Manifest.View = ViewScript
	_, err = Encode(&a.Manifest)
	require.NoError(t, err)
}

func TestFileStore_ScriptViewRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	a := newScriptArtifact(t, "chart")
	require.NoError(t, s.Put(ctx, a))

	js, err := os.ReadFile(fsPath(root, "chart", ScriptFile))
	require.NoError(t, err)
	assert.Equal(t, goodScript, string(js), "stored byte for byte")
	assert.NoFileExists(t, fsPath(root, "chart", ViewFile), "no view.a2ui.json beside a script view")

	got, err := s.Get(ctx, "chart")
	require.NoError(t, err)
	assert.Equal(t, ViewScript, got.Manifest.View)
	assert.Equal(t, goodScript, string(got.View))
	assert.Equal(t, a.Rev, got.Rev)

	src, err := s.Source(ctx, "chart")
	require.NoError(t, err)
	assert.Equal(t, ScriptFile, src.ViewFile)
	assert.Equal(t, goodScript, src.View)

	list, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Empty(t, list[0].Error)
}

func TestFileStore_ScriptRevCoversViewJS(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	a := newScriptArtifact(t, "chart")
	require.NoError(t, s.Put(ctx, a))
	require.NoError(t, os.WriteFile(fsPath(root, "chart", ScriptFile), []byte("// edited\n"+goodScript), 0o644))
	again := newScriptArtifact(t, "chart")
	again.Rev = a.Rev
	assert.ErrorIs(t, s.Put(ctx, again), ErrConflict, "an edit to view.js is a conflict like any other file")
}

func TestFileStore_ViewKindMismatchesAreBroken(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(t *testing.T, root string){
		"script manifest with only view.a2ui.json": func(t *testing.T, root string) {
			require.NoError(t, os.Remove(fsPath(root, "chart", ScriptFile)))
			require.NoError(t, os.WriteFile(fsPath(root, "chart", ViewFile), []byte(goodView), 0o644))
		},
		"both view files": func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(fsPath(root, "chart", ViewFile), []byte(goodView), 0o644))
		},
		"missing view.js": func(t *testing.T, root string) {
			require.NoError(t, os.Remove(fsPath(root, "chart", ScriptFile)))
		},
		"oversized view.js": func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(fsPath(root, "chart", ScriptFile), []byte(strings.Repeat("a", MaxScriptBytes+1)), 0o644))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			s := NewFileStore(root)
			require.NoError(t, s.Put(ctx, newScriptArtifact(t, "chart")))
			mutate(t, root)
			_, err := s.Get(ctx, "chart")
			require.ErrorIs(t, err, ErrInvalid)
			list, err := s.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.NotEmpty(t, list[0].Error)
		})
	}
}

func TestFileStore_ChangingTheViewKindRemovesTheOldFile(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	a := putFresh(t, s, "chart") // a2ui
	assert.FileExists(t, fsPath(root, "chart", ViewFile))

	toScript := newScriptArtifact(t, "chart")
	toScript.Rev = a.Rev
	require.NoError(t, s.Put(ctx, toScript))
	assert.NoFileExists(t, fsPath(root, "chart", ViewFile))
	assert.FileExists(t, fsPath(root, "chart", ScriptFile))
	_, err := s.Get(ctx, "chart")
	require.NoError(t, err)

	back := newArtifact(t, "chart")
	back.Rev = toScript.Rev
	require.NoError(t, s.Put(ctx, back))
	assert.NoFileExists(t, fsPath(root, "chart", ScriptFile))
	assert.FileExists(t, fsPath(root, "chart", ViewFile))
}

func TestFileStore_DeleteRemovesTheScriptToo(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	ctx := context.Background()
	require.NoError(t, s.Put(ctx, newScriptArtifact(t, "chart")))
	require.NoError(t, s.Delete(ctx, "chart"))
	assert.NoDirExists(t, fsPath(root, "chart", ""))
}

func TestMock_ScriptView(t *testing.T) {
	m := &Mock{}
	ctx := context.Background()
	require.NoError(t, m.Put(ctx, newScriptArtifact(t, "chart")))
	src, err := m.Source(ctx, "chart")
	require.NoError(t, err)
	assert.Equal(t, ScriptFile, src.ViewFile)
	assert.Equal(t, goodScript, src.View)
}

func TestValidateScript_RefusesCharactersThatHideCode(t *testing.T) {
	m := &Manifest{View: ViewScript}
	for name, r := range map[string]string{
		"NUL": "\x00", "ESC": "\x1b", "BEL": "\x07", "DEL": "\x7f", "C1 NEL": "\u0085", "C1 CSI": "\u009b",
		"RLO": "\u202e", "LRI": "\u2066", "PDI": "\u2069", "LRM": "\u200e", "RLM": "\u200f",
		"ZWSP": "\u200b", "ZWJ": "\u200d", "WJ": "\u2060", "BOM": "\ufeff",
		"LS": "\u2028", "PS": "\u2029", "SHY": "\u00ad", "ALM": "\u061c", "IAA": "\ufff9", "ORC": "\ufffc",
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateView(m, []byte("draw()"+r+"\n"))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
	require.NoError(t, ValidateView(m, []byte("a\tb\r\n\u00e9 \u4e2d\n")), "tab, CR, LF and ordinary non-ASCII are fine")
}

// JavaScript ends a line comment at U+2028, so this runs evil() while an
// editor or terminal shows one comment line.
func TestValidateScript_RefusesALineSeparatorEndingAComment(t *testing.T) {
	err := ValidateView(&Manifest{View: ViewScript}, []byte("// harmless comment\u2028evil()\n"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalid)
	assert.Contains(t, err.Error(), "U+2028")
}

func TestDecode_RefusesControlCharactersInTheManifest(t *testing.T) {
	for name, r := range map[string]string{"ESC": "\x1b", "NUL": "\x00", "C1": "\u009b", "RLO": "\u202e", "DEL": "\x7f", "LS": "\u2028", "PS": "\u2029"} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(goodManifest + "# note" + r + "\n"))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
	_, err := Decode([]byte(goodManifest + "# caf\u00e9\n"))
	require.NoError(t, err)
}

func TestRenderPage_ChecksAScriptViewsPageAgainstTheManifest(t *testing.T) {
	a := newScriptArtifact(t, "chart")
	m := &a.Manifest
	require.NoError(t, Validate(m))
	out, err := RenderPage(m, []byte(`{"title":"T","components":[{"id":"a","type":"text","text":"hi"}]}`))
	require.NoError(t, err)
	assert.Contains(t, string(out), `"hi"`)

	for name, page := range map[string]string{
		"unknown type":          `{"title":"T","components":[{"id":"a","type":"iframe"}]}`,
		"unknown field":         `{"title":"T","components":[{"id":"a","type":"text","text":"x","onclick":"x()"}]}`,
		"query not in manifest": `{"title":"T","components":[{"id":"a","type":"table","query":"nope"}]}`,
		"not an object":         `[1]`,
		"not json":              `{`,
		"over the cap":          `{"title":"` + strings.Repeat("a", MaxViewBytes) + `","components":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := RenderPage(m, []byte(page))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}
