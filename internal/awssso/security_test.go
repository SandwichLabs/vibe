package awssso

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routed answers every request from a fakeAWS in memory and records the hosts
// they were addressed to, so a test can see where the SDK would have sent them.
type routed struct {
	f     *fakeAWS
	mu    sync.Mutex
	hosts []string
}

func (r *routed) Do(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.hosts = append(r.hosts, req.URL.Scheme+"://"+req.URL.Host)
	r.mu.Unlock()
	rec := httptest.NewRecorder()
	r.f.serve(rec, req)
	return rec.Result(), nil
}

// Nothing in the process environment changes which identity is used or where
// the token goes: endpoints come from sso_region alone.
func TestAWSEnvironmentChangesNothing(t *testing.T) {
	for k, v := range map[string]string{
		"AWS_ACCESS_KEY_ID": "AKIAHOSTKEY", "AWS_SECRET_ACCESS_KEY": "hostsecret", "AWS_SESSION_TOKEN": "hosttoken",
		"AWS_PROFILE": "other", "AWS_DEFAULT_PROFILE": "other", "AWS_REGION": "ap-south-1", "AWS_DEFAULT_REGION": "ap-south-1",
		"AWS_ENDPOINT_URL": "https://evil.example", "AWS_ENDPOINT_URL_SSO": "https://evil.example", "AWS_ENDPOINT_URL_SSO_OIDC": "https://evil.example",
		"AWS_CONFIG_FILE": "/nonexistent/config", "AWS_SHARED_CREDENTIALS_FILE": "/nonexistent/creds",
		"AWS_EC2_METADATA_SERVICE_ENDPOINT": "http://127.0.0.1:1", "AWS_USE_FIPS_ENDPOINT": "true", "AWS_USE_DUALSTACK_ENDPOINT": "true",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI": "http://127.0.0.1:2", "AWS_WEB_IDENTITY_TOKEN_FILE": "/nonexistent/token",
		"AWS_ROLE_ARN": "arn:aws:iam::1:role/x", "HTTPS_PROXY": "", "AWS_SSO_SESSION": "evil",
	} {
		t.Setenv(k, v)
	}
	f := newFake(t)
	f.verification = "https://corp.awsapps.com/verify"
	rt := &routed{f: f}
	h := home(t, sessionCfg)
	c := &clock{now: time.Now()}
	p := New(h, nil, WithHTTPClient(&http.Client{Transport: roundTripper{rt}}), WithClock(c.Now), WithSleep(c.Sleep))

	require.NoError(t, p.DeviceLogin(context.Background(), "dev", func(Prompt) {}))
	creds, err := p.Credentials(context.Background(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "ASIAFAKE", creds.KeyID, "the host's AWS_ACCESS_KEY_ID is not the identity")

	rt.mu.Lock()
	defer rt.mu.Unlock()
	require.NotEmpty(t, rt.hosts)
	for _, h := range rt.hosts {
		assert.Contains(t, []string{"https://oidc.us-east-1.amazonaws.com", "https://portal.sso.us-east-1.amazonaws.com"}, h)
	}
}

type roundTripper struct{ r *routed }

func (t roundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return t.r.Do(req) }

func TestFIPSAndDualStackFlagsDoNotMoveEndpointsEither(t *testing.T) {
	// Config-file switches are the SDK's own shared-config keys; Vibe reads none of them.
	f := newFake(t)
	rt := &routed{f: f}
	cfg := sessionCfg + "\nuse_fips_endpoint = true\nuse_dualstack_endpoint = true\nsso_endpoint = https://evil.example\n"
	h := home(t, cfg)
	writeCache(t, h, sessionProf(), validToken())
	_, err := New(h, nil, WithHTTPClient(&http.Client{Transport: roundTripper{rt}})).Credentials(context.Background(), "dev")
	require.NoError(t, err)
	for _, host := range rt.hosts {
		assert.Equal(t, "https://portal.sso.us-east-1.amazonaws.com", host)
	}
}

// Every failure path, formatted every way, and the structured logger: no token,
// refresh token, client secret, role key or config content, and no start URL,
// account or role either.
func TestNoSecretReachesAnErrorOrALog(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	var all []error

	f := newFake(t)
	h := home(t, sessionCfg+"\n"+legacyCfg+"\n[profile static]\naws_secret_access_key = "+needleCfg+"\naws_access_key_id = a\n")
	c := &clock{now: time.Now()}
	p := New(h, nil, fastOpts(f, c)...)

	_, e := p.Credentials(ctx, "dev") // missing
	all = append(all, e)
	_, e = p.Credentials(ctx, "static")
	all = append(all, e)
	bad := validToken()
	bad.AccessToken = needleAccess + "_x"
	writeCache(t, h, sessionProf(), bad)
	_, e = p.Credentials(ctx, "dev") // refused (the portal echoes the token)
	all = append(all, e)
	f.mu.Lock()
	f.portalStatus, f.portalType = 500, "InternalFailure"
	f.mu.Unlock()
	writeCache(t, h, sessionProf(), validToken())
	_, e = p.Credentials(ctx, "dev", Bypass())
	all = append(all, e)
	f.mu.Lock()
	f.tokenSeq = []string{"AccessDeniedException"}
	f.mu.Unlock()
	all = append(all, p.DeviceLogin(ctx, "old", func(Prompt) {}))
	f.verification = "https://evil.example/" + needleAccess
	all = append(all, p.DeviceLogin(ctx, "dev", func(Prompt) {}))
	_, e = p.LoadProfile(ctx, "nope")
	all = append(all, e)

	for i, err := range all {
		require.Error(t, err, i)
		for _, form := range []string{err.Error(), sprintf("%v", err), sprintf("%+v", err), sprintf("%#v", err)} {
			noLeak(t, "error", form)
			assert.NotContains(t, form, "corp.awsapps.com")
			assert.NotContains(t, form, "123456789012")
			assert.NotContains(t, form, "Reader")
			assert.NotContains(t, form, h)
		}
		log.Error("failed", "err", err)
	}
	prof, err := p.LoadProfile(ctx, "dev")
	require.NoError(t, err)
	log.Info("profile", "profile", prof, "creds", Creds{KeyID: "ASIAFAKE", Secret: needleKey, Token: needleSession})
	noLeak(t, "slog", buf.String())
	assert.NotContains(t, buf.String(), "corp.awsapps.com")
	assert.NotContains(t, buf.String(), "123456789012")
	assert.NotContains(t, buf.String(), "ASIAFAKE")
	assert.Contains(t, buf.String(), `"profile":"dev"`)

	var sin *SignInNeeded
	assert.True(t, errors.As(all[0], &sin))
}
