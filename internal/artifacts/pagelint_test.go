package artifacts

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVetPage_DateLayoutMustBeAName(t *testing.T) {
	for _, ok := range []string{`{{ date (param "top") }}`, `{{ date (param "top") "iso" }}`, `{{ date (param "top") "short" }}`,
		`{{ date (param "top") "long" }}`, `{{ date (param "top") "month" }}`, `{{ "2024-01-05" | date }}`, `{{ date (param "top") $x }}`} {
		_, err := vetIndexW(t, `{{ $x := "long" }}<p>`+ok+`</p>`)
		assert.NoError(t, err, ok)
	}
	for _, bad := range []string{`{{ date (param "top") "2006-01-02" }}`, `{{ date (param "top") "Jan 2" }}`, `{{ date (param "top") "ISO" }}`} {
		_, err := vetIndexW(t, "<p>"+bad+"</p>")
		require.Error(t, err, bad)
		assert.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "not one of iso, short, long, month", bad)
		assert.Contains(t, err.Error(), "html/index.html.tmpl")
	}
	// A piped value reaches date last, so the literal would be taken as the value.
	_, err := vetIndexW(t, `<p>{{ param "top" | date "long" }}</p>`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "value first")
}

func TestVetPage_FieldsOnRowsAreRefused(t *testing.T) {
	bad := map[string]string{
		"range rows":          `{{ range (query "by_region").Rows }}{{ .region }}{{ end }}`,
		"range rows variable": `{{ $r := (query "by_region").Rows }}{{ range $r }}{{ .total }}{{ end }}`,
		"element variable":    `{{ range $row := (query "by_region").Rows }}{{ $row.region }}{{ end }}`,
		"two variables":       `{{ range $i, $row := (query "by_region").Rows }}{{ $row.region }}{{ end }}`,
		"result variable":     `{{ $q := query "by_region" }}{{ range $q.Rows }}{{ .region }}{{ end }}`,
		"index then field":    `{{ $q := query "by_region" }}{{ with index $q.Rows 0 }}{{ .region }}{{ end }}`,
		"nested in if":        `{{ range (query "by_region").Rows }}{{ if .total }}x{{ end }}{{ end }}`,
		"in a call":           `{{ range (query "by_region").Rows }}{{ number .total 0 }}{{ end }}`,
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := vetIndexW(t, "<p>"+body+"</p>")
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
			assert.Contains(t, err.Error(), "array of cells")
			assert.Contains(t, err.Error(), "index . 0")
		})
	}
	good := map[string]string{
		"index":            `{{ range (query "by_region").Rows }}{{ index . 0 }}{{ end }}`,
		"range over cells": `{{ range (query "by_region").Rows }}{{ range . }}{{ . }}{{ end }}{{ end }}`,
		"with a row":       `{{ $q := query "by_region" }}{{ with index $q.Rows 0 }}{{ index . 0 }}{{ end }}`,
		"result fields":    `{{ $q := query "by_region" }}{{ len $q.Rows }}{{ range $q.Columns }}{{ . }}{{ end }}{{ if $q.Truncated }}t{{ end }}`,
		"with result":      `{{ with query "by_region" }}{{ len .Rows }}{{ .Columns }}{{ end }}`,
		"dot is outer":     `{{ range (query "by_region").Rows }}{{ index . 0 }}{{ end }}{{ with $.X }}{{ .y }}{{ end }}`,
		"params":           `{{ range params }}{{ .Name }} {{ .Type }}{{ end }}`,
		"scale band":       `{{ $n := 3 }}{{ range $i, $r := (query "by_region").Rows }}{{ $b := scaleBand $n $i 0 200 }}{{ $b.Start }}{{ end }}`,
		"shadowed":         `{{ $r := (query "by_region").Rows }}{{ $r := param "top" }}{{ $r.x }}`,
	}
	for name, body := range good {
		t.Run("ok "+name, func(t *testing.T) {
			_, err := vetIndexW(t, "<p>"+body+"</p>")
			if name == "dot is outer" || name == "shadowed" {
				// "$.X" and "$r.x" are runtime errors the vet does not claim to know.
				assert.NotContains(t, errText(err), "array of cells")
				return
			}
			assert.NoError(t, err)
		})
	}
}

