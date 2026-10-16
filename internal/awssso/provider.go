package awssso

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	ssotypes "github.com/aws/aws-sdk-go-v2/service/sso/types"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	oidctypes "github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
	"github.com/aws/smithy-go"
)

const (
	callTimeout   = 5 * time.Second
	expirySkew    = 5 * time.Minute
	negativeCache = 30 * time.Second
)

// Provider resolves SSO role credentials and runs the device flow. It holds
// the per-profile caches; make one per process and share it. Safe for
// concurrent use.
type Provider struct {
	home   string
	getenv func(string) string

	// test seams, set by options only
	oidcEndpoint, portalEndpoint string
	httpClient                   *http.Client
	now                          func() time.Time
	sleep                        func(context.Context, time.Duration) error
	timeout                      time.Duration
	tmpSuffix                    func() string

	// optErr is a misconfigured option (a non-loopback test endpoint); every
	// call that could reach the network returns it first.
	optErr error

	mu      sync.Mutex
	entries map[string]*entry
}

// entry is one identity's cache. Its lock serialises resolution, so N callers on
// one profile cost one wait when AWS is slow or unreachable.
type entry struct {
	sem      chan struct{} // 1-slot lock that a caller can abandon with its context
	creds    Creds
	have     bool
	negErr   error
	negUntil time.Time
	// negToken identifies the token the remembered error was about; a different
	// token in the cache (an `aws sso login` done elsewhere) voids it.
	negToken string
}

func newEntry() *entry { return &entry{sem: make(chan struct{}, 1)} }

func (e *entry) lock(ctx context.Context) error {
	select {
	case e.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *entry) unlock() { <-e.sem }

// Option configures a Provider.
type Option func(*Provider)

// WithTestEndpoints sends OIDC and portal calls to these base URLs (loopback
// fakes). It is a package option, not an environment variable, and it also lets
// a verification URL on the OIDC fake's host through the host check.
//
// Only loopback endpoints (127.0.0.0/8, ::1, localhost) are accepted; any other
// value makes Credentials and DeviceLogin fail before any network call. Wire it
// only from tests or the planned `ssofake` build tag, never from config or env.
func WithTestEndpoints(oidc, portal string) Option {
	return func(p *Provider) {
		loopbackURL := func(raw string) bool {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return false
			}
			h := u.Hostname()
			if h == "localhost" {
				return true
			}
			ip := net.ParseIP(h)
			return ip != nil && ip.IsLoopback()
		}
		if !loopbackURL(oidc) || !loopbackURL(portal) {
			p.optErr = errors.New("awssso: test endpoints must be loopback URLs")
			return
		}
		p.oidcEndpoint, p.portalEndpoint = oidc, portal
	}
}

// WithHTTPClient sets the HTTP client for both services (tests record or fake).
func WithHTTPClient(c *http.Client) Option { return func(p *Provider) { p.httpClient = c } }

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(p *Provider) { p.now = now } }

// WithSleep replaces the device flow's wait between polls (tests).
func WithSleep(f func(context.Context, time.Duration) error) Option {
	return func(p *Provider) { p.sleep = f }
}

// WithCallTimeout sets the per-call timeout (default 5 s).
func WithCallTimeout(d time.Duration) Option { return func(p *Provider) { p.timeout = d } }

// New returns a Provider. home is the person's home directory (the token cache
// and default config live under it); getenv is the process environment snapshot
// (only AWS_CONFIG_FILE and AWS_SHARED_CREDENTIALS_FILE are read, as the SDK
// does). Never pass a workspace .env overlay as getenv.
func New(home string, getenv func(string) string, opts ...Option) *Provider {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	p := &Provider{home: home, getenv: getenv, now: time.Now, sleep: sleepCtx, timeout: callTimeout,
		tmpSuffix: randomSuffix, entries: map[string]*entry{}}
	for _, o := range opts {
		o(p)
	}
	return p
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// CredOption tunes one Credentials call.
type CredOption func(*credCall)

type credCall struct{ bypass bool }

func (p *Provider) entryFor(prof Profile) *entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := prof.fingerprint()
	e := p.entries[k]
	if e == nil {
		e = newEntry()
		p.entries[k] = e
	}
	return e
}

func (p *Provider) clearCaches(name string) {
	p.mu.Lock()
	var hit []*entry
	for k, e := range p.entries {
		if strings.HasPrefix(k, name+"\x00") {
			hit = append(hit, e)
		}
	}
	p.mu.Unlock()
	for _, e := range hit {
		e.sem <- struct{}{}
		e.have, e.negErr, e.negToken = false, nil, ""
		e.unlock()
	}
}

// Credentials resolves role credentials for the named SSO profile. A cached
// success is reused until five minutes before it expires. An unreachable AWS or
// a portal refusal is remembered for 30 seconds; a missing or expired local
// token is not (it costs no network, and a sign-in made elsewhere must be seen
// at once). Errors: *ProfileError, *SignInNeeded, *ServiceError, ErrUnreachable,
// or the caller's context error.
func (p *Provider) Credentials(ctx context.Context, name string, opts ...CredOption) (Creds, error) {
	var call credCall
	for _, o := range opts {
		o(&call)
	}
	if p.optErr != nil {
		return Creds{}, p.optErr
	}
	prof, err := p.LoadProfile(ctx, name)
	if err != nil {
		return Creds{}, err
	}
	e := p.entryFor(prof)
	if err := e.lock(ctx); err != nil {
		return Creds{}, err
	}
	defer e.unlock()
	if err := ctx.Err(); err != nil {
		return Creds{}, err
	}
	tokenID := p.tokenID(prof)
	if !call.bypass {
		if e.have && p.now().Before(e.creds.Expires.Add(-expirySkew)) {
			return e.creds, nil
		}
		if e.negErr != nil && p.now().Before(e.negUntil) && e.negToken == tokenID {
			return Creds{}, e.negErr
		}
	}
	e.have, e.negErr, e.negToken = false, nil, ""
	creds, err := p.resolve(ctx, prof)
	if err != nil {
		if remembered(err) {
			e.negErr, e.negUntil, e.negToken = err, p.now().Add(negativeCache), tokenID
		}
		return Creds{}, err
	}
	e.creds, e.have = creds, true
	return creds, nil
}

