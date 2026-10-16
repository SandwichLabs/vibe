// Package ssofake is a loopback IAM Identity Center for tests and for the
// ssofake dev build (tools/ssofake): the OIDC device-authorization endpoints and
// the portal that turns an access token into role credentials. Nothing here
// talks to AWS. The values it hands out are needles (Needle*), so a test can
// assert none of them reaches a log, an event or a file it should not.
package ssofake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Needles: the secret values the fake issues. None may appear in an event, a
// status, an error, a log line or a workspace file.
const (
	NeedleAccess       = "NEEDLE_FAKE_ACCESS_TOKEN"
	NeedleRefresh      = "NEEDLE_FAKE_REFRESH_TOKEN"
	NeedleClientSecret = "NEEDLE_FAKE_CLIENT_SECRET"
	NeedleSecretKey    = "NEEDLE_FAKE_ROLE_SECRET_KEY"
	NeedleSessionToken = "NEEDLE_FAKE_ROLE_SESSION_TOKEN"
	// UserCode is the code every device authorization shows.
	UserCode = "ABCD-EFGH"
)

// Creds are the role credentials the portal hands out.
type Creds struct {
	KeyID, Secret, Token string
	Expires              time.Time
}

// Fake is the server. It is an http.Handler: wrap it in httptest.NewServer, or
// serve it on a port. Safe for concurrent use.
type Fake struct {
	mu sync.Mutex

	creds      func() Creds
	credLife   time.Duration
	verifyURL  string
	control    bool
	tokenSeq   int
	codeSeq    int
	pending    map[string]bool // device code -> approved
	denied     map[string]bool
	valid      map[string]bool
	refuseRfsh bool
	portalHits int
	registered int
}

// Option configures a Fake.
type Option func(*Fake)

// WithCredentials sets how role credentials are minted (a MinIO STS call, say).
// The default is fixed needles.
func WithCredentials(f func() Creds) Option { return func(s *Fake) { s.creds = f } }

// WithCredLife sets how long the credentials the default minter issues, and the
// expiry the portal reports for them, last (default one hour).
func WithCredLife(d time.Duration) Option { return func(s *Fake) { s.credLife = d } }

// WithVerificationURL makes the device authorization return this page instead
// of the fake's own /verify (a hostile address, in a test).
func WithVerificationURL(u string) Option { return func(s *Fake) { s.verifyURL = u } }

// WithControl serves /_ctl/approve, /_ctl/deny and /_ctl/expire so a script can
// drive the fake (the dev tool). Tests call the methods instead.
func WithControl() Option { return func(s *Fake) { s.control = true } }

// New returns a Fake with the default credentials.
func New(opts ...Option) *Fake {
	f := &Fake{credLife: time.Hour, pending: map[string]bool{}, denied: map[string]bool{}, valid: map[string]bool{}}
	for _, o := range opts {
		o(f)
	}
	if f.creds == nil {
		f.creds = func() Creds {
			return Creds{KeyID: "ASIAFAKE", Secret: NeedleSecretKey, Token: NeedleSessionToken, Expires: time.Now().Add(f.credLife)}
		}
	}
	return f
}

// Approve approves every device authorization waiting for the person (the
// browser step): the next poll of each gets a token.
func (f *Fake) Approve() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.pending {
		f.pending[c] = true
	}
}

// Deny makes the waiting device authorizations answer access_denied.
func (f *Fake) Deny() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.pending {
		f.denied[c] = true
	}
}

// Expire ends every session issued so far: the portal refuses its tokens and
// the refresh grant is refused, as when the Identity Center session lapsed. A
// new approval signs in again.
func (f *Fake) Expire() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.valid = map[string]bool{}
	f.refuseRfsh = true
}

// Waiting reports how many device authorizations await approval.
func (f *Fake) Waiting() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for c, ok := range f.pending {
		if !ok && !f.denied[c] {
			n++
		}
	}
	return n
}

// PortalHits is how many times the portal was asked for role credentials.
func (f *Fake) PortalHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.portalHits
}

// Registrations is how many OIDC clients were registered (one per flow).
func (f *Fake) Registrations() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registered
}

