package queries

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Mock is an in-memory Store for tests of packages that depend on one. Set Err
// to make every method fail; Conflict[id] makes the next Update of that id
// return ErrConflict.
type Mock struct {
	mu       sync.Mutex
	byID     map[string]Query
	seq      int
	Err      error
	Conflict map[string]bool
}

var _ Store = (*Mock)(nil)

func (m *Mock) List(context.Context) ([]Query, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	out := make([]Query, 0, len(m.byID))
	for _, q := range m.byID {
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

func (m *Mock) Get(_ context.Context, id string) (*Query, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	q, ok := m.byID[id]
	if !ok {
		return nil, &notFoundError{id: id}
	}
	return &q, nil
}

func (m *Mock) Create(_ context.Context, q *Query) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if m.byID == nil {
		m.byID = map[string]Query{}
	}
	if q.ID == "" {
		m.seq++
		q.ID = fmt.Sprintf("q%d", m.seq)
	}
	m.byID[q.ID] = *q
	return nil
}

func (m *Mock) Update(_ context.Context, q *Query, _ ...WriteOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if _, ok := m.byID[q.ID]; !ok {
		return &notFoundError{id: q.ID}
	}
	if m.Conflict[q.ID] {
		delete(m.Conflict, q.ID)
		return fmt.Errorf("failed to update query %q: %w", q.ID, ErrConflict)
	}
	m.byID[q.ID] = *q
	return nil
}

func (m *Mock) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if _, ok := m.byID[id]; !ok {
		return &notFoundError{id: id}
	}
	delete(m.byID, id)
	return nil
}
