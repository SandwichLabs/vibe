package tui

import (
	"database/sql"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/session/sessiontest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	_ "github.com/marcboeker/go-duckdb/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTablesModel_LoadAndNavigate(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE widgets (id INTEGER, name TEXT); INSERT INTO widgets VALUES (1, 'a'), (2, 'b');`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE gadgets (id INTEGER);`)
	require.NoError(t, err)

	m := NewTablesModel()
	m.LoadTables(sessiontest.ForTests(db))
	require.Len(t, m.items, 2)
	assert.Equal(t, "gadgets", m.items[0].Name) // alphabetical; the default connection's tables keep bare names
	assert.Equal(t, "widgets", m.items[1].Name)
	assert.Equal(t, 2, m.items[1].RowCount)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, nil)
	assert.Equal(t, 1, m.cursor)

	view := m.View(80, 20)
	assert.Contains(t, view, "widgets")
	assert.Contains(t, view, "gadgets")
}

func TestTablesModel_EnterBrowsesRows(t *testing.T) {
	m := NewTablesModel()
	m.items = []TableInfo{{Name: "widgets", RowCount: 2}}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	assert.True(t, m.browsing)
	require.NotNil(t, cmd)

	msg := cmd()
	dataMsg, ok := msg.(tableDataMsg)
	require.True(t, ok)
	assert.Error(t, dataMsg.err, "no engine available")

	m.SetData(dataMsg)

	m.Back()
	assert.False(t, m.browsing)
}

func TestDisplayTableName(t *testing.T) {
	ti := func(cat, schema, name string) session.TableInfo {
		return session.TableInfo{Catalog: cat, Schema: schema, Name: name}
	}
	assert.Equal(t, "orders", displayTableName(ti("local", "main", "orders"), "local", "main"))
	assert.Equal(t, "sales.orders", displayTableName(ti("sales", "main", "orders"), "local", "main"))
	assert.Equal(t, "sales.archive.orders", displayTableName(ti("sales", "archive", "orders"), "local", "main"))
	assert.Equal(t, "local.archive.orders", displayTableName(ti("local", "archive", "orders"), "local", "main"))
}
