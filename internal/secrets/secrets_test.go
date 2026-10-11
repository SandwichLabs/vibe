package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

const testPW = "hunter2-NEEDLE-pw"

// fakeKeyring is an in-memory KeyringBackend; block makes every call hang.
type fakeKeyring struct {
	mu    sync.Mutex
	m     map[string]string
	block chan struct{}
	err   error
}

func newFake() *fakeKeyring { return &fakeKeyring{m: map[string]string{}} }

func (f *fakeKeyring) wait() {
	if f.block != nil {
		<-f.block
	}
}
func (f *fakeKeyring) Get(s, u string) (string, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.m[s+"|"+u]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}
func (f *fakeKeyring) Set(s, u, p string) error {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.m[s+"|"+u] = p
	return nil
}
func (f *fakeKeyring) Delete(s, u string) error {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.m[s+"|"+u]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.m, s+"|"+u)
	return nil
}

func newResolver(f *fakeKeyring, env map[string]string) *Resolver {
	return NewResolver(
		WithKeychain(NewKeyringStore(WithKeyringBackend(f), WithKeyringService("vibe-test"), WithKeyringTimeout(200*time.Millisecond))),
		WithEnv(func(k string) string { return env[k] }),
		WithResolverLogger(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))),
	)
}

var (
	ref  = WorkspaceRef("ws-1", "prod_pg")
	bind = Binding{Fingerprint: "fp-a", Server: "db.internal:5432"}
)

func TestRefValidate(t *testing.T) {
	assert.NoError(t, ref.Validate())
	assert.NoError(t, UserRef("md").Validate())
	assert.NoError(t, AppRef("llm-anthropic").Validate())
	assert.Error(t, Ref{Scope: ScopeWorkspace, Name: "x"}.Validate(), "workspace needs owner")
	assert.Error(t, Ref{Scope: ScopeUser, Owner: "o", Name: "x"}.Validate())
	assert.Error(t, Ref{Scope: "nope", Name: "x"}.Validate())
	assert.Error(t, Ref{Scope: ScopeApp, Name: "a/b"}.Validate())
	assert.Equal(t, "workspace/ws-1/prod_pg", ref.Account())
	assert.Equal(t, "user//md", UserRef("md").Account())
}

func TestKeyringRoundTripAndNotFound(t *testing.T) {
	f := newFake()
	s := NewKeyringStore(WithKeyringBackend(f), WithKeyringService("svc"))
	ctx := context.Background()

	_, err := s.Get(ctx, ref)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, s.Set(ctx, ref, []byte("v1")))
	got, err := s.Get(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))
	assert.Contains(t, f.m, "svc|workspace/ws-1/prod_pg")
	require.NoError(t, s.Delete(ctx, ref))
	require.NoError(t, s.Delete(ctx, ref), "deleting a missing item is not an error")
	_, err = s.Get(ctx, ref)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestKeyringServiceFromEnv(t *testing.T) {
	t.Setenv("VIBE_KEYRING_SERVICE", "vibe-envtest")
	assert.Equal(t, "vibe-envtest", NewKeyringStore().Service())
	t.Setenv("VIBE_KEYRING_SERVICE", "")
	assert.Equal(t, "vibe", NewKeyringStore().Service())
}

func TestKeyringTimeoutIsUnavailable(t *testing.T) {
	f := newFake()
	f.block = make(chan struct{})
	defer close(f.block)
	s := NewKeyringStore(WithKeyringBackend(f), WithKeyringTimeout(50*time.Millisecond))

	start := time.Now()
	_, err := s.Get(context.Background(), ref)
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Contains(t, err.Error(), "keychain locked or not responding")
	assert.Less(t, time.Since(start), 2*time.Second)

	require.ErrorIs(t, s.Set(context.Background(), ref, []byte("x")), ErrUnavailable)
	require.ErrorIs(t, s.Delete(context.Background(), ref), ErrUnavailable)
	assert.Error(t, s.Probe(context.Background()))
}

func TestKeyringBackendErrorIsUnavailable(t *testing.T) {
	f := newFake()
	f.err = errors.New("dbus: no such bus")
	s := NewKeyringStore(WithKeyringBackend(f))
	_, err := s.Get(context.Background(), ref)
	require.ErrorIs(t, err, ErrUnavailable)
	assert.NotErrorIs(t, err, ErrNotFound)
}

