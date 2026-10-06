package notebooks

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Mock is an in-memory Store for tests of packages that depend on one. Set
// Err to make every method fail; set Conflict[id] to make Save of that id
// return ErrConflict (once, then it clears).
type Mock struct {
	mu       sync.Mutex
	books    map[string]Notebook
	Err      error
	Conflict map[string]bool
}

var _ Store = (*Mock)(nil)

func (m *Mock) init() {
	if m.books == nil {
		m.books = map[string]Notebook{}
	}
}

func (m *Mock) List(context.Context) ([]Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	out := make([]Summary, 0, len(m.books))
	for _, nb := range m.books {
		out = append(out, Summary{ID: nb.ID, Title: nb.Title, CellCount: len(nb.Cells), UpdatedAt: nb.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Mock) Get(_ context.Context, id string) (Notebook, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return Notebook{}, nil, m.Err
	}
	nb, ok := m.books[id]
	if !ok {
		return Notebook{}, nil, fmt.Errorf("failed to get notebook %q: %w", id, ErrNotFound)
	}
	return nb, nil, nil
}

func (m *Mock) Save(_ context.Context, raw any, _ ...SaveOption) (Notebook, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return Notebook{}, nil, m.Err
	}
	m.init()
	nb, warnings, err := Coerce(raw)
	if err != nil {
		return Notebook{}, warnings, err
	}
	if m.Conflict[nb.ID] {
		delete(m.Conflict, nb.ID)
		return Notebook{}, warnings, fmt.Errorf("failed to save notebook %q: %w", nb.ID, ErrConflict)
	}
	nb.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	m.books[nb.ID] = nb
	return nb, warnings, nil
}

func (m *Mock) Create(ctx context.Context, title string) (Notebook, error) {
	nb, _, err := m.Save(ctx, notebookToRaw(Empty(title)))
	return nb, err
}

func (m *Mock) Insert(ctx context.Context, nb Notebook) (Notebook, []string, error) {
	return m.Save(ctx, notebookToRaw(nb))
}

func (m *Mock) Rename(_ context.Context, id, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	nb, ok := m.books[id]
	if !ok {
		return fmt.Errorf("notebook %q not found", id)
	}
	nb.Title = title
	m.books[id] = nb
	return nil
}

func (m *Mock) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	delete(m.books, id)
	return nil
}
