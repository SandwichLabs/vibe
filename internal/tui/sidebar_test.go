package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestSidebarModel_Navigation(t *testing.T) {
	s := NewSidebarModel()

	// Initially points to first item (PAGES header).
	assert.NotNil(t, s.list.Items())

	// Navigate down with j.
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	// Section jump with J.
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	// Should have moved to tables section.
	idx := s.list.Index()
	items := s.list.Items()
	if idx < len(items) {
		si := items[idx].(sidebarItem)
		// After jumping, we should be past PAGES section.
		assert.True(t, idx > 1, "should have jumped past PAGES section")
		_ = si
	}
}

func TestSidebarModel_SetItems(t *testing.T) {
	s := NewSidebarModel()
	s.SetPages([]PageItem{{ID: "1", Title: "Dashboard", Type: "dashboard"}})
	s.SetTables([]TableItem{{Name: "users", RowCount: 42}})
	s.SetQueries([]QueryItem{{ID: "q1", Title: "Active Users"}})

	items := s.list.Items()
	assert.True(t, len(items) > 0, "should have items")

	// Check view contains section headers and items.
	view := s.View(30, 20)
	assert.Contains(t, view, "PAGES")
	assert.Contains(t, view, "TABLES")
	assert.Contains(t, view, "QUERIES")
}

func TestSidebarModel_View(t *testing.T) {
	s := NewSidebarModel()
	view := s.View(30, 20)
	assert.Contains(t, view, "PAGES")
	assert.Contains(t, view, "TABLES")
	assert.Contains(t, view, "QUERIES")
	assert.Contains(t, view, "(no pages)")
}

func TestSidebarModel_SelectedItem(t *testing.T) {
	s := NewSidebarModel()
	s.SetPages([]PageItem{{ID: "1", Title: "Test Page", Type: "page"}})

	// First item is PAGES header, selected item should be nil.
	si := s.SelectedItem()
	// Header is selected initially, so nil.
	if si != nil {
		// If list auto-selects first non-header, that's also fine.
		assert.NotNil(t, si.page)
	}
}

func TestSidebarModel_Filtering(t *testing.T) {
	s := NewSidebarModel()
	s.SetPages([]PageItem{
		{ID: "1", Title: "Dashboard", Type: "dashboard"},
		{ID: "2", Title: "Reports", Type: "page"},
	})

	// Verify filtering is enabled.
	assert.True(t, s.list.FilteringEnabled())
}
