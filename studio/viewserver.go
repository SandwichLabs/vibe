package main

// The view server (#160): serves a custom artifact view (view: script) to its
// Worker, and the pinned libraries, under /assay-view/ through Wails'
// AssetServer.Middleware. It is a helper, not a binding.
//
// Routes (anything else under /assay-view/ is 404, or 400 when the path could
// make Wails inject its runtime):
//
//	/assay-view/<token>/worker.js   the host-owned bootstrap (viewhost/worker.js)
//	/assay-view/<token>/view.js     the artifact's view, the bytes that were hashed
//	/assay-view/lib/<file>.js       one pinned library from the embedded build
//	/assay-view/<token>/page?...    a static HTML page, rendered in Go (viewserver_page.go)
//	/assay-view/<token>/assets/...  a file of that page's html/assets/
//
// A token is 32 random bytes, issued by OpenArtifactView after it re-checked the
// trust decision, and is bound to one artifact name, one ViewHash and one
// workspace for this process. The view's bytes are read once, when the token is
// issued, hashed, compared with the allowed hash and held in memory; those
// bytes are what view.js serves. A request is also refused (and the token
// dropped) when the decision no longer allows exactly that hash, so a changed or
// revoked view gets no bytes. Every response, errors included, carries a CSP,
// nosniff and no-store. Nothing from the workspace is reachable except through
// a token.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/assay/internal/artifacts"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

const (
	viewPrefix = "/assay-view"
	viewLibDir = "/assay-view/lib/"
	// maxViewGrants bounds the bytes held for tokens (each view.js is at most
	// 1 MB). A new token for an artifact replaces that artifact's older ones.
	maxViewGrants = 32
)

var (
	viewTokenRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
	viewLibRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.js$`)
	// viewOriginRe is what the page may name as its own origin: the CSP needs it
	// spelled out (a path source needs a host), and a webview does not always
	// tell the server its scheme (the custom scheme is stripped from the request).
	viewOriginRe = regexp.MustCompile(`^(https?|wails)://[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)
)

// viewGrant is what one token stands for.
type viewGrant struct {
	name   string
	hash   string
	origin string
	// ws is the workspace the token was issued in (compared, never used here).
	ws any
	// script is the view.js that was hashed, held so what runs is what was hashed.
	script []byte
	// pins are the metric and saved-query revisions the allow covers: the view's
	// queries run with them pinned (semantics.WithPins).
	pins map[string]string
	// manifest is the artifact.yaml that was hashed, decoded once: the view's
	// queries run from it, not from the file as it is on disk at each run.
	manifest *artifacts.Manifest
	// page, run and limit make this the grant of a static HTML page (the files
	// that were hashed, the runner that answers its queries, the render rate
	// limit); okUntil is when the last positive re-check stops being trusted.
	page    *artifacts.Artifact
	run     artifacts.PageRunner
	limit   renderLimiter
	okUntil time.Time
	// ctx ends when the token is dropped or replaced, the workspace changes or
	// the decision stops allowing it: the queries run for this grant stop with it.
	ctx    context.Context
	cancel context.CancelFunc
}

// viewServer holds the tokens and serves the routes.
type viewServer struct {
	worker []byte
	libs   fs.FS
	// stillAllowed re-checks that the grant's artifact is, right now, allowed at
	// the grant's hash in the open workspace.
	stillAllowed func(*viewGrant) bool
	// verdictTTL is how long a page grant's positive re-check is reused: a page
	// makes a request per asset, and each re-check reads and vets the whole file
	// set. What is served never depends on it (the held bytes are the hashed
	// bytes); it bounds how late a revoke or an edit is noticed. Zero rechecks
	// every request.
	verdictTTL time.Duration
	// log receives the causes of failed renders (debug only; never shown).
	log *slog.Logger
	// base is the parent of every grant's context: the app's lifecycle context
	// once Studio has started, so shutdown stops a view's queries too.
	base context.Context

	mu     sync.Mutex
	grants map[string]*viewGrant
	order  []string
}

// newViewServer builds the server. libs is the directory of pinned libraries
// (files directly in it), worker the bootstrap script.
func newViewServer(worker []byte, libs fs.FS, stillAllowed func(*viewGrant) bool) *viewServer {
	return &viewServer{worker: worker, libs: libs, stillAllowed: stillAllowed, grants: map[string]*viewGrant{}, verdictTTL: pageVerdictTTL}
}

// setBase makes ctx the parent of grants issued from now on.
func (v *viewServer) setBase(ctx context.Context) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.base = ctx
}

// errBadViewOrigin is the fixed refusal of an origin that is not a plain one.
var errBadViewOrigin = errors.New("that is not a valid origin")

// issue creates a token for the grant, replacing the artifact's older ones.
func (v *viewServer) issue(g *viewGrant) (string, error) {
	if !viewOriginRe.MatchString(g.origin) {
		return "", errBadViewOrigin
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("failed to make a view token: %w", err)
	}
	token := hex.EncodeToString(raw[:])
	v.mu.Lock()
	defer v.mu.Unlock()
	parent := v.base
	if parent == nil {
		parent = context.Background()
	}
	g.ctx, g.cancel = context.WithCancel(parent)
	for t, old := range v.grants {
		if old.name == g.name {
			v.dropLocked(t)
		}
	}
	for len(v.order) >= maxViewGrants {
		v.dropLocked(v.order[0])
	}
	v.grants[token] = g
	v.order = append(v.order, token)
	return token, nil
}

