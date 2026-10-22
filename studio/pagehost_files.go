package main

// What Studio sends a page window (docs/plans/html-artifacts.md, H4): the
// page's files as they were hashed, the rendered entry document, the host's
// theme stylesheet and the vendored libraries the page names. The helper holds
// them in memory and serves them under vibe-page://<name>/; it has no other
// source, so a path the page asks for that is not here does not exist.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/pagehost"
)

// sentFile is one file of the page window's file set.
type sentFile struct {
	path string
	ct   string
	data []byte
}

const (
	themePath = "_vibe/theme.css"
	libPrefix = "vibe-lib/"
	cssType   = "text/css; charset=utf-8"
	jsType    = "text/javascript; charset=utf-8"
)

// libRefRe finds a vendored library a document or a script names.
var libRefRe = regexp.MustCompile(`vibe-lib/([A-Za-z0-9][A-Za-z0-9._-]*\.js)`)

// pageFileTypes is the Content-Type of a page file by extension. Templates are
// never sent: they are rendered in Studio and nothing asks for their source.
func pageFileType(p string) (string, bool) {
	switch path.Ext(p) {
	case ".js", ".mjs":
		return jsType, true
	case ".tmpl":
		return "", false
	}
	t, ok := pageType[path.Ext(p)]
	return t, ok
}

// pageFileSet is every file the window serves: the page's own (not templates),
// the theme and the libraries it names.
func pageFileSet(page *artifacts.Artifact, doc []byte, themeCSS string, libs fs.FS) ([]sentFile, error) {
	var out []sentFile
	named := map[string]bool{}
	scan := func(b []byte) {
		for _, m := range libRefRe.FindAllSubmatch(b, -1) {
			named[string(m[1])] = true
		}
	}
	scan(doc)
	for _, f := range page.Files {
		ct, ok := pageFileType(f.Path)
		if !ok {
			continue
		}
		out = append(out, sentFile{path: f.Path, ct: ct, data: f.Data})
		if ct == jsType {
			scan(f.Data)
		}
	}
	out = append(out, sentFile{path: themePath, ct: cssType, data: []byte(themeCSS)})
	names := make([]string, 0, len(named))
	for n := range named {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if libs == nil {
			break
		}
		b, err := fs.ReadFile(libs, n)
		if err != nil {
			continue // a name that is not vendored is a 404 for the page, not an error
		}
		out = append(out, sentFile{path: libPrefix + n, ct: jsType, data: b})
	}
	total := len(doc)
	for _, f := range out {
		total += len(f.data)
	}
	if len(out) > pagehost.MaxFiles || total > pagehost.MaxTotalBytes {
		return nil, fmt.Errorf("the page is too large to open: %d files, %d bytes", len(out), total)
	}
	return out, nil
}

// chunks splits data into protocol chunks; an empty body is one empty chunk.
func chunks(data []byte) [][]byte {
	if len(data) == 0 {
		return [][]byte{nil}
	}
	var out [][]byte
	for len(data) > 0 {
		n := min(len(data), pagehost.ChunkBytes)
		out = append(out, data[:n])
		data = data[n:]
	}
	return out
}

// fileMessages is the file's chunks as protocol messages.
func fileMessages(f sentFile) []*pagehost.Message {
	parts := chunks(f.data)
	out := make([]*pagehost.Message, len(parts))
	for i, p := range parts {
		out[i] = &pagehost.Message{T: pagehost.TypeFile, Path: f.path, CT: f.ct, Seq: i, Last: i == len(parts)-1, Data: p}
	}
	return out
}

// docMessages is a document's chunks as replies to request id (0 is the
// document the window opens with).
func docMessages(id int64, status int, body []byte) []*pagehost.Message {
	parts := chunks(body)
	out := make([]*pagehost.Message, len(parts))
	for i, p := range parts {
		out[i] = &pagehost.Message{T: pagehost.TypeDocr, ID: id, Status: status, Seq: i, Last: i == len(parts)-1, Data: p}
	}
	return out
}

var headOpenRe = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)

// withThemeLink puts the host stylesheet at the top of the document's head, so
// the page's own styles come after it and win. A document with no head tag is
// served as rendered: the theme is a convenience, not a requirement.
func withThemeLink(doc []byte) []byte {
	loc := headOpenRe.FindIndex(doc)
	if loc == nil {
		return doc
	}
	const link = `<link rel="stylesheet" href="` + themePath + `">`
	var b bytes.Buffer
	b.Grow(len(doc) + len(link))
	b.Write(doc[:loc[1]])
	b.WriteString(link)
	b.Write(doc[loc[1]:])
	return b.Bytes()
}

