package awssso

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func needSignIn(t *testing.T, err error, r Reason) {
	t.Helper()
	var s *SignInNeeded
	require.ErrorAs(t, err, &s, "%v", err)
	assert.Equal(t, r, s.Reason)
	assert.Equal(t, "dev", s.Profile)
	noLeak(t, "sign-in error", err.Error())
}

func TestCredentialsFromACLIFormatCache(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	for name, tc := range map[string]struct {
		cfg  string
		prof Profile
	}{"sso-session": {sessionCfg, sessionProf()}, "legacy": {legacyCfg, legacyProf()}} {
		t.Run(name, func(t *testing.T) {
			h := home(t, tc.cfg)
			writeCache(t, h, tc.prof, validToken())
			p := New(h, nil, f.opts()...)
			st := p.Session(tc.prof)
			assert.True(t, st.SignedIn)
			assert.False(t, st.Refreshable)
			creds, err := p.Credentials(ctx, tc.prof.Name)
			require.NoError(t, err)
			assert.Equal(t, "ASIAFAKE", creds.KeyID)
			assert.Equal(t, needleKey, creds.Secret)
			assert.Equal(t, needleSession, creds.Token)
			assert.WithinDuration(t, time.Now().Add(time.Hour), creds.Expires, time.Minute)
		})
	}
}

func TestCredentialsAreCachedUntilNearExpiryAndBypassed(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	h := home(t, sessionCfg)
	writeCache(t, h, sessionProf(), validToken())
	now := time.Now()
	var mu sync.Mutex
	p := New(h, nil, append(f.opts(), WithClock(func() time.Time { mu.Lock(); defer mu.Unlock(); return now }))...)

	_, err := p.Credentials(ctx, "dev")
	require.NoError(t, err)
	_, err = p.Credentials(ctx, "dev")
	require.NoError(t, err)
	assert.Equal(t, 1, f.hits(), "the second call is served from memory")
	_, err = p.Credentials(ctx, "dev", Bypass())
	require.NoError(t, err)
	assert.Equal(t, 2, f.hits(), "Bypass re-resolves")

	mu.Lock()
	now = now.Add(56 * time.Minute) // within 5 minutes of the 1h expiry
	mu.Unlock()
	_, err = p.Credentials(ctx, "dev")
	require.NoError(t, err)
	assert.Equal(t, 3, f.hits(), "rotated before it expires")
}

func TestCredentialsDistinguishMissingExpiredRefusedAndOffline(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	h := home(t, sessionCfg)
	p := New(h, nil, f.opts()...)

	_, err := p.Credentials(ctx, "dev")
	needSignIn(t, err, ReasonMissing)

	expired := validToken()
	expired.ExpiresAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	path := writeCache(t, h, sessionProf(), expired)
	_, err = p.Credentials(ctx, "dev")
	needSignIn(t, err, ReasonExpired)
	assert.Zero(t, f.hits(), "decided locally")

	// A local verdict is not remembered: a sign-in made elsewhere is seen at once.
	require.NoError(t, os.Remove(path))
	writeCache(t, h, sessionProf(), validToken())
	_, err = p.Credentials(ctx, "dev")
	require.NoError(t, err)

	// The portal refuses a token it does not know.
	bad := validToken()
	bad.AccessToken = needleAccess + "_revoked"
	writeCache(t, h, sessionProf(), bad)
	_, err = p.Credentials(ctx, "dev", Bypass())
	needSignIn(t, err, ReasonRefused)
	assert.NotContains(t, err.Error(), "revoked", "the portal's message (which echoes the token) is not passed on")

	// Offline: the server is gone.
	writeCache(t, h, sessionProf(), validToken())
	f.Close()
	_, err = p.Credentials(ctx, "dev", Bypass())
	require.ErrorIs(t, err, ErrUnreachable)
	noLeak(t, "offline", err.Error())
}

func TestOfflineAndRefusalAreRememberedFor30Seconds(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	h := home(t, sessionCfg)
	writeCache(t, h, sessionProf(), validToken())
	now := time.Now()
	var mu sync.Mutex
	p := New(h, nil, append(f.opts(), WithClock(func() time.Time { mu.Lock(); defer mu.Unlock(); return now }))...)
	f.mu.Lock()
	f.portalStatus, f.portalType = 429, "TooManyRequestsException"
	f.mu.Unlock()
	_, err := p.Credentials(ctx, "dev")
	var se *ServiceError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, "TooManyRequestsException", se.Code)
	_, err = p.Credentials(ctx, "dev")
	require.ErrorAs(t, err, &se)
	assert.Equal(t, 1, f.hits(), "remembered")
	mu.Lock()
	now = now.Add(31 * time.Second)
	mu.Unlock()
	f.mu.Lock()
	f.portalStatus = 0
	f.mu.Unlock()
	_, err = p.Credentials(ctx, "dev")
	require.NoError(t, err)
}

