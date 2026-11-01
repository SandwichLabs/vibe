package workspace

import (
	"context"
	"crypto/sha1" //nolint:gosec // the AWS CLI names its token cache by SHA-1
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/secrets"
)

const (
	ssoKeyNeedle     = "NEEDLE_SSO_ROLE_SECRET"
	ssoSessionNeedle = "NEEDLE_SSO_SESSION_TOKEN"
	ssoTokenNeedle   = "NEEDLE_SSO_ACCESS_TOKEN"
	ssoAccount       = "123456789012"
	ssoRole          = "NeedleRoleName"
)

const ssoConfig = `[profile dev]
sso_session = corp
sso_account_id = ` + ssoAccount + `
sso_role_name = ` + ssoRole + `
region = us-east-1

[sso-session corp]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1
`

// fakeSSO is a loopback IAM Identity Center portal plus a fake home folder with
// an SSO profile "dev". Nothing leaves the machine.
type fakeSSO struct {
	home   string
	portal *httptest.Server
	hits   atomic.Int64
	exp    atomic.Int64 // expiry of the credentials the portal hands out, unix ms
}

// newFakeSSO sets HOME to a throwaway folder (the resolver reads it through
// os.UserHomeDir) and, when signedIn, caches a valid SSO token.
func newFakeSSO(t *testing.T, signedIn bool) *fakeSSO {
	t.Helper()
	f := &fakeSSO{home: t.TempDir()}
	f.exp.Store(time.Now().Add(time.Hour).UnixMilli())
	t.Setenv("HOME", f.home)
	for _, k := range []string{"AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
		t.Setenv(k, "")
	}
	require.NoError(t, os.MkdirAll(filepath.Join(f.home, ".aws"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(f.home, ".aws", "config"), []byte(ssoConfig), 0o600))
	f.portal = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.Header.Get("x-amz-sso_bearer_token") != ssoTokenNeedle {
			w.Header().Set("X-Amzn-Errortype", "UnauthorizedException")
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"message":"expired"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"roleCredentials": map[string]any{
			"accessKeyId": "ASIAFAKE", "secretAccessKey": ssoKeyNeedle, "sessionToken": ssoSessionNeedle, "expiration": f.exp.Load()}})
	}))
	t.Cleanup(f.portal.Close)
	if signedIn {
		f.cacheToken(t, time.Now().Add(time.Hour))
	}
	return f
}

func (f *fakeSSO) cacheToken(t *testing.T, expires time.Time) {
	t.Helper()
	sum := sha1.Sum([]byte("corp")) //nolint:gosec // see import
	p := filepath.Join(f.home, ".aws", "sso", "cache", hex.EncodeToString(sum[:])+".json")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
	b, _ := json.Marshal(map[string]string{"startUrl": "https://corp.awsapps.com/start", "region": "us-east-1",
		"accessToken": ssoTokenNeedle, "expiresAt": expires.UTC().Format(time.RFC3339)})
	require.NoError(t, os.WriteFile(p, b, 0o600))
}

// resolver is the one workspace.NewResolver, pointed at the fake portal (which
// also lifts the AWS-endpoint rule), over an in-memory keychain.
func (f *fakeSSO) resolver() *secrets.Resolver {
	return NewResolver(WithSSOTestEndpoints(f.portal.URL, f.portal.URL),
		WithSecretsOptions(secrets.WithKeychain(secrets.NewMemoryStore())))
}

// strictResolver is the production rule: no test endpoints.
func strictResolver() *secrets.Resolver {
	return NewResolver(WithSecretsOptions(secrets.WithKeychain(secrets.NewMemoryStore())))
}

