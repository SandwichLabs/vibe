package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

// fakeClock is the wall clock the hub compares deadlines with. Moving it models
// both time passing and a laptop waking from sleep (the wall clock jumps while a
// monotonic timer would not have run).
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeSSO is a credential source shaped like the aws-sso method: each call
// issues a new token that ends ttl after the fake clock, a Fresh context is
// counted, and it can be made to fail or to stall (a portal that does not
// answer).
type fakeSSO struct {
	clock *fakeClock
	ttl   time.Duration

	mu     sync.Mutex
	issued int
	fresh  int
	calls  int
	err    error
	gate   chan struct{} // when set, a call waits for it to be closed
	hit    chan struct{} // when set, a call signals here before waiting
}

func (f *fakeSSO) fn(ctx context.Context) (secrets.Credential, error) {
	f.mu.Lock()
	f.calls++
	if secrets.IsFresh(ctx) {
		f.fresh++
	}
	gate, hit, err := f.gate, f.hit, f.err
	f.mu.Unlock()
	if hit != nil {
		select {
		case hit <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return secrets.Credential{}, ctx.Err()
		}
	}
	if err != nil {
		return secrets.Credential{}, err
	}
	f.mu.Lock()
	f.issued++
	n := f.issued
	f.mu.Unlock()
	return secrets.Credential{Token: fmt.Sprintf("tok%d", n), Expires: f.clock.now().Add(f.ttl)}, nil
}

func (f *fakeSSO) set(fn func(*fakeSSO)) {
	f.mu.Lock()
	fn(f)
	f.mu.Unlock()
}

func (f *fakeSSO) count() (calls, fresh int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.fresh
}

func signInNeeded() error {
	return &secrets.ErrNeedsCredentials{
		Method: "aws-sso:dev", Reason: "sign-in needed",
		SignIn: &secrets.SignInHint{Kind: secrets.MethodAWSSSO, Subject: "dev", Reason: "expired"},
	}
}

// recType is stubType that records every token SecretSteps is asked to write.
type recType struct {
	stubType
	mu   *sync.Mutex
	seen *[]string
}

func (r recType) SecretSteps(sp Spec, c secrets.Credential) ([]SecretStep, error) {
	r.mu.Lock()
	*r.seen = append(*r.seen, c.Token)
	r.mu.Unlock()
	return r.stubType.SecretSteps(sp, c)
}

func (r recType) tokens() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), *r.seen...)
}

type rotEnv struct {
	s     Session
	h     *hub
	clock *fakeClock
	sso   *fakeSSO
	rec   recType
	saw   *atomic.Bool
	bare  *atomic.Int32
	heard *rotHeard
}

type rotHeard struct {
	mu    sync.Mutex
	names []string
}

func (r *rotHeard) add(n string) {
	r.mu.Lock()
	r.names = append(r.names, n)
	r.mu.Unlock()
}

func (r *rotHeard) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.names...)
}

// newRotEnv opens a hub on a fake clock with an SSO-like storage connection
// "store" and a lake "lake" that reads with its secret, both connected.
func newRotEnv(t *testing.T) *rotEnv {
	t.Helper()
	clock := newFakeClock()
	heard := &rotHeard{}
	saw, bare := &atomic.Bool{}, &atomic.Int32{}
	rec := recType{stubType: stubType{ext: "json"}, mu: &sync.Mutex{}, seen: new([]string)}
	s := newTestSession(t, WithClock(clock.now), WithStatusListener(heard.add),
		WithConnectionTypes(rec, lakeyType{sawSecret: saw, bare: bare}))
	h, ok := s.(*hub)
	require.True(t, ok)
	e := &rotEnv{s: s, h: h, clock: clock, rec: rec, saw: saw, bare: bare, heard: heard,
		sso: &fakeSSO{clock: clock, ttl: time.Hour}}
	require.NoError(t, s.Register(Spec{Name: "store", Type: "stub", Credential: e.sso.fn}))
	require.NoError(t, s.Register(Spec{Name: "lake", Type: "lakey", Requires: []string{"store"}}))
	e.sso.set(func(f *fakeSSO) { f.calls, f.fresh = 0, 0 })
	s.Preflight(context.Background(), "store")
	require.NoError(t, s.Connect(context.Background(), "lake"))
	require.Equal(t, StatusOK, status(t, s, "store").Kind)
	require.Equal(t, 1, attachedCatalogs(t, s, "lake"))
	// Setup resolved a few times; count from the live secret, made from tok1.
	e.sso.set(func(f *fakeSSO) { f.calls, f.fresh, f.issued = 0, 0, 1 })
	rec.mu.Lock()
	*rec.seen = []string{"tok1"}
	rec.mu.Unlock()
	return e
}

