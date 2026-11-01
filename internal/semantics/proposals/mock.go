package proposals

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/SandwichLabs/assay/internal/filestore"
)

// Mock is an in-memory Store for tests of packages that depend on one. It
// keeps file bytes, so strict decoding, revisions and conflicts behave like
// the FileStore. Set Err to make every method fail. The zero value is ready.
type Mock struct {
	mu    sync.Mutex
	once  sync.Once
	c     *core
	files map[string][]byte
	Err   error
	// Now stamps decisions; nil means time.Now.
	Now func() time.Time
}

var _ Store = (*Mock)(nil)

func (m *Mock) core() *core {
	m.once.Do(func() {
		m.c = &core{b: m, logger: slog.New(slog.DiscardHandler), now: func() time.Time {
			if m.Now != nil {
				return m.Now()
			}
			return time.Now()
		}}
	})
	return m.c
}

// SetFile puts raw bytes at an id, valid or not.
func (m *Mock) SetFile(id string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[id] = append([]byte(nil), data...)
}

func (m *Mock) read(id string) ([]byte, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, "", m.Err
	}
	data, ok := m.files[id]
	if !ok {
		return nil, "", fmt.Errorf("%s: %w", id, filestore.ErrNotFound)
	}
	return append([]byte(nil), data...), filestore.HashBytes(data), nil
}

func (m *Mock) write(id string, data []byte, expect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	cur, exists := m.files[id]
	switch {
	case expect == "" && exists:
		return &filestore.ConflictError{Path: id, Existing: true, Got: filestore.HashBytes(cur)}
	case expect != "" && !exists:
		return &filestore.ConflictError{Path: id, Missing: true, Want: expect}
	case expect != "" && filestore.HashBytes(cur) != expect:
		return &filestore.ConflictError{Path: id, Want: expect, Got: filestore.HashBytes(cur)}
	}
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[id] = append([]byte(nil), data...)
	return nil
}

func (m *Mock) remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, id)
	return m.Err
}

func (m *Mock) ids() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	out := make([]string, 0, len(m.files))
	for id := range m.files {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// Create implements Proposer.
func (m *Mock) Create(ctx context.Context, p *Proposal) error { return m.core().Create(ctx, p) }

// List implements Proposer.
func (m *Mock) List(ctx context.Context) ([]Proposal, error) { return m.core().List(ctx) }

// OpenCount implements Proposer.
func (m *Mock) OpenCount(ctx context.Context) (int, error) { return m.core().OpenCount(ctx) }

// Get implements Store.
func (m *Mock) Get(ctx context.Context, id string) (*Proposal, error) { return m.core().Get(ctx, id) }

// Decide implements Store.
func (m *Mock) Decide(ctx context.Context, id, status, rev string) error {
	return m.core().Decide(ctx, id, status, rev)
}