func openWith(t *testing.T, r *secrets.Resolver) Workspace {
	t.Helper()
	res, err := Init(filepath.Join(t.TempDir(), "acme"))
	require.NoError(t, err)
	_ = res.Workspace.Close()
	ws, err := Open(res.Workspace.Root(), WithConfigDir(t.TempDir()), WithUserConfig(UserConfig{Version: ConfigVersion}), WithResolver(r))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func s3Conn(name string, opts map[string]string) ConnectionDef {
	return ConnectionDef{Name: name, Type: "s3", Scope: "s3://lake/raw/", Region: "us-east-1", Options: opts}
}

func TestValidateSignInAWSSSO(t *testing.T) {
	for _, ok := range []string{"aws-sso:dev", "aws-sso:Dev_1.team+x@corp-2", "aws-sso:" + strings.Repeat("a", 64)} {
		assert.NoError(t, ValidateSignIn(ok), ok)
	}
	for _, bad := range []string{"aws-sso:", "aws-sso", "aws-sso:-dev", "aws-sso:a b", "aws-sso:a/b", "aws-sso:../x", "aws-sso:" + strings.Repeat("a", 65), "aws-sso:a\nb", "aws-sso:é"} {
		assert.Error(t, ValidateSignIn(bad), bad)
	}
}

func TestCheckSignInTable(t *testing.T) {
	newFakeSSO(t, false)
	r := strictResolver()
	cases := []struct {
		name string
		def  ConnectionDef
		want string // "" = accepted
	}{
		{"AWS default endpoint", s3Conn("a", nil), ""},
		{"regional endpoint", s3Conn("a", map[string]string{"endpoint": "s3.us-west-2.amazonaws.com"}), ""},
		{"fips and china", s3Conn("a", map[string]string{"endpoint": "s3-fips.us-gov-west-1.amazonaws.com"}), ""},
		{"vpc endpoint", s3Conn("a", map[string]string{"endpoint": "bucket.vpce-0abc.s3.us-east-1.vpce.amazonaws.com"}), ""},
		{"minio", s3Conn("a", map[string]string{"endpoint": "minio:9000"}), "only for AWS S3 endpoints"},
		{"port smuggled before the suffix", s3Conn("a", map[string]string{"endpoint": "evil.example:443.amazonaws.com"}), "only for AWS S3 endpoints"},
		{"upper case", s3Conn("a", map[string]string{"endpoint": "S3.US-EAST-1.AMAZONAWS.COM"}), "only for AWS S3 endpoints"},
		{"suffix of another host", s3Conn("a", map[string]string{"endpoint": "amazonaws.com.evil.example"}), "only for AWS S3 endpoints"},
		{"bare suffix", s3Conn("a", map[string]string{"endpoint": "amazonaws.com"}), "only for AWS S3 endpoints"},
		{"with a port", s3Conn("a", map[string]string{"endpoint": "s3.amazonaws.com:443"}), "only for AWS S3 endpoints"},
		{"with a scheme", s3Conn("a", map[string]string{"endpoint": "https://s3.amazonaws.com"}), "only for AWS S3 endpoints"},
		{"s3 dash region", s3Conn("a", map[string]string{"endpoint": "s3-us-west-2.amazonaws.com"}), ""},
		{"s3 dualstack", s3Conn("a", map[string]string{"endpoint": "s3.dualstack.us-east-1.amazonaws.com"}), ""},
		{"s3 china", s3Conn("a", map[string]string{"endpoint": "s3.cn-north-1.amazonaws.com.cn"}), ""},
		{"api gateway", s3Conn("a", map[string]string{"endpoint": "abc123defg.execute-api.us-east-1.amazonaws.com"}), "only for AWS S3 endpoints"},
		{"appsync", s3Conn("a", map[string]string{"endpoint": "xyz.appsync-api.us-east-1.amazonaws.com"}), "only for AWS S3 endpoints"},
		{"ec2", s3Conn("a", map[string]string{"endpoint": "ec2-1-2-3-4.compute-1.amazonaws.com"}), "only for AWS S3 endpoints"},
		{"elb", s3Conn("a", map[string]string{"endpoint": "my-alb-1234.us-east-1.elb.amazonaws.com"}), "only for AWS S3 endpoints"},
		{"rds", s3Conn("a", map[string]string{"endpoint": "mydb.abc.us-east-1.rds.amazonaws.com"}), "only for AWS S3 endpoints"},
		{"s3 object lambda", s3Conn("a", map[string]string{"endpoint": "ap-123456789012.s3-object-lambda.us-east-1.amazonaws.com"}), "only for AWS S3 endpoints"},
		{"s3 label inside an api gateway host", s3Conn("a", map[string]string{"endpoint": "s3.execute-api.us-east-1.amazonaws.com"}), "only for AWS S3 endpoints"},
		{"env variable", s3Conn("a", map[string]string{"endpoint": "${EP}"}), "${VARIABLE}"},
		{"tls off", s3Conn("a", map[string]string{"use_ssl": "false"}), "use_ssl=false"},
		{"tls off on an aws endpoint", s3Conn("a", map[string]string{"endpoint": "s3.amazonaws.com", "use_ssl": "false"}), "use_ssl=false"},
		{"tls on is fine", s3Conn("a", map[string]string{"use_ssl": "true"}), ""},
		{"postgres", pgConn("a"), "take"},
		{"gcs", ConnectionDef{Name: "a", Type: "gcs", Scope: "gs://b/"}, "cannot sign in"},
		{"lake takes no credential", ConnectionDef{Name: "a", Type: "ducklake", Path: "x.ducklake"}, "no credentials"},
	}
	for _, tc := range cases {
		err := CheckSignIn(r, tc.def, "aws-sso:dev")
		if tc.want == "" {
			assert.NoError(t, err, tc.name)
			continue
		}
		require.Error(t, err, tc.name)
		assert.Contains(t, err.Error(), tc.want, tc.name)
	}
	// Other methods are not held to the AWS rule.
	require.NoError(t, CheckSignIn(r, s3Conn("a", map[string]string{"endpoint": "minio:9000", "use_ssl": "false"}), "env:MINIO"))
	// The test hook lifts only the endpoint rule.
	f := newFakeSSO(t, false)
	require.NoError(t, CheckSignIn(f.resolver(), s3Conn("a", map[string]string{"endpoint": "minio:9000", "use_ssl": "false"}), "aws-sso:dev"))
	require.Error(t, CheckSignIn(f.resolver(), pgConn("a"), "aws-sso:dev"), "the type still has to take it")
}

func TestSetSignInEnforcesTypeAndEndpointAndRefusesToSaveAMinIOConnection(t *testing.T) {
	newFakeSSO(t, false)
	ws := openWith(t, strictResolver())
	require.NoError(t, ws.AddConnection(ScopeLocal, s3Conn("aws", nil)))
	require.NoError(t, ws.AddConnection(ScopeLocal, s3Conn("minio", map[string]string{"endpoint": "minio:9000", "use_ssl": "false"})))
	require.NoError(t, ws.AddConnection(ScopeLocal, pgConn("pg")))

	require.NoError(t, ws.SetSignIn("aws", "aws-sso:dev"))
	assert.Equal(t, "aws-sso:dev", mustConn(t, ws, "aws").SignIn)
	require.Error(t, ws.SetSignIn("minio", "aws-sso:dev"))
	assert.Equal(t, "keychain", mustConn(t, ws, "minio").SignIn, "a refused sign-in is not recorded")
	require.Error(t, ws.SetSignIn("pg", "aws-sso:dev"))
	require.Error(t, ws.SetSignIn("aws", "aws-sso:bad name"))
}

func TestAWSSSOIsBoundToItsDefinitionLikeEnv(t *testing.T) {
	f := newFakeSSO(t, true)
	ws := openWith(t, f.resolver())
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, s3Conn("lake", nil)))
	require.NoError(t, ws.SetSignIn("lake", "aws-sso:dev"))

	cred, _, err := ConnectionCredential(ctx, ws, "lake", nil)
	require.NoError(t, err)
	assert.Equal(t, "ASIAFAKE", cred.KeyID)
	assert.Equal(t, ssoKeyNeedle, cred.Secret)
	assert.Equal(t, ssoSessionNeedle, cred.Token, "the session token reaches the secret")
	assert.False(t, cred.Expires.IsZero())

	// A pulled commit changes the region: the credentials are withheld.
	changed := s3Conn("lake", nil)
	changed.Region = "eu-west-1"
	require.NoError(t, ws.UpdateConnection(ScopeShared, changed))
	_, _, err = ConnectionCredential(ctx, ws, "lake", nil)
	var fc *secrets.ErrFingerprintChanged
	require.ErrorAs(t, err, &fc)
	assert.Contains(t, err.Error(), "server changed")
	st, err := ws.CredentialStatus(ctx, "lake")
	require.NoError(t, err)
	assert.False(t, st.FingerprintMatches)
	assert.False(t, st.SignedIn)

	// Choosing it again for the new definition asks first; confirming restores it.
	require.ErrorIs(t, ws.SetSignIn("lake", "aws-sso:dev"), ErrRebindNeedsConfirm)
	require.NoError(t, ws.RebindSignIn("lake", "aws-sso:dev"))
	_, _, err = ConnectionCredential(ctx, ws, "lake", nil)
	require.NoError(t, err)

	// The scope too.
	changed.Scope = "s3://lake/other/"
	require.NoError(t, ws.UpdateConnection(ScopeShared, changed))
	_, _, err = ConnectionCredential(ctx, ws, "lake", nil)
	require.ErrorAs(t, err, &fc)
}

