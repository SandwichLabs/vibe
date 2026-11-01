package artifacts

import (
	"errors"
	"io"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	tagNameRe  = regexp.MustCompile(`^[a-z][a-z0-9-]*(:[a-z][a-z0-9-]*)?$`)
	attrNameRe = regexp.MustCompile(`^[a-z_:][a-z0-9_:.-]*$`)
)

// refusedTags are elements a page template may not contain: they load another
// document, change how every URL resolves, or have no use in a page that the
// host does not already cover.
var refusedTags = map[string]string{
	"base": "<base> would change how every URL resolves", "iframe": "frames are not allowed", "frame": "frames are not allowed",
	"frameset": "frames are not allowed", "object": "<object> loads another document", "embed": "<embed> loads another document",
	"applet": "<applet> is not allowed", "portal": "<portal> is not allowed", "plaintext": "<plaintext> is not allowed",
	"noscript": "<noscript> is live markup in a page without script, but the HTML tokenizer reads it as text, so it cannot be checked",
	"xmp":      "<xmp> is not allowed", "noembed": "<noembed> is not allowed", "noframes": "<noframes> is not allowed",
	"set": "SVG <set> can change a URL attribute after the page is checked", "animate": "SVG <animate> can change a URL attribute after the page is checked",
}

// refusedAttrs are attributes that open a connection or a window on their own
// (P1 found target=_blank, ping and a preconnect hint leak under every CSP
// layer) or carry a document.
var refusedAttrs = map[string]bool{
	"target": true, "ping": true, "download": true, "srcdoc": true, "manifest": true,
	"codebase": true, "longdesc": true, "background": true, "cite": true, "profile": true,
}

// urlAttrs are attributes whose value is a URL. Each must be a link call, a
// #fragment or a literal relative assets/ path.
var urlAttrs = map[string]bool{
	"href": true, "src": true, "srcset": true, "imagesrcset": true, "poster": true, "action": true,
	"formaction": true, "xlink:href": true, "data": true, "icon": true, "usemap": true,
	"lowsrc": true, "dynsrc": true, "attributionsrc": true,
}

// paintAttrs are presentation attributes (and CSS properties written as
// attributes) whose value may be url(...): a template action may not fill them,
// since data could then supply a URL that no allow-list sees.
var paintAttrs = map[string]bool{
	"fill": true, "stroke": true, "filter": true, "clip-path": true, "mask": true, "mask-image": true,
	"mask-border-source": true, "marker-start": true, "marker-mid": true, "marker-end": true, "marker": true,
	"cursor": true, "shape-inside": true, "shape-subtract": true, "background-image": true,
	"list-style-image": true, "border-image-source": true, "content": true,
}

// vetMarkup tokenizes a skeleton and applies the markup rules: no action in a
// tag name or attribute list, the element and attribute refusals, the URL
// allow-list, the CSS rules for style, and the scripted test. It reports
// whether the template is scripted. A skeleton that ends inside a tag or a
// comment is refused, because the tokenizer drops such a tail silently.
func vetMarkup(file string, sk *skeleton, files map[string]bool, strict bool) ([]string, error) {
	src := sk.text.String()
	// A "<" followed by an action would let data supply the tag name that the
	// tokenizer cannot see ("<" then "script" from a database row is a
	// <script> tag in the output). Refuse it before tokenizing.
	if strings.Contains(src, "<"+string(markOpen)) || strings.Contains(src, "</"+string(markOpen)) {
		return nil, invalid("html/%s: a template action cannot directly follow \"<\" or \"</\" (data would be building a tag name)", file)
	}
	mv := &markupVet{file: file, root: file, sk: sk, files: files, origins: map[string]bool{}, strict: strict}
	// An <svg> or <math> anywhere in the expansion may be in force before a
	// <style> that precedes it in the text (a range body runs again, a branch
	// may be taken first): read everything as foreign content from the start.
	if low := strings.ToLower(src); strings.Contains(low, "<svg") || strings.Contains(low, "<math") {
		mv.foreign = true
	}
	z := html.NewTokenizer(strings.NewReader(src))
	consumed := 0
	for {
		tt := z.Next()
		if tt != html.ErrorToken {
			// Copied: TagName and TagAttr rewrite the buffer Raw returns.
			raw := append([]byte(nil), z.Raw()...)
			if f := sk.originAt(consumed); f != "" {
				mv.file = f
			} else {
				mv.file = mv.root
			}
			consumed += len(raw)
			if err := mv.token(z, tt, raw); err != nil {
				return nil, err
			}
			continue
		}
		if !errors.Is(z.Err(), io.EOF) {
			return nil, invalid("html/%s: the markup cannot be read: %v", file, z.Err())
		}
		if consumed < len(src) {
			return nil, invalid("html/%s ends inside a tag, so the page's markup cannot be checked", file)
		}
		if mv.inStyle {
			if err := mv.endStyle(); err != nil {
				return nil, err
			}
		}
		return sortedKeys(mv.origins), nil
	}
}