// tokenID names the token currently in the cache (a hash, never the token), so a
// remembered answer about one token is not applied to another.
func (p *Provider) tokenID(prof Profile) string {
	t, ok := readToken(tokenPath(p.home, prof.cacheKey()))
	if !ok {
		return ""
	}
	sum := sha256.Sum256([]byte(t.AccessToken + "\x00" + t.ExpiresAt))
	return hex.EncodeToString(sum[:])
}

// remembered: only answers that cost network time are negatively cached.
func remembered(err error) bool {
	var s *SignInNeeded
	if errors.As(err, &s) {
		return s.Reason == ReasonRefused
	}
	return errors.Is(err, ErrUnreachable) || errors.As(err, new(*ServiceError))
}

func (p *Provider) resolve(ctx context.Context, prof Profile) (Creds, error) {
	st := p.stateOf(prof)
	if !st.SignedIn {
		return Creds{}, &SignInNeeded{Profile: prof.Name, Reason: st.Reason}
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	access, err := p.accessToken(ctx, prof)
	if err != nil {
		return Creds{}, p.classify(ctx, prof, err)
	}
	out, err := p.portalClient(prof.Region).GetRoleCredentials(ctx, &sso.GetRoleCredentialsInput{
		AccessToken: aws.String(access), AccountId: aws.String(prof.AccountID), RoleName: aws.String(prof.RoleName)})
	if err != nil {
		return Creds{}, p.classify(ctx, prof, err)
	}
	rc := out.RoleCredentials
	if rc == nil || aws.ToString(rc.AccessKeyId) == "" || aws.ToString(rc.SecretAccessKey) == "" || aws.ToString(rc.SessionToken) == "" {
		return Creds{}, &ServiceError{Profile: prof.Name, Code: "MalformedResponse"}
	}
	return Creds{KeyID: aws.ToString(rc.AccessKeyId), Secret: aws.ToString(rc.SecretAccessKey),
		Token: aws.ToString(rc.SessionToken), Expires: time.UnixMilli(rc.Expiration).UTC()}, nil
}

// accessToken returns the SSO access token: refreshed through the SDK's token
// provider for an sso-session profile (it rewrites the cache), read as is for a
// legacy one (already checked unexpired by the caller).
func (p *Provider) accessToken(ctx context.Context, prof Profile) (string, error) {
	path := tokenPath(p.home, prof.cacheKey())
	if prof.Kind == KindSession {
		tok, err := ssocreds.NewSSOTokenProvider(p.oidcClient(prof.Region), path).RetrieveBearerToken(ctx)
		return tok.Value, err
	}
	t, ok := readToken(path)
	if !ok {
		return "", &SignInNeeded{Profile: prof.Name, Reason: ReasonMissing}
	}
	return t.AccessToken, nil
}

// classify turns an SDK error into one of this package's, never wrapping the
// SDK's text (it can carry request ids, paths and response bodies).
func (p *Provider) classify(ctx context.Context, prof Profile, err error) error {
	if ce := ctx.Err(); ce != nil && errors.Is(ce, context.Canceled) {
		return ce
	}
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	if errors.As(err, &pathErr) || errors.As(err, &linkErr) {
		return ErrCacheWrite
	}
	var unauth *ssotypes.UnauthorizedException
	var sin *SignInNeeded
	switch {
	case errors.As(err, &unauth):
		return &SignInNeeded{Profile: prof.Name, Reason: ReasonRefused}
	case errors.As(err, &sin):
		return sin
	case isRefreshRefusal(err):
		return &SignInNeeded{Profile: prof.Name, Reason: ReasonExpired}
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		return &ServiceError{Profile: prof.Name, Code: api.ErrorCode()}
	}
	return ErrUnreachable
}

// isRefreshRefusal: the OIDC service answered that the refresh token or client
// is no longer valid.
func isRefreshRefusal(err error) bool {
	var (
		grant   *oidctypes.InvalidGrantException
		expired *oidctypes.ExpiredTokenException
		client  *oidctypes.InvalidClientException
		unauthC *oidctypes.UnauthorizedClientException
		denied  *oidctypes.AccessDeniedException
	)
	return errors.As(err, &grant) || errors.As(err, &expired) || errors.As(err, &client) ||
		errors.As(err, &unauthC) || errors.As(err, &denied)
}

func (p *Provider) portalClient(region string) *sso.Client {
	o := sso.Options{Region: region, Retryer: aws.NopRetryer{}}
	if p.httpClient != nil {
		o.HTTPClient = p.httpClient
	}
	if p.portalEndpoint != "" {
		o.BaseEndpoint = aws.String(p.portalEndpoint)
	}
	return sso.New(o)
}

func (p *Provider) oidcClient(region string) *ssooidc.Client {
	o := ssooidc.Options{Region: region, Retryer: aws.NopRetryer{}}
	if p.httpClient != nil {
		o.HTTPClient = p.httpClient
	}
	if p.oidcEndpoint != "" {
		o.BaseEndpoint = aws.String(p.oidcEndpoint)
	}
	return ssooidc.New(o)
}
