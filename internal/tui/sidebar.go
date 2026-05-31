package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	sectionPages   = 0
	sectionTables  = 1
	sectionQueries = 2
)

// PageItem represents a page in the sidebar list.
type PageItem struct {
	ID    string
	Title string
	Type  string
}

// TableItem represents a table in the sidebar list.
type TableItem struct {
	Name     string
	RowCount int
}

// QueryItem represents a saved query in the sidebar list.
type QueryItem struct {
	ID    string
	Title string
}

// sidebarItem is a union type for all sidebar list entries.
type sidebarItem struct {
	kind       int // sectionPages, sectionTables, sectionQueries, or -1 for header
	page       *PageItem
	table      *TableItem
	query      *QueryItem
	isHeader   bool
	headerText string
}

func (i sidebarItem) FilterValue() string {
	switch {
	case i.isHeader:
		return ""
	case i.page != nil:
		return i.page.Title
	case i.table != nil:
		return i.table.Name
	case i.query != nil:
		return i.query.Title
	}
	return ""
}

// sidebarDelegate renders sidebar items.
type sidebarDelegate struct{}

func (d sidebarDelegate) Height() int                             { return 1 }
func (d sidebarDelegate) Spacing() int                            { return 0 }
func (d sidebarDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d sidebarDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	si, ok := item.(sidebarItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	if si.isHeader {
		style := sectionHeaderStyle.Width(m.Width())
		if isSelected {
			// Headers shouldn't visually look "selected" the same way,
			// but the list model may land on them. Render them the same.
			style = style.Foreground(accentColor)
		}
		fmt.Fprint(w, style.Render(si.headerText))
		return
	}

	var label string
	switch {
	case si.page != nil:
		label = si.page.Title
	case si.table != nil:
		label = fmt.Sprintf("%s (%d rows)", si.table.Name, si.table.RowCount)
	case si.query != nil:
		label = si.query.Title
	}

	if isSelected {
		fmt.Fprint(w, selectedItemStyle.Width(m.Width()).Render("> "+label))
	} else {
		fmt.Fprint(w, listItemStyle.Width(m.Width()).Render(label))
	}
}

// SidebarModel manages the sidebar navigation panel.
type SidebarModel struct {
	list    list.Model
	pages   []PageItem
	tables  []TableItem
	queries []QueryItem
	section int
}

// NewSidebarModel creates a new sidebar model.
func NewSidebarModel() SidebarModel {
	delegate := sidebarDelegate{}
	l := list.New([]list.Item{}, delegate, 20, 20)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.Styles.NoItems = dimStyle
	l.SetShowFilter(true)
	l.FilterInput.Prompt = "/ "
	l.FilterInput.PromptStyle = lipgloss.NewStyle().Foreground(accentColor)

	s := SidebarModel{
		list:    l,
		pages:   []PageItem{},
		tables:  []TableItem{},
		queries: []QueryItem{},
		section: sectionPages,
	}
	s.rebuildItems()
	return s
}

// SetPages replaces the pages list.
func (s *SidebarModel) SetPages(pages []PageItem) {
	s.pages = pages
	s.rebuildItems()
}

// SetTables replaces the tables list.
func (s *SidebarModel) SetTables(tables []TableItem) {
	s.tables = tables
	s.rebuildItems()
}

// SetQueries replaces the queries list.
func (s *SidebarModel) SetQueries(queries []QueryItem) {
	s.queries = queries
	s.rebuildItems()
}

// rebuildItems creates the flat item list with section headers interspersed.
func (s *SidebarModel) rebuildItems() {
	var items []list.Item

	// Pages section
	items = append(items, sidebarItem{isHeader: true, headerText: "PAGES", kind: -1})
	if len(s.pages) == 0 {
		items = append(items, sidebarItem{isHeader: true, headerText: "  (no pages)", kind: -1})
	}
	for i := range s.pages {
		items = append(items, sidebarItem{kind: sectionPages, page: &s.pages[i]})
	}

	// Tables section
	items = append(items, sidebarItem{isHeader: true, headerText: "TABLES", kind: -1})
	if len(s.tables) == 0 {
		items = append(items, sidebarItem{isHeader: true, headerText: "  (no tables)", kind: -1})
	}
	for i := range s.tables {
		items = append(items, sidebarItem{kind: sectionTables, table: &s.tables[i]})
	}

	// Queries section
	items = append(items, sidebarItem{isHeader: true, headerText: "QUERIES", kind: -1})
	if len(s.queries) == 0 {
		items = append(items, sidebarItem{isHeader: true, headerText: "  (no queries)", kind: -1})
	}
	for i := range s.queries {
		items = append(items, sidebarItem{kind: sectionQueries, query: &s.queries[i]})
	}

	s.list.SetItems(items)
}

// SelectedItem returns the currently selected sidebarItem, or nil if a header is selected.
func (s *SidebarModel) SelectedItem() *sidebarItem {
	item := s.list.SelectedItem()
	if item == nil {
		return nil
	}
	si, ok := item.(sidebarItem)
	if !ok || si.isHeader {
		return nil
	}
	return &si
}

// SelectedPage returns the currently selected page, or nil if none.
func (s *SidebarModel) SelectedPage() *PageItem {
	si := s.SelectedItem()
	if si == nil || si.page == nil {
		return nil
	}
	return si.page
}

// SelectedTable returns the currently selected table, or nil if none.
func (s *SidebarModel) SelectedTable() *TableItem {
	si := s.SelectedItem()
	if si == nil || si.table == nil {
		return nil
	}
	return si.table
}

// SelectedQuery returns the currently selected query, or nil if none.
func (s *SidebarModel) SelectedQuery() *QueryItem {
	si := s.SelectedItem()
	if si == nil || si.query == nil {
		return nil
	}
	return si.query
}

// Update handles messages for the sidebar, delegating to the list model.
func (s SidebarModel) Update(msg tea.Msg) (SidebarModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "J":
			s.jumpToNextSection()
			return s, nil
		case "K":
			s.jumpToPrevSection()
			return s, nil
		}
	}

	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return s, cmd
}