func (v *viewServer) dropLocked(token string) {
	if g := v.grants[token]; g != nil && g.cancel != nil {
		g.cancel()
	}
	delete(v.grants, token)
	for i, t := range v.order {
		if t == token {
			v.order = append(v.order[:i], v.order[i+1:]...)
			break
		}
	}
}

// clear drops every grant (the workspace closed or switched), cancelling their queries.
func (v *viewServer) clear() {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for t := range v.grants {
		v.dropLocked(t)
	}
}

// lookup returns the grant for a token that is still allowed; a grant that no
// longer is gets dropped.
func (v *viewServer) lookup(token string) *viewGrant {
	v.mu.Lock()
	g := v.grants[token]
	v.mu.Unlock()
	if g == nil {
		return nil
	}
	if g.page != nil && v.verdictTTL > 0 {
		v.mu.Lock()
		fresh := time.Now().Before(g.okUntil)
		v.mu.Unlock()
		if fresh {
			return g
		}
	}
	if v.stillAllowed != nil && !v.stillAllowed(g) {
		v.mu.Lock()
		v.dropLocked(token)
		v.mu.Unlock()
		return nil
	}
	if g.page != nil {
		v.mu.Lock()
		g.okUntil = time.Now().Add(v.verdictTTL)
		v.mu.Unlock()
	}
	return g
}

// dropArtifact drops every token of the named artifact (its decision changed).
func (v *viewServer) dropArtifact(name string) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for t, g := range v.grants {
		if g.name == name {
			v.dropLocked(t)
		}
	}
}

// Middleware is the AssetServer middleware: /assay-view/ is answered here, every
// other path goes on to the asset handler untouched.
func (v *viewServer) Middleware() assetserver.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p := r.URL.Path; p == viewPrefix || strings.HasPrefix(p, viewPrefix+"/") {
				v.serve(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// denyCSP is the policy of every response that is not a token's script: nothing
// may load, and no script is allowed.
const denyCSP = "default-src 'none'; script-src 'none'; connect-src 'none'; worker-src 'none'; img-src 'none'; base-uri 'none'; form-action 'none'"

// scriptCSP is the policy a view's Worker runs under, set on its worker.js
// response (the spike showed the worker's own response CSP is what holds it).
// Only that token's view.js and the pinned libraries may be loaded, with no
// network, nested workers, images or eval.
func scriptCSP(origin, token string) string {
	return "default-src 'none'; script-src " + origin + "/assay-view/" + token + "/view.js " + origin + viewLibDir +
		"; connect-src 'none'; worker-src 'none'; img-src 'none'; base-uri 'none'; form-action 'none'"
}

func (v *viewServer) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	// Wails injects its runtime into a path that ends in "/" or "/index.html",
	// and swaps a 404 there for its own page: answer these 400, never 404.
	if strings.HasSuffix(p, "/") || strings.HasSuffix(p, "/index.html") ||
		strings.ContainsAny(p, "\\\x00") || strings.Contains(p, "//") || path.Clean(p) != p {
		reply(w, r, http.StatusBadRequest, pageDenyCSP, "", nil)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		reply(w, r, http.StatusMethodNotAllowed, pageDenyCSP, "", nil)
		return
	}
	parts := strings.Split(strings.TrimPrefix(p, viewPrefix+"/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "lib":
		v.serveLib(w, r, parts[1])
	case len(parts) == 2 && viewTokenRe.MatchString(parts[0]) && (parts[1] == "worker.js" || parts[1] == "view.js"):
		v.serveToken(w, r, parts[0], parts[1])
	case len(parts) == 2 && viewTokenRe.MatchString(parts[0]) && parts[1] == "page":
		v.servePage(w, r, parts[0])
	case len(parts) >= 3 && viewTokenRe.MatchString(parts[0]) && parts[1] == "assets":
		v.serveAsset(w, r, parts[0], strings.Join(parts[1:], "/"))
	default:
		reply(w, r, http.StatusNotFound, pageDenyCSP, "", nil)
	}
}

func (v *viewServer) serveLib(w http.ResponseWriter, r *http.Request, file string) {
	if !viewLibRe.MatchString(file) || v.libs == nil {
		reply(w, r, http.StatusNotFound, denyCSP, "", nil)
		return
	}
	data, err := fs.ReadFile(v.libs, file)
	if err != nil {
		reply(w, r, http.StatusNotFound, denyCSP, "", nil)
		return
	}
	reply(w, r, http.StatusOK, denyCSP, "text/javascript; charset=utf-8", data)
}

func (v *viewServer) serveToken(w http.ResponseWriter, r *http.Request, token, file string) {
	g := v.lookup(token)
	if g == nil || g.page != nil {
		reply(w, r, http.StatusNotFound, denyCSP, "", nil)
		return
	}
	body := g.script
	if file == "worker.js" {
		body = v.worker
	}
	reply(w, r, http.StatusOK, scriptCSP(g.origin, token), "text/javascript; charset=utf-8", body)
}

// reply writes one response with the headers every /assay-view/ response has.
func reply(w http.ResponseWriter, r *http.Request, status int, csp, contentType string, body []byte) {
	h := w.Header()
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
