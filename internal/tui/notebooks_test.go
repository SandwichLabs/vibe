package tui

import (
	"database/sql"
	"github.com/sandwich-labs/vibe-app/internal/session"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	_ "github.com/mutecomm/go-sqlcipher/v4" // registers "sqlite3"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeContainer struct{ db *sql.DB }

func (f *fakeContainer) Notebooks() notebooks.Store { return notebooks.NewSQLiteStore(f.db) }
func (f *fakeContainer) DuckDBDB() *sql.DB          { return f.db }
func (f *fakeContainer) Save() error                { return nil }
func (f *fakeContainer) Close() error               { return nil }

func newNotebookTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE _vibe_notebooks (
		id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', cells TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT ''
	);`)
	require.NoError(t, err)

	_, _, err = notebooks.Insert(db, notebooks.Notebook{
		ID:    "nb1",
		Title: "Sales",
		Cells: []notebooks.Cell{
			{ID: "c1", Type: notebooks.CellMarkdown, Source: "# Notes"},
			{ID: "c2", Type: notebooks.CellSQL, Source: "SELECT 1", View: "table"},
		},
	})
	require.NoError(t, err)
	return db
}

func TestNotebooksModel_LoadAndNavigate(t *testing.T) {
	db := newNotebookTestDB(t)

	m := NewNotebooksModel()
	m.LoadNotebooks(notebooks.NewSQLiteStore(db))
	require.Len(t, m.items, 1)
	assert.Equal(t, "Sales", m.items[0].Title)
	assert.Equal(t, 2, m.items[0].CellCount)

	view := m.View(80, 20)
	assert.Contains(t, view, "Sales")
}

func TestNotebooksModel_EnterDumpsCells(t *testing.T) {
	db := newNotebookTestDB(t)
	container := &fakeContainer{db: db}

	m := NewNotebooksModel()
	m.SetSize(80, 20)
	m.LoadNotebooks(notebooks.NewSQLiteStore(db))

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}, container)
	assert.True(t, m.viewing)
	assert.Empty(t, m.err)

	view := m.View(80, 20)
	assert.Contains(t, view, "# Notes")
	assert.Contains(t, view, "SELECT 1")

	m.Back()
	assert.False(t, m.viewing)
}

func (f *fakeContainer) Session() session.Session { return nil }