// vetIndexW vets the good page with a replaced entry the way a write does.
func vetIndexW(t *testing.T, index string) (*PageInfo, error) {
	t.Helper()
	files := goodPageFiles()
	files["index.html.tmpl"] = index
	return VetPageForWrite(pageManifest(t, ""), toFiles(files))
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestVetPage_ResultFieldsAreCaseSensitive(t *testing.T) {
	_, err := vetIndexW(t, `<p>{{ range (query "by_region").rows }}x{{ end }}</p>`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only .Rows, .Columns and .Truncated")
}

func TestVetPage_ReportsEveryFindingTogether(t *testing.T) {
	_, err := vetIndexW(t, `<p>{{ date (param "top") "2006" }}{{ range (query "by_region").Rows }}{{ .a }}{{ end }}</p><script>go()</script>`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalid)
	for _, w := range []string{"not one of iso", "array of cells", "inline <script>"} {
		assert.Contains(t, err.Error(), w)
	}
}

func TestVetPage_InlineScript(t *testing.T) {
	for _, bad := range []string{`<script>alert(1)</script>`, `<SCRIPT type="text/javascript">1</SCRIPT>`, `<script type="module">1</script>`,
		`<svg><script>1</script></svg>`, `{{ if param "top" }}<script>1</script>{{ end }}`} {
		_, err := vetIndexW(t, bad)
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), "inline <script> never runs", bad)
		assert.Contains(t, err.Error(), `<script src="app.js">`, bad)
		assert.Contains(t, err.Error(), "application/json")
	}
	// A script file, a vendored library and a JSON data block are the right shapes.
	files := goodPageFiles()
	files["app.js"] = "1"
	files["index.html.tmpl"] = `<p>x</p><script type="application/json" id="d">{{ json (query "by_region") }}</script><script src="app.js"></script><script src="assay-lib/d3-scale.js"></script>`
	info, err := VetPageForWrite(pageManifest(t, ""), toFiles(files))
	require.NoError(t, err)
	assert.Equal(t, PageScripted, info.Kind)
	for _, typ := range []string{"application/json", "application/ld+json", "text/plain"} {
		_, err := vetIndexW(t, `<script type="`+typ+`">{}</script>`)
		require.NoError(t, err, typ)
	}
}

// The pages the data-artifacts skill teaches must pass the vet: the lint checks
// have to leave valid documented pages alone.
func TestVetPage_TheSkillsExamplesPass(t *testing.T) {
	raw, err := os.ReadFile("../skills/data-artifacts/SKILL.md")
	require.NoError(t, err)
	blocks := map[string][]string{}
	re := regexp.MustCompile("(?s)```(?:html|js|css) ([^\n]+)\n(.*?)```")
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		blocks[m[1]] = append(blocks[m[1]], m[2])
	}
	require.Len(t, blocks["index.html.tmpl"], 2, "the static and the scripted example")
	manifest := `version: 1
title: Doc page
view: html
parameters:
  - {name: from, type: date, default: today-30d}
queries:
  - {name: by_region, sql: "SELECT 'a' AS region, 1 AS revenue WHERE $from IS NOT NULL"}
  - {name: peak, sql: "SELECT 1"}
`
	for i, index := range blocks["index.html.tmpl"] {
		files := []PageFile{{Path: "index.html.tmpl", Data: []byte(index)}, {Path: "assets/style.css", Data: []byte(blocks["assets/style.css"][0])}}
		if js := blocks["app.js"]; i == 1 && len(js) > 0 {
			files = append(files, PageFile{Path: "app.js", Data: []byte(js[0])})
		}
		m := mustDecode(t, manifest)
		_, err := VetPage(m, files)
		if err != nil && strings.Contains(err.Error(), "is not a query the manifest declares") {
			continue // the example names queries this tiny manifest lacks; the lint did not object
		}
		assert.NoError(t, err, "example %d", i)
	}
}

// A page already on disk with these mistakes still loads, lists and verifies
// (VetPage is the load-time vet and is unchanged); only VetPageForWrite refuses it.
func TestStore_StoredPageWithRenderMistakesStillLoads(t *testing.T) {
	{
		root := t.TempDir()
		s := NewFileStore(root)
		ctx := context.Background()
		require.NoError(t, s.Put(ctx, newPageArtifact(t, "old")))
		bad := map[string]string{
			"inline": `<script>alert(1)</script>`,
			"date":   `<p>{{ date (param "top") "2006-01-02" }}</p>`,
			"field":  `<p>{{ range (query "by_region").Rows }}{{ .region }}{{ end }}</p>`,
		}
		for name, index := range bad {
			require.NoError(t, os.WriteFile(pagePath(root, "old", "index.html.tmpl"), []byte(index), 0o644), name)
			got, err := s.Get(ctx, "old")
			require.NoError(t, err, name)
			require.NotNil(t, got.Page, name)
			list, err := s.List(ctx)
			require.NoError(t, err, name)
			require.Len(t, list, 1)
			assert.Empty(t, list[0].Error, name)

			// The store's own write is lenient too (bundle imports and conflict
			// resolution use it); fresh content is held to VetPageForWrite by the
			// agent and by Studio's New.
			got.Files = setFile(got.Files, "index.html.tmpl", index)
			require.NoError(t, s.Put(ctx, got), name)
			_, err = VetPageForWrite(&got.Manifest, got.Files)
			assert.ErrorIs(t, err, ErrInvalid, name)
		}
	}
}