func TestConcurrentCallsCostOneResolution(t *testing.T) {
	f := newFake(t)
	h := home(t, sessionCfg)
	writeCache(t, h, sessionProf(), validToken())
	p := New(h, nil, f.opts()...)
	var wg sync.WaitGroup
	var fails atomic.Int32
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Credentials(context.Background(), "dev"); err != nil {
				fails.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Zero(t, fails.Load())
	assert.Equal(t, 1, f.hits())
}

func TestOfflineWaitsAtMostOneTimeoutPerProfile(t *testing.T) {
	// A server that never answers: N concurrent callers wait one timeout, not N.
	hang := make(chan struct{})
	slow := newFake(t)
	slow.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-hang })
	t.Cleanup(func() { close(hang) })
	h := home(t, sessionCfg)
	writeCache(t, h, sessionProf(), validToken())
	p := New(h, nil, append(slow.opts(), WithCallTimeout(150*time.Millisecond))...)
	start := time.Now()
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Credentials(context.Background(), "dev")
			assert.ErrorIs(t, err, ErrUnreachable)
		}()
	}
	wg.Wait()
	assert.Less(t, time.Since(start), 600*time.Millisecond)
}

func TestCallerCancellationIsNotUnreachable(t *testing.T) {
	f := newFake(t)
	h := home(t, sessionCfg)
	writeCache(t, h, sessionProf(), validToken())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(h, nil, f.opts()...).Credentials(ctx, "dev")
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "%v", err)
}

func TestExpiredTokenIsRefreshedAndWrittenBack(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	h := home(t, sessionCfg)
	tok := validToken()
	tok.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	tok.RefreshToken, tok.ClientID, tok.ClientSecret = needleRefresh, "cid-1", needleClient
	path := writeCache(t, h, sessionProf(), tok)
	p := New(h, nil, f.opts()...)

	st := p.Session(sessionProf())
	assert.True(t, st.SignedIn)
	assert.True(t, st.Refreshable)

	creds, err := p.Credentials(ctx, "dev")
	require.NoError(t, err)
	assert.Equal(t, "ASIAFAKE", creds.KeyID)
	assert.Equal(t, []string{"refresh_token"}, f.deviceCalls)
	m := cacheJSON(t, path)
	assert.Equal(t, "REFRESHED_"+needleAccess, m["accessToken"], "the SDK wrote the refreshed token back to the same file")
	assert.Equal(t, needleRefresh+"_2", m["refreshToken"])
	assert.Equal(t, "cid-1", m["clientId"], "the registration survives a refresh")
}

func TestRefreshRefusedNeedsSignIn(t *testing.T) {
	f := newFake(t)
	f.refreshErr = "InvalidGrantException"
	h := home(t, sessionCfg)
	tok := validToken()
	tok.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	tok.RefreshToken, tok.ClientID, tok.ClientSecret = needleRefresh, "cid-1", needleClient
	writeCache(t, h, sessionProf(), tok)
	_, err := New(h, nil, f.opts()...).Credentials(context.Background(), "dev")
	needSignIn(t, err, ReasonExpired)
}

func TestLegacyAndPartialRegistrationsCannotRefresh(t *testing.T) {
	f := newFake(t)
	exp := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	// legacy: even with the fields, the profile has no token provider.
	h := home(t, legacyCfg)
	tok := validToken()
	tok.ExpiresAt, tok.RefreshToken, tok.ClientID, tok.ClientSecret = exp, needleRefresh, "c", needleClient
	writeCache(t, h, legacyProf(), tok)
	p := New(h, nil, f.opts()...)
	assert.False(t, p.Session(legacyProf()).SignedIn)
	// sso-session but no clientSecret: the SDK would refuse; so do we.
	h2 := home(t, sessionCfg)
	tok2 := validToken()
	tok2.ExpiresAt, tok2.RefreshToken, tok2.ClientID = exp, needleRefresh, "c"
	writeCache(t, h2, sessionProf(), tok2)
	st := New(h2, nil, f.opts()...).Session(sessionProf())
	assert.False(t, st.SignedIn)
	assert.Equal(t, ReasonExpired, st.Reason)
	assert.Empty(t, f.deviceCalls)
}

