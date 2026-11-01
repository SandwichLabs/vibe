package artifacts

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pageManifestText is goodManifest as a view: html page; extra goes under html:.
func pageManifestText(extra string) string {
	return strings.Replace(goodManifest, "view: a2ui\n", "view: html\n"+extra, 1)
}

func pageManifest(t *testing.T, extra string) *Manifest {
	t.Helper()
	return mustDecode(t, pageManifestText(extra))
}

const goodIndex = `<!doctype html>
<html><head><meta charset="utf-8"><title>Sales</title>
<link rel="stylesheet" href="assets/style.css">
</head><body>
{{ $rows := query "by_region" }}
<h1>{{ param "top" }} regions</h1>
<table>{{ range $rows.Rows }}<tr><td>{{ index . 0 }}</td><td>{{ number (index . 1) 0 }}</td></tr>{{ end }}</table>
<svg width="200" height="100">{{ range $i, $r := $rows.Rows }}{{ $b := scaleBand (len $rows.Rows) $i 0 200 }}<rect x="{{ $b.Start }}" width="{{ $b.Width }}" height="10"/>{{ end }}</svg>
<a href="{{ link "top" 5 }}">top 5</a> <a href="#end">end</a> <img src="assets/logo.svg">
<p id="end" style="width: {{ param "top" }}px; background: url(assets/bg.png)">{{ template "foot" . }}</p>
</body></html>`

func goodPageFiles() map[string]string {
	return map[string]string{
		"index.html.tmpl":  goodIndex,
		"foot.tmpl":        `{{ define "foot" }}<small>{{ range (query "regions").Rows }}{{ index . 0 }} {{ end }}</small>{{ end }}`,
		"assets/style.css": `body { font-family: sans-serif; background: url(assets/bg.png); }`,
		"assets/logo.svg":  `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`,
		"assets/bg.png":    "\x89PNG\r\n\x1a\n....",
	}
}