type markupVet struct {
	// file is the template file the current token came from; root the one the
	// expansion started at.
	file    string
	root    string
	sk      *skeleton
	files   map[string]bool
	origins map[string]bool // files that made the page scripted
	// strict adds the write-time rules (an inline <script> is refused).
	strict bool
	// foreign is set once an <svg> or <math> start tag was seen: from then on
	// no element's content is treated as raw text (the HTML parser does not
	// in foreign content, so a <script> inside <svg><style> is a script).
	foreign bool
	// style accumulates the text of a <style> element.
	inStyle bool
	style   strings.Builder
}

func (mv *markupVet) token(z *html.Tokenizer, tt html.TokenType, raw []byte) error {
	wasStyle := mv.inStyle
	switch tt {
	case html.StartTagToken, html.SelfClosingTagToken:
		if err := mv.startTag(z, tt); err != nil {
			return err
		}
	case html.EndTagToken:
		name, _ := z.TagName()
		if mv.inStyle && string(name) == "style" {
			return mv.endStyle()
		}
	case html.CommentToken:
		// html/template removes <!-- --> comments from its output, which can
		// join the text on either side into a tag (<<!-- -->script>), and the
		// browser reads <!x>, <?x>, <![CDATA[ and </ x> as comments whose end
		// the tokenizer and the browser may place differently in foreign
		// content. A template comment is {{/* ... */}}.
		return invalid("html/%s: HTML comments are not allowed in a page template (use {{/* ... */}})", mv.file)
	}
	if wasStyle {
		if tt == html.TextToken && mv.foreign {
			// Inside <svg> the browser decodes entities in <style> text.
			raw = []byte(html.UnescapeString(string(raw)))
		}
		mv.style.Write(raw)
	}
	return nil
}

func (mv *markupVet) endStyle() error {
	css := mv.style.String()
	mv.inStyle = false
	mv.style.Reset()
	if err := checkCSS(css, true); err != nil {
		return invalid("html/%s: <style>: %v", mv.file, err)
	}
	return nil
}

type attr struct{ key, val string }

func (mv *markupVet) startTag(z *html.Tokenizer, tt html.TokenType) error {
	nameB, hasAttr := z.TagName()
	name := string(nameB)
	var attrs []attr
	for hasAttr {
		var k, v []byte
		k, v, hasAttr = z.TagAttr()
		attrs = append(attrs, attr{string(k), string(v)})
	}
	if name == "svg" || name == "math" {
		mv.foreign = true
	}
	if mv.foreign {
		z.NextIsNotRawText()
	}
	if err := mv.checkTag(name, attrs); err != nil {
		return err
	}
	if name == "style" && tt == html.StartTagToken && !mv.inStyle {
		mv.inStyle = true
	}
	return nil
}