func TestKeyringCallerCancel(t *testing.T) {
	f := newFake()
	f.block = make(chan struct{})
	defer close(f.block)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewKeyringStore(WithKeyringBackend(f)).Get(ctx, ref)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEnvMapping(t *testing.T) {
	env := map[string]string{
		"PG_PROD_USER": "rosa", "PG_PROD_PASSWORD": "pw-secret-1",
		"S3_KEY_ID": "AKIA123", "S3_SECRET": "s3-secret-value",
		"MOTHERDUCK_TOKEN": "md-token-xyz",
	}
	r := newResolver(newFake(), env)
	ctx := context.Background()

	c, err := r.Resolve(ctx, ref, "env:PG_PROD", bind)
	require.NoError(t, err)
	assert.Equal(t, Credential{User: "rosa", Password: "pw-secret-1"}, c)

	c, err = r.Resolve(ctx, ref, "env:S3", bind)
	require.NoError(t, err)
	assert.Equal(t, "AKIA123", c.KeyID)
	assert.Equal(t, "s3-secret-value", c.Secret)

	c, err = r.Resolve(ctx, ref, "env:MOTHERDUCK", bind)
	require.NoError(t, err)
	assert.Equal(t, "md-token-xyz", c.Token, "env:MOTHERDUCK reads MOTHERDUCK_TOKEN")

	_, err = r.Resolve(ctx, ref, "env:NOTHING_SET", bind)
	var nc *ErrNeedsCredentials
	require.ErrorAs(t, err, &nc)
	assert.Contains(t, err.Error(), "Environment variables")

	assert.Equal(t, []string{"P_USER", "P_PASSWORD", "P_TOKEN", "P_KEY_ID", "P_SECRET"}, EnvVarNames("P"))
	// Env credentials are read-only.
	require.ErrorIs(t, r.SetCredentials(ctx, ref, "env:PG_PROD", Credential{User: "x"}, bind, false), ErrReadOnly)
	// Env resolution registers secrets with the redactor.
	assert.NotContains(t, r.Redactor().Redact("boom pw-secret-1 boom"), "pw-secret-1")
}

func TestParseMethod(t *testing.T) {
	for in, want := range map[string]string{"": "keychain", "keychain": "keychain", "prompt": "prompt", "env:X": "env"} {
		k, _, err := ParseMethod(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, k)
	}
	_, p, _ := ParseMethod("env:PG")
	assert.Equal(t, "PG", p)
	for _, bad := range []string{"file", "env:", "plain"} {
		_, _, err := ParseMethod(bad)
		assert.Error(t, err, bad)
	}
}

func TestKeychainFlowAndWriteMerge(t *testing.T) {
	f := newFake()
	r := newResolver(f, nil)
	ctx := context.Background()

	_, err := r.Resolve(ctx, ref, "keychain", bind)
	var nc *ErrNeedsCredentials
	require.ErrorAs(t, err, &nc)

	require.NoError(t, r.SetCredentials(ctx, ref, "keychain", Credential{User: "rosa", Password: testPW}, bind, false))
	c, err := r.Resolve(ctx, ref, "", bind)
	require.NoError(t, err)
	assert.Equal(t, "rosa", c.User)
	assert.Equal(t, testPW, c.Password)
	assert.Equal(t, "db.internal:5432", c.Server)

	// Changing only the username keeps the write-only password.
	require.NoError(t, r.SetCredentials(ctx, ref, "keychain", Credential{User: "rosa2"}, bind, false))
	c, err = r.Resolve(ctx, ref, "keychain", bind)
	require.NoError(t, err)
	assert.Equal(t, "rosa2", c.User)
	assert.Equal(t, testPW, c.Password)

	require.NoError(t, r.ClearCredentials(ctx, ref))
	_, err = r.Resolve(ctx, ref, "keychain", bind)
	require.ErrorAs(t, err, &nc)
}

func TestFingerprintMismatchHandsOutNoSecret(t *testing.T) {
	r := newResolver(newFake(), nil)
	ctx := context.Background()
	require.NoError(t, r.SetCredentials(ctx, ref, "keychain", Credential{User: "rosa", Password: testPW}, bind, false))

	moved := Binding{Fingerprint: "fp-b", Server: "db-new.internal:5432"}
	c, err := r.Resolve(ctx, ref, "keychain", moved)
	var fc *ErrFingerprintChanged
	require.ErrorAs(t, err, &fc)
	assert.Equal(t, "db.internal:5432", fc.Old)
	assert.Equal(t, "db-new.internal:5432", fc.New)
	assert.Zero(t, c, "no part of the credential is returned")
	assert.NotContains(t, err.Error(), testPW)
	assert.Contains(t, err.Error(), "the server changed since you signed in (db.internal:5432 to db-new.internal:5432)")

	// Re-saving after a server change must not carry the old password over.
	require.NoError(t, r.SetCredentials(ctx, ref, "keychain", Credential{User: "rosa"}, moved, false))
	c, err = r.Resolve(ctx, ref, "keychain", moved)
	require.NoError(t, err)
	assert.Empty(t, c.Password)

	// An entry saved before fingerprints existed fails closed.
	raw, _ := Credential{User: "u", Password: testPW}.Marshal()
	require.NoError(t, r.Keychain().Set(ctx, ref, raw))
	_, err = r.Resolve(ctx, ref, "keychain", bind)
	assert.ErrorAs(t, err, &fc)
}

func TestKeychainUnavailablePropagates(t *testing.T) {
	f := newFake()
	f.err = errors.New("no secret service")
	r := newResolver(f, nil)
	_, err := r.Resolve(context.Background(), ref, "keychain", bind)
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Error(t, r.SetCredentials(context.Background(), ref, "keychain", Credential{Password: testPW}, bind, false))
}

func TestPromptIsMemoryOnlyUnlessRemembered(t *testing.T) {
	f := newFake()
	r := newResolver(f, nil)
	ctx := context.Background()

	require.NoError(t, r.SetCredentials(ctx, ref, "prompt", Credential{User: "rosa", Password: testPW}, bind, false))
	c, err := r.Resolve(ctx, ref, "prompt", bind)
	require.NoError(t, err)
	assert.Equal(t, testPW, c.Password)
	assert.Empty(t, f.m, "nothing reached the keychain (D9: remember is off by default)")

	require.NoError(t, r.RememberPrompted(ctx, ref))
	assert.Len(t, f.m, 1)
	_, err = r.Resolve(ctx, ref, "keychain", bind)
	require.NoError(t, err)

	// remember at set time
	ref2 := WorkspaceRef("ws-1", "other")
	require.NoError(t, r.SetCredentials(ctx, ref2, "prompt", Credential{Password: testPW + "2"}, bind, true))
	assert.Len(t, f.m, 2)

	require.NoError(t, r.ClearCredentials(ctx, ref))
	_, err = r.Resolve(ctx, ref, "prompt", bind)
	assert.Error(t, err)
	assert.Error(t, r.RememberPrompted(ctx, ref))
}

func TestStatus(t *testing.T) {
	f := newFake()
	r := newResolver(f, map[string]string{"PG_USER": "envrosa", "PG_PASSWORD": "envpw-secret"})
	ctx := context.Background()

	st := r.Status(ctx, ref, "keychain", bind)
	assert.Equal(t, CredentialStatus{Method: "keychain", FingerprintMatches: true, Available: true}, st)

	require.NoError(t, r.SetCredentials(ctx, ref, "keychain", Credential{User: "rosa", KeyID: "AKIA9", Password: testPW}, bind, false))
	st = r.Status(ctx, ref, "keychain", bind)
	assert.True(t, st.SecretSet)
	assert.True(t, st.FingerprintMatches)
	assert.Equal(t, "rosa", st.Username)
	assert.Equal(t, "AKIA9", st.KeyID)

	st = r.Status(ctx, ref, "keychain", Binding{Fingerprint: "other", Server: "elsewhere:1"})
	assert.False(t, st.FingerprintMatches)
	assert.Contains(t, st.Detail, "server changed")

	st = r.Status(ctx, ref, "env:PG", bind)
	assert.True(t, st.SecretSet)
	assert.Equal(t, "envrosa", st.Username)
	require.Len(t, st.Env, 5)
	assert.Equal(t, EnvVarStatus{Name: "PG_USER", Set: true}, st.Env[0])
	assert.Equal(t, EnvVarStatus{Name: "PG_TOKEN", Set: false}, st.Env[2])

	f.err = errors.New("bus gone")
	st = r.Status(ctx, ref, "keychain", bind)
	assert.False(t, st.Available)
	assert.Contains(t, st.Detail, "environment variables")

	// No secret anywhere in any status.
	for _, s := range []CredentialStatus{st, r.Status(ctx, ref, "env:PG", bind)} {
		assert.NotContains(t, fmt.Sprintf("%+v", s), testPW)
		assert.NotContains(t, fmt.Sprintf("%+v", s), "envpw-secret")
	}
}

func TestProbe(t *testing.T) {
	f := newFake()
	r := newResolver(f, nil)
	p := r.ProbeKeychain(context.Background())
	assert.True(t, p.Available)
	assert.NotEmpty(t, p.Backend)

	f.err = errors.New("no bus")
	p = r.ProbeKeychain(context.Background())
	assert.False(t, p.Available)
	assert.Contains(t, p.Detail, "environment variables")
}

func TestCredentialPrintsSafely(t *testing.T) {
	c := Credential{User: "rosa", Password: testPW, Token: "tok-NEEDLE-1", Secret: "sec-NEEDLE-2"}
	for _, s := range []string{fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c), c.String()} {
		assert.NotContains(t, s, "NEEDLE")
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "cred", c)
	assert.NotContains(t, buf.String(), "NEEDLE")
	assert.Contains(t, buf.String(), "rosa")
}