func TestAWSSSOWithAHandEditedNonAWSEndpointIsRefusedAtResolve(t *testing.T) {
	newFakeSSO(t, true)
	ws := openWith(t, strictResolver())
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeLocal, s3Conn("lake", nil)))
	require.NoError(t, ws.SetSignIn("lake", "aws-sso:dev"))

	// local.yaml edited by hand: the endpoint now points at a stranger. The
	// fingerprint is recomputed to match, so only the endpoint rule can stop it.
	bad := s3Conn("lake", map[string]string{"endpoint": "evil.example:443.amazonaws.com"})
	l := ws.Local()
	l.Connections = []ConnectionDef{bad}
	resolved, err := bad.Resolve(nil, ScopeLocal)
	require.NoError(t, err)
	l.SignIn = map[string]string{"lake": "aws-sso:dev"}
	l.SignInFingerprint = map[string]string{"lake": Fingerprint(resolved)}
	l.SignInServer = map[string]string{"lake": ServerDescription(resolved)}
	require.NoError(t, ws.SaveLocal(l))

	_, _, err = ConnectionCredential(ctx, ws, "lake", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only for AWS S3 endpoints")

	// The Test path applies the same rule.
	c := mustConn(t, ws, "lake")
	rc, err := ResolveConnection(ws, c, nil)
	require.NoError(t, err)
	_, err = TestCredential(ctx, ws, rc, "", secrets.Credential{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only for AWS S3 endpoints")
}

func TestStoredCredentialsCannotBeAttachedToAnAWSSSOConnection(t *testing.T) {
	f := newFakeSSO(t, true)
	ws := openWith(t, f.resolver())
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeLocal, s3Conn("lake", nil)))
	require.NoError(t, ws.SetSignIn("lake", "aws-sso:dev"))
	err := ws.SetCredentials(ctx, "lake", secrets.Credential{KeyID: "AKIA", Secret: "NEEDLE_TYPED"}, false)
	require.ErrorIs(t, err, secrets.ErrReadOnly)
	assert.Contains(t, err.Error(), "sign in with your profile")
}

