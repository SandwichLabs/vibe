package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/SandwichLabs/slaw/internal/awssso"
	"github.com/SandwichLabs/slaw/internal/connections"
	"github.com/SandwichLabs/slaw/internal/secrets"
)

// awsEndpointRE is the only custom endpoint an AWS SSO identity may be sent to:
// an S3-shaped lower-case host under amazonaws.com (regional, dash-region,
// FIPS, dual-stack and VPC-interface S3 endpoints), with no port, colon or
// scheme. Other AWS services run customer code behind AWS's own certificates
// (execute-api, appsync-api, EC2, ELB, RDS, s3-object-lambda), so "any host
// under amazonaws.com" would hand a signed request to a stranger. endptRE
// allows a port, so a suffix check would also let "evil.example:443.amazonaws.com"
// through.
var awsEndpointRE = regexp.MustCompile(`^(?:[a-z0-9-]+\.)*s3(?:-fips)?(?:[.-]dualstack)?(?:[.-][a-z0-9-]+)?(?:\.vpce)?\.amazonaws\.com(?:\.cn)?$`)

// resolverConfig collects what NewResolver builds the resolver from.
type resolverConfig struct {
	secrets []secrets.ResolverOption
	sso     []awssso.Option
	// anyEndpoint lifts the AWS-endpoint rule: set only with test endpoints.
	anyEndpoint bool
}

// ResolverOption configures NewResolver.
type ResolverOption func(*resolverConfig)

// WithSecretsOptions passes options to the underlying secrets.NewResolver
// (keychain, redactor, logger, environment).
func WithSecretsOptions(o ...secrets.ResolverOption) ResolverOption {
	return func(c *resolverConfig) { c.secrets = append(c.secrets, o...) }
}

// WithSSOOptions passes options (clock, HTTP client, timeouts) to the AWS SSO
// provider. They never lift the endpoint rule.
func WithSSOOptions(o ...awssso.Option) ResolverOption {
	return func(c *resolverConfig) { c.sso = append(c.sso, o...) }
}

// WithSSOTestEndpoints points the AWS SSO provider at loopback fakes and lifts
// the AWS-endpoint rule, so a test can sign in to a local S3 (MinIO). It is a
// Go option for tests; no config file, flag or environment variable reaches it.
func WithSSOTestEndpoints(oidc, portal string) ResolverOption {
	return func(c *resolverConfig) {
		c.sso = append(c.sso, awssso.WithTestEndpoints(oidc, portal))
		c.anyEndpoint = true
	}
}

// NewResolver is the one constructor of the credential resolver: keychain,
// process memory, the environment as it was at start (SnapshotEnv), and the
// aws-sso sign-in method. The CLI, Studio and workspace.Open all build theirs
// here, so no process knows fewer sign-in methods than another.
func NewResolver(opts ...ResolverOption) *secrets.Resolver {
	var c resolverConfig
	for _, o := range opts {
		o(&c)
	}
	env := SnapshotEnv()
	home, herr := os.UserHomeDir()
	var m secrets.MethodProvider = ssoMethod{anyEndpoint: c.anyEndpoint, homeErr: herr}
	if herr == nil {
		m = ssoMethod{p: awssso.New(home, env, c.sso...), anyEndpoint: c.anyEndpoint}
	}
	base := []secrets.ResolverOption{secrets.WithEnv(env), secrets.WithMethodProvider(secrets.MethodAWSSSO, m)}
	return secrets.NewResolver(append(base, c.secrets...)...)
}

// ssoMethod adapts awssso to secrets.MethodProvider. It lives here, not in
// secrets, so secrets imports no AWS code.
type ssoMethod struct {
	p           *awssso.Provider
	anyEndpoint bool
	homeErr     error
}

var (
	_ secrets.MethodProvider = ssoMethod{}
	_ secrets.TargetChecker  = ssoMethod{}
)

func (m ssoMethod) unavailable() error {
	if m.p != nil {
		return nil
	}
	return errors.New("the AWS SSO sign-in is not available: the home folder could not be found")
}

// Resolve returns the role credentials of the profile, from the provider's
// in-memory cache until five minutes before they expire, otherwise through the
// token cache and the portal. It never starts a sign-in: a missing or ended
// session is *secrets.ErrNeedsCredentials with a SignIn hint.
func (m ssoMethod) Resolve(ctx context.Context, profile string) (secrets.Credential, error) {
	if err := m.unavailable(); err != nil {
		return secrets.Credential{}, err
	}
	var opts []awssso.CredOption
	if secrets.IsFresh(ctx) {
		// A rotation asked for new keys: not the cached set it is replacing.
		opts = append(opts, awssso.Bypass())
	}
	c, err := m.p.Credentials(ctx, profile, opts...)
	if err != nil {
		return secrets.Credential{}, needsSignIn(profile, err)
	}
	return secrets.Credential{KeyID: c.KeyID, Secret: c.Secret, Token: c.Token, Expires: c.Expires}, nil
}