// jumpToNextSection moves cursor to the first item after the next section header.
func (s *SidebarModel) jumpToNextSection() {
	items := s.list.Items()
	current := s.list.Index()
	n := len(items)
	if n == 0 {
		return
	}

	// Find the next header after current position.
	foundHeader := false
	for i := 1; i < n; i++ {
		idx := (current + i) % n
		si, ok := items[idx].(sidebarItem)
		if ok && si.isHeader && strings.TrimSpace(si.headerText) != "" && !strings.HasPrefix(si.headerText, "  ") {
			foundHeader = true
			// Select the item after this header (or the header itself if it's the last).
			nextIdx := (idx + 1) % n
			s.list.Select(nextIdx)
			break
		}
	}
	if !foundHeader {
		s.list.Select(0)
	}
}

// jumpToPrevSection moves cursor to the first item in the previous section.
func (s *SidebarModel) jumpToPrevSection() {
	items := s.list.Items()
	current := s.list.Index()
	n := len(items)
	if n == 0 {
		return
	}

	// Find the previous header before current position.
	headerCount := 0
	for i := 1; i < n; i++ {
		idx := (current - i + n) % n
		si, ok := items[idx].(sidebarItem)
		if ok && si.isHeader && strings.TrimSpace(si.headerText) != "" && !strings.HasPrefix(si.headerText, "  ") {
			headerCount++
			if headerCount == 1 {
				// This is the header of our current section, skip to previous.
				continue
			}
			// Select the item after this header.
			nextIdx := (idx + 1) % n
			s.list.Select(nextIdx)
			return
		}
	}
	// Wrap around to last section.
	s.list.Select(0)
}

// View renders the sidebar within the given dimensions.
func (s SidebarModel) View(width, height int) string {
	s.list.SetSize(width, height)
	return s.list.View()
}