func (e *rotEnv) touch(t *testing.T) error {
	t.Helper()
	return e.s.ConnectReferenced(context.Background(), "", "SELECT 1 FROM lake.main.t")
}

func (e *rotEnv) expiresAt() time.Time {
	e.h.mu.RLock()
	defer e.h.mu.RUnlock()
	return e.h.conns["store"].expires
}

func TestRotateFiresInsideTheWindowOnly(t *testing.T) {
	e := newRotEnv(t)
	first := e.expiresAt()
	require.Equal(t, e.clock.now().Add(time.Hour), first)

	e.clock.advance(54 * time.Minute) // deadline minus 6 minutes: not yet
	require.NoError(t, e.touch(t))
	calls, _ := e.sso.count()
	assert.Zero(t, calls, "no rotation before deadline minus 5 minutes")

	e.clock.advance(61 * time.Second) // deadline minus 4 minutes: due
	require.NoError(t, e.touch(t))
	calls, fresh := e.sso.count()
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, fresh, "a rotation asks for fresh keys, not the cached set")
	assert.Equal(t, []string{"tok1", "tok2"}, e.rec.tokens(), "the secret was rebuilt from the new keys")
	assert.Equal(t, 1, secretCount(t, e.s), "replaced in place, never absent or doubled")
	assert.Equal(t, 1, attachedCatalogs(t, e.s, "lake"), "the lake stays attached")
	assert.Equal(t, StatusOK, status(t, e.s, "store").Kind)
	assert.Equal(t, e.clock.now().Add(time.Hour), e.expiresAt(), "the new deadline is kept")

	require.NoError(t, e.touch(t))
	calls, _ = e.sso.count()
	assert.Equal(t, 1, calls, "a renewed secret is not due again")
	assert.Empty(t, e.heard.list(), "a rotation that worked changes no status")
}

func TestRotateAfterASuspend(t *testing.T) {
	e := newRotEnv(t)
	e.clock.advance(5 * time.Hour) // the lid was closed: the wall clock jumped, no timer ran
	require.NoError(t, e.touch(t))
	assert.Equal(t, []string{"tok1", "tok2"}, e.rec.tokens(), "the first statement after waking renews before it runs")
	assert.Equal(t, 1, attachedCatalogs(t, e.s, "lake"))
	assert.Equal(t, 1, secretCount(t, e.s))
}

func TestRotateWhenSignedOutFailsClosed(t *testing.T) {
	e := newRotEnv(t)
	e.sso.set(func(f *fakeSSO) { f.err = signInNeeded() })
	e.clock.advance(56 * time.Minute)

	err := e.touch(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AWS session for profile dev has expired")
	assert.Zero(t, secretCount(t, e.s), "the secret is dropped")
	assert.Zero(t, attachedCatalogs(t, e.s, "lake"), "the lake that read with it is detached")
	st := status(t, e.s, "store")
	assert.Equal(t, StatusNeedsCredentials, st.Kind)
	require.NotNil(t, st.SignIn)
	assert.Equal(t, "dev", st.SignIn.Subject)
	// The statement names the lake, so it was dialled once more and refused.
	assert.Equal(t, `needs the "store" connection: it needs credentials`, status(t, e.s, "lake").Message)
	assert.Equal(t, []string{"store"}, e.heard.list(), "the status listener hears of it")

	// And it stays closed: a later statement does not read anonymously.
	_, qerr := e.s.DB().Exec("SELECT * FROM lake.main.t")
	require.Error(t, qerr)
	require.Error(t, e.touch(t))
	assert.Zero(t, e.bare.Load())
}

func TestRotateTransientFailureKeepsWorkingKeysAndIsThrottled(t *testing.T) {
	e := newRotEnv(t)
	e.sso.set(func(f *fakeSSO) {
		f.err = &secrets.ErrMethodFailed{Kind: secrets.MethodAWSSSO, Msg: "couldn't reach AWS sign-in", Err: errors.New("down")}
	})
	e.clock.advance(56 * time.Minute)

	require.NoError(t, e.touch(t), "the keys still work: the statement goes on")
	calls, _ := e.sso.count()
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, secretCount(t, e.s))
	assert.Equal(t, 1, attachedCatalogs(t, e.s, "lake"))

	for i := 0; i < 20; i++ { // a portal that is down is asked once per 30 s, not per statement
		require.NoError(t, e.touch(t))
	}
	calls, _ = e.sso.count()
	assert.Equal(t, 1, calls)

	e.clock.advance(31 * time.Second)
	require.NoError(t, e.touch(t))
	calls, _ = e.sso.count()
	assert.Equal(t, 2, calls, "the next attempt waits for the interval")

	e.clock.advance(5 * time.Minute) // now the keys have ended: no more waiting, fail closed
	err := e.touch(t)
	require.Error(t, err)
	assert.Zero(t, secretCount(t, e.s))
	assert.Zero(t, attachedCatalogs(t, e.s, "lake"))
	assert.Equal(t, StatusError, status(t, e.s, "store").Kind)
	assert.Equal(t, []string{"store"}, e.heard.list())
}