// needsSignIn maps a "sign in needed" answer to the typed error with its hint
// (a refused session reads as expired); every other error passes through.
func needsSignIn(profile string, err error) error {
	var need *awssso.SignInNeeded
	if !errors.As(err, &need) {
		// Every other awssso error is worded for the person already.
		return &secrets.ErrMethodFailed{Kind: secrets.MethodAWSSSO, Msg: err.Error(), Err: err}
	}
	reason := "expired"
	if need.Reason == awssso.ReasonMissing {
		reason = "missing"
	}
	return &secrets.ErrNeedsCredentials{
		Method: secrets.MethodAWSSSOPref + profile,
		Reason: "sign-in needed",
		SignIn: &secrets.SignInHint{Kind: secrets.MethodAWSSSO, Subject: profile, Reason: reason},
	}
}

// Status reads the token cache; it makes no network call.
func (m ssoMethod) Status(ctx context.Context, profile string) secrets.MethodStatus {
	if err := m.unavailable(); err != nil {
		return secrets.MethodStatus{Detail: err.Error()}
	}
	prof, err := m.p.LoadProfile(ctx, profile)
	if err != nil {
		return secrets.MethodStatus{Detail: err.Error()}
	}
	st := m.p.Session(prof)
	if st.SignedIn {
		return secrets.MethodStatus{SignedIn: true, Expires: st.Expires, Detail: "Signed in with AWS profile " + profile + "."}
	}
	reason := "expired"
	if st.Reason == awssso.ReasonMissing {
		reason = "missing"
	}
	hint := &secrets.ErrNeedsCredentials{SignIn: &secrets.SignInHint{Kind: secrets.MethodAWSSSO, Subject: profile, Reason: reason}}
	return secrets.MethodStatus{Expires: st.Expires, Detail: hint.Error()}
}

// CheckTarget refuses a connection an AWS identity must not be sent to: not an
// S3 connection, TLS off, or an endpoint that is not an amazonaws.com host.
func (m ssoMethod) CheckTarget(_ string, t secrets.Target) error {
	if m.anyEndpoint {
		return nil
	}
	switch {
	case t.Type != "s3":
		return errors.New("an AWS SSO sign-in is for S3 connections")
	case t.Plaintext:
		return errors.New("an AWS SSO sign-in is never used with use_ssl=false: AWS credentials go only to AWS over TLS")
	case strings.Contains(t.Endpoint, "${"):
		return errors.New("an AWS SSO sign-in cannot use an endpoint written as a ${VARIABLE}: write the S3 endpoint host itself, or leave it empty")
	case t.Endpoint != "" && !awsEndpointRE.MatchString(t.Endpoint):
		return errors.New("an AWS SSO sign-in is only for AWS S3 endpoints: leave the endpoint empty, or use an S3 host such as s3.us-west-2.amazonaws.com")
	}
	return nil
}

// boundSignIn reports whether a method has no stored value of its own, so the
// binding to its definition lives with the person's choice (SignInFingerprint):
// the environment and an AWS SSO profile.
func boundSignIn(method string) bool {
	kind, _, err := secrets.ParseMethod(method)
	return err == nil && secrets.StoresNoSecret(kind)
}

// targetOf is the non-secret destination of a resolved definition.
func targetOf(d ConnectionDef) secrets.Target {
	ssl := strings.TrimSpace(d.Options["use_ssl"])
	return secrets.Target{Type: d.Type, Endpoint: d.Options["endpoint"], Plaintext: ssl != "" && ssl != "true"}
}

// CheckSignIn is every static check of choosing method for the (environment
// resolved) definition d: the method is well formed, the type takes it
// (TypeInfo.SignInMethods), and the method's identity may go to d's endpoint.
// SetSignIn runs it, and so does `slaw conn add` before it saves anything.
func CheckSignIn(res *secrets.Resolver, d ConnectionDef, method string) error {
	if err := ValidateSignIn(method); err != nil {
		return err
	}
	kind, _, err := secrets.ParseMethod(method)
	if err != nil {
		return err
	}
	info, err := connections.TypeInfoFor(d.Type)
	if err != nil {
		return err
	}
	if !signInKindAllowed(info.SignInMethods, kind) {
		if len(info.SignInMethods) == 0 {
			return fmt.Errorf("%s connections take no credentials, so they have no sign-in method", d.Type)
		}
		return fmt.Errorf("%s connections cannot sign in with %q (they take %s)", d.Type, kind, strings.Join(info.SignInMethods, ", "))
	}
	if err := res.CheckTarget(method, targetOf(d)); err != nil {
		return fmt.Errorf("connection %q: %w", d.Name, err)
	}
	return nil
}

func signInKindAllowed(allowed []string, kind string) bool {
	for _, a := range allowed {
		if a == kind {
			return true
		}
	}
	return false
}

var _ secrets.DeviceSigner = ssoMethod{}

// DeviceLogin runs the OIDC device flow for the profile (awssso.DeviceLogin) and
// writes the shared token cache. It opens no browser: show gets a URL that has
// passed the host check, and the caller decides how to present it.
func (m ssoMethod) DeviceLogin(ctx context.Context, profile string, show func(secrets.DevicePrompt)) error {
	if err := m.unavailable(); err != nil {
		return err
	}
	return m.p.DeviceLogin(ctx, profile, func(p awssso.Prompt) {
		show(secrets.DevicePrompt{URL: p.URL, Code: p.Code, ExpiresAt: p.ExpiresAt})
	})
}