func TestCredentialStatusOfAWSSSOHasNoKey(t *testing.T) {
	f := newFakeSSO(t, true)
	ws := openWith(t, f.resolver())
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeLocal, s3Conn("lake", nil)))
	require.NoError(t, ws.SetSignIn("lake", "aws-sso:dev"))
	st, err := ws.CredentialStatus(ctx, "lake")
	require.NoError(t, err)
	assert.Equal(t, "aws-sso:dev", st.Method)
	assert.Equal(t, "dev", st.Profile)
	assert.True(t, st.SignedIn)
	require.NotNil(t, st.ExpiresAt)
	assert.True(t, st.FingerprintMatches)
	b, _ := json.Marshal(st)
	for _, n := range []string{ssoKeyNeedle, ssoSessionNeedle, ssoTokenNeedle, ssoAccount, ssoRole, "corp.awsapps.com"} {
		assert.NotContains(t, string(b), n)
	}
	assert.Equal(t, int64(0), f.hits.Load(), "status reads the token cache and makes no network call")

	// Sign out (the cache file disappears): the status says so, with the hint.
	require.NoError(t, os.RemoveAll(filepath.Join(f.home, ".aws", "sso")))
	st, err = ws.CredentialStatus(ctx, "lake")
	require.NoError(t, err)
	assert.False(t, st.SignedIn)
	assert.Contains(t, st.Detail, "aws sso login --profile dev")
}