func toFiles(m map[string]string) []PageFile {
	var out []PageFile
	for p, s := range m {
		out = append(out, PageFile{Path: p, Data: []byte(s)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// vetWith vets the good page with some files replaced or (empty text and
// remove true) dropped.
func vetWith(t *testing.T, manifestExtra string, edit func(map[string]string)) (*PageInfo, error) {
	t.Helper()
	files := goodPageFiles()
	if edit != nil {
		edit(files)
	}
	return VetPage(pageManifest(t, manifestExtra), toFiles(files))
}

func vetIndex(t *testing.T, index string) (*PageInfo, error) {
	t.Helper()
	return vetWith(t, "", func(f map[string]string) { f["index.html.tmpl"] = index })
}

func TestVetPage_GoodStaticPage(t *testing.T) {
	info, err := vetWith(t, "", nil)
	require.NoError(t, err)
	assert.Equal(t, PageStatic, info.Kind)
	assert.Equal(t, DefaultEntry, info.Entry)
	assert.Equal(t, []string{"by_region", "regions"}, info.Queries, "the queries a page uses, from every template")
	assert.Equal(t, []string{"top"}, info.Params)
	assert.Empty(t, info.ScriptedBy)
}

func TestHTMLSpec_Manifest(t *testing.T) {
	m := pageManifest(t, "")
	require.NotNil(t, m.HTML)
	assert.Equal(t, DefaultEntry, m.HTML.Entry)

	m = pageManifest(t, "html:\n  entry: home.html.tmpl\n  window: {width: 1200, height: 800}\n  capabilities:\n    state: {scope: local}\n    download: {}\n    ask: {}\n")
	assert.Equal(t, "home.html.tmpl", m.HTML.Entry)
	assert.Equal(t, 1200, m.HTML.Window.Width)
	assert.Equal(t, []string{"ask", "download", "state"}, CapabilityNames(m))

	// It survives an encode/decode cycle.
	out, err := Encode(m)
	require.NoError(t, err)
	again, err := Decode(out)
	require.NoError(t, err)
	assert.Equal(t, m.HTML, again.HTML)

	for name, text := range map[string]string{
		"html with a2ui":       strings.Replace(goodManifest, "view: a2ui\n", "view: a2ui\nhtml: {entry: a.tmpl}\n", 1),
		"html with no view":    strings.TrimSuffix(goodManifest, "view: a2ui\n") + "html: {}\n",
		"unknown capability":   pageManifestText("html:\n  capabilities:\n    network: {}\n"),
		"unknown html key":     pageManifestText("html:\n  csp: none\n"),
		"state scope shared":   pageManifestText("html:\n  capabilities:\n    state: {scope: shared}\n"),
		"download with config": pageManifestText("html:\n  capabilities:\n    download: {scope: local}\n"),
		"entry in a folder":    pageManifestText("html:\n  entry: a/b.tmpl\n"),
		"entry not a template": pageManifestText("html:\n  entry: index.html\n"),
		"entry dotfile":        pageManifestText("html:\n  entry: .index.tmpl\n"),
		"window too small":     pageManifestText("html:\n  window: {width: 10, height: 800}\n"),
		"window too big":       pageManifestText("html:\n  window: {width: 800, height: 99999}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(text))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestVetPage_CapabilitiesAndWindowNeedScript(t *testing.T) {
	caps := "html:\n  capabilities:\n    state: {}\n"
	_, err := vetWith(t, caps, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capabilities need script")
	_, err = vetWith(t, "html:\n  window: {width: 800, height: 600}\n", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "window needs script")

	info, err := vetWith(t, caps, func(f map[string]string) { f["assets/app.js"] = "draw()" })
	require.NoError(t, err)
	assert.Equal(t, PageScripted, info.Kind)
	assert.Equal(t, []string{"state"}, info.Capabilities)
}

func TestVetPage_DerivedKind(t *testing.T) {
	for name, tc := range map[string]struct {
		index    string
		extra    map[string]string
		scripted bool
		refused  bool
	}{
		"plain":                      {index: `<p>hi</p>`},
		"script element":             {index: `<script>draw()</script>`, scripted: true},
		"script upper case":          {index: `<SCRIPT>draw()</SCRIPT>`, scripted: true},
		"script with src":            {index: `<script src="assets/app.js"></script>`, extra: map[string]string{"assets/app.js": "1"}, scripted: true},
		"onclick":                    {index: `<button onclick="go()">x</button>`, scripted: true},
		"onerror on img":             {index: `<img src="assets/logo.svg" onerror="x()">`, scripted: true},
		"any on-attribute":           {index: `<p onmouseover="x()">x</p>`, scripted: true},
		"js file only":               {index: `<p>hi</p>`, extra: map[string]string{"assets/x.js": "1"}, scripted: true},
		"mjs file only":              {index: `<p>hi</p>`, extra: map[string]string{"assets/x.mjs": "1"}, scripted: true},
		"script in a define":         {index: `{{ define "x" }}<script>1</script>{{ end }}{{ template "x" }}`, scripted: true},
		"script in a partial":        {index: `<p>{{ template "foot.tmpl" }}</p>`, extra: map[string]string{"foot.tmpl": `<script>1</script>`}, scripted: true},
		"script in one branch":       {index: `{{ if param "live" }}<b>x</b>{{ else }}<script>1</script>{{ end }}`, scripted: true},
		"svg script":                 {index: `<svg><script>1</script></svg>`, scripted: true},
		"script hiding in svg style": {index: `<svg><style><script>alert(1)</script></style></svg>`, scripted: true},
		"script hiding in svg title": {index: `<svg><title><script>alert(1)</script></title></svg>`, scripted: true},
		"script as text in a title":  {index: `<title>&lt;script&gt;</title>`},
		"script in a comment":        {index: `<!-- <script>1</script> --><p>x</p>`, refused: true},
		"script split by a partial":  {index: `<scr{{ template "foot.tmpl" }}>1</script>`, extra: map[string]string{"foot.tmpl": `ipt`}, scripted: true},
		"tag name by an action":      {index: `<{{ param "top" }} class=a>`, refused: true},
		"script tag then an action":  {index: `<script{{ template "foot.tmpl" }}>1</script>`, extra: map[string]string{"foot.tmpl": ` src=x`}, refused: true},
		"attribute name by action":   {index: `<p {{ param "top" }}="1">x</p>`, refused: true},
		"conditional attribute":      {index: `<p {{ if param "live" }}class="a"{{ end }}>x</p>`, refused: true},
		"comment hides a split tag":  {index: `<scr<!-- x -->ipt>1</script>`, refused: true},
		"ends inside a tag":          {index: `<p>x</p><scr`, refused: true},
		"ends inside an attribute":   {index: `<a href="assets/x`, refused: true},
		"unterminated comment":       {index: `<p>x</p><!-- <script>`, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			info, err := vetWith(t, "", func(f map[string]string) {
				f["index.html.tmpl"] = tc.index
				for p, s := range tc.extra {
					f[p] = s
				}
			})
			if tc.refused {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalid)
				return
			}
			require.NoError(t, err)
			if tc.scripted {
				assert.Equal(t, PageScripted, info.Kind)
				assert.NotEmpty(t, info.ScriptedBy)
			} else {
				assert.Equal(t, PageStatic, info.Kind)
			}
		})
	}
}

func TestVetPage_FunctionNamesAreLiteralsTheManifestDeclares(t *testing.T) {
	good := []string{
		`{{ (query "regions").Rows }}`,
		`{{ param "top" }}`,
		`{{ link "top" 3 "note" "x" }}`,
		`{{ link }}`,
		`{{ params }}`,
	}
	for _, g := range good {
		_, err := vetIndex(t, "<p>"+g+"</p>")
		assert.NoError(t, err, g)
	}
	bad := map[string]string{
		"computed name":          `{{ query .Name }}`,
		"printf name":            `{{ query (printf "by_%s" "region") }}`,
		"piped name":             `{{ "regions" | query }}`,
		"variable name":          `{{ $n := "regions" }}{{ query $n }}`,
		"no argument":            `{{ query }}`,
		"two arguments":          `{{ query "regions" "x" }}`,
		"unknown query":          `{{ query "nope" }}`,
		"unknown parameter":      `{{ param "nope" }}`,
		"computed param":         `{{ param .X }}`,
		"link computed name":     `{{ link .Name 1 }}`,
		"link unknown name":      `{{ link "nope" 1 }}`,
		"link second name":       `{{ link "top" 1 "nope" 2 }}`,
		"link piped value":       `{{ 5 | link "top" }}`,
		"link odd pairs":         `{{ link "top" }}`,
		"query as a value":       `{{ printf "%v" query }}`,
		"call builtin":           `{{ call .F }}`,
		"unknown function":       `{{ safeHTML "x" }}`,
		"query in nested parens": `{{ index (query .X).Rows 0 }}`,
		"query in if":            `{{ if query .X }}x{{ end }}`,
		"query in range":         `{{ range (query .X).Rows }}x{{ end }}`,
		"query in with":          `{{ with query .X }}x{{ end }}`,
		"query in template arg":  `{{ template "foot.tmpl" (query .X) }}`,
	}
	for name, action := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := vetWith(t, "", func(f map[string]string) {
				f["index.html.tmpl"] = "<p>" + action + "</p>"
				f["foot.tmpl"] = ""
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestVetPage_TemplateNamesAndTheEscaper(t *testing.T) {
	for name, tc := range map[string]struct {
		index string
		extra map[string]string
	}{
		"path escape":          {index: `{{ template "../x.tmpl" }}`},
		"absolute path":        {index: `{{ template "/etc/passwd" }}`},
		"undefined template":   {index: `{{ template "missing" }}`},
		"nested path":          {index: `{{ template "assets/x.tmpl" }}`},
		"duplicate define":     {index: `{{ define "foot" }}a{{ end }}{{ template "foot" }}`, extra: map[string]string{"foot.tmpl": `{{ define "foot" }}b{{ end }}`}},
		"define a file name":   {index: `{{ define "foot.tmpl" }}a{{ end }}`, extra: map[string]string{"foot.tmpl": `x`}},
		"parse error":          {index: `{{ if }}`},
		"unclosed action":      {index: `{{ param "top" `},
		"branch ends in a tag": {index: `{{ if param "live" }}<a href="{{ else }}x{{ end }}`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := vetWith(t, "", func(f map[string]string) {
				f["index.html.tmpl"] = tc.index
				for p, s := range tc.extra {
					f[p] = s
				}
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
	// A define and a call of it across partials is fine.
	_, err := vetIndex(t, `{{ template "foot" }}`)
	assert.NoError(t, err)
}

func TestVetPage_URLAttributeAllowList(t *testing.T) {
	ok := []string{
		`<a href="{{ link "top" 3 }}">x</a>`,
		`<a href="#top">x</a>`,
		`<a href="assets/logo.svg">x</a>`,
		`<img src="assets/logo.svg" srcset="assets/logo.svg 1x, assets/bg.png 2x">`,
		`<link rel="stylesheet" href="assets/style.css">`,
		`<link rel="Stylesheet" href="assets/style.css" media="print">`,
		`<svg><use href="#a"/></svg>`,
		`<form action="{{ link }}"><input name=a></form>`,
		`<p style="background:url('assets/bg.png')">x</p>`,
		`<style>p { background: url(assets/bg.png) } /* url(http://x) */</style>`,
		`<style>p { width: {{ param "top" }}px }</style>`,
		`<meta charset="utf-8"><meta name="viewport" content="width=device-width">`,
		`<script src="assay-lib/d3-scale.js"></script>`,
		`<script src="assay-lib/vega-lite.js"></script>`,
	}
	for _, h := range ok {
		_, err := vetIndex(t, h)
		assert.NoError(t, err, h)
	}
	bad := []string{
		`<a href="https://example.com">x</a>`,
		`<a href="//example.com/x">x</a>`,
		`<a href="/index.html">x</a>`,
		`<a href="/">x</a>`,
		`<a href="javascript:alert(1)">x</a>`,
		`<a href="java&#9;script:alert(1)">x</a>`,
		`<a href="data:text/html,x">x</a>`,
		`<a href="mailto:a@b.c">x</a>`,
		`<a href="assets/../index.html">x</a>`,
		`<a href="assets/x?y=1">x</a>`,
		`<a href=" assets/logo.svg">x</a>`,
		`<a href="logo.svg">x</a>`,
		`<a href="">x</a>`,
		`<a href>x</a>`,
		`<a href="{{ param "top" }}">x</a>`,
		`<a href="assets/{{ param "top" }}.svg">x</a>`,
		`<a href="{{ link "top" 3 }}x">x</a>`,
		`<a href="{{ if param "live" }}assets/a.svg{{ else }}https://x.test{{ end }}">x</a>`,
		`<a href="{{ template "foot" }}">x</a>`,
		`<a href="{{ (link "top" 3) }}">x</a>`,
		`<a href="{{ link "top" 3 | printf "%s" }}">x</a>`,
		`<a href="{{ $u := link "top" 3 }}">x</a>`,
		`<img src="http://x.test/a.png">`,
		`<img src="assets/a.png" srcset="https://x.test/a.png 2x">`,
		`<img srcset="assets/a.png 1x, {{ param "top" }} 2x">`,
		`<video poster="https://x.test/p.png"></video>`,
		`<form action="https://x.test/"></form>`,
		`<button formaction="https://x.test/">x</button>`,
		`<svg><image xlink:href="https://x.test/a.png"/></svg>`,
		`<svg><a href="https://x.test/">x</a></svg>`,
		`<a href="assets/a.svg" target="_blank">x</a>`,
		`<a href="assets/a.svg" ping="https://x.test/">x</a>`,
		`<a href="assets/a.svg" download>x</a>`,
		`<a href="assets/a.svg" TARGET=_blank>x</a>`,
		`<script src="assay-lib/x.css"></script>`,
		`<script src="assay-lib/../x.js"></script>`,
		`<script src="assay-lib/a/b.js"></script>`,
		`<script src="/assay-lib/d3.js"></script>`,
		`<script src="assay-lib/d3.js?x=1"></script>`,
		`<script src="assay-lib/{{ param "top" }}.js"></script>`,
		`<img src="assay-lib/d3.js">`,
		`<link rel="stylesheet" href="assay-lib/d3.js">`,
		`<link rel="preconnect" href="https://x.test">`,
		`<link rel="dns-prefetch" href="//x.test">`,
		`<link rel="stylesheet" href="https://x.test/a.css">`,
		`<link rel="stylesheet" href="assets/a.css" crossorigin>`,
		`<link rel="stylesheet" href="{{ param "top" }}">`,
		`<link rel="icon" href="assets/a.png">`,
		`<link href="assets/style.css">`,
		`<link rel="stylesheet">`,
		`<meta http-equiv="refresh" content="0;url=https://x.test">`,
		`<base href="https://x.test/">`,
		`<svg><a id="a"><set attributeName="href" to="https://x.test/"/></a></svg>`,
		`<svg><a><animate attributeName="href" values="https://x.test/"/></a></svg>`,
		`<iframe src="assets/a.svg"></iframe>`,
		`<object data="assets/a.svg"></object>`,
		`<embed src="assets/a.svg">`,
		`<iframe srcdoc="<p>x</p>"></iframe>`,
		`<p style="background:url(https://x.test/a.png)">x</p>`,
		`<p style="background:url(//x.test/a.png)">x</p>`,
		`<p style="background:url({{ param "top" }})">x</p>`,
		`<p style="background:u\72l(https://x.test/a.png)">x</p>`,
		`<p style="background:image-set('a.png' 1x)">x</p>`,
		`<p style="background:url(data:image/png;base64,AAAA)">x</p>`,
		`<style>@import url(assets/a.css);</style>`,
		`<style>p { background: url(https://x.test/a.png) }</style>`,
		`<style>p { background: url( 'https://x.test/a.png' ) }</style>`,
		`<p style="background:url(assets/a.png">x</p>`,
		`<{{ "a" }}>x`,
	}
	for _, h := range bad {
		_, err := vetWith(t, "", func(f map[string]string) { f["index.html.tmpl"] = "<body>" + h + "</body>" })
		if assert.Error(t, err, h) {
			assert.ErrorIs(t, err, ErrInvalid, h)
		}
	}
}

func TestVetPage_ScriptSrcRules(t *testing.T) {
	with := func(src string) error {
		_, err := vetWith(t, "", func(f map[string]string) {
			f["index.html.tmpl"] = `<script src="` + src + `"></script>`
			f["app.js"] = "1"
			f["assets/app.js"] = "1"
		})
		return err
	}
	assert.NoError(t, with("assets/app.js"))
	assert.NoError(t, with("app.js"), "a top-level script of the file set")
	assert.Error(t, with("missing.js"))
	assert.Error(t, with("https://cdn.test/d3.js"))
	assert.Error(t, with("//cdn.test/d3.js"))
	assert.Error(t, with("{{ param \"top\" }}"))
}

func TestVetPage_FileSetRules(t *testing.T) {
	bigText := strings.Repeat("a", MaxPageFileBytes+1)
	many := func(f map[string]string) {
		for i := 0; i < MaxPageFiles; i++ {
			f[fmt.Sprintf("assets/f%d.json", i)] = "{}"
		}
	}
	for name, edit := range map[string]func(map[string]string){
		"no entry":              func(f map[string]string) { delete(f, "index.html.tmpl") },
		"dotfile":               func(f map[string]string) { f[".env"] = "x" },
		"dotfile in assets":     func(f map[string]string) { f["assets/.hidden.json"] = "{}" },
		"unknown extension":     func(f map[string]string) { f["assets/a.html"] = "x" },
		"no extension":          func(f map[string]string) { f["README"] = "x" },
		"upper-case extension":  func(f map[string]string) { f["assets/a.JS"] = "x" },
		"gif":                   func(f map[string]string) { f["assets/a.gif"] = "GIF89a" },
		"wasm":                  func(f map[string]string) { f["assets/a.wasm"] = "\x00asm" },
		"template in assets":    func(f map[string]string) { f["assets/p.tmpl"] = "x" },
		"folder outside assets": func(f map[string]string) { f["js/app.js"] = "1" },
		"too deep":              func(f map[string]string) { f["assets/a/b/c.json"] = "{}" },
		"reserved _assay":       func(f map[string]string) { f["_assay/theme.css"] = "x" },
		"reserved assay-lib":    func(f map[string]string) { f["assay-lib/d3.js"] = "x" },
		"reserved file name":    func(f map[string]string) { f["assay-lib"] = "x" },
		"dot dot":               func(f map[string]string) { f["assets/../x.json"] = "{}" },
		"absolute":              func(f map[string]string) { f["/x.json"] = "{}" },
		"backslash":             func(f map[string]string) { f["assets\\x.json"] = "{}" },
		"leading dash":          func(f map[string]string) { f["-x.json"] = "{}" },
		"case-folded duplicate": func(f map[string]string) { f["assets/Style.css"] = "p{}"; f["assets/style.CSS"] = "p{}" },
		"case duplicate":        func(f map[string]string) { f["assets/LOGO.svg"] = f["assets/logo.svg"] },
		"too many files":        many,
		"file over a MiB":       func(f map[string]string) { f["assets/big.json"] = `"` + bigText + `"` },
		"template over 256 KiB": func(f map[string]string) { f["big.tmpl"] = strings.Repeat("a", MaxTemplateBytes+1) },
		"total over 5 MiB": func(f map[string]string) {
			for i := 0; i < 6; i++ {
				f[fmt.Sprintf("assets/b%d.json", i)] = `"` + strings.Repeat("a", MaxPageFileBytes-10) + `"`
			}
		},
		"png that is html":       func(f map[string]string) { f["assets/a.png"] = "<html><script>1</script>" },
		"jpg that is html":       func(f map[string]string) { f["assets/a.jpg"] = "<html>" },
		"webp that is html":      func(f map[string]string) { f["assets/a.webp"] = "<html>" },
		"woff2 that is html":     func(f map[string]string) { f["assets/a.woff2"] = "<html>" },
		"bad json":               func(f map[string]string) { f["assets/a.json"] = "{" },
		"two json values":        func(f map[string]string) { f["assets/a.json"] = "{}{}" },
		"template with NUL":      func(f map[string]string) { f["index.html.tmpl"] = "<p>x\x00</p>" },
		"template with U+E000":   func(f map[string]string) { f["index.html.tmpl"] = "<p>x</p>" },
		"template with bidi":     func(f map[string]string) { f["index.html.tmpl"] = "<p>x‮</p>" },
		"script with zero width": func(f map[string]string) { f["app.js"] = "a​b" },
		"not utf-8":              func(f map[string]string) { f["app.js"] = "a\xff" },
		"svg with script": func(f map[string]string) {
			f["assets/logo.svg"] = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
		},
		"svg with prefixed script": func(f map[string]string) {
			f["assets/logo.svg"] = `<svg xmlns:s="http://www.w3.org/2000/svg"><s:script>alert(1)</s:script></svg>`
		},
		"svg with onload": func(f map[string]string) { f["assets/logo.svg"] = `<svg onload="alert(1)"></svg>` },
		"svg with javascript url": func(f map[string]string) {
			f["assets/logo.svg"] = `<svg><a href="jav&#x61;script:alert(1)"><text>x</text></a></svg>`
		},
		"svg with foreignObject": func(f map[string]string) {
			f["assets/logo.svg"] = `<svg><foreignObject><iframe src="x"></iframe></foreignObject></svg>`
		},
		"svg with doctype": func(f map[string]string) { f["assets/logo.svg"] = `<!DOCTYPE svg [<!ENTITY x "y">]><svg/>` },
		"css import":       func(f map[string]string) { f["assets/style.css"] = `@import "x.css";` },
		"css external url": func(f map[string]string) { f["assets/style.css"] = `p{background:url(http://x.test/a.png)}` },
		"css escape":       func(f map[string]string) { f["assets/style.css"] = `p{background:\75rl(http://x.test/a.png)}` },
	} {
		t.Run(name, func(t *testing.T) {
			files := goodPageFiles()
			edit(files)
			_, err := VetPage(pageManifest(t, ""), toFiles(files))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
	// The limits themselves are allowed.
	files := goodPageFiles()
	files["assets/ok.json"] = `"` + strings.Repeat("a", MaxPageFileBytes-2) + `"`
	files["assets/d/e.json"] = "{}"
	_, err := VetPage(pageManifest(t, ""), toFiles(files))
	assert.NoError(t, err)
}

func TestVetPage_NoRepeatedPath(t *testing.T) {
	files := toFiles(goodPageFiles())
	files = append(files, files[0])
	_, err := VetPage(pageManifest(t, ""), files)
	require.Error(t, err)
}

func TestVetPage_NotAnHTMLManifest(t *testing.T) {
	_, err := VetPage(mustDecode(t, goodManifest), toFiles(goodPageFiles()))
	require.ErrorIs(t, err, ErrInvalid)
}

func TestPageHash_ChangesWithEveryPartOfTheFileSet(t *testing.T) {
	m := []byte("version: 1\n")
	info := &PageInfo{Kind: PageStatic, Entry: "index.html.tmpl"}
	base := []PageFile{{"index.html.tmpl", []byte("<p>a</p>")}, {"assets/s.css", []byte("p{}")}}
	h := func(mm []byte, files []PageFile, i *PageInfo) string {
		t.Helper()
		out, err := PageHash(mm, files, i)
		require.NoError(t, err)
		return out
	}
	ref := h(m, base, info)
	assert.Equal(t, ref, h(m, []PageFile{base[1], base[0]}, info), "order does not matter")
	assert.NotEqual(t, ref, h([]byte("version: 2\n"), base, info), "the manifest is entry 0")
	assert.NotEqual(t, ref, h(m, base[:1], info), "a removed file")
	assert.NotEqual(t, ref, h(m, append(append([]PageFile(nil), base...), PageFile{"assets/x.json", []byte("{}")}), info), "an added file")
	assert.NotEqual(t, ref, h(m, []PageFile{base[0], {"assets/t.css", base[1].Data}}, info), "a renamed file")
	assert.NotEqual(t, ref, h(m, []PageFile{{"index.html.tmpl", []byte("<p>b</p>")}, base[1]}, info), "an edited file")
	assert.NotEqual(t, ref, h(m, base, &PageInfo{Kind: PageScripted, Entry: "index.html.tmpl"}), "the derived kind")
	assert.NotEqual(t, ref, h(m, base, &PageInfo{Kind: PageStatic, Entry: "index.html.tmpl", Capabilities: []string{"ask"}}), "the capabilities")
	assert.NotEqual(t, ref, h(m, base, &PageInfo{Kind: PageStatic, Entry: "assets/s.css"}), "the role of a file (which one is the entry)")
	assert.Equal(t, h(m, base, &PageInfo{Kind: PageStatic, Entry: "index.html.tmpl", Capabilities: []string{"b", "a"}}),
		h(m, base, &PageInfo{Kind: PageStatic, Entry: "index.html.tmpl", Capabilities: []string{"a", "b"}}), "capabilities are sorted")

	// A byte moved across a file boundary.
	a := h(m, []PageFile{{"a.tmpl", []byte("xy")}, {"b.tmpl", []byte("z")}}, info)
	b := h(m, []PageFile{{"a.tmpl", []byte("x")}, {"b.tmpl", []byte("yz")}}, info)
	assert.NotEqual(t, a, b)
	// A byte moved from the manifest into a file.
	c := h([]byte("m<script>"), []PageFile{{"a.tmpl", []byte("x")}}, info)
	d := h([]byte("m"), []PageFile{{"a.tmpl", []byte("<script>x")}}, info)
	assert.NotEqual(t, c, d)
	// A byte moved between a path and its content.
	assert.NotEqual(t, h(m, []PageFile{{"ab.json", []byte("c")}}, info), h(m, []PageFile{{"a.json", []byte("bc")}}, info))

	_, err := PageHash(m, []PageFile{{"a", nil}, {"a", nil}}, info)
	assert.ErrorIs(t, err, ErrInvalid, "two files with one path")
	assert.NotEqual(t, ref, mustHash(t, m, []ViewPart{{Path: "index.html.tmpl", Data: []byte("<p>a</p>")}}), "ViewHash is a different encoding")
}

// TestViewHash_Golden pins ViewHash: the hashes of view: script artifacts
// people already allowed must never change (they reset once, with the domain tag, in the Vibe to Assay rename).
func TestViewHash_Golden(t *testing.T) {
	got := mustHash(t, []byte("version: 1\n"), []ViewPart{{Path: ScriptFile, Data: []byte("draw()")}})
	assert.Equal(t, "3e6d35498da5f7e6870df04f2ad2d2354e18ac8f388aa786f965833351bbb92f", got)
}

// mustNotBeStatic runs an index (and partials) through the vet: it must be
// refused or classified scripted, never accepted as a static page.
func mustNotBeStatic(t *testing.T, index string, extra map[string]string) {
	t.Helper()
	info, err := vetWith(t, "", func(f map[string]string) {
		f["index.html.tmpl"] = index
		for p, s := range extra {
			f[p] = s
		}
	})
	if err == nil {
		assert.Equal(t, PageScripted, info.Kind, "accepted as a static page:\n%s", index)
	}
}

const evil = `<link rel=preconnect href=https://evil.example>`

// The Opus review's repros (P2 review): every one was accepted as static.
func TestVetPage_ReviewRepros(t *testing.T) {
	for name, tc := range map[string]struct {
		index string
		extra map[string]string
	}{
		"noscript preconnect":  {index: `<noscript>` + evil + `</noscript>`},
		"noscript script":      {index: `<noscript><script>alert(1)</script></noscript>`},
		"noscript target":      {index: `<noscript><a href="https://evil.example" target=_blank>x</a></noscript>`},
		"xmp closed by caller": {index: `<xmp>{{ template "c.tmpl" }}` + evil, extra: map[string]string{"c.tmpl": `</xmp>`}},
		"xmp branches":         {index: `{{ if param "live" }}<xmp>{{ end }}` + evil + `{{ if param "live" }}</xmp>{{ end }}`},
		"noembed":              {index: `<noembed>{{ template "c.tmpl" }}<script>alert(1)</script>`, extra: map[string]string{"c.tmpl": `</noembed>`}},
		"noframes":             {index: `<noframes>{{ template "c.tmpl" }}<img src=x onerror=alert(1)>`, extra: map[string]string{"c.tmpl": `</noframes>`}},
		"style caller": {index: `<style>{{ template "c.tmpl" }}`, extra: map[string]string{
			"c.tmpl": `<a title="</style><img src=x onerror=alert(1)>` + evil + `">x</a>`}},
		"textarea caller": {index: `<textarea>{{ template "c.tmpl" }}`, extra: map[string]string{
			"c.tmpl": `<a title="</textarea><img src=x onerror=alert(1)>">x</a>`}},
		"title caller define":      {index: `<title>{{ template "c" }}{{ define "c" }}<a title="</title><img src=x onerror=alert(1)>">x</a>{{ end }}`},
		"svg define style":         {index: `<svg>{{ template "s" }}</svg>{{ define "s" }}<style><p>` + evil + `</style>{{ end }}`},
		"svg opened by a partial":  {index: `{{ template "o.tmpl" }}<style><img src=x onerror=alert(1)></style>`, extra: map[string]string{"o.tmpl": `<svg>`}},
		"math opened by a partial": {index: `{{ template "o.tmpl" }}<style><p><a href="https://evil.example">x</a></style>`, extra: map[string]string{"o.tmpl": `<math>`}},
		"css never checked":        {index: `<style>p{background:url(https://evil.example/x)}{{ template "c.tmpl" }}`, extra: map[string]string{"c.tmpl": `</style>`}},
		"css never closed":         {index: `<style>p{background:url(https://evil.example/x)}`},
		"svg style entities":       {index: `<svg><style>rect{fill:url&#40;https://evil.example/x&#41;}</style></svg>`},
		"svg style import":         {index: `<svg><style>@&#105;mport "https://evil.example/x.css";</style></svg>`},
		"svg fill url":             {index: `<svg><rect fill="url(https://evil.example/x.svg#a)"/></svg>`},
		"svg filter url":           {index: `<svg><rect filter="url(https://evil.example/x.svg#a)"/></svg>`},
		"svg cursor url":           {index: `<svg><rect cursor="url(https://evil.example/c.png), auto"/></svg>`},
		"svg mask url":             {index: `<svg><rect mask="URL( 'https://evil.example/m.svg#a' )"/></svg>`},
		"svg fill from data":       {index: `<svg><rect fill="{{ param "note" }}"/></svg>`},
		"svg stroke from data":     {index: `<svg><rect stroke="{{ param "note" }}"/></svg>`},
		"comment elision":          {index: `<<!-- -->script>alert(1)<<!-- -->/script>`},
		"comment bang close":       {index: `<!-- --!>` + evil + ` -->`},
		"empty comment":            {index: `<!-->` + evil + `-->`},
		"cdata":                    {index: `<![CDATA[ <script>alert(1)</script> ]]>`},
		"processing instruction":   {index: `<?x <script>alert(1)</script> ?>`},
		"bogus end tag":            {index: `</ <script>alert(1)</script>>`},
		"chained call":             {index: `{{ call.X }}`},
		"chained query":            {index: `{{ query.Rows }}`},
		"lowsrc":                   {index: `<img src="assets/bg.png" lowsrc="https://evil.example/x">`},
		"dynsrc":                   {index: `<img src="assets/bg.png" dynsrc="https://evil.example/x">`},
		"attributionsrc":           {index: `<a href="#x" attributionsrc="https://evil.example/x">x</a>`},
		"recursion":                {index: `{{ define "a" }}{{ template "b" }}{{ end }}{{ define "b" }}{{ template "a" }}{{ end }}{{ template "a" }}`},
		"self recursion":           {index: `{{ define "a" }}x{{ template "a" }}{{ end }}{{ template "a" }}`},
	} {
		t.Run(name, func(t *testing.T) { mustNotBeStatic(t, tc.index, tc.extra) })
	}
}

func TestVetPage_ReviewRepros_ExpectedRefusals(t *testing.T) {
	for name, index := range map[string]string{
		"noscript":     `<noscript><p>x</p></noscript>`,
		"xmp":          `<xmp>x</xmp>`,
		"comment":      `<!-- hi --><p>x</p>`,
		"chained call": `{{ call.X }}`,
		"recursion":    `{{ define "a" }}{{ template "a" }}{{ end }}{{ template "a" }}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := vetIndex(t, index)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

// What a caller's context does to a callee: the vet reads the callee where it
// is called, so a partial that closes the caller's <style> is seen closing it.
func TestVetPage_CalleesAreReadInTheirCallersContext(t *testing.T) {
	// A partial that opens a <script> element makes the page scripted, and the
	// file that did it is named.
	info, err := vetWith(t, "", func(f map[string]string) {
		f["index.html.tmpl"] = `<p>{{ template "c.tmpl" }}</p>`
		f["c.tmpl"] = `<script>1</script>`
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"c.tmpl"}, info.ScriptedBy)

	// An honest svg with an honest style in a partial is fine.
	_, err = vetWith(t, "", func(f map[string]string) {
		f["index.html.tmpl"] = `<svg>{{ template "s.tmpl" }}</svg>`
		f["s.tmpl"] = `<style>rect{fill:red}</style><rect width="1" height="1"/>`
	})
	assert.NoError(t, err)

	// A partial no one calls is still vetted.
	_, err = vetWith(t, "", func(f map[string]string) { f["orphan.tmpl"] = `<a href="https://evil.example">x</a>` })
	assert.Error(t, err)
}

// The vet never runs a page: a loop the escaper would never finish, or a chain
// of calls that doubles at each level, is bounded.
func TestVetPage_IsBounded(t *testing.T) {
	var fan strings.Builder
	fan.WriteString(`{{define "a0"}}{{end}}`)
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&fan, `{{define "a%d"}}{{template "a%d"}}{{template "a%d"}}{{end}}`, i, i-1, i-1)
	}
	fan.WriteString(`{{template "a30"}}`)
	for name, index := range map[string]string{
		"range over a huge number": `{{ range 3000000000 }}{{ end }}`,
		"nested ranges":            `{{ range 100000 }}{{ range 100000 }}{{ end }}{{ end }}`,
		"doubling calls":           fan.String(),
	} {
		t.Run(name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = vetIndex(t, index)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("VetPage is still running after 5 seconds")
			}
		})
	}
	// The bound is a refusal, with a message.
	_, err := vetIndex(t, fan.String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call each other")
}

func TestVetPage_SVGAssets(t *testing.T) {
	for name, svg := range map[string]string{
		"xml-stylesheet":   `<?xml version="1.0"?><?xml-stylesheet href="https://evil.example/x.xsl" type="text/xsl"?><svg/>`,
		"external href":    `<svg><image href="https://evil.example/x.png"/></svg>`,
		"external xlink":   `<svg><use xlink:href="https://evil.example/x.svg#a"/></svg>`,
		"external fill":    `<svg><rect fill="url(https://evil.example/x.svg#a)"/></svg>`,
		"style import":     `<svg><style>@import "https://evil.example/x.css";</style></svg>`,
		"style entity url": `<svg><style>rect{fill:url&#40;https://evil.example/x&#41;}</style></svg>`,
		"style url":        `<svg><style>rect{fill:url(https://evil.example/x)}</style></svg>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := vetWith(t, "", func(f map[string]string) { f["assets/logo.svg"] = svg })
			require.Error(t, err)
		})
	}
	// An honest svg stays fine: the xml declaration, a local link, a gradient.
	_, err := vetWith(t, "", func(f map[string]string) {
		f["assets/logo.svg"] = `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="g"/></defs><rect fill="url(#g)"/><use href="#g"/><style>rect{stroke:red}</style></svg>`
	})
	assert.NoError(t, err)
}

func TestVetPage_OnlyTemplatesAndScriptsAtTheTop(t *testing.T) {
	for _, p := range []string{"style.css", "logo.svg", "a.png", "a.jpg", "a.webp", "f.woff2", "d.json"} {
		_, err := vetWith(t, "", func(f map[string]string) {
			f[p] = map[string]string{"style.css": "p{}", "logo.svg": "<svg/>", "a.png": "\x89PNG\r\n\x1a\n", "a.jpg": "\xff\xd8\xff", "a.webp": "RIFF....WEBP", "f.woff2": "wOF2", "d.json": "{}"}[p]
		})
		require.Error(t, err, p)
		assert.Contains(t, err.Error(), "assets/", p)
	}
	_, err := vetWith(t, "", func(f map[string]string) { f["app.js"] = "1"; f["m.mjs"] = "1" })
	assert.NoError(t, err)
}

// A range body runs again, so what it leaves open is in force at the start of
// the next pass; a branch may be taken in either order.
func TestVetPage_RangeAndOrderDependentForeignContent(t *testing.T) {
	rng := func(body string) string { return `{{ range (query "regions").Rows }}` + body + `{{ end }}` }
	for name, tc := range map[string]struct {
		index string
		extra map[string]string
	}{
		"svg after style in a range":  {index: rng(`<style><img src=x onerror=alert(1)></style><svg>`)},
		"math after style in a range": {index: rng(`<style><img src=x onerror=alert(1)></style><math>`)},
		"link in style, then svg":     {index: rng(`<style><p>` + evil + `</style><svg>`)},
		"two partials in a range": {
			index: rng(`{{ template "s.tmpl" }}{{ template "o.tmpl" }}`),
			extra: map[string]string{"s.tmpl": `<style><img src=x onerror=alert(1)></style>`, "o.tmpl": `<svg>`},
		},
		"svg later in the page": {index: `<style><img src=x onerror=alert(1)></style><p>x</p><svg></svg>`},
	} {
		t.Run(name, func(t *testing.T) { mustNotBeStatic(t, tc.index, tc.extra) })
	}
	// An honest chart in a range stays fine.
	_, err := vetIndex(t, `<svg>{{ range (query "regions").Rows }}<rect width="1" height="1"/>{{ end }}</svg>`)
	assert.NoError(t, err)
}

// One budget covers the whole page: many templates that each call one big
// define must not each pay for the expansion.
func TestVetPage_ExpansionBudgetIsPerPage(t *testing.T) {
	big := strings.Repeat("<p>x</p>", 28000) // about 224 KB, under the template cap
	files := map[string]string{
		"index.html.tmpl": `<p>x</p>`,
		"big.tmpl":        `{{ define "big" }}` + big + `{{ end }}`,
	}
	for i := 0; i < 150; i++ {
		files[fmt.Sprintf("o%d.tmpl", i)] = `{{ template "big" }}{{ template "big" }}{{ template "big" }}{{ template "big" }}`
	}
	done := make(chan error, 1)
	go func() {
		_, err := VetPage(pageManifest(t, ""), toFiles(files))
		done <- err
	}()
	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expand to more than")
	case <-time.After(10 * time.Second):
		t.Fatal("VetPage is still running after 10 seconds")
	}
}

func TestCheckPageFilePath_ErrorsCarryNoRawControlOrBidi(t *testing.T) {
	for _, p := range []string{"a\x00b.tmpl", "x‮.tmpl", "assets/ .css", "bad\x1b[2J/x.css"} {
		err := CheckPageFilePath(p)
		require.Error(t, err)
		for _, r := range err.Error() {
			assert.True(t, r >= 0x20 && r < 0x7f, "non-ASCII or control %U in %q", r, err.Error())
		}
	}
}
