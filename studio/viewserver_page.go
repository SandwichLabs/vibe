package main

// The static page routes of the view server (docs/plans/html-artifacts.md, H3).
// A static HTML artifact renders inside Studio's window in a sandbox="" frame
// with no script, so everything that keeps it harmless is in the response:
//
//	/vibe-view/<token>/page?<params>   the page, rendered in Go from the files
//	                                   that were hashed, values from the query
//	                                   string only, always through Bind
//	/vibe-view/<token>/assets/<path>   a file under html/assets/ (never a query)
//
// The artifact comes from the token, never the path. Every response, errors
// included, carries the page CSP (a page's own or the sandboxed deny policy),
// nosniff and no-store. A token's sub-resources load only from assets/, which
// runs no query, so an <img> or a CSS url() cannot call /page; as a second
// layer renders are limited to 4 in flight and 60 a minute per token (the
// webview sends no Sec-Fetch-* header on these requests in the measured
// WebKitGTK and WKWebView builds, so the route cannot always tell a navigation
// from a sub-resource, and refuses it when the header says it is one).

import (
	"bytes"
	"context"
	"errors"
	"github.com/SandwichLabs/vibe/internal/engine"
	"net/http"
	"net/url"
	"path"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

// Limits of the page route.
const (
	maxRendersInFlight = 4
	maxRendersPerMin   = 60
	// pageVerdictTTL is the default for viewServer.verdictTTL.
	pageVerdictTTL = 500 * time.Millisecond
)

// pageDenyCSP is the policy of every response that is not a page's own: a
// sandboxed document that loads and runs nothing.
const pageDenyCSP = "sandbox; " + denyCSP

// pageCSP is H3's policy for a token's page and assets. <a> is the token's
// assets/ directory on the Studio origin: sub-resources load from there only.
func pageCSP(origin, token string) string {
	a := origin + viewPrefix + "/" + token + "/assets/"
	return "sandbox; default-src 'none'; script-src 'none'; style-src " + a + " 'unsafe-inline'; img-src " + a + " data:; font-src " + a +
		"; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'"
}

// renderLimiter admits at most maxRendersInFlight renders at once and
// maxRendersPerMin in any minute.
type renderLimiter struct {
	mu       sync.Mutex
	inFlight int
	starts   []time.Time
}

// acquire admits a render, or reports that the limit is reached. Call release
// when the render ends.
func (l *renderLimiter) acquire(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-time.Minute)
	keep := l.starts[:0]
	for _, t := range l.starts {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	l.starts = keep
	if l.inFlight >= maxRendersInFlight || len(l.starts) >= maxRendersPerMin {
		return false
	}
	l.inFlight++
	l.starts = append(l.starts, now)
	return true
}

func (l *renderLimiter) release() {
	l.mu.Lock()
	l.inFlight--
	l.mu.Unlock()
}

// pageType is the Content-Type of a page asset by extension (the closed list
// of H1); anything else is not served.
var pageType = map[string]string{
	".css": "text/css; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg",
	".webp": "image/webp", ".woff2": "font/woff2", ".json": "application/json; charset=utf-8",
}

// navigatesAFrame reports whether the request's own Sec-Fetch headers, when
// the webview sends them, say it is a frame navigation. An absent header is
// not evidence of a sub-resource (see the file comment), so it is allowed.
func navigatesAFrame(r *http.Request) bool {
	if d := r.Header.Get("Sec-Fetch-Dest"); d != "" && d != "iframe" && d != "frame" && d != "document" {
		return false
	}
	if m := r.Header.Get("Sec-Fetch-Mode"); m != "" && m != "navigate" && m != "nested-navigate" {
		return false
	}
	return true
}

// pageGrant returns the token's page grant, or writes the refusal.
func (v *viewServer) pageGrant(w http.ResponseWriter, r *http.Request, token string) *viewGrant {
	g := v.lookup(token)
	if g == nil || g.page == nil {
		reply(w, r, http.StatusNotFound, pageDenyCSP, "", nil)
		return nil
	}
	return g
}

func (v *viewServer) servePage(w http.ResponseWriter, r *http.Request, token string) {
	g := v.pageGrant(w, r, token)
	if g == nil {
		return
	}
	csp := pageCSP(g.origin, token)
	if !navigatesAFrame(r) {
		reply(w, r, http.StatusForbidden, csp, "text/html; charset=utf-8", artifacts.PageErrorHTML(nil))
		return
	}
	if r.Method == http.MethodHead {
		reply(w, r, http.StatusOK, csp, "text/html; charset=utf-8", nil)
		return
	}
	if len(r.URL.RawQuery) > artifacts.MaxPageQueryBytes {
		reply(w, r, http.StatusRequestURITooLong, csp, "text/html; charset=utf-8", artifacts.PageErrorHTML(&artifacts.RenderError{Kind: artifacts.RenderBadValues}))
		return
	}
	if !g.limit.acquire(time.Now()) {
		w.Header().Set("Retry-After", "5")
		reply(w, r, http.StatusTooManyRequests, csp, "text/html; charset=utf-8", artifacts.PageErrorHTML(&artifacts.RenderError{Kind: artifacts.RenderTooSlow}))
		return
	}
	defer g.limit.release()

	var out bytes.Buffer
	err := renderPage(g, r.URL.RawQuery, &out)
	// The files may have changed while the page rendered: its bytes are
	// delivered only if the page is still allowed at the hash it was served at.
	if v.lookup(token) == nil {
		reply(w, r, http.StatusNotFound, pageDenyCSP, "", nil)
		return
	}
	if err != nil {
		v.logRender(g, err)
		reply(w, r, renderStatus(err), csp, "text/html; charset=utf-8", artifacts.PageErrorHTML(err))
		return
	}
	reply(w, r, http.StatusOK, csp, "text/html; charset=utf-8", out.Bytes())
}

// renderPage reads the values from the raw query string (a pair the standard
// parser would drop, such as one with a semicolon, is a refusal, not a default)
// and renders the grant's page.
func renderPage(g *viewGrant, rawQuery string, out *bytes.Buffer) error {
	return renderPageQuery(g.ctx, g.run, g.page, rawQuery, out)
}

// renderPageQuery is renderPage over the pieces a page window also has: the
// context, the runner pinned to the page's manifest, the page.
func renderPageQuery(ctx context.Context, run artifacts.PageRunner, page *artifacts.Artifact, rawQuery string, out *bytes.Buffer) error {
	bad := &artifacts.RenderError{Kind: artifacts.RenderBadValues}
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return bad
	}
	values, err := artifacts.ParsePageQuery(&page.Manifest, q)
	if err != nil {
		return bad
	}
	return artifacts.RenderHTMLPage(ctx, run, page, values, out)
}

