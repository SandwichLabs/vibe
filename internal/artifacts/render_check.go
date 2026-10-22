package artifacts

import (
	"errors"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// checkRendered is the defence in depth behind the save-time vet: the markup
// the page actually produced, tokenized again, must still hold the vet's
// rules. The template's markup was checked at save, and html/template escapes
// every value by context; this catches a bug in either, or a file that
// reached the renderer without the vet. It refuses a script or frame element,
// an inline event handler, an attribute that opens a connection or a window,
// a <meta http-equiv>, a <link> that is not a local stylesheet, and any URL
// attribute that is not this page's own route, a #fragment or a literal
// relative assets/ path. It reads text it has no opinion on, so a hostile row
// value that was escaped to text is fine, and is not rewritten. A scripted
// page's own script elements and handlers are allowed (they were vetted as
// the page's code and are what makes it scripted); everything else holds.
func checkRendered(out []byte, scripted bool, files map[string]bool) error {
	z := html.NewTokenizer(strings.NewReader(string(out)))
	foreign := false
	for {
		switch tt := z.Next(); tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return nil
			}
			return errors.New("the rendered markup cannot be read")
		case html.StartTagToken, html.SelfClosingTagToken:
			nameB, hasAttr := z.TagName()
			name := string(nameB)
			var attrs []attr
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs = append(attrs, attr{string(k), string(v)})
			}
			if name == "svg" || name == "math" {
				foreign = true
			}
			if foreign {
				z.NextIsNotRawText()
			}
			if err := checkRenderedTag(name, attrs, scripted, files); err != nil {
				return err
			}
		}
	}
}

func checkRenderedTag(name string, attrs []attr, scripted bool, files map[string]bool) error {
	local := name[strings.LastIndexByte(name, ':')+1:]
	if _, bad := refusedTags[local]; bad || (local == "script" && !scripted) {
		return errors.New("the rendered page holds a <" + local + "> element")
	}
	for _, a := range attrs {
		switch {
		case refusedAttrs[a.key], strings.HasPrefix(a.key, "on") && !scripted:
			return errors.New("the rendered page holds a " + a.key + " attribute")
		case a.key == "style":
			if err := checkCSS(a.val, false); err != nil {
				return err
			}
		case local == "script" && scripted && a.key == "src" && scriptSrcOK(a.val, files):
			// The page's own script or a vendored library: the code that makes it scripted.
		case urlAttrs[a.key]:
			if err := checkRenderedURL(a); err != nil {
				return err
			}
		}
	}
	switch local {
	case "meta":
		if _, has := attrOf(attrs, "http-equiv"); has {
			return errors.New("the rendered page holds a <meta http-equiv>")
		}
	case "link":
		if rel, _ := attrOf(attrs, "rel"); strings.ToLower(strings.TrimSpace(rel)) != "stylesheet" {
			return errors.New("the rendered page holds a <link> that is not a stylesheet")
		}
	}
	return nil
}

// ownRoute reports whether s is this page's own route ("page" or "page?...").
func ownRoute(s string) bool {
	return s == pageRoute || strings.HasPrefix(s, pageRoute+"?")
}

func checkRenderedURL(a attr) error {
	cands := []string{a.val}
	if a.key == "srcset" || a.key == "imagesrcset" {
		cands = cands[:0]
		for _, c := range strings.Split(a.val, ",") {
			if f := strings.Fields(c); len(f) > 0 {
				cands = append(cands, f[0])
			}
		}
	}
	for _, c := range cands {
		if c != "" && !literalPageURL(c) && !ownRoute(c) {
			return errors.New("the rendered page holds a " + a.key + " URL that is not its own route, a fragment or an asset")
		}
	}
	return nil
}
