package awssso

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clock is a settable clock plus a sleep that records waits and advances it.
type clock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	c.now = c.now.Add(d)
	return nil
}

func fastOpts(f *fakeAWS, c *clock) []Option {
	return append(f.opts(), WithClock(c.Now), WithSleep(c.Sleep))
}

func login(t *testing.T, p *Provider, name string) (Prompt, error) {
	t.Helper()
	var got []Prompt
	err := p.DeviceLogin(context.Background(), name, func(pr Prompt) { got = append(got, pr) })
	if len(got) > 0 {
		require.Len(t, got, 1, "show is called once")
		return got[0], err
	}
	return Prompt{}, err
}

func TestDeviceLoginSessionProfileWritesACLICompatibleCache(t *testing.T) {
	f := newFake(t)
	f.tokenSeq = []string{"AuthorizationPendingException", "AuthorizationPendingException", ""}
	h := home(t, sessionCfg)
	c := &clock{now: time.Now()}
	p := New(h, nil, fastOpts(f, c)...)

	pr, err := login(t, p, "dev")
	require.NoError(t, err)
	assert.Equal(t, "ABCD-EFGH", pr.Code)
	assert.Contains(t, pr.URL, f.URL)
	assert.WithinDuration(t, c.Now(), pr.ExpiresAt.Add(time.Duration(len(c.waits))*time.Second), 11*time.Minute)
	assert.Equal(t, []time.Duration{time.Second, time.Second, time.Second}, c.waits, "the server's interval, three polls")

	reg := f.bodies("/client/register")[0]
	assert.Equal(t, "public", reg["clientType"])
	assert.Equal(t, []any{"sso:account:access", "custom:scope"}, reg["scopes"], "the profile's registration scopes")
	assert.Equal(t, []any{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"}, reg["grantTypes"])
	assert.Equal(t, "https://corp.awsapps.com/start", reg["issuerUrl"])
	assert.Equal(t, "https://corp.awsapps.com/start", f.bodies("/device_authorization")[0]["startUrl"])

	path := tokenPath(h, "corp")
	m := cacheJSON(t, path)
	assert.Equal(t, needleAccess, m["accessToken"])
	assert.Equal(t, needleRefresh, m["refreshToken"])
	assert.Equal(t, "cid-1", m["clientId"])
	assert.Equal(t, needleClient, m["clientSecret"])
	assert.Equal(t, "https://corp.awsapps.com/start", m["startUrl"])
	assert.Equal(t, "us-east-1", m["region"])
	assert.NotEmpty(t, m["registrationExpiresAt"])
	exp, _ := m["expiresAt"].(string)
	_, err = time.Parse(time.RFC3339, exp)
	require.NoError(t, err)

	assert.Equal(t, os.FileMode(0o600), fileMode(t, path))
	assert.Equal(t, os.FileMode(0o700), fileMode(t, filepath.Dir(path)))
	ents, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, ents, 1, "no temp file is left behind")

	assert.True(t, p.Session(sessionProf()).SignedIn)
	_, err = p.Credentials(context.Background(), "dev")
	require.NoError(t, err)
}

// A token Assay wrote is refreshed by the SDK's own provider: the registration
// and refresh token are in the file.
func TestDeviceLoginTokenIsRefreshableBySSOTokenProvider(t *testing.T) {
	f := newFake(t)
	h := home(t, sessionCfg)
	// Assay's clock is 2 hours ago, so the token it writes has already expired by the real clock the SDK uses.
	c := &clock{now: time.Now().Add(-2 * time.Hour)}
	_, err := login(t, New(h, nil, fastOpts(f, c)...), "dev")
	require.NoError(t, err)

	p := New(h, nil, f.opts()...) // real clock
	creds, err := p.Credentials(context.Background(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "ASIAFAKE", creds.KeyID)
	assert.Equal(t, []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"}, f.deviceCalls)
	assert.Equal(t, "REFRESHED_"+needleAccess, cacheJSON(t, tokenPath(h, "corp"))["accessToken"])
}

func TestDeviceLoginLegacyProfileGetsNoRefreshToken(t *testing.T) {
	f := newFake(t)
	h := home(t, legacyCfg)
	c := &clock{now: time.Now()}
	_, err := login(t, New(h, nil, fastOpts(f, c)...), "old")
	require.NoError(t, err)
	reg := f.bodies("/client/register")[0]
	assert.NotContains(t, reg, "issuerUrl")
	assert.NotContains(t, reg, "grantTypes")
	assert.Equal(t, []any{"sso:account:access"}, reg["scopes"])
	m := cacheJSON(t, tokenPath(h, "https://corp.awsapps.com/start"))
	assert.Equal(t, needleAccess, m["accessToken"])
	for _, k := range []string{"refreshToken", "clientId", "clientSecret", "registrationExpiresAt"} {
		assert.NotContains(t, m, k)
	}
	assert.Equal(t, "eu-west-1", m["region"])
}

func TestDeviceLoginClearsTheNegativeCache(t *testing.T) {
	f := newFake(t)
	h := home(t, sessionCfg)
	writeCache(t, h, sessionProf(), validToken())
	f.mu.Lock()
	f.portalStatus, f.portalType = 500, "InternalFailure"
	f.mu.Unlock()
	c := &clock{now: time.Now()}
	p := New(h, nil, fastOpts(f, c)...)
	_, err := p.Credentials(context.Background(), "dev")
	require.Error(t, err)
	f.mu.Lock()
	f.portalStatus = 0
	f.mu.Unlock()
	_, err = p.Credentials(context.Background(), "dev")
	require.Error(t, err, "still remembered")
	_, err = login(t, p, "dev")
	require.NoError(t, err)
	_, err = p.Credentials(context.Background(), "dev")
	require.NoError(t, err, "a successful sign-in clears it")
}

func TestDeviceFlowOutcomes(t *testing.T) {
	t.Run("slow_down grows the interval by 5 seconds", func(t *testing.T) {
		f := newFake(t)
		f.tokenSeq = []string{"SlowDownException", "AuthorizationPendingException", "SlowDownException", ""}
		c := &clock{now: time.Now()}
		_, err := login(t, New(home(t, sessionCfg), nil, fastOpts(f, c)...), "dev")
		require.NoError(t, err)
		assert.Equal(t, []time.Duration{1 * time.Second, 6 * time.Second, 6 * time.Second, 11 * time.Second}, c.waits)
	})
	t.Run("expired_token", func(t *testing.T) {
		f := newFake(t)
		f.tokenSeq = []string{"AuthorizationPendingException", "ExpiredTokenException"}
		c := &clock{now: time.Now()}
		h := home(t, sessionCfg)
		_, err := login(t, New(h, nil, fastOpts(f, c)...), "dev")
		require.ErrorIs(t, err, ErrDeviceCodeExpired)
		assert.NoFileExists(t, tokenPath(h, "corp"))
	})
	t.Run("access_denied", func(t *testing.T) {
		f := newFake(t)
		f.tokenSeq = []string{"AccessDeniedException"}
		c := &clock{now: time.Now()}
		h := home(t, sessionCfg)
		_, err := login(t, New(h, nil, fastOpts(f, c)...), "dev")
		require.ErrorIs(t, err, ErrAccessDenied)
		assert.NoFileExists(t, tokenPath(h, "corp"))
	})
	t.Run("the code's lifetime ends the wait", func(t *testing.T) {
		f := newFake(t)
		f.expiresIn = 3
		f.tokenSeq = []string{"AuthorizationPendingException", "AuthorizationPendingException", "AuthorizationPendingException", "AuthorizationPendingException"}
		c := &clock{now: time.Now()}
		_, err := login(t, New(home(t, sessionCfg), nil, fastOpts(f, c)...), "dev")
		require.ErrorIs(t, err, ErrDeviceCodeExpired)
	})
	t.Run("a service error stops the flow", func(t *testing.T) {
		f := newFake(t)
		f.tokenSeq = []string{"InvalidClientException"}
		c := &clock{now: time.Now()}
		_, err := login(t, New(home(t, sessionCfg), nil, fastOpts(f, c)...), "dev")
		var se *ServiceError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, "InvalidClientException", se.Code)
	})
	t.Run("cancellation", func(t *testing.T) {
		f := newFake(t)
		f.tokenSeq = []string{"AuthorizationPendingException", "AuthorizationPendingException"}
		ctx, cancel := context.WithCancel(context.Background())
		c := &clock{now: time.Now()}
		p := New(home(t, sessionCfg), nil, fastOpts(f, c)...)
		err := p.DeviceLogin(ctx, "dev", func(Prompt) { cancel() })
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("a server that goes away is unreachable after three failures", func(t *testing.T) {
		f := newFake(t)
		c := &clock{now: time.Now()}
		p := New(home(t, sessionCfg), nil, fastOpts(f, c)...)
		err := p.DeviceLogin(context.Background(), "dev", func(Prompt) { f.Close() })
		require.ErrorIs(t, err, ErrUnreachable)
		assert.Len(t, c.waits, 3)
	})
	t.Run("a non-SSO profile never starts a flow", func(t *testing.T) {
		f := newFake(t)
		h := home(t, "[profile s]\naws_access_key_id = a\naws_secret_access_key = b\n")
		_, err := login(t, New(h, nil, f.opts()...), "s")
		require.Error(t, err)
		assert.Empty(t, f.bodies("/client/register"))
	})
	t.Run("a nil show is refused", func(t *testing.T) {
		require.Error(t, New(home(t, sessionCfg), nil).DeviceLogin(context.Background(), "dev", nil))
	})
}

func TestDeviceLoginRefusesUnsafeVerificationURLs(t *testing.T) {
	for _, bad := range []string{
		"http://corp.awsapps.com/verify", "file:///etc/passwd", "javascript:alert(1)", "https://evil.example/",
		"https://amazonaws.com.evil.example/", "https://xn--e1afmkfd.awsapps.com/", "https://corp.awsapps.com:8443/x",
		"https://user:pw@corp.awsapps.com/x", "https://corp.awsapps.com.evil.com/", "https://evilawsapps.com/",
		"https://CORP.awsapps.com/", "https://corp.awsapps.com%2eevil.com/", "https:corp.awsapps.com", "//corp.awsapps.com/", "",
		"https://aws/", "https://amazonaws.com/", "https://attacker-bucket.s3.amazonaws.com/phish.html", "https://attacker-bucket.s3.us-east-1.amazonaws.com/p", "https://bucket.s3-website-us-east-1.amazonaws.com/", "https://abc123.execute-api.us-east-1.amazonaws.com/prod/", "https://abcd.lambda-url.us-east-1.on.aws/", "https://x.aws/", "https://device.sso.eu-west-1.amazonaws.com/", "https://x.portal.eu-west-1.app.aws/", "https://awsapps.com/", "https://corp.awsapps.com\\@evil.com/", "https://[::1]/", "https://corp.awsapps.com./",
	} {
		if bad == "" {
			continue // empty means "absent", exercised elsewhere
		}
		for _, field := range []string{"complete", "plain"} {
			f := newFake(t)
			if field == "complete" {
				f.verification = bad
			} else {
				f.verification = "https://corp.awsapps.com/verify"
				f.verificationPlain = bad
			}
			h := home(t, sessionCfg)
			c := &clock{now: time.Now()}
			shown := false
			err := New(h, nil, append(f.opts(), WithClock(c.Now), WithSleep(c.Sleep))...).DeviceLogin(context.Background(), "dev", func(Prompt) { shown = true })
			require.Error(t, err, "%s %q", field, bad)
			assert.False(t, shown, "%s %q", field, bad)
			assert.Empty(t, f.deviceCalls, "never polled")
			assert.NoFileExists(t, tokenPath(h, "corp"))
		}
	}
}

func TestVerificationURLAllowList(t *testing.T) {
	prof := Profile{Name: "dev", Region: "us-east-1", StartURL: "https://corp.awsapps.com/start"}
	for _, ok := range []string{
		"https://device.sso.us-east-1.amazonaws.com/?user_code=ABCD-EFGH", "https://corp.awsapps.com/start/#/device?user_code=X",
		"https://d-123.awsapps.com/start", "https://x.awsapps.cn/start",
		"https://ssoins-1234.portal.us-east-1.app.aws/#/device?user_code=X",
	} {
		assert.NoError(t, checkVerificationURL(ok, prof, ""), ok)
	}
	cn := Profile{Name: "dev", Region: "cn-north-1", StartURL: "https://corp.awsapps.cn/start"}
	assert.NoError(t, checkVerificationURL("https://device.sso.cn-north-1.amazonaws.com.cn/x", cn, ""))
	assert.Error(t, checkVerificationURL("https://device.sso.cn-north-1.amazonaws.com/x", cn, ""))
	assert.Error(t, checkVerificationURL("https://corp.example.com/start", Profile{Region: "us-east-1", StartURL: "https://corp.example.com/start"}, ""), "the start URL host is not trusted on its own")
	assert.NoError(t, checkVerificationURL("http://127.0.0.1:5555/verify", prof, "127.0.0.1:5555"), "the test endpoint")
	assert.Error(t, checkVerificationURL("http://127.0.0.1:6666/verify", prof, "127.0.0.1:5555"))
	assert.Error(t, checkVerificationURL("https://evil.example/", prof, "127.0.0.1:5555"))
}

func TestPollingIsBoundedByTheCodeLifetimeAndCaps(t *testing.T) {
	f := newFake(t)
	f.interval = 1 << 30
	f.expiresIn = 1 << 30
	f.tokenSeq = []string{"AuthorizationPendingException", "AuthorizationPendingException"}
	start := time.Now()
	c := &clock{now: start}
	pr, err := login(t, New(home(t, sessionCfg), nil, fastOpts(f, c)...), "dev")
	require.NoError(t, err)
	assert.LessOrEqual(t, pr.ExpiresAt.Sub(start), maxCodeLife)
	for _, w := range c.waits {
		assert.LessOrEqual(t, w, maxInterval)
	}
	// A short code life ends the wait at the deadline, not after a long interval.
	f2 := newFake(t)
	f2.interval, f2.expiresIn = 3600, 7
	f2.tokenSeq = []string{"AuthorizationPendingException"}
	c2 := &clock{now: time.Now()}
	_, err = login(t, New(home(t, sessionCfg), nil, fastOpts(f2, c2)...), "dev")
	require.ErrorIs(t, err, ErrDeviceCodeExpired)
	for _, w := range c2.waits {
		assert.LessOrEqual(t, w, 7*time.Second)
	}
}

func TestWriteTokenNeverFollowsASymlink(t *testing.T) {
	h := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o600))
	path := tokenPath(h, "corp")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

	// A symlink planted at the final name is replaced, not followed.
	require.NoError(t, os.Symlink(victim, path))
	// And one planted at the temp name the writer tries first is skipped.
	calls := 0
	p := New(h, nil)
	p.tmpSuffix = func() string {
		calls++
		if calls == 1 {
			return "planted"
		}
		return "second"
	}
	require.NoError(t, os.Symlink(victim, path+".tmp-planted"))

	require.NoError(t, p.writeToken(path, tokenFile{AccessToken: needleAccess, ExpiresAt: "2099-01-01T00:00:00Z"}))
	b, err := os.ReadFile(victim)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(b), "the symlink target was never written")
	st, err := os.Lstat(path)
	require.NoError(t, err)
	assert.True(t, st.Mode().IsRegular(), "the cache name is now a regular file")
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	assert.Equal(t, 2, calls)
}
