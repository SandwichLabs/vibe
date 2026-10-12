package skills

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Mock is an in-memory Store for tests of packages that depend on one. Set Err
// to make every method fail; Conflict[name] makes the next Put of that name
// return ErrConflict.
type Mock struct {
	mu       sync.Mutex
	byName   map[string]Stored
	Err      error
	Conflict map[string]bool
}

var _ Store = (*Mock)(nil)

func (m *Mock) List(context.Context) ([]Stored, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	out := make([]Stored, 0, len(m.byName))
	for _, s := range m.byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Mock) Get(_ context.Context, name string) (*Stored, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	s, ok := m.byName[name]
	if !ok {
		return nil, fmt.Errorf("skill %q: %w", name, ErrNotFound)
	}
	return &s, nil
}

func (m *Mock) Put(_ context.Context, s *Stored, _ ...PutOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	if m.Conflict[s.Name] {
		delete(m.Conflict, s.Name)
		return fmt.Errorf("failed to save skill %q: %w", s.Name, ErrConflict)
	}
	if m.byName == nil {
		m.byName = map[string]Stored{}
	}
	if s.Source == "" {
		s.Source = SourceUser
	}
	m.byName[s.Name] = *s
	return nil
}

func (m *Mock) Delete(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	delete(m.byName, name)
	return nil
}
