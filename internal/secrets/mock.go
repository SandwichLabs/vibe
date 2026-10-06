package secrets

import (
	"context"
	"sync"
)

// Mock is a Store for tests. Unset *Fn fields fall back to an in-memory map;
// every call is recorded in Calls.
type Mock struct {
	GetFn    func(ctx context.Context, ref Ref) ([]byte, error)
	SetFn    func(ctx context.Context, ref Ref, value []byte) error
	DeleteFn func(ctx context.Context, ref Ref) error

	mu    sync.Mutex
	mem   *MemoryStore
	Calls []string // "get <ref>", "set <ref>", "delete <ref>" (never values)
}

var _ Store = (*Mock)(nil)

func (m *Mock) store() *MemoryStore {
	if m.mem == nil {
		m.mem = NewMemoryStore()
	}
	return m.mem
}

func (m *Mock) record(op string, ref Ref) {
	m.mu.Lock()
	m.Calls = append(m.Calls, op+" "+ref.String())
	m.mu.Unlock()
}

// Get implements Store.
func (m *Mock) Get(ctx context.Context, ref Ref) ([]byte, error) {
	m.record("get", ref)
	if m.GetFn != nil {
		return m.GetFn(ctx, ref)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store().Get(ctx, ref)
}

// Set implements Store.
func (m *Mock) Set(ctx context.Context, ref Ref, v []byte) error {
	m.record("set", ref)
	if m.SetFn != nil {
		return m.SetFn(ctx, ref, v)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store().Set(ctx, ref, v)
}

// Delete implements Store.
func (m *Mock) Delete(ctx context.Context, ref Ref) error {
	m.record("delete", ref)
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, ref)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store().Delete(ctx, ref)
}
