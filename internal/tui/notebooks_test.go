package tui

import (
	"context"
	"testing"

	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/session"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWorkspace is a WorkspaceAccess over a notebook store and an optional
// session.
type fakeWorkspace struct {
	nb   notebooks.Store
	sess session.Session
}

func (f *fakeWorkspace) Notebooks() notebooks.Store { return f.nb }
func (f *fakeWorkspace) Session() session.Session   { return f.sess }

// newNotebookTestStore is a file store in a temp workspace folder holding one
// notebook, "Sales".
func newNotebookTestStore(t *testing.T) notebooks.Store {
	t.Helper()
	store := notebooks.NewFileStore(t.TempDir())
	nb := notebooks.Empty("Sales")
	nb.Cells = []notebooks.Cell{
		{ID: "c1", Type: notebooks.CellMarkdown, Source: "# Notes"},
		{ID: "c2", Type: notebooks.CellSQL, Source: "SELECT 1", View: "table"},
	}
	_, _, err := store.Insert(context.Background(), nb)
	require.NoError(t, err)
	return store
}

func TestNotebooksModel_LoadAndNavigate(t *testing.T) {
	store := newNotebookTestStore(t)

	m := NewNotebooksModel()
	m.LoadNotebooks(store)
	require.Len(t, m.items, 1)
	assert.Equal(t, "Sales", m.items[0].Title)
	assert.Equal(t, 2, m.items[0].CellCount)

	view := m.View(80, 20)
	assert.Contains(t, view, "Sales")
}

func TestNotebooksModel_EnterDumpsCells(t *testing.T) {
	store := newNotebookTestStore(t)
	ws := &fakeWorkspace{nb: store}

	m := NewNotebooksModel()
	m.SetSize(80, 20)
	m.LoadNotebooks(store)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}, ws)
	assert.True(t, m.viewing)
	assert.Empty(t, m.err)

	view := m.View(80, 20)
	assert.Contains(t, view, "# Notes")
	assert.Contains(t, view, "SELECT 1")

	m.Back()
	assert.False(t, m.viewing)
}