func (mv *markupVet) checkTag(name string, attrs []attr) error {
	if hasMark(name) {
		return invalid("html/%s: a template action cannot appear inside a tag name or between a tag's attributes (<%s...)", mv.file, strings.TrimFunc(name, func(r rune) bool { return r == markOpen || r == markClose || (r >= '0' && r <= '9') }))
	}
	if !tagNameRe.MatchString(name) {
		return invalid("html/%s: <%s> is not a usable tag name", mv.file, truncateForError(name))
	}
	local := name[strings.LastIndexByte(name, ':')+1:]
	if why, bad := refusedTags[local]; bad {
		return invalid("html/%s: <%s>: %s", mv.file, name, why)
	}
	if local == "script" {
		if mv.strict {
			if err := mv.checkInlineScript(attrs); err != nil {
				return err
			}
		}
		mv.origins[mv.file] = true
	}
	for _, a := range attrs {
		if err := mv.checkAttr(name, local, a); err != nil {
			return err
		}
	}
	return mv.checkElement(name, local, attrs)
}

func attrOf(attrs []attr, key string) (string, bool) {
	for _, a := range attrs {
		if a.key == key {
			return a.val, true
		}
	}
	return "", false
}

// checkElement applies the rules that depend on an element's attributes:
// <meta http-equiv>, and <link>, which may only be a stylesheet in assets/.
func (mv *markupVet) checkElement(name, local string, attrs []attr) error {
	switch local {
	case "meta":
		if _, has := attrOf(attrs, "http-equiv"); has {
			return invalid("html/%s: <meta http-equiv> is not allowed (it can refresh or redirect the page)", mv.file)
		}
	case "link":
		rel, _ := attrOf(attrs, "rel")
		if hasMark(rel) || strings.ToLower(strings.TrimSpace(rel)) != "stylesheet" {
			return invalid("html/%s: <link> is only allowed with rel=\"stylesheet\" (other values start network connections)", mv.file)
		}
		for _, a := range attrs {
			switch a.key {
			case "rel", "href", "type", "media":
			default:
				return invalid("html/%s: <link> may only have rel, href, type and media", mv.file)
			}
		}
		href, has := attrOf(attrs, "href")
		if !has || !assetURLRe.MatchString(href) || strings.Contains(href, "..") {
			return invalid("html/%s: <link rel=\"stylesheet\"> needs a literal relative href into %s/", mv.file, assetsDir)
		}
	}
	return nil
}

func (mv *markupVet) checkAttr(tag, local string, a attr) error {
	if hasMark(a.key) {
		return invalid("html/%s: a template action cannot appear in an attribute name or between attributes (<%s ...>)", mv.file, tag)
	}
	if !attrNameRe.MatchString(a.key) {
		return invalid("html/%s: %q is not a usable attribute name", mv.file, truncateForError(a.key))
	}
	if refusedAttrs[a.key] {
		return invalid("html/%s: the %s attribute is not allowed (it opens a connection or a window)", mv.file, a.key)
	}
	switch {
	case strings.HasPrefix(a.key, "on"):
		mv.origins[mv.file] = true
	case a.key == "style":
		if err := checkCSS(a.val, true); err != nil {
			return invalid("html/%s: style attribute: %v", mv.file, err)
		}
	case urlAttrs[a.key]:
		return mv.checkURL(tag, local, a)
	case paintAttrs[a.key] && hasMark(a.val):
		return invalid("html/%s: the %s attribute of <%s> may not be filled by a template action (it can hold url(...))", mv.file, a.key, tag)
	}
	if strings.Contains(strings.ToLower(strings.Join(strings.Fields(a.val), "")), "url(") {
		if err := checkCSS(a.val, true); err != nil {
			return invalid("html/%s: the %s attribute of <%s>: %v", mv.file, a.key, tag, err)
		}
	}
	return nil
}