// renderStatus is the HTTP status of a failed render.
func renderStatus(err error) int {
	var re *artifacts.RenderError
	if errors.As(err, &re) {
		switch re.Kind {
		case artifacts.RenderBadValues:
			return http.StatusUnprocessableEntity
		case artifacts.RenderTooLarge:
			return http.StatusInsufficientStorage
		case artifacts.RenderTooSlow:
			return http.StatusGatewayTimeout
		}
	}
	return http.StatusInternalServerError
}

// logRender records why a render failed, at debug level: the cause can hold a
// path or a policy reason and is never shown.
func (v *viewServer) logRender(g *viewGrant, err error) {
	if v.log != nil {
		v.log.Debug("page render failed", "artifact", g.name, "error", engine.ErrorForLog(err), "cause", engine.ErrorForLog(errors.Unwrap(err)))
	}
}

func (v *viewServer) serveAsset(w http.ResponseWriter, r *http.Request, token, rel string) {
	g := v.pageGrant(w, r, token)
	if g == nil {
		return
	}
	csp := pageCSP(g.origin, token)
	typ, ok := pageType[path.Ext(rel)]
	if !ok || r.URL.RawQuery != "" {
		reply(w, r, http.StatusNotFound, csp, "", nil)
		return
	}
	// Only a file the hash covers, found by its exact path under html/assets/.
	for _, f := range g.page.Files {
		if f.Path == rel {
			reply(w, r, http.StatusOK, csp, typ, f.Data)
			return
		}
	}
	reply(w, r, http.StatusNotFound, csp, "", nil)
}