func TestRotateNeverRunsForCredentialsWithoutADeadline(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int32
	s := newTestSession(t, WithClock(clock.now), WithConnectionTypes(stubType{ext: "json"}))
	require.NoError(t, s.Register(Spec{Name: "store", Type: "stub", Credential: func(context.Context) (secrets.Credential, error) {
		calls.Add(1)
		return secrets.Credential{Token: "static"}, nil // a keychain or env credential has no Expires
	}}))
	s.Preflight(context.Background(), "store")
	require.Equal(t, StatusOK, status(t, s, "store").Kind)
	before := calls.Load()
	clock.advance(1000 * time.Hour)
	require.NoError(t, s.ConnectReferenced(context.Background(), "", "SELECT 1"))
	assert.Equal(t, before, calls.Load())
	assert.Equal(t, 1, secretCount(t, s))
}

// The network part of a rotation holds no lock that anything else needs.
func TestRotateDoesNotHoldAdminMuAcrossTheNetworkCall(t *testing.T) {
	e := newRotEnv(t)
	gate, hit := make(chan struct{}), make(chan struct{}, 1)
	e.sso.set(func(f *fakeSSO) { f.gate, f.hit = gate, hit })
	e.clock.advance(56 * time.Minute)

	done := make(chan error, 1)
	go func() { done <- e.touch(t) }()
	<-hit // the portal call is in flight and stalled

	other := make(chan error, 1)
	go func() {
		ctx := context.Background()
		if err := e.s.Register(Spec{Name: "other", Type: "lakey", Requires: []string{"store"}}); err != nil {
			other <- err
			return
		}
		other <- e.s.Connect(ctx, "other") // an ATTACH needs adminMu
	}()
	select {
	case err := <-other:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("an ATTACH of another connection waited on the stalled portal")
	}
	assert.Equal(t, StatusOK, status(t, e.s, "other").Kind)
	close(gate)
	require.NoError(t, <-done)
	assert.Equal(t, []string{"tok1", "tok2"}, e.rec.tokens())
}

// Callers that arrive while a rotation is in flight wait for it: one portal
// call, not one each.
func TestRotateConcurrentCallersShareOneRotation(t *testing.T) {
	e := newRotEnv(t)
	gate, hit := make(chan struct{}), make(chan struct{}, 1)
	e.sso.set(func(f *fakeSSO) { f.gate, f.hit = gate, hit })
	e.clock.advance(56 * time.Minute)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- e.touch(t) }()
	}
	<-hit
	time.Sleep(50 * time.Millisecond) // let the others find the rotation in flight
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	calls, _ := e.sso.count()
	assert.Equal(t, 1, calls)
	assert.Equal(t, []string{"tok1", "tok2"}, e.rec.tokens())
}