func TestParseCredentialErrorDoesNotQuote(t *testing.T) {
	_, err := ParseCredential([]byte(`{"password": "oops-NEEDLE`))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "NEEDLE")
}

func TestRedactor(t *testing.T) {
	r := NewRedactor()
	r.Add("hunter2-NEEDLE", "tok-NEEDLE-abcdef", "ab") // "ab" too short, ignored

	t.Run("postgres connect error keeps the user", func(t *testing.T) {
		got := r.Redact("Unable to connect to Postgres at user='rosa' password='wrong-pw' host='db' port=5432")
		assert.Contains(t, got, "user='rosa'")
		assert.Contains(t, got, "password='***'")
		assert.NotContains(t, got, "wrong-pw")
	})
	t.Run("quote forms", func(t *testing.T) {
		for _, in := range []string{`password=pw1x`, `PASSWORD = "pw1x"`, `token=pw1x`, `secret='pw1x'`, `password='it''s pw1x'`, `password='a\'b pw1x'`} {
			got := r.Redact("fail: " + in + " end")
			assert.NotContains(t, got, "pw1x", in)
			assert.NotContains(t, got, "it''s", in)
			assert.Contains(t, got, "end", in)
		}
	})
	t.Run("multi secret and longest first", func(t *testing.T) {
		got := r.Redact("a hunter2-NEEDLE b tok-NEEDLE-abcdef c hunter2-NEEDLE")
		assert.Equal(t, "a *** b *** c ***", got)
		r2 := NewRedactor()
		r2.Add("secretvalue", "secretvalue-longer")
		assert.Equal(t, "x *** y", r2.Redact("x secretvalue-longer y"))
	})
	t.Run("short values are not replaced", func(t *testing.T) {
		assert.Equal(t, "about the abc", r.Redact("about the abc"))
	})
	t.Run("plain text untouched", func(t *testing.T) {
		in := "Catalog Error: Table with name foo does not exist! Did you mean primary key: id"
		assert.Equal(t, in, r.Redact(in))
	})
	t.Run("nil redactor still applies patterns", func(t *testing.T) {
		var nr *Redactor
		assert.Equal(t, "password='***'", nr.Redact("password='x'"))
		nr.Add("whatever")
	})
	t.Run("redact error", func(t *testing.T) {
		assert.Equal(t, "", r.RedactError(nil))
		assert.NotContains(t, r.RedactError(errors.New("bad hunter2-NEEDLE")), "NEEDLE")
	})
}

