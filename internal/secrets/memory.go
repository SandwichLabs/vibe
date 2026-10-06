package secrets

import (
	"context"
	"sync"
)

// MemoryStore is the "prompt" sign-in: values live for the life of the
// process and are zeroed on Clear. It is also the mock's backing store.
type MemoryStore struct {
	mu sync.Mutex
	m  map[Ref][]byte
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{m: map[Ref][]byte{}} }

// Get implements Store.
func (s *MemoryStore) Get(_ context.Context, ref Ref) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[ref]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), v...), nil
}

// Set implements Store.
func (s *MemoryStore) Set(_ context.Context, ref Ref, value []byte) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zero(ref)
	s.m[ref] = append([]byte(nil), value...)
	return nil
}

// Delete implements Store.
func (s *MemoryStore) Delete(_ context.Context, ref Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zero(ref)
	delete(s.m, ref)
	return nil
}

// Clear zeroes and forgets every entry.
func (s *MemoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ref := range s.m {
		s.zero(ref)
	}
	s.m = map[Ref][]byte{}
}

func (s *MemoryStore) zero(ref Ref) {
	for i := range s.m[ref] {
		s.m[ref][i] = 0
	}
}

var _ Store = (*MemoryStore)(nil)