func write(w http.ResponseWriter, code int, errType string, v any) {
	w.Header().Set("Content-Type", "application/json")
	if errType != "" {
		w.Header().Set("X-Amzn-Errortype", errType)
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ServeHTTP implements http.Handler.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	if b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20)); len(b) > 0 {
		_ = json.Unmarshal(b, &body)
	}
	switch {
	case r.URL.Path == "/client/register":
		f.registered++
		write(w, 200, "", map[string]any{"clientId": fmt.Sprintf("cid-%d", f.registered), "clientSecret": NeedleClientSecret,
			"clientSecretExpiresAt": time.Now().Add(90 * 24 * time.Hour).Unix(), "clientIdIssuedAt": time.Now().Unix()})
	case r.URL.Path == "/device_authorization":
		f.deviceAuthorization(w, r)
	case r.URL.Path == "/token":
		f.token(w, body)
	case r.URL.Path == "/federation/credentials":
		f.portal(w, r)
	case r.URL.Path == "/verify":
		_, _ = w.Write([]byte("fake IAM Identity Center: approve the sign-in with /_ctl/approve"))
	case f.control && strings.HasPrefix(r.URL.Path, "/_ctl/"):
		f.ctl(w, strings.TrimPrefix(r.URL.Path, "/_ctl/"))
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) deviceAuthorization(w http.ResponseWriter, r *http.Request) {
	f.codeSeq++
	code := fmt.Sprintf("dev-%d", f.codeSeq)
	f.pending[code] = false
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	v := f.verifyURL
	if v == "" {
		v = fmt.Sprintf("%s://%s/verify?user_code=%s", scheme, r.Host, UserCode)
	}
	write(w, 200, "", map[string]any{"deviceCode": code, "userCode": UserCode, "verificationUri": strings.SplitN(v, "?", 2)[0],
		"verificationUriComplete": v, "interval": 1, "expiresIn": 600})
}

func (f *Fake) token(w http.ResponseWriter, body map[string]any) {
	grant, _ := body["grantType"].(string)
	if grant == "refresh_token" {
		if f.refuseRfsh {
			write(w, 400, "InvalidGrantException", map[string]any{"error": "invalid_grant"})
			return
		}
		f.tokenSeq++
		at := fmt.Sprintf("%s_%d", NeedleAccess, f.tokenSeq)
		f.valid[at] = true
		write(w, 200, "", map[string]any{"accessToken": at, "expiresIn": 3600, "refreshToken": NeedleRefresh, "tokenType": "Bearer"})
		return
	}
	code, _ := body["deviceCode"].(string)
	approved, known := f.pending[code]
	switch {
	case !known:
		write(w, 400, "InvalidGrantException", map[string]any{"error": "invalid_grant"})
	case f.denied[code]:
		write(w, 400, "AccessDeniedException", map[string]any{"error": "access_denied"})
	case !approved:
		write(w, 400, "AuthorizationPendingException", map[string]any{"error": "authorization_pending"})
	default:
		delete(f.pending, code)
		f.tokenSeq++
		at := fmt.Sprintf("%s_%d", NeedleAccess, f.tokenSeq)
		f.valid[at] = true
		f.refuseRfsh = false
		write(w, 200, "", map[string]any{"accessToken": at, "expiresIn": 3600, "refreshToken": NeedleRefresh, "tokenType": "Bearer"})
	}
}

func (f *Fake) portal(w http.ResponseWriter, r *http.Request) {
	f.portalHits++
	if !f.valid[r.Header.Get("x-amz-sso_bearer_token")] {
		write(w, 401, "UnauthorizedException", map[string]any{"message": "session expired"})
		return
	}
	c := f.creds()
	write(w, 200, "", map[string]any{"roleCredentials": map[string]any{"accessKeyId": c.KeyID, "secretAccessKey": c.Secret,
		"sessionToken": c.Token, "expiration": c.Expires.UnixMilli()}})
}

func (f *Fake) ctl(w http.ResponseWriter, what string) {
	switch what {
	case "approve":
		for c := range f.pending {
			f.pending[c] = true
		}
	case "deny":
		for c := range f.pending {
			f.denied[c] = true
		}
	case "expire":
		f.valid = map[string]bool{}
		f.refuseRfsh = true
	case "state":
		n := 0
		for c, ok := range f.pending {
			if !ok && !f.denied[c] {
				n++
			}
		}
		write(w, 200, "", map[string]any{"waiting": n, "portalHits": f.portalHits, "registrations": f.registered, "validTokens": len(f.valid)})
		return
	default:
		write(w, 404, "", map[string]any{"error": "unknown control"})
		return
	}
	write(w, 200, "", map[string]any{"ok": true})
}