func TestRedactingHandler(t *testing.T) {
	r := NewRedactor()
	r.Add("hunter2-NEEDLE")
	var buf bytes.Buffer
	log := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}), r))

	log.Info("connect failed for hunter2-NEEDLE", "err", errors.New("password='hunter2-NEEDLE' rejected"),
		"s", "x hunter2-NEEDLE", "n", 3,
		slog.Group("g", slog.String("inner", "hunter2-NEEDLE")))
	log.With("pre", "hunter2-NEEDLE").WithGroup("grp").Debug("later", "k", fmt.Sprint("v ", "hunter2-NEEDLE"))
	log.Info("with valuer", "cred", Credential{Password: "hunter2-NEEDLE", User: "rosa"})

	out := buf.String()
	assert.NotContains(t, out, "NEEDLE")
	assert.Contains(t, out, `"n":3`)
	assert.Contains(t, out, "rosa")
	assert.True(t, strings.Count(out, "\n") == 3)
}

func TestMockRecordsWithoutValues(t *testing.T) {
	m := &Mock{}
	ctx := context.Background()
	require.NoError(t, m.Set(ctx, ref, []byte(testPW)))
	_, _ = m.Get(ctx, ref)
	require.NoError(t, m.Delete(ctx, ref))
	assert.Equal(t, []string{"set workspace/ws-1/prod_pg", "get workspace/ws-1/prod_pg", "delete workspace/ws-1/prod_pg"}, m.Calls)

	boom := errors.New("boom")
	m2 := &Mock{GetFn: func(context.Context, Ref) ([]byte, error) { return nil, boom }}
	_, err := m2.Get(ctx, ref)
	assert.ErrorIs(t, err, boom)
}