var (
	themeNameRe  = regexp.MustCompile(`^--vibe-[a-z0-9][a-z0-9-]{0,40}$`)
	themeValueRe = regexp.MustCompile(`^[A-Za-z0-9 #(),.%/'"_+-]{1,160}$`)
)

// themeStylesheet builds _vibe/theme.css from the roles the person's theme set
// on Studio's window. Only a --vibe-* name with a plain value gets through; a
// value that could close a declaration, open a function that loads something
// or unbalance a quote is dropped, so this is not a way to write CSS.
func themeStylesheet(roles map[string]string) string {
	names := make([]string, 0, len(roles))
	for n := range roles {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(":root{")
	scheme := ""
	for _, n := range names {
		v := strings.TrimSpace(roles[n])
		if n == "--vibe-theme-type" {
			if v == "light" || v == "dark" {
				scheme = v
			}
			continue
		}
		if !themeNameRe.MatchString(n) || !safeThemeValue(v) {
			continue
		}
		fmt.Fprintf(&b, "%s:%s;", n, v)
	}
	if scheme != "" {
		fmt.Fprintf(&b, "color-scheme:%s;", scheme)
	}
	b.WriteString("}\n")
	return b.String()
}

func safeThemeValue(v string) bool {
	if !themeValueRe.MatchString(v) || strings.Count(v, `"`)%2 != 0 || strings.Count(v, "'")%2 != 0 {
		return false
	}
	l := strings.ToLower(v)
	return !strings.Contains(l, "url") && !strings.Contains(l, "expression") && !strings.Contains(l, "image") && !strings.Contains(l, "//") && !strings.Contains(l, "/*")
}

// Default window size when the manifest names none.
const (
	defaultWindowWidth  = 960
	defaultWindowHeight = 720
)

// buildWindowSetup renders the page's first document and returns every message
// that opens the window: open, the files, the document, go. A render that
// fails refuses the open with the renderer's fixed text.
func buildWindowSetup(ctx context.Context, run artifacts.PageRunner, page *artifacts.Artifact, req pageOpen, libs fs.FS, caps []string) ([]*pagehost.Message, error) {
	var body bytes.Buffer
	if err := renderPageQuery(ctx, run, page, req.query, &body); err != nil {
		var re *artifacts.RenderError
		if errors.As(err, &re) {
			return nil, re
		}
		return nil, errPageWindowSetup
	}
	doc := withThemeLink(body.Bytes())
	files, err := pageFileSet(page, doc, themeStylesheet(req.theme), libs)
	if err != nil {
		return nil, errPageTooLarge
	}
	params, err := pageParamsJSON(&page.Manifest, req.query)
	if err != nil {
		return nil, errPageWindowSetup
	}
	w, h := defaultWindowWidth, defaultWindowHeight
	if win := page.Manifest.HTML.Window; win != nil {
		w, h = win.Width, win.Height
	}
	msgs := []*pagehost.Message{{
		T: pagehost.TypeOpen, Name: req.name, Title: windowTitle(&page.Manifest, req.name), W: w, H: h, Params: params, Q: req.query, Caps: caps,
	}}
	for _, f := range files {
		msgs = append(msgs, fileMessages(f)...)
	}
	msgs = append(msgs, docMessages(0, 200, doc)...)
	return append(msgs, &pagehost.Message{T: pagehost.TypeGo}), nil
}

// pageParamsJSON is what vibe.params() returns: for each declared parameter its
// type, label, default, static options, and the value this page was drawn with.
func pageParamsJSON(m *artifacts.Manifest, rawQuery string) ([]byte, error) {
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to read the query string: %w", err)
	}
	supplied, err := artifacts.ParsePageQuery(m, q)
	if err != nil {
		return nil, fmt.Errorf("failed to read the parameter values: %w", err)
	}
	defaults, err := artifacts.Defaults(m, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the defaults: %w", err)
	}
	out := make(map[string]any, len(m.Parameters))
	for _, p := range m.Parameters {
		value, ok := supplied[p.Name]
		if !ok {
			value = defaults[p.Name]
		}
		entry := map[string]any{"type": p.Type, "label": p.Label, "default": defaults[p.Name], "value": value}
		if len(p.Options) > 0 {
			entry["options"] = p.Options
		}
		out[p.Name] = entry
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the parameters: %w", err)
	}
	return b, nil
}