func TestAMissingOrEndedSessionIsATypedSignInWithNoNetwork(t *testing.T) {
	f := newFakeSSO(t, false)
	r := f.resolver()
	ctx := context.Background()
	ref := secrets.Ref{Name: "lake"}

	_, err := r.Resolve(ctx, ref, "aws-sso:dev", secrets.Binding{Type: "s3"})
	var nc *secrets.ErrNeedsCredentials
	require.ErrorAs(t, err, &nc)
	require.NotNil(t, nc.SignIn)
	assert.Equal(t, secrets.SignInHint{Kind: "aws-sso", Subject: "dev", Reason: "missing"}, *nc.SignIn)
	assert.Contains(t, err.Error(), "aws sso login --profile dev")
	assert.NotContains(t, err.Error(), "_USER", "no X_USER / X_PASSWORD advice")
	for _, n := range []string{ssoAccount, ssoRole, "corp.awsapps.com"} {
		assert.NotContains(t, err.Error(), n, "the message says no more than the person needs")
	}

	// An expired token (no refresh token) reads "expired".
	f.cacheToken(t, time.Now().Add(-time.Hour))
	_, err = r.Resolve(ctx, ref, "aws-sso:dev", secrets.Binding{Type: "s3"})
	require.ErrorAs(t, err, &nc)
	assert.Equal(t, "expired", nc.SignIn.Reason)
	assert.Contains(t, err.Error(), "has expired")
	assert.Equal(t, int64(0), f.hits.Load(), "neither answer calls the portal")

	// A profile that is not in ~/.aws/config is a plain failure that names it.
	_, err = r.Resolve(ctx, ref, "aws-sso:nope", secrets.Binding{Type: "s3"})
	var mf *secrets.ErrMethodFailed
	require.ErrorAs(t, err, &mf)
	assert.Contains(t, err.Error(), "nope")
	assert.NotContains(t, err.Error(), f.home, "no file path")
}

func TestAPortalThatRefusesTheTokenReadsAsExpired(t *testing.T) {
	f := newFakeSSO(t, true)
	// A different token than the one the portal accepts.
	sum := sha1.Sum([]byte("corp")) //nolint:gosec // see import
	p := filepath.Join(f.home, ".aws", "sso", "cache", hex.EncodeToString(sum[:])+".json")
	b, _ := json.Marshal(map[string]string{"startUrl": "https://corp.awsapps.com/start", "region": "us-east-1",
		"accessToken": "REVOKED", "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	require.NoError(t, os.WriteFile(p, b, 0o600))
	_, err := f.resolver().Resolve(context.Background(), secrets.Ref{Name: "lake"}, "aws-sso:dev", secrets.Binding{Type: "s3"})
	var nc *secrets.ErrNeedsCredentials
	require.ErrorAs(t, err, &nc)
	assert.Equal(t, "expired", nc.SignIn.Reason, "refused maps to expired")
}

func TestASignedInProfileFeedsTheRedactor(t *testing.T) {
	f := newFakeSSO(t, true)
	r := f.resolver()
	_, err := r.Resolve(context.Background(), secrets.Ref{Name: "lake"}, "aws-sso:dev", secrets.Binding{Type: "s3"})
	require.NoError(t, err)
	for _, n := range []string{ssoKeyNeedle, ssoSessionNeedle} {
		assert.NotContains(t, r.Redactor().Redact("boom "+n+" boom"), n)
	}
}

func TestEveryResolverConstructionSiteKnowsAWSSSO(t *testing.T) {
	newFakeSSO(t, false)
	known := func(t *testing.T, r *secrets.Resolver) {
		t.Helper()
		st := r.Status(context.Background(), secrets.Ref{Name: "x"}, "aws-sso:dev", secrets.Binding{})
		assert.True(t, st.Available, st.Detail)
		assert.Equal(t, "dev", st.Profile)
	}
	t.Run("NewResolver", func(t *testing.T) { known(t, NewResolver()) })
	t.Run("workspace.Open default", func(t *testing.T) {
		res, err := Init(filepath.Join(t.TempDir(), "acme"))
		require.NoError(t, err)
		_ = res.Workspace.Close()
		ws, err := Open(res.Workspace.Root(), WithConfigDir(t.TempDir()), WithUserConfig(UserConfig{Version: ConfigVersion}))
		require.NoError(t, err)
		defer ws.Close() //nolint:errcheck
		known(t, ws.Resolver())
	})
	t.Run("a .env layer keeps it", func(t *testing.T) {
		known(t, NewResolver().WithEnvLayer(func(string) (string, bool) { return "", false }, nil))
	})
}

func TestAWSSSOEnvironmentCannotChangeTheIdentity(t *testing.T) {
	f := newFakeSSO(t, true)
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAHOST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "NEEDLE_HOST_SECRET")
	t.Setenv("AWS_PROFILE", "other")
	r := NewResolver(WithSSOTestEndpoints(f.portal.URL, f.portal.URL))
	cred, err := r.Resolve(context.Background(), secrets.Ref{Name: "x"}, "aws-sso:dev", secrets.Binding{Type: "s3"})
	require.NoError(t, err)
	assert.Equal(t, "ASIAFAKE", cred.KeyID)
	assert.Equal(t, ssoKeyNeedle, cred.Secret)
}

func TestAnEndpointOnlyChangeNamesBothEndpointsInTheServerText(t *testing.T) {
	f := newFakeSSO(t, true)
	ws := openWith(t, f.resolver())
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, s3Conn("lake", nil)))
	require.NoError(t, ws.SetSignIn("lake", "aws-sso:dev"))

	changed := s3Conn("lake", map[string]string{"endpoint": "s3.us-west-2.amazonaws.com", "url_style": "path"})
	require.NoError(t, ws.UpdateConnection(ScopeShared, changed))
	_, _, err := ConnectionCredential(ctx, ws, "lake", nil)
	var fc *secrets.ErrFingerprintChanged
	require.ErrorAs(t, err, &fc)
	assert.NotEqual(t, fc.Old, fc.New, "the text shows a difference")
	assert.Equal(t, "s3://lake/raw/", fc.Old)
	assert.Contains(t, fc.New, "s3.us-west-2.amazonaws.com")
	assert.Contains(t, err.Error(), "s3://lake/raw/ to s3://lake/raw/ via s3.us-west-2.amazonaws.com (path style)")

	// Both ends named when the endpoint changes from one host to another.
	require.NoError(t, ws.RebindSignIn("lake", "aws-sso:dev"))
	other := s3Conn("lake", map[string]string{"endpoint": "s3.eu-west-1.amazonaws.com", "url_style": "path"})
	require.NoError(t, ws.UpdateConnection(ScopeShared, other))
	_, _, err = ConnectionCredential(ctx, ws, "lake", nil)
	require.ErrorAs(t, err, &fc)
	assert.Contains(t, fc.Old, "s3.us-west-2.amazonaws.com")
	assert.Contains(t, fc.New, "s3.eu-west-1.amazonaws.com")
}