// What a rotation fetched is discarded when the entry it was fetched for is
// gone, replaced or preflighted meanwhile.
func TestRotateResultOfAStaleEntryIsDiscarded(t *testing.T) {
	for _, how := range []string{"unregister and register again", "preflight"} {
		t.Run(how, func(t *testing.T) {
			e := newRotEnv(t)
			ctx := context.Background()
			gate, hit := make(chan struct{}), make(chan struct{}, 1)
			e.sso.set(func(f *fakeSSO) { f.gate, f.hit = gate, hit })
			e.clock.advance(56 * time.Minute)

			done := make(chan error, 1)
			go func() { done <- e.touch(t) }()
			<-hit

			// While the portal stalls, the definition changes: the new store has
			// its own credential, and is connected with it.
			newCred := &fakeSSO{clock: e.clock, ttl: 2 * time.Hour}
			switch how {
			case "preflight":
				e.sso.set(func(f *fakeSSO) { f.gate = nil })
				e.s.Preflight(ctx, "store")
			default:
				require.NoError(t, e.s.Detach(ctx, "store"))
				require.NoError(t, e.s.Register(Spec{Name: "store", Type: "stub", Credential: newCred.fn}))
				e.s.Preflight(ctx, "store")
			}
			require.Equal(t, StatusOK, status(t, e.s, "store").Kind)
			before := e.rec.tokens()

			e.sso.set(func(f *fakeSSO) { f.gate = nil })
			close(gate)
			<-done
			assert.Equal(t, before, e.rec.tokens(), "the stale rotation wrote nothing")
			assert.Equal(t, StatusOK, status(t, e.s, "store").Kind)
			assert.Equal(t, 1, secretCount(t, e.s))
		})
	}
}

// A stale rotation that failed must not sign the new entry out either.
func TestRotateStaleFailureLeavesTheNewEntryAlone(t *testing.T) {
	e := newRotEnv(t)
	ctx := context.Background()
	gate, hit := make(chan struct{}), make(chan struct{}, 1)
	e.sso.set(func(f *fakeSSO) { f.gate, f.hit, f.err = gate, hit, signInNeeded() })
	e.clock.advance(56 * time.Minute)
	done := make(chan error, 1)
	go func() { done <- e.touch(t) }()
	<-hit

	good := &fakeSSO{clock: e.clock, ttl: 2 * time.Hour}
	require.NoError(t, e.s.Detach(ctx, "store"))
	require.NoError(t, e.s.Register(Spec{Name: "store", Type: "stub", Credential: good.fn}))
	e.s.Preflight(ctx, "store")
	close(gate)
	require.NoError(t, <-done)
	assert.Equal(t, StatusOK, status(t, e.s, "store").Kind)
	assert.Equal(t, 1, secretCount(t, e.s))
	assert.Empty(t, e.heard.list())
}

// An expired-token answer from storage starts one rotation, and a second one
// right behind it is throttled.
func TestExpiredTokenReadForcesOneRotation(t *testing.T) {
	e := newRotEnv(t)
	raw := errors.New(`HTTP Error: HTTP GET error reading 's3://lake/a.parquet' in region 'us-east-1' (HTTP 403 Forbidden)` +
		"\n<Error><Code>ExpiredToken</Code><Message>The provided token has expired.</Message></Error>")
	require.True(t, IsExpiredToken(raw.Error()))

	err := e.h.readErr(context.Background(), "failed to run query", raw)
	require.Error(t, err)
	calls, fresh := e.sso.count()
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, fresh)
	assert.Equal(t, []string{"tok1", "tok2"}, e.rec.tokens())

	_ = e.h.readErr(context.Background(), "failed to run query", raw)
	calls, _ = e.sso.count()
	assert.Equal(t, 1, calls, "rate-limited: one forced rotation per 30 s")

	e.clock.advance(31 * time.Second)
	e.sso.set(func(f *fakeSSO) { f.err = signInNeeded() })
	_ = e.h.readErr(context.Background(), "failed to run query", raw)
	assert.Equal(t, StatusNeedsCredentials, status(t, e.s, "store").Kind, "a rotation forced by the server's answer fails closed")
	assert.Zero(t, attachedCatalogs(t, e.s, "lake"))
}

