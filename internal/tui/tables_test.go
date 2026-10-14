package tui

import (
	"database/sql"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/session/sessiontest"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	_ "github.com/duckdb/duckdb-go/v2"
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

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	assert.Equal(t, 1, m.cursor)

	view := m.View(80, 20)
	assert.Contains(t, view, "widgets")
	assert.Contains(t, view, "gadgets")
}

func TestTablesModel_EnterBrowsesRows(t *testing.T) {
	m := NewTablesModel()
	m.items = []TableInfo{{Name: "widgets", RowCount: 2}}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.True(t, m.browsing)
	require.NotNil(t, cmd)

	msg := cmd()
	dataMsg, ok := msg.(tableDataMsg)
	require.True(t, ok)
	assert.Error(t, dataMsg.err, "no session available")

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

func TestFormatRowCount(t *testing.T) {
	assert.Equal(t, "0 rows", formatRowCount(0, false))
	assert.Equal(t, "999 rows", formatRowCount(999, false))
	assert.Equal(t, "~1,234 rows", formatRowCount(1234, true))
	assert.Equal(t, "1,234,567 rows", formatRowCount(1234567, false))
}

// A table with statistics lists as an estimate ("~"); an empty one has none and
// is counted exactly.
func TestTablesModel_EstimateAndExactFallback(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE big AS SELECT range AS id FROM range(20000); CREATE TABLE empty_t (id INTEGER);`)
	require.NoError(t, err)

	m := NewTablesModel()
	m.LoadTables(sessiontest.ForTests(db))
	byName := map[string]TableInfo{}
	for _, it := range m.items {
		byName[it.Name] = it
	}
	assert.True(t, byName["big"].Approx)
	assert.Equal(t, 20000, byName["big"].RowCount)
	assert.False(t, byName["empty_t"].Approx)
	assert.Equal(t, 0, byName["empty_t"].RowCount)
	assert.Contains(t, m.View(80, 20), "~20,000 rows")
	assert.Contains(t, m.View(80, 20), "0 rows")
}

// A small local table is counted exactly, not estimated.
func TestTablesModel_SmallLocalTableIsExact(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE small AS SELECT range AS id FROM range(50)`)
	require.NoError(t, err)
	m := NewTablesModel()
	m.LoadTables(sessiontest.ForTests(db))
	require.Len(t, m.items, 1)
	assert.False(t, m.items[0].Approx)
	assert.Equal(t, 50, m.items[0].RowCount)
}

// Enter on a listed table pages its rows through crud.Page.
func TestTablesModel_EnterLoadsRowsViaPage(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE widgets (id INTEGER, name TEXT); INSERT INTO widgets VALUES (1, 'a'), (2, 'b');`)
	require.NoError(t, err)

	m := NewTablesModel()
	m.LoadTables(sessiontest.ForTests(db))
	require.Len(t, m.items, 1)

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg, ok := cmd().(tableDataMsg)
	require.True(t, ok)
	require.NoError(t, msg.err)
	assert.Len(t, msg.columns, 2)
	assert.Len(t, msg.rows, 2)
}

// Browsing a narrower table after a wider one must not render stale wide rows.
func TestTableBrowser_NarrowerAfterWiderDoesNotPanic(t *testing.T) {
	b := NewTableBrowserModel()
	b.SetSize(80, 20)
	wide := []table.Column{{Title: "a", Width: 8}, {Title: "b", Width: 8}, {Title: "c", Width: 8}}
	b.SetData(wide, []table.Row{{"1", "2", "3"}, {"4", "5", "6"}}, nil)
	narrow := []table.Column{{Title: "a", Width: 8}}
	assert.NotPanics(t, func() {
		b.SetData(narrow, []table.Row{{"x"}}, nil)
		_ = b.View(80, 20)
	})
}