func TestMoveFromUserScopeCarriesTheSignInToLocal(t *testing.T) {
	f := newFakeSSO(t, true)
	ws := openWith(t, f.resolver())
	require.NoError(t, ws.AddConnection(ScopeUser, s3Conn("lake", nil)))
	require.NoError(t, ws.SetSignIn("lake", "aws-sso:dev"))
	require.NoError(t, ws.MoveConnection(context.Background(), "lake", ScopeUser, ScopeLocal))
	c := mustConn(t, ws, "lake")
	assert.Equal(t, ScopeLocal, c.Scope)
	assert.Equal(t, "aws-sso:dev", c.SignIn, "the choice moved with the connection")
	_, _, err := ConnectionCredential(context.Background(), ws, "lake", nil)
	require.NoError(t, err, "and it is still bound to the same definition")
}

// A rotation asks for new keys with secrets.WithFresh: the resolver then skips
// the provider's cache (a plain resolve is served from it, with no portal call).
func TestFreshContextBypassesTheSSOCache(t *testing.T) {
	f := newFakeSSO(t, true)
	r := f.resolver()
	ctx := context.Background()
	ref := secrets.Ref{Name: "lake"}
	b := secrets.Binding{Type: "s3"}

	c1, err := r.Resolve(ctx, ref, "aws-sso:dev", b)
	require.NoError(t, err)
	assert.False(t, c1.Expires.IsZero(), "an SSO credential carries its deadline")
	require.Equal(t, int64(1), f.hits.Load())
	_, err = r.Resolve(ctx, ref, "aws-sso:dev", b)
	require.NoError(t, err)
	assert.Equal(t, int64(1), f.hits.Load(), "served from the cache")

	_, err = r.Resolve(secrets.WithFresh(ctx), ref, "aws-sso:dev", b)
	require.NoError(t, err)
	assert.Equal(t, int64(2), f.hits.Load(), "a fresh resolve asks the portal")
	_, err = r.Resolve(ctx, ref, "aws-sso:dev", b)
	require.NoError(t, err)
	assert.Equal(t, int64(2), f.hits.Load(), "and its answer is what the cache now holds")

	// Signed out: a fresh resolve is the typed sign-in error, not a stale success.
	require.NoError(t, os.RemoveAll(filepath.Join(f.home, ".aws", "sso")))
	_, err = r.Resolve(secrets.WithFresh(ctx), ref, "aws-sso:dev", b)
	var nc *secrets.ErrNeedsCredentials
	require.ErrorAs(t, err, &nc)
	require.NotNil(t, nc.SignIn)
}
