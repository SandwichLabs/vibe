package session

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Lakes and a table connect, the storage credential flaps (Preflight), the
// storage connection is detached and registered again, and queries run, all at
// once. No lake may ever attach (or stay attached) without the storage secret.
func TestRequiresStressNoDependentWithoutTheSecret(t *testing.T) {
	var total int32
	for round := 0; round < 3; round++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		bare := &atomic.Int32{}
		s := newTestSession(t, WithConnectionTypes(stubType{ext: "json"}, lakeyType{sawSecret: &atomic.Bool{}, bare: bare}, tblType{}))
		cred := &switchable{}
		cred.have.Store(true)
		require.NoError(t, s.Register(Spec{Name: "store", Type: "stub", Credential: cred.fn}))
		for _, n := range []string{"l1", "l2"} {
			require.NoError(t, s.Register(Spec{Name: n, Type: "lakey", Requires: []string{"store"}}))
		}
		require.NoError(t, s.Register(Spec{Name: "tb", Type: "tbl", Requires: []string{"store"}}))

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
		worker(func() { _ = s.Connect(ctx, "l1") })
		worker(func() { _ = s.ConnectReferenced(ctx, "", "SELECT 1 FROM l2.main.t") })
		worker(func() { cred.have.Store(!cred.have.Load()); s.Preflight(ctx, "store") })
		worker(func() { s.Preflight(ctx, "tb") })
		worker(func() {
			_ = s.Detach(ctx, "store")
			_ = s.Register(Spec{Name: "store", Type: "stub", Credential: cred.fn})
			s.Preflight(ctx, "store")
		})
		worker(func() {
			_ = s.Detach(ctx, "l1")
			_ = s.Register(Spec{Name: "l1", Type: "lakey", Requires: []string{"store"}})
		})
		var viol atomic.Int32
		worker(func() { // invariant: an attached dependent implies an attached storage connection
			h, _ := s.(*hub)
			h.adminMu.Lock()
			h.mu.RLock()
			st := h.conns["store"]
			for _, n := range []string{"l1", "l2", "tb"} {
				if e := h.conns[n]; e != nil && e.attach && (st == nil || !st.attach) {
					viol.Add(1)
				}
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
	assert.Zero(t, total, "a dependent attached, or stayed attached, without the storage secret")
}

// A dependent that cannot be detached keeps the storage secret in place (and
// the store attached): the change is refused, never half applied.
func TestRequiresUndetachableDependentKeepsTheSecret(t *testing.T) {
	ctx := context.Background()
	s, _ := requiresSession(t, "json")
	require.NoError(t, s.Register(Spec{Name: "store", Type: "stub", Credential: credOK("tok")}))
	require.NoError(t, s.Register(Spec{Name: "lake", Type: "lakey", Requires: []string{"store"}}))
	require.NoError(t, s.Connect(ctx, "lake"))
	h, ok := s.(*hub)
	require.True(t, ok)
	_, err := s.DB().Exec("DETACH lake") // the catalog is gone behind the hub's back: DETACH will now fail
	require.NoError(t, err)

	require.Error(t, s.Detach(ctx, "store"))
	assert.Equal(t, 1, secretCount(t, s), "the secret stays")
	_, att := h.entryOf("store")
	assert.True(t, att, "the store stays attached")
}
