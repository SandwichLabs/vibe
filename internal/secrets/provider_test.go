package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	gotSubject string
	cred       Credential
	err        error
	status     MethodStatus
}

func (f *fakeProvider) Resolve(_ context.Context, subject string) (Credential, error) {
	f.gotSubject = subject
	return f.cred, f.err
}
func (f *fakeProvider) Status(_ context.Context, subject string) MethodStatus {
	f.gotSubject = subject
	return f.status
}

func TestParseMethodAWSSSO(t *testing.T) {
	kind, subject, err := ParseMethod("aws-sso:dev")
	require.NoError(t, err)
	assert.Equal(t, "aws-sso", kind)
	assert.Equal(t, "dev", subject)
	for _, bad := range []string{"aws-sso:", "aws-sso", "AWS-SSO:dev", "aws-profile:dev"} {
		_, _, err := ParseMethod(bad)
		assert.Error(t, err, bad)
	}
}

func TestResolveRoutesAWSSSOToTheProviderWithTheProfileOnly(t *testing.T) {
	const keyNeedle, tokNeedle = "NEEDLE_SSO_SECRET_KEY", "NEEDLE_SSO_SESSION_TOKEN"
	exp := time.Now().Add(time.Hour)
	fp := &fakeProvider{cred: Credential{KeyID: "ASIA", Secret: keyNeedle, Token: tokNeedle, Expires: exp}}
	r := NewResolver(WithKeychain(NewMemoryStore()), WithMethodProvider(MethodAWSSSO, fp))

	// The credential has no stored fingerprint, and the binding is not compared.
	got, err := r.Resolve(context.Background(), Ref{Scope: ScopeWorkspace, Name: "lake"}, "aws-sso:dev", Binding{Fingerprint: "anything", Type: "s3"})
	require.NoError(t, err)
	assert.Equal(t, "dev", fp.gotSubject, "the provider gets the profile and nothing else")
	assert.Equal(t, "ASIA", got.KeyID)
	assert.True(t, got.Expires.Equal(exp))

	// It is scrubbed like every other resolved credential.
	red := r.Redactor().Redact("boom " + keyNeedle + " and " + tokNeedle)
	assert.NotContains(t, red, keyNeedle)
	assert.NotContains(t, red, tokNeedle)

	// Expires is never serialised.
	raw, err := got.Marshal()
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "xpires")
	b, _ := json.Marshal(got)
	assert.NotContains(t, string(b), "xpires")
}

func TestResolveAWSSSONeedsSignInWordsTheError(t *testing.T) {
	for reason, want := range map[string]string{
		"expired": "AWS session for profile dev has expired. Sign in, or run `aws sso login --profile dev`.",
		"missing": "No AWS session for profile dev. Sign in, or run `aws sso login --profile dev`.",
	} {
		fp := &fakeProvider{err: &ErrNeedsCredentials{SignIn: &SignInHint{Kind: MethodAWSSSO, Subject: "dev", Reason: reason}}}
		r := NewResolver(WithKeychain(NewMemoryStore()), WithMethodProvider(MethodAWSSSO, fp))
		_, err := r.Resolve(context.Background(), Ref{Name: "lake"}, "aws-sso:dev", Binding{Type: "s3"})
		var nc *ErrNeedsCredentials
		require.ErrorAs(t, err, &nc)
		assert.Equal(t, want, err.Error(), "no X_USER / X_PASSWORD advice for an SSO sign-in")
		assert.Equal(t, "lake", nc.Ref.Name)
		assert.Equal(t, "s3", nc.Type)
		assert.Equal(t, "aws-sso:dev", nc.Method)
	}
}

func TestAWSSSOIsKnownOnlyWhereARegisteredProviderExists(t *testing.T) {
	r := NewResolver(WithKeychain(NewMemoryStore())) // no provider
	_, err := r.Resolve(context.Background(), Ref{Name: "x"}, "aws-sso:dev", Binding{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available in this process")
	st := r.Status(context.Background(), Ref{Name: "x"}, "aws-sso:dev", Binding{})
	assert.False(t, st.Available)
}

func TestStoredSecretOperationsRefuseAWSSSO(t *testing.T) {
	r := NewResolver(WithKeychain(NewMemoryStore()), WithMethodProvider(MethodAWSSSO, &fakeProvider{}))
	ctx := context.Background()
	ref := Ref{Name: "lake"}
	err := r.SetCredentials(ctx, ref, "aws-sso:dev", Credential{KeyID: "k", Secret: "s"}, Binding{}, false)
	require.ErrorIs(t, err, ErrReadOnly)
	assert.Contains(t, err.Error(), "sign in with your profile")
	err = r.ClearCredentialField(ctx, ref, "aws-sso:dev", FieldSecret)
	require.ErrorIs(t, err, ErrReadOnly)
}

func TestWithEnvLayerKeepsTheMethodProvider(t *testing.T) {
	fp := &fakeProvider{cred: Credential{KeyID: "ASIA", Secret: "s"}}
	base := NewResolver(WithKeychain(NewMemoryStore()), WithMethodProvider(MethodAWSSSO, fp))
	derived := base.WithEnvLayer(func(string) (string, bool) { return "", false }, nil)
	_, err := derived.Resolve(context.Background(), Ref{Name: "x"}, "aws-sso:dev", Binding{})
	require.NoError(t, err)
	assert.Equal(t, "dev", fp.gotSubject)
}

func TestAWSSSOStatusCarriesNoKey(t *testing.T) {
	exp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	fp := &fakeProvider{status: MethodStatus{SignedIn: true, Expires: exp, Detail: "Signed in with AWS profile dev."}, cred: Credential{Secret: "NEEDLE_NEVER"}}
	r := NewResolver(WithKeychain(NewMemoryStore()), WithMethodProvider(MethodAWSSSO, fp))
	st := r.Status(context.Background(), Ref{Name: "x"}, "aws-sso:dev", Binding{})
	assert.Equal(t, "dev", st.Profile)
	assert.True(t, st.SignedIn)
	require.NotNil(t, st.ExpiresAt)
	assert.True(t, st.Available)
	assert.False(t, st.SecretSet)
	b, err := json.Marshal(st)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "NEEDLE_NEVER")
	assert.Contains(t, string(b), `"profile":"dev"`)
}

func TestMethodFailedKeepsItsMessageAndCause(t *testing.T) {
	cause := errors.New("underlying")
	err := error(&ErrMethodFailed{Kind: MethodAWSSSO, Msg: "couldn't reach AWS sign-in", Err: cause})
	assert.Equal(t, "couldn't reach AWS sign-in", err.Error())
	assert.ErrorIs(t, err, cause)
}

type checker struct{ fakeProvider }

func (checker) CheckTarget(_ string, t Target) error {
	if t.Plaintext {
		return errors.New("no plaintext")
	}
	return nil
}

func TestCheckTargetRoutesToAProviderThatChecks(t *testing.T) {
	r := NewResolver(WithKeychain(NewMemoryStore()), WithMethodProvider(MethodAWSSSO, &checker{}))
	require.Error(t, r.CheckTarget("aws-sso:dev", Target{Plaintext: true}))
	require.NoError(t, r.CheckTarget("aws-sso:dev", Target{}))
	require.NoError(t, r.CheckTarget("keychain", Target{Plaintext: true}), "other methods have no checker")
	require.Error(t, r.CheckTarget("bogus", Target{}))
}
