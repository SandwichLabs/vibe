package secrets

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// MethodAWSSSO is the sign-in method kind of "aws-sso:PROFILE": short-lived
// role credentials from a person's AWS IAM Identity Center profile. Nothing is
// stored; a MethodProvider resolves them when asked.
const MethodAWSSSO = "aws-sso"

// MethodAWSSSOPref is the prefix of the method string.
const MethodAWSSSOPref = MethodAWSSSO + ":"

// SignInHint says, in typed non-secret form, that a person must sign in with an
// external identity (an AWS SSO profile) before a credential exists. It carries
// no account id, role name or start URL.
type SignInHint struct {
	// Kind is the method kind ("aws-sso").
	Kind string `json:"kind"`
	// Subject is what the person signs in to: the profile name.
	Subject string `json:"subject"`
	// Reason is "missing" (no session) or "expired" (the session ended or was
	// refused).
	Reason string `json:"reason"`
}

// ErrMethodFailed is a sign-in method's provider failing for a reason that is
// not "sign in": a profile that cannot be used, an AWS answer that is an error,
// no network. The message is already safe to show (it names the profile and the
// problem, never a secret or a file's contents); Err is the cause.
type ErrMethodFailed struct {
	Kind string
	Msg  string
	Err  error
}

func (e *ErrMethodFailed) Error() string { return e.Msg }

// Unwrap returns the cause.
func (e *ErrMethodFailed) Unwrap() error { return e.Err }

// MethodProvider resolves credentials for a sign-in method whose secrets are
// not stored (an AWS SSO profile). secrets knows nothing about the provider's
// service: it only routes the method kind to it. Implementations return
// *ErrNeedsCredentials (with SignIn set) when the person has to sign in.
type MethodProvider interface {
	// Resolve returns the credential for subject (the part after "kind:").
	Resolve(ctx context.Context, subject string) (Credential, error)
	// Status describes the session for subject without any network call and
	// without a secret.
	Status(ctx context.Context, subject string) MethodStatus
}

// MethodStatus is what a MethodProvider reports about a sign-in. It never
// carries a secret.
type MethodStatus struct {
	// SignedIn: a session exists that can produce credentials.
	SignedIn bool
	// Expires is when the session ends (zero when unknown or absent).
	Expires time.Time
	// Detail is a plain sentence for the person.
	Detail string
}

// WithMethodProvider registers the provider for a sign-in method kind. The
// provider is kept by WithEnvLayer copies.
func WithMethodProvider(kind string, p MethodProvider) ResolverOption {
	return func(r *Resolver) {
		if r.providers == nil {
			r.providers = map[string]MethodProvider{}
		}
		r.providers[kind] = p
	}
}

// StoresNoSecret reports whether a sign-in method kind keeps no secret of its
// own: the environment (read-only) and an AWS SSO profile (resolved on demand).
// Setting or clearing a stored credential under it is refused.
func StoresNoSecret(kind string) bool { return kind == "env" || kind == MethodAWSSSO }

// provider returns the provider for kind, or an error naming the method when
// this process has none (a resolver built without it).
func (r *Resolver) provider(kind string) (MethodProvider, error) {
	if p, ok := r.providers[kind]; ok && p != nil {
		return p, nil
	}
	return nil, fmt.Errorf("the %s sign-in method is not available in this process", kind)
}

// resolveProvided asks the method's provider. An ErrNeedsCredentials without a
// connection yet gets this one's name, so its message and the UI agree.
func (r *Resolver) resolveProvided(ctx context.Context, kind, subject, method string, ref Ref, typ string) (Credential, error) {
	p, err := r.provider(kind)
	if err != nil {
		return Credential{}, err
	}
	cred, err := p.Resolve(ctx, subject)
	if err != nil {
		var nc *ErrNeedsCredentials
		if errors.As(err, &nc) {
			nc.Method, nc.Ref, nc.Type = method, ref, typ
		}
		return Credential{}, err
	}
	r.redactor.AddCredential(cred)
	return cred, nil
}

// providedStatus is Status for a method resolved by a provider.
func (r *Resolver) providedStatus(ctx context.Context, kind, subject, method string) CredentialStatus {
	st := CredentialStatus{Method: method, Available: true, FingerprintMatches: true, Profile: subject}
	p, err := r.provider(kind)
	if err != nil {
		st.Available = false
		st.Detail = err.Error()
		return st
	}
	ms := p.Status(ctx, subject)
	st.SignedIn, st.Detail = ms.SignedIn, ms.Detail
	if !ms.Expires.IsZero() {
		exp := ms.Expires
		st.ExpiresAt = &exp
	}
	return st
}

// Target describes where a connection would send the credential a method
// resolves: enough for a provider to refuse a destination its identity must
// never reach. Values are the connection's resolved, non-secret definition.
type Target struct {
	// Type is the connection type ("s3").
	Type string
	// Endpoint is the custom endpoint host ("" for the provider's own service).
	Endpoint string
	// Plaintext is true when the connection turns TLS off.
	Plaintext bool
}

// TargetChecker is an optional extension of MethodProvider: it refuses a
// Target that the method's identity must not be sent to (AWS credentials go
// only to AWS, over TLS).
type TargetChecker interface {
	CheckTarget(subject string, t Target) error
}

// CheckTarget asks the provider of method's kind whether t is an acceptable
// destination. Methods with no provider or no checker accept everything.
func (r *Resolver) CheckTarget(method string, t Target) error {
	kind, subject, err := ParseMethod(method)
	if err != nil {
		return err
	}
	if kind == MethodAWSSSO {
		// Fail closed: a resolver with no provider could not resolve the method
		// later either, and must not wave a destination through now.
		if _, err := r.provider(kind); err != nil {
			return err
		}
	}
	if c, ok := r.providers[kind].(TargetChecker); ok {
		return c.CheckTarget(subject, t)
	}
	return nil
}

// DevicePrompt is what a person is shown to finish a device sign-in: open URL
// and check that the page shows Code. Neither is a secret. URL has already
// passed the provider's host check.
type DevicePrompt struct {
	URL       string
	Code      string
	ExpiresAt time.Time
}

// DeviceSigner is an optional extension of MethodProvider: it runs the
// provider's interactive sign-in (the OIDC device flow). It never opens a
// browser itself; show is called once with what to display and must not block.
type DeviceSigner interface {
	DeviceLogin(ctx context.Context, subject string, show func(DevicePrompt)) error
}

// DeviceSignIn runs the device sign-in of method's provider and blocks until
// the person approves, it is denied or expires, or ctx ends. Only an explicit
// person action calls it (a Studio binding, one CLI command); resolving a
// credential never does.
func (r *Resolver) DeviceSignIn(ctx context.Context, method string, show func(DevicePrompt)) error {
	kind, subject, err := ParseMethod(method)
	if err != nil {
		return err
	}
	p, err := r.provider(kind)
	if err != nil {
		return err
	}
	ds, ok := p.(DeviceSigner)
	if !ok {
		return fmt.Errorf("the %s sign-in method cannot sign in from Assay", kind)
	}
	return ds.DeviceLogin(ctx, subject, show)
}