func TestIsExpiredToken(t *testing.T) {
	for msg, want := range map[string]bool{
		"HTTP Error: HTTP GET error reading 'u' in region 'r' (HTTP 403 Forbidden)\n<Code>ExpiredToken</Code>":        true,
		"HTTP Error: ... (HTTP 403 Forbidden)\nInvalidTokenId: The security token included in the request is invalid": true,
		"HTTP Error: ... (HTTP 403 Forbidden) the security token included in the request is expired":                  true,
		"HTTP Error: ... (HTTP 403 Forbidden)\nSignatureDoesNotMatch":                                                 false,
		"HTTP Error: ... (HTTP 403 Forbidden)\nAccessDenied":                                                          false,
		"ExpiredToken without a status":                        false,
		"HTTP Error: ... (HTTP 404 Not Found)\nInvalidTokenId": false,
		// A statement's own text is not the store's answer.
		`Catalog Error: Table with name "403 ExpiredToken" does not exist!` + "\nLINE 1: SELECT * FROM \"403 ExpiredToken\"": false,
		"Binder Error: ...\nLINE 1: SELECT '(HTTP 403 Forbidden) ExpiredToken'":                                              false,
	} {
		assert.Equal(t, want, IsExpiredToken(msg), msg)
	}
	kind, _, _ := classifyAttachError(errors.New("(HTTP 403 Forbidden): ExpiredToken"), func(s string) string { return s })
	assert.Equal(t, StatusNeedsCredentials, kind)
	assert.Equal(t, "s3://b/x.parquet", errorURL("HTTP GET error reading 's3://b/x.parquet' in region 'r'"))
	assert.Empty(t, errorURL("nothing"))
	assert.True(t, coversURL("s3://b/p/", "http://127.0.0.1:9000/b/p/x.csv"))
	assert.True(t, coversURL("s3://b/", "https://b.s3.us-east-1.amazonaws.com/x.csv"))
	assert.False(t, coversURL("s3://b/", "http://127.0.0.1:9000/other/x.csv"))
	assert.False(t, coversURL("s3://b/p/", "https://b.s3.amazonaws.com/q/x.csv"))
}

// A statement that merely mentions 403 ExpiredToken is not the store's answer,
// and a forced rotation that fails for a transient reason keeps working keys.
func TestExpiredTokenTextInAStatementDoesNothingAndTransientForcedKeepsKeys(t *testing.T) {
	e := newRotEnv(t)
	ctx := context.Background()
	e.sso.set(func(f *fakeSSO) {
		f.err = &secrets.ErrMethodFailed{Kind: secrets.MethodAWSSSO, Msg: "down", Err: errors.New("down")}
	})

	_, err := e.s.Query(ctx, `SELECT * FROM "403 ExpiredToken"`)
	require.Error(t, err)
	calls, _ := e.sso.count()
	assert.Zero(t, calls, "no rotation for text a statement carries")

	raw := errors.New("HTTP Error: HTTP GET error reading 's3://x/a' in region 'r' (HTTP 403 Forbidden)\n<Code>ExpiredToken</Code>")
	_ = e.h.readErr(ctx, "failed", raw) // a real answer, but the portal hiccups: the keys are valid for an hour
	calls, _ = e.sso.count()
	assert.Equal(t, 1, calls)
	assert.Equal(t, StatusOK, status(t, e.s, "store").Kind)
	assert.Equal(t, 1, attachedCatalogs(t, e.s, "lake"))
	assert.Equal(t, 1, secretCount(t, e.s))
	assert.Empty(t, e.heard.list())
}

// An expired-token answer rotates the connection whose scope covers the URL in
// the error, not the others.
func TestExpiredTokenRotatesOnlyTheCoveringConnection(t *testing.T) {
	clock := newFakeClock()
	rec := recType{stubType: stubType{ext: "json"}, mu: &sync.Mutex{}, seen: new([]string)}
	s := newTestSession(t, WithClock(clock.now), WithConnectionTypes(rec))
	h, _ := s.(*hub)
	a := &fakeSSO{clock: clock, ttl: time.Hour}
	b := &fakeSSO{clock: clock, ttl: time.Hour}
	require.NoError(t, s.Register(Spec{Name: "sa", Type: "stub", Params: map[string]string{"scope": "s3://a/"}, Credential: a.fn}))
	require.NoError(t, s.Register(Spec{Name: "sb", Type: "stub", Params: map[string]string{"scope": "s3://b/"}, Credential: b.fn}))
	s.Preflight(context.Background(), "sa")
	s.Preflight(context.Background(), "sb")
	a.set(func(f *fakeSSO) { f.calls = 0 })
	b.set(func(f *fakeSSO) { f.calls = 0 })
	raw := errors.New("HTTP Error: HTTP GET error reading 's3://a/x.parquet' in region 'r' (HTTP 403 Forbidden)\nExpiredToken")
	_ = h.readErr(context.Background(), "failed", raw)
	ca, _ := a.count()
	cb, _ := b.count()
	assert.Equal(t, 1, ca)
	assert.Zero(t, cb)
	// No URL in the message: every secret, rate-limited.
	clock.advance(31 * time.Second)
	_ = h.readErr(context.Background(), "failed", errors.New("(HTTP 403 Forbidden) ExpiredToken"))
	ca, _ = a.count()
	cb, _ = b.count()
	assert.Equal(t, 2, ca)
	assert.Equal(t, 1, cb)
}

