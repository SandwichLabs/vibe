package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/workspace"
)

const cliScriptManifest = "version: 1\ntitle: Chart\nview: script\nqueries:\n  - name: rows\n    sql: SELECT 1 AS x\n"
const cliScriptSource = "function draw(ctx) { return '<svg xmlns=\"http://www.w3.org/2000/svg\"/>'; }\n"

func seedScriptArtifact(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "artifacts", "chart")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(cliScriptManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "view.js"), []byte(cliScriptSource), 0o644))
	return dir
}

func viewHashOf(t *testing.T, ws string) string {
	t.Helper()
	out, stderr, err := runVibe("artifact", "view", "status", "chart", "--json", "--workspace", ws)
	require.NoError(t, err, stderr)
	var st map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &st))
	h, _ := st["hash"].(string)
	require.Regexp(t, `^[0-9a-f]{64}$`, h)
	return h
}

func TestArtifactView_StatusAllowDenyRevoke(t *testing.T) {
	ws := newWS(t)
	dir := seedScriptArtifact(t, ws)

	out, stderr, err := runVibe("artifact", "list", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Regexp(t, `chart\s+Chart\s+script`, out)

	out, stderr, err = runVibe("artifact", "view", "status", "chart", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "chart: not decided")
	assert.Contains(t, out, "function draw", "status shows the source that will be allowed")
	hash := viewHashOf(t, ws)

	// Without a terminal, allow needs the hash that was shown.
	_, stderr, err = runVibe("artifact", "view", "allow", "chart", "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error()+stderr, "--hash")
	_, _, err = runVibe("artifact", "view", "allow", "chart", "--hash", "0000", "--yes", "--workspace", ws)
	require.Error(t, err, "a hash that is not the files' hash is refused")

	out, stderr, err = runVibe("artifact", "view", "allow", "chart", "--hash", hash, "--yes", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "chart: allowed")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "view.js"), []byte(cliScriptSource+"// edit\n"), 0o644))
	out, _, err = runVibe("artifact", "view", "status", "chart", "--workspace", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "changed since you allowed it")

	// The hash of the old files no longer allows.
	_, _, err = runVibe("artifact", "view", "allow", "chart", "--hash", hash, "--yes", "--workspace", ws)
	require.Error(t, err)

	out, _, err = runVibe("artifact", "view", "deny", "chart", "--workspace", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "chart: denied")
	out, _, err = runVibe("artifact", "view", "revoke", "chart", "--workspace", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "chart: not decided")

	// Nothing was written in the workspace.
	assert.NoFileExists(t, filepath.Join(ws, "views.json"))
	_ = filepath.WalkDir(ws, func(p string, d os.DirEntry, _ error) error {
		assert.NotEqual(t, "views.json", d.Name(), p)
		return nil
	})
}

func TestArtifactView_StatusOfAPlainArtifactAndAMissingOne(t *testing.T) {
	ws := newWS(t)
	seedArtifacts(t, ws)
	out, _, err := runVibe("artifact", "view", "status", "sales", "--workspace", ws)
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`sales: view kind "a2ui", nothing to allow`), out)
	out, _, err = runVibe("artifact", "view", "status", "nope", "--workspace", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "cannot be used")
	_, _, err = runVibe("artifact", "view", "deny", "sales", "--workspace", ws)
	require.Error(t, err)
}

func TestArtifactView_AllowWithHashNeedsYes(t *testing.T) {
	ws := newWS(t)
	seedScriptArtifact(t, ws)
	_, _, err := runVibe("artifact", "view", "allow", "chart", "--hash", viewHashOf(t, ws), "--workspace", ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--yes")
}

func TestArtifactView_StatusEscapesControlCharacters(t *testing.T) {
	st := workspace.ArtifactViewStatus{Name: "chart", Kind: "script", State: "undecided", Hash: "h",
		Manifest: "title: x\x1b[2J\n", Source: "draw()\u202e // \r\x1b[31mred\u2028evil()\n"}
	var buf bytes.Buffer
	printViewStatus(&buf, st, true)
	out := buf.String()
	assert.NotContains(t, out, "\x1b")
	assert.NotContains(t, out, "\u202e")
	assert.NotContains(t, out, "\r")
	assert.Contains(t, out, "<U+001B>")
	assert.Contains(t, out, "<U+202E>")
	assert.NotContains(t, out, "\u2028")
	assert.Contains(t, out, "<U+2028>")
}

func TestArtifactView_StatusOfAnHTMLPageListsItsFiles(t *testing.T) {
	ws := newWS(t)
	dir := filepath.Join(ws, "artifacts", "pg")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "html"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte("version: 1\ntitle: P\nview: html\nqueries:\n  - name: q\n    sql: SELECT 1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "index.html.tmpl"), []byte(`<p>{{ query "q" }}</p>`), 0o644))

	out, stderr, err := runVibe("artifact", "view", "status", "pg", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "pg: not decided")
	assert.Contains(t, out, "page: static")
	assert.Contains(t, out, "q (on the workspace database)")
	assert.Contains(t, out, "never asks for a password")
	assert.Contains(t, out, "index.html.tmpl (entry,")
	assert.Contains(t, out, `--- html/index.html.tmpl`)
	assert.Contains(t, out, `{{ query "q" }}`, "status shows the text that will be allowed")

	out, stderr, err = runVibe("artifact", "view", "status", "pg", "--json", "--workspace", ws)
	require.NoError(t, err, stderr)
	var st map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &st))
	hash, _ := st["hash"].(string)
	require.Regexp(t, `^[0-9a-f]{64}$`, hash)
	assert.NotContains(t, out, "PageFiles", "the raw bytes are not part of the JSON")

	_, stderr, err = runVibe("artifact", "view", "allow", "pg", "--hash", hash, "--yes", "--workspace", ws)
	require.NoError(t, err, stderr)
	out, _, err = runVibe("artifact", "view", "status", "pg", "--workspace", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "pg: allowed")
}

func TestArtifactView_StatusOfAScriptedPageListsCapabilities(t *testing.T) {
	ws := newWS(t)
	dir := filepath.Join(ws, "artifacts", "app")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "html"), 0o755))
	m := "version: 1\ntitle: A\nview: html\nhtml:\n  capabilities:\n    state: {scope: local}\n    download: {}\n    ask: {}\nqueries:\n  - name: q\n    sql: SELECT 1\n  - name: hidden\n    sql: SELECT 2\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(m), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "index.html.tmpl"), []byte(`<script src="app.js"></script>{{ len (query "q").Rows }}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "app.js"), []byte("1"), 0o644))

	out, stderr, err := runVibe("artifact", "view", "status", "app", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "page: scripted")
	assert.Contains(t, out, "q (on the workspace database, used by its templates)")
	assert.Contains(t, out, "hidden (on the workspace database, script only)", "a script can run every manifest query, so the prompt lists them all")
	assert.Contains(t, out, "capabilities: ask, download, state")
	assert.Contains(t, out, "state: keeps small notes for this page on this computer only")
	assert.Contains(t, out, "download: can offer a file it made")
	assert.Contains(t, out, "ask: This page can send text, which may include query results, to ")
	// Which provider is named depends on the machine's model setup (the default
	// local endpoint counts as set up); the no-provider wording is pinned in
	// studio/page_disclosure_test.go.

	list, _, err := runVibe("artifact", "list", "--workspace", ws)
	require.NoError(t, err)
	assert.Contains(t, list, "html (scripted)")
}