func TestMalformedCachesAreMissing(t *testing.T) {
	f := newFake(t)
	for name, body := range map[string]string{"not json": "{", "no token": `{"expiresAt":"2099-01-01T00:00:00Z"}`,
		"no expiry": `{"accessToken":"x"}`, "bad time": `{"accessToken":"x","expiresAt":"2099-01-01 00:00:00UTC"}`, "empty": ""} {
		h := home(t, sessionCfg)
		path := tokenPath(h, "corp")
		require.NoError(t, os.MkdirAll(filepathDir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		_, err := New(h, nil, f.opts()...).Credentials(context.Background(), "dev")
		needSignIn(t, err, ReasonMissing)
		_ = name
	}
}

// A remembered refusal is about one token: a different token in the cache (an
// `aws sso login` done elsewhere) voids it at once.
func TestRefusalIsForgottenWhenAnotherTokenAppears(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	h := home(t, sessionCfg)
	bad := validToken()
	bad.AccessToken = needleAccess + "_revoked"
	writeCache(t, h, sessionProf(), bad)
	p := New(h, nil, f.opts()...)
	_, err := p.Credentials(ctx, "dev")
	needSignIn(t, err, ReasonRefused)
	_, err = p.Credentials(ctx, "dev")
	needSignIn(t, err, ReasonRefused)
	assert.Equal(t, 1, f.hits(), "the refusal is remembered for the same token")
	writeCache(t, h, sessionProf(), validToken()) // the person signed in elsewhere
	_, err = p.Credentials(ctx, "dev")
	require.NoError(t, err)
}

func TestACacheWriteFailureIsNotUnreachableOrRemembered(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	f := newFake(t)
	h := home(t, sessionCfg)
	tok := validToken()
	tok.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	tok.RefreshToken, tok.ClientID, tok.ClientSecret = needleRefresh, "cid-1", needleClient
	path := writeCache(t, h, sessionProf(), tok)
	dir := filepath.Dir(path)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	p := New(h, nil, f.opts()...)
	_, err := p.Credentials(context.Background(), "dev")
	require.ErrorIs(t, err, ErrCacheWrite)
	assert.NotErrorIs(t, err, ErrUnreachable)
	noLeak(t, "cache write", err.Error())
	require.NoError(t, os.Chmod(dir, 0o700))
	_, err = p.Credentials(context.Background(), "dev")
	assert.NotErrorIs(t, err, ErrCacheWrite, "not remembered")
}

func TestACancelledCallerNeverWaitsOnAnotherCallsResolution(t *testing.T) {
	hang := make(chan struct{})
	slow := newFake(t)
	slow.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-hang })
	t.Cleanup(func() { close(hang) })
	h := home(t, sessionCfg)
	writeCache(t, h, sessionProf(), validToken())
	p := New(h, nil, append(slow.opts(), WithCallTimeout(2*time.Second))...)
	go func() { _, _ = p.Credentials(context.Background(), "dev") }()
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := p.Credentials(ctx, "dev")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second)
}

func TestTestEndpointsMustBeLoopback(t *testing.T) {
	h := home(t, sessionCfg)
	writeCache(t, h, sessionProf(), validToken())
	for _, bad := range [][2]string{{"https://evil.example", "https://evil.example"}, {"http://127.0.0.1:9", "http://169.254.169.254"}, {"", ""}} {
		p := New(h, nil, WithTestEndpoints(bad[0], bad[1]))
		_, err := p.Credentials(context.Background(), "dev")
		require.Error(t, err, "%v", bad)
		assert.Contains(t, err.Error(), "loopback")
		assert.Error(t, p.DeviceLogin(context.Background(), "dev", func(Prompt) { t.Fatal("no prompt") }))
	}
	p := New("", nil, WithTestEndpoints("http://127.0.0.1:9", "http://localhost:9"))
	assert.NoError(t, p.optErr)
}

func TestAnEmptyPortalAnswerIsAnErrorNotACrash(t *testing.T) {
	f := newFake(t)
	f.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{}")) })
	h := home(t, sessionCfg)
	writeCache(t, h, sessionProf(), validToken())
	_, err := New(h, nil, f.opts()...).Credentials(context.Background(), "dev")
	var se *ServiceError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, "MalformedResponse", se.Code)
}
