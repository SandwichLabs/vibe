package artifacts

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// assetURLRe is a literal relative URL into a page's assets: assets/ then
// plain path segments. No scheme, no host, no "..", no query, no whitespace.
var assetURLRe = regexp.MustCompile(`^assets/[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)*$`)

// fragmentRe is a same-page "#fragment".
var fragmentRe = regexp.MustCompile(`^#[A-Za-z0-9_:.-]*$`)

var (
	cssBadFuncRe = regexp.MustCompile(`(?i)(^|[^a-z0-9_-])(image-set|-webkit-image-set|image|src|cross-fade|-webkit-cross-fade|element|-moz-element|paint|expression)\s*\(`)
	cssURLRe     = regexp.MustCompile(`(?i)url\s*\(`)
)

// literalPageURL reports whether s is a URL a page may write literally: a
// same-page fragment or a relative path into assets/.
func literalPageURL(s string) bool {
	return fragmentRe.MatchString(s) || (assetURLRe.MatchString(s) && !strings.Contains(s, ".."))
}

// checkCSS refuses CSS that could load something from outside the page's
// assets: a backslash escape (which can spell any function name), @import, a
// function that takes a URL other than url(), and a url() whose argument is
// not a literal assets/ path or a #fragment. With markers allowed (a style
// attribute or element in a template), a template action may appear in the
// value but never inside url(...). The page CSP is the gate for a static page;
// this keeps the file set honest at save.
func checkCSS(css string, markersOK bool) error {
	if !markersOK && strings.ContainsAny(css, string([]rune{markOpen, markClose})) {
		return errors.New("a stylesheet cannot hold template actions")
	}
	css, err := stripCSSComments(css)
	if err != nil {
		return err
	}
	if strings.Contains(css, `\`) {
		return errors.New("CSS escapes (a backslash) are not allowed")
	}
	if strings.Contains(strings.ToLower(css), "@import") {
		return errors.New("@import is not allowed")
	}
	if cssBadFuncRe.MatchString(css) {
		return errors.New("only url(assets/...) may load a resource in CSS")
	}
	for _, loc := range cssURLRe.FindAllStringIndex(css, -1) {
		rest := css[loc[1]:]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return errors.New("unterminated url()")
		}
		arg := strings.Trim(strings.TrimSpace(rest[:end]), `'"`)
		if !literalPageURL(arg) {
			return errors.New("url() may only name a literal assets/ path or a #fragment")
		}
	}
	return nil
}

func stripCSSComments(css string) (string, error) {
	var b strings.Builder
	for {
		i := strings.Index(css, "/*")
		if i < 0 {
			b.WriteString(css)
			return b.String(), nil
		}
		b.WriteString(css[:i])
		j := strings.Index(css[i+2:], "*/")
		if j < 0 {
			return "", errors.New("unterminated CSS comment")
		}
		css = css[i+2+j+2:]
	}
}

// activeSVGTags are elements an SVG asset must not hold: script, an embedded
// HTML island, and anything that loads a document.
var activeSVGTags = map[string]bool{
	"script": true, "foreignobject": true, "iframe": true, "object": true, "embed": true,
	"animate": true, "set": true, "animatemotion": true, "animatetransform": true,
}

// checkSVG refuses an SVG asset that holds active content. A page shows an SVG
// in an <img>, where it is inert, but a link can navigate to it as a document
// (and a scripted page serves it from the page's own origin), so an SVG that
// holds script, an event handler, a javascript: URL, a DOCTYPE or entity
// declaration, or an element that loads another document is refused at save.
func checkSVG(data []byte) error {
	low := strings.ToLower(string(data))
	if strings.Contains(low, "<!doctype") || strings.Contains(low, "<!entity") {
		return errors.New("an SVG with a DOCTYPE or entity declaration is not allowed")
	}
	z := html.NewTokenizer(strings.NewReader(string(data)))
	var style strings.Builder
	inStyle := false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return nil
			}
			return z.Err()
		case html.CommentToken:
			// The tokenizer reports <?xml-stylesheet ...?> and other
			// processing instructions as comments; only the XML declaration
			// is allowed.
			if raw := string(z.Raw()); strings.HasPrefix(raw, "<?") && !strings.HasPrefix(strings.ToLower(raw), "<?xml ") {
				return errors.New("an SVG with a processing instruction (xml-stylesheet, XSLT) is not allowed")
			}
		case html.TextToken:
			if inStyle {
				style.WriteString(html.UnescapeString(string(z.Raw())))
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			z.NextIsNotRawText()
			name, hasAttr := z.TagName()
			if tt == html.EndTagToken && inStyle && strings.EqualFold(string(name), "style") {
				inStyle = false
				if err := checkCSS(style.String(), false); err != nil {
					return fmt.Errorf("<style>: %w", err)
				}
				style.Reset()
			}
			if tt == html.StartTagToken && strings.EqualFold(string(name), "style") {
				inStyle = true
			}
			if err := checkSVGTag(string(name), hasAttr, z); err != nil {
				return err
			}
		}
	}
}

func checkSVGTag(name string, hasAttr bool, z *html.Tokenizer) error {
	local := strings.ToLower(name)
	if i := strings.LastIndexByte(local, ':'); i >= 0 {
		local = local[i+1:]
	}
	if activeSVGTags[local] {
		return errors.New("an SVG with <" + local + "> is not allowed (it must be inert)")
	}
	for hasAttr {
		var k, v []byte
		k, v, hasAttr = z.TagAttr()
		if err := checkSVGAttr(strings.ToLower(string(k)), string(v)); err != nil {
			return err
		}
	}
	return nil
}

// checkSVGAttr applies the page's own URL rules to an SVG asset's attributes:
// an event handler or script URL is refused, a link or url(...) may name only
// a #fragment or a literal assets/ path.
func checkSVGAttr(key, val string) error {
	clean := strings.ToLower(stripURLSpace(val))
	if strings.HasPrefix(key, "on") || strings.HasPrefix(clean, "javascript:") || strings.HasPrefix(clean, "data:text/html") {
		return errors.New("an SVG with an event handler or javascript: URL is not allowed")
	}
	switch key {
	case "href", "xlink:href", "src":
		if !literalPageURL(strings.TrimSpace(val)) {
			return errors.New("an SVG's links may only name a #fragment or a literal assets/ path")
		}
	}
	if strings.Contains(strings.Join(strings.Fields(clean), ""), "url(") {
		if err := checkCSS(val, false); err != nil {
			return err
		}
	}
	return nil
}

// stripURLSpace removes what a browser removes from a URL before reading its
// scheme: leading and trailing C0 controls and spaces, and tabs and newlines
// anywhere ("java\tscript:" is "javascript:").
func stripURLSpace(s string) string {
	s = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(s)
	return strings.TrimFunc(s, func(r rune) bool { return r <= 0x20 })
}