func TestResolverWithMockStore(t *testing.T) {
	m := &Mock{}
	r := NewResolver(WithKeychain(m))
	ctx := context.Background()
	require.NoError(t, r.SetCredentials(ctx, ref, "keychain", Credential{Password: testPW}, bind, false))
	_, err := r.Resolve(ctx, ref, "keychain", bind)
	require.NoError(t, err)
	assert.NotContains(t, strings.Join(m.Calls, ","), testPW)
}

type countingKeyring struct {
	*fakeKeyring
	mu    sync.Mutex
	calls int
}

func (c *countingKeyring) Get(s, u string) (string, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.fakeKeyring.Get(s, u)
}

func TestKeyringCircuitBreakerAndSingleFlight(t *testing.T) {
	f := newFake()
	f.block = make(chan struct{})
	cb := &countingKeyring{fakeKeyring: f}
	s := NewKeyringStore(WithKeyringBackend(cb), WithKeyringTimeout(40*time.Millisecond), WithKeyringCooldown(150*time.Millisecond))
	ctx := context.Background()

	// Concurrent Gets of one item share one backend call.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.Get(ctx, ref) }()
	}
	wg.Wait()
	cb.mu.Lock()
	assert.Equal(t, 1, cb.calls, "single flight")
	cb.mu.Unlock()

	// During the cooldown nothing new is started, for any operation.
	for i := 0; i < 10; i++ {
		_, err := s.Get(ctx, ref)
		require.ErrorIs(t, err, ErrUnavailable)
		require.ErrorIs(t, s.Set(ctx, ref, []byte("x")), ErrUnavailable)
	}
	cb.mu.Lock()
	assert.Equal(t, 1, cb.calls, "no goroutine growth while down")
	cb.mu.Unlock()

	// After the cooldown, with the keychain back, it works again.
	close(f.block)
	time.Sleep(200 * time.Millisecond)
	_, err := s.Get(ctx, ref)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSetDoesNotOverwriteBlindWhenKeychainIsDown(t *testing.T) {
	f := newFake()
	r := newResolver(f, nil)
	ctx := context.Background()
	require.NoError(t, r.SetCredentials(ctx, ref, "keychain", Credential{User: "rosa", Password: testPW}, bind, false))
	f.err = errors.New("bus gone")
	require.Error(t, r.SetCredentials(ctx, ref, "keychain", Credential{User: "x"}, bind, false))
	f.err = nil
	c, err := r.Resolve(ctx, ref, "keychain", bind)
	require.NoError(t, err)
	assert.Equal(t, testPW, c.Password, "untouched")
}