// checkURL is H2's URL attribute allow-list.
func (mv *markupVet) checkURL(tag, local string, a attr) error {
	val := a.val
	if a.key == "srcset" || a.key == "imagesrcset" {
		return mv.checkSrcset(tag, a)
	}
	if i, ok := markIndex(val); ok {
		if m := mv.sk.marks[i]; m.kind == markAction && m.linkCall {
			return nil
		}
		return invalid("html/%s: the %s attribute of <%s> may be filled only by a link call, not another template action", mv.file, a.key, tag)
	}
	if hasMark(val) {
		return invalid("html/%s: the %s attribute of <%s> may not hold a template action beside other text (a URL is a link call, a #fragment or a literal assets/ path)", mv.file, a.key, tag)
	}
	if local == "script" && a.key == "src" && scriptSrcOK(val, mv.files) {
		return nil
	}
	if !literalPageURL(val) {
		return invalid("html/%s: the %s attribute of <%s> is %q, which is not allowed: a URL is a link call, a #fragment or a literal relative %s/ path", mv.file, a.key, tag, truncateForError(val), assetsDir)
	}
	return nil
}

func (mv *markupVet) checkSrcset(tag string, a attr) error {
	if hasMark(a.val) {
		return invalid("html/%s: the %s attribute of <%s> may not hold a template action", mv.file, a.key, tag)
	}
	for _, cand := range strings.Split(a.val, ",") {
		fields := strings.Fields(cand)
		if len(fields) == 0 || !literalPageURL(fields[0]) {
			return invalid("html/%s: every URL in the %s attribute of <%s> must be a literal relative %s/ path", mv.file, a.key, tag, assetsDir)
		}
	}
	return nil
}

// slawLibRe is a vendored library the page window serves: slaw-lib/<name>.js.
// Whether Studio vendors that name is not known here (the host answers 404 for
// one it does not).
var slawLibRe = regexp.MustCompile(`^slaw-lib/[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.js$`)

// scriptSrcOK is the one rule for a <script src>: a page's own script file
// directly under html/, or a vendored library. files holds the page's paths.
func scriptSrcOK(val string, files map[string]bool) bool {
	if slawLibRe.MatchString(val) {
		return true
	}
	return files[val] && !strings.Contains(val, "/") && (path.Ext(val) == ".js" || path.Ext(val) == ".mjs")
}

// checkInlineScript refuses a <script> that holds code in the page: the page's
// Content-Security-Policy has script-src 'self' and no 'unsafe-inline', so it
// would never run. Code goes in a .js file under html/ loaded by
// <script src="app.js">; data goes in a <script type="application/json">
// block written with {{ json ... }}, which is not code and is allowed.
func (mv *markupVet) checkInlineScript(attrs []attr) error {
	if _, has := attrOf(attrs, "src"); has {
		return nil
	}
	typ, _ := attrOf(attrs, "type")
	if hasMark(typ) || !isScriptMIME(typ) {
		return nil
	}
	return invalid("html/%s: an inline <script> never runs: the page's Content-Security-Policy allows scripts only from the page's own .js files, so inline code is blocked. "+
		"Move the code to a file such as html/app.js and load it with <script src=\"app.js\"></script> (a page with a script file is a scripted page, which the person allows in Studio); "+
		"to pass data to it, write <script type=\"application/json\" id=\"data\">{{ json (query \"name\") }}</script> and read it with JSON.parse(document.getElementById(\"data\").textContent)", mv.file)
}

// isScriptMIME reports whether a <script type> value is one the browser runs
// as code: empty, "module", or a JavaScript MIME type. Any other type makes the
// element an inert data block.
func isScriptMIME(typ string) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	if t == "" || t == "module" {
		return true
	}
	switch t {
	case "text/javascript", "application/javascript", "application/x-javascript", "text/ecmascript", "application/ecmascript",
		"text/jscript", "text/livescript", "text/x-javascript", "text/x-ecmascript", "text/javascript1.0", "text/javascript1.1",
		"text/javascript1.2", "text/javascript1.3", "text/javascript1.4", "text/javascript1.5", "text/jscript1.0", "application/x-ecmascript":
		return true
	}
	return false
}
