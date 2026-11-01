package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/secrets"
)

// Statements rotate a secret that is always about to end, while the portal
// flaps between working, sign-in-needed and down, and the storage connection is
// preflighted, detached and registered again, and lakes connect and detach: all
// at once. A lake may never attach (or stay attached) without the storage
// secret, in DuckDB itself, and a rotation never reads the entry it was started
// for after adminMu (the race detector and the invariant worker watch for it).
func TestRotateStressNoDependentWithoutTheSecret(t *testing.T) {
	var total int32
	for round := 0; round < 3; round++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		clock := newFakeClock()
		bare := &atomic.Int32{}
		s := newTestSession(t, WithClock(clock.now), WithStatusListener(func(string) {}),
			WithConnectionTypes(stubType{ext: "json"}, lakeyType{sawSecret: &atomic.Bool{}, bare: bare}))
		h, _ := s.(*hub)
		sso := &fakeSSO{clock: clock, ttl: 10 * time.Minute} // always inside the 5 minute window after a nudge
		var mode atomic.Int32                                // 0 works, 1 sign-in needed, 2 portal down
		cred := func(c context.Context) (secrets.Credential, error) {
			switch mode.Load() {
			case 1:
				return secrets.Credential{}, signInNeeded()
			case 2:
				return secrets.Credential{}, &secrets.ErrMethodFailed{Kind: secrets.MethodAWSSSO, Msg: "down", Err: errors.New("down")}
			}
			return sso.fn(c)
		}
		require.NoError(t, s.Register(Spec{Name: "store", Type: "stub", Credential: cred}))
		for _, n := range []string{"l1", "l2"} {
			require.NoError(t, s.Register(Spec{Name: n, Type: "lakey", Requires: []string{"store"}}))
		}
		s.Preflight(ctx, "store")

		var wg sync.WaitGroup
		stop := make(chan struct{})
		worker := func(f func()) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						f()
					}
				}
			}()
		}
		worker(func() { clock.advance(time.Minute); _ = s.ConnectReferenced(ctx, "", "SELECT 1 FROM l1.main.t") })
		worker(func() { clock.advance(40 * time.Second); _ = s.ConnectReferenced(ctx, "", "SELECT 1") })
		worker(func() { // a read that failed with an expired token
			_ = h.readErr(ctx, "x", errors.New("HTTP 403 Forbidden ExpiredToken"))
			time.Sleep(time.Millisecond)
		})
		worker(func() { _ = s.Connect(ctx, "l2") })
		worker(func() { mode.Store((mode.Load() + 1) % 3); time.Sleep(2 * time.Millisecond) })
		worker(func() { s.Preflight(ctx, "store") })
		worker(func() {
			_ = s.Detach(ctx, "store")
			_ = s.Register(Spec{Name: "store", Type: "stub", Credential: cred})
			s.Preflight(ctx, "store")
		})
		worker(func() {
			_ = s.Detach(ctx, "l1")
			_ = s.Register(Spec{Name: "l1", Type: "lakey", Requires: []string{"store"}})
		})
		var viol atomic.Int32
		worker(func() { // invariant, checked in DuckDB itself under adminMu
			h.adminMu.Lock()
			var secretsNow int
			_ = h.db.QueryRow("SELECT count(*) FROM duckdb_secrets() WHERE name = 'stub_store'").Scan(&secretsNow)
			h.mu.RLock()
			st := h.conns["store"]
			for _, n := range []string{"l1", "l2"} {
				if e := h.conns[n]; e != nil && e.attach && (st == nil || !st.attach || secretsNow != 1) {
					viol.Add(1)
				}
			}
			if st != nil && st.attach && secretsNow != 1 {
				viol.Add(1) // an attached store always has exactly its one secret
			}
			if st != nil && !st.attach && secretsNow != 0 {
				viol.Add(1) // a detached store has none
			}
			h.mu.RUnlock()
			h.adminMu.Unlock()
			time.Sleep(time.Millisecond)
		})
		time.Sleep(time.Second)
		close(stop)
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("deadlock")
		}
		total += bare.Load() + viol.Load()
		cancel()
	}
	assert.Zero(t, total, "a dependent was attached without the storage secret, or the secret outlived its entry")
}