// Ended keys whose sign-out is refused (a dependent will not detach) are not
// retried on every statement.
func TestRotateRefusedSignOutKeepsTheInterval(t *testing.T) {
	e := newRotEnv(t)
	e.sso.set(func(f *fakeSSO) { f.err = signInNeeded() })
	_, err := e.s.DB().Exec("DETACH lake") // behind the hub's back: its DETACH will fail
	require.NoError(t, err)
	e.clock.advance(2 * time.Hour) // the keys have ended
	for i := 0; i < 10; i++ {
		_ = e.touch(t)
	}
	calls, _ := e.sso.count()
	assert.Equal(t, 1, calls)
	e.clock.advance(31 * time.Second)
	_ = e.touch(t)
	calls, _ = e.sso.count()
	assert.Equal(t, 2, calls)
}

// A sign-in made elsewhere (`aws sso login`) revives a signed-out storage
// connection on the next statement, at most once per interval, so a long-lived
// `vibe mcp serve` needs no restart (#131 step 7 review F2).
func TestSignedOutConnectionIsRevivedWhenTheSessionReturns(t *testing.T) {
	e := newRotEnv(t)
	e.sso.set(func(f *fakeSSO) { f.err = signInNeeded() })
	e.clock.advance(56 * time.Minute)
	require.Error(t, e.touch(t))
	require.Equal(t, StatusNeedsCredentials, status(t, e.s, "store").Kind)
	require.Zero(t, attachedCatalogs(t, e.s, "lake"))
	e.heard.mu.Lock()
	e.heard.names = nil
	e.heard.mu.Unlock()

	// Still signed out: the first statement after the sign-out tried once; a
	// second inside the interval does not look again.
	e.sso.set(func(f *fakeSSO) { f.calls = 0 })
	_ = e.touch(t)
	calls, _ := e.sso.count()
	assert.Zero(t, calls, "no second look within 30 s")

	// The person signs in elsewhere; the interval has passed.
	e.sso.set(func(f *fakeSSO) { f.err = nil })
	e.clock.advance(31 * time.Second)
	require.NoError(t, e.touch(t))
	assert.Equal(t, StatusOK, status(t, e.s, "store").Kind, "the storage connection is ready again")
	assert.Equal(t, 1, attachedCatalogs(t, e.s, "lake"), "and its lake follows")
	assert.Contains(t, e.heard.list(), "store", "the status listener hears of it")
}

func TestOnlyAnSSOSignOutIsRevived(t *testing.T) {
	e := newRotEnv(t)
	e.sso.set(func(f *fakeSSO) { f.err = errors.New("boom"); f.calls = 0 })
	require.NoError(t, e.s.Register(Spec{Name: "other", Type: "stub", Credential: func(context.Context) (secrets.Credential, error) {
		return secrets.Credential{}, &secrets.ErrNeedsCredentials{Method: "keychain", Reason: "none"}
	}}))
	e.s.Preflight(context.Background(), "other")
	require.Equal(t, StatusNeedsCredentials, status(t, e.s, "other").Kind)
	e.clock.advance(time.Minute)
	n := 0
	e.sso.set(func(f *fakeSSO) { n = f.calls })
	require.NoError(t, e.touch(t))
	calls, _ := e.sso.count()
	assert.Equal(t, n, calls, "a keychain sign-out has no hint and is not re-read")
}