func TestClearCredentialField(t *testing.T) {
	r := newResolver(newFake(), nil)
	ctx := context.Background()
	require.NoError(t, r.SetCredentials(ctx, ref, "keychain", Credential{User: "rosa", KeyID: "AK", Password: testPW, Token: "tok-123456"}, bind, false))
	require.NoError(t, r.ClearCredentialField(ctx, ref, "keychain", FieldToken))
	require.NoError(t, r.ClearCredentialField(ctx, ref, "keychain", FieldUser))
	c, err := r.Resolve(ctx, ref, "keychain", bind)
	require.NoError(t, err)
	assert.Empty(t, c.Token)
	assert.Empty(t, c.User)
	assert.Equal(t, testPW, c.Password)
	assert.Error(t, r.ClearCredentialField(ctx, ref, "keychain", "nope"))
	assert.ErrorIs(t, r.ClearCredentialField(ctx, ref, "env:X", FieldUser), ErrReadOnly)
}

func TestKeyringLateAnswerEndsCooldown(t *testing.T) {
	f := newFake()
	f.block = make(chan struct{})
	s := NewKeyringStore(WithKeyringBackend(f), WithKeyringTimeout(30*time.Millisecond), WithKeyringCooldown(time.Minute))
	_, err := s.Get(context.Background(), ref)
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Contains(t, err.Error(), "try again in")
	_, err = s.Get(context.Background(), ref)
	require.ErrorIs(t, err, ErrUnavailable, "still cooling down")

	close(f.block) // the slow unlock finishes
	require.Eventually(t, func() bool {
		_, err := s.Get(context.Background(), ref)
		return errors.Is(err, ErrNotFound)
	}, time.Second, 10*time.Millisecond, "a late answer ends the fail-fast window")
}

func TestCallerCancelDoesNotStartCooldown(t *testing.T) {
	f := newFake()
	f.block = make(chan struct{})
	s := NewKeyringStore(WithKeyringBackend(f), WithKeyringTimeout(time.Minute), WithKeyringCooldown(time.Minute))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := s.Get(ctx, ref)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotErrorIs(t, err, ErrUnavailable)
	close(f.block)
	require.Eventually(t, func() bool {
		_, err := s.Get(context.Background(), ref)
		return errors.Is(err, ErrNotFound)
	}, time.Second, 10*time.Millisecond, "no cooldown was started by the caller's own deadline")
}

func TestGetAfterSetDoesNotJoinAStaleRead(t *testing.T) {
	f := newFake()
	gate := make(chan struct{})
	slow := &slowGetKeyring{fakeKeyring: f, gate: gate}
	s := NewKeyringStore(WithKeyringBackend(slow), WithKeyringTimeout(2*time.Second))
	ctx := context.Background()
	require.NoError(t, s.Set(ctx, ref, []byte("old")))

	oldRead := make(chan string, 1)
	go func() { b, _ := s.Get(ctx, ref); oldRead <- string(b) }()
	time.Sleep(50 * time.Millisecond) // the first Get is now parked in the backend
	require.NoError(t, s.Set(ctx, ref, []byte("new")))
	slow.allowFast()
	got, err := s.Get(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
	close(gate)
	assert.Equal(t, "old", <-oldRead)
}

type slowGetKeyring struct {
	*fakeKeyring
	gate chan struct{}
	mu   sync.Mutex
	fast bool
}

func (s *slowGetKeyring) allowFast() { s.mu.Lock(); s.fast = true; s.mu.Unlock() }
func (s *slowGetKeyring) Get(svc, u string) (string, error) {
	s.mu.Lock()
	fast := s.fast
	s.mu.Unlock()
	v, err := s.fakeKeyring.Get(svc, u) // value as of the call
	if !fast {
		<-s.gate
	}
	return v, err
}

func TestMotherDuckMissingCredentialHintNamesTheTokenVariable(t *testing.T) {
	r := newResolver(newFake(), nil)
	ctx := context.Background()
	md := Binding{Fingerprint: "fp", Type: "motherduck"}
	for _, method := range []string{"env:MOTHERDUCK", "keychain"} {
		_, err := r.Resolve(ctx, Ref{Scope: ScopeUser, Name: "md"}, method, md)
		require.Error(t, err, method)
		assert.Contains(t, err.Error(), "MOTHERDUCK_TOKEN", method)
		assert.NotContains(t, err.Error(), "MD_USER")
		assert.NotContains(t, err.Error(), "MD_PASSWORD")
	}
}
