> **SUPERSEDED (2026-09-22).** The TUI is being cut to a SQL REPL and read-only browser (#69); the charmbracelet v2 upgrade and rich page viewer planned here are not going ahead.
> See [`../direction.md`](../direction.md). Kept for history; do not plan new work from it.

# Plan: Rich TUI with charmbracelet v2 libraries

## Context

The Vibe Studio TUI (`internal/tui/`) is entirely hand-rolled — no `charmbracelet/bubbles` components. The sidebar, REPL input, table browser, and page viewer all use manual `strings.Builder` rendering and character-by-character key handling. This results in a basic UX: no filtering, no scrolling, no markdown rendering, no real table data display.

The goal is to:
1. Upgrade all charmbracelet dependencies to v2 (`charm.land/` import paths)
2. Replace hand-rolled components with `bubbles/v2` equivalents (list, table, textinput, viewport)
3. Add glamour markdown rendering in the page viewer
4. Add adaptive/responsive layout using lipgloss

## v2 Library Migration (Critical Assumptions)

### Import Path Changes
All charmbracelet libraries moved to `charm.land/` vanity imports in v2:

| v1 | v2 |
|---|---|
| `github.com/charmbracelet/bubbletea` | `charm.land/bubbletea/v2` |
| `github.com/charmbracelet/lipgloss` | `charm.land/lipgloss/v2` |
| N/A (new dep) | `charm.land/bubbles/v2/{list,table,textinput,viewport,key,help}` |
| `github.com/charmbracelet/glamour` | `github.com/charmbracelet/glamour` (NO v2 exists, stays at v0.10.0) |

### Breaking API Changes

**bubbletea v2:**
- `View()` returns `tea.View` instead of `string` — use `tea.NewView(s)` to wrap string output
- `tea.KeyMsg` → `tea.KeyPressMsg` (separate `KeyPressMsg`/`KeyReleaseMsg` types)
- `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}` → `tea.KeyPressMsg{Code: 'x'}` (tests must update)
- `tea.KeyMsg{Type: tea.KeyEnter}` → `tea.KeyPressMsg{Code: tea.KeyEnter}` (special key codes)
- `tea.Quit` is now `func() tea.Msg` (a Cmd) — usage as `return m, tea.Quit` stays the same
- `msg.String()` still works on `KeyPressMsg`

**lipgloss v2:**
- Core styling API unchanged: `NewStyle()`, `Bold()`, `Foreground()`, `Render()`, `JoinHorizontal/Vertical`, `Place` all work the same
- `lipgloss.Color("39")` still works (returns `image/color.Color` now but usage is identical)
- Border functions are the same: `NormalBorder()`, `RoundedBorder()`, etc.
- `lipgloss.Width()`, `lipgloss.Height()` unchanged

**bubbles/v2:**
- `list.ItemDelegate.Render` signature: `Render(w io.Writer, m list.Model, index int, item list.Item)` — takes `list.Model` by value (not pointer)
- `viewport.New()` uses functional options: `viewport.New()`, then `vp.SetWidth(w)`, `vp.SetHeight(h)`
- `viewport.SetContent(string)`, `viewport.GotoBottom()`, `viewport.View() string` — unchanged
- `table.New(table.WithColumns(cols), table.WithRows(rows))` — functional options constructor
- `textinput.New()` — same API, `.Prompt`, `.CharLimit`, `.Focus()`, `.Blur()`, `.Value()`, `.SetValue()`
- `key.NewBinding(key.WithKeys(...), key.WithHelp(...))` — same as before
- `key.Matches(msg, binding)` for key matching instead of `switch msg.String()`
- `help.New()`, `help.ShortHelpView()`, `help.FullHelpView()` — same API

## Files to Modify

### All files (import path migration)
Every `.go` file importing charmbracelet packages needs import path updates:

| Package | Files affected |
|---------|---------------|
| `bubbletea` | `app.go`, `sidebar.go`, `repl.go`, `editor.go`, `page_wizard.go`, `save.go`, `app_test.go`, `sidebar_test.go`, `repl_test.go`, `cmd/studio.go` |
| `lipgloss` | `styles.go`, `layout.go`, `help.go`, `sidebar.go`, `internal/renderer/tui/{chart,detail,form,metric}.go` |
| `glamour` | `internal/renderer/tui/text.go` (NO change — glamour has no v2) |

### TUI component rewrites

| File | Change |
|------|--------|
| `go.mod` | Replace v1 deps with v2 `charm.land/` deps; add `charm.land/bubbles/v2` |
| `internal/tui/keymap.go` | Rewrite: `bubbles/v2/key` bindings + `help.KeyMap` interface |
| `internal/tui/help.go` | Rewrite: use `bubbles/v2/help.Model` |
| `internal/tui/styles.go` | Add table/list component styles |
| `internal/tui/sidebar.go` | Rewrite: `bubbles/v2/list.Model` with custom `ItemDelegate` |
| `internal/tui/repl.go` | Rewrite: `bubbles/v2/textinput` + `bubbles/v2/viewport` |
| `internal/tui/page_view.go` | Rewrite: glamour markdown rendering + `bubbles/v2/viewport` |
| `internal/tui/table_browser.go` | Rewrite: `bubbles/v2/table` with async data loading |
| `internal/tui/query_editor.go` | Add `bubbles/v2/viewport` for scrolling |
| `internal/tui/layout.go` | Adaptive sizing: REPL expands when focused, sidebar collapses on narrow terminals |
| `internal/tui/app.go` | Integrate all new components, `tea.View` return, `KeyPressMsg`, focus management |
| `internal/tui/page_wizard.go` | Update `tea.KeyMsg` → `tea.KeyPressMsg` |
| `internal/tui/save.go` | Update `tea.KeyMsg` → `tea.KeyPressMsg` |
| `internal/tui/editor.go` | Update import path only |
| `internal/tui/app_test.go` | Update for v2 APIs and new component APIs |
| `internal/tui/sidebar_test.go` | Update for `list.Model` API |
| `internal/tui/repl_test.go` | Update for `textinput`/`viewport` API |
| `cmd/studio.go` | Update import path |
| `internal/renderer/tui/*.go` | Update lipgloss import paths |

## Implementation Steps

### Step 1: Dependency migration

```bash
# Remove v1 deps, add v2
go get charm.land/bubbletea/v2@latest charm.land/bubbles/v2@latest charm.land/lipgloss/v2@latest
# Remove old deps
go mod edit -droprequire github.com/charmbracelet/bubbletea
go mod edit -droprequire github.com/charmbracelet/lipgloss
go mod edit -droprequire github.com/charmbracelet/bubbles
go mod tidy
```

### Step 2: Mass import path update

Update all `.go` files:
- `github.com/charmbracelet/bubbletea` → `charm.land/bubbletea/v2`
- `github.com/charmbracelet/lipgloss` → `charm.land/lipgloss/v2`
- `github.com/charmbracelet/bubbles/...` → `charm.land/bubbles/v2/...`
- Leave `github.com/charmbracelet/glamour` unchanged (no v2)

### Step 3: Update all `tea.KeyMsg` → `tea.KeyPressMsg`

In ALL files that handle key messages:
- `tea.KeyMsg` type assertions → `tea.KeyPressMsg`
- Test constructors: `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}` → `tea.KeyPressMsg{Code: 'x'}`
- `tea.KeyMsg{Type: tea.KeyEnter}` → `tea.KeyPressMsg{Code: tea.KeyEnter}`
- `tea.KeyMsg{Type: tea.KeyBackspace}` → `tea.KeyPressMsg{Code: tea.KeyBackspace}`
- `tea.KeyMsg{Type: tea.KeyUp}` → `tea.KeyPressMsg{Code: tea.KeyUp}`
- `tea.KeyMsg{Type: tea.KeyDown}` → `tea.KeyPressMsg{Code: tea.KeyDown}`

Files: `app.go`, `sidebar.go` (if still accepting KeyMsg), `repl.go`, `page_wizard.go`, `save.go`, all `*_test.go`

### Step 4: Update `View()` return types

In `app.go`: `func (a *App) View() tea.View` returning `tea.NewView(string)`

Note: Sub-components (`sidebar`, `repl`, `content`, etc.) keep returning `string` from their `View()` methods — only the top-level `tea.Model` needs `tea.View`.

### Step 5: Keymap + Help system

**`keymap.go`** — Replace `keyBinding` struct with `bubbles/v2/key.Binding` and implement `help.KeyMap`:

```go
type KeyMap struct {
    CycleFocus, ToggleREPL, Select, Quit, ForceQuit key.Binding
    Save, Help                                       key.Binding
    NavUp, NavDown, NextSection, PrevSection         key.Binding
    ScrollUp, ScrollDown                             key.Binding
    HistoryUp, HistoryDown, Filter                   key.Binding
}

func (k KeyMap) ShortHelp() []key.Binding  { ... } // 4 key summary
func (k KeyMap) FullHelp() [][]key.Binding { ... } // grouped by category
func DefaultKeyMap() KeyMap                { ... }
```

**`help.go`** — Replace `renderHelp()` with `help.Model`. Short help renders in the status bar. Full help renders in the overlay (toggled by `?`).

### Step 6: Styles update

**`styles.go`** — Add styles for bubbles components:
- `defaultTableStyles()` for `bubbles/v2/table` (header, selected row, cell)
- Keep existing color palette (`accentColor`, `highlightColor`, etc.)

### Step 7: Sidebar with `bubbles/v2/list`

**`sidebar.go`** — Replace hand-rolled list with `list.Model`.

Union item type for all three sections:
```go
type sidebarItem struct {
    kind       int    // sectionPages/sectionTables/sectionQueries
    page       *PageItem
    table      *TableItem
    query      *QueryItem
    isHeader   bool
    headerText string
}
func (i sidebarItem) FilterValue() string { ... } // implements list.Item
```

Custom `list.ItemDelegate` to render:
- Section headers: bold accent text
- Pages: title
- Tables: `name (N rows)`
- Queries: title

`SetPages`/`SetTables`/`SetQueries` rebuild the flat item list, call `s.list.SetItems()`.

`Update` signature: `(msg tea.Msg) (SidebarModel, tea.Cmd)` — delegates to `list.Model.Update()`.

Built-in `/` filtering comes for free. J/K section jumping: find next header item index and call `s.list.Select(idx+1)`.

### Step 8: REPL with `textinput` + `viewport`

**`repl.go`** — Replace hand-rolled input with `textinput.Model` and add `viewport.Model` for scrollable output.

```go
type REPLModel struct {
    input         textinput.Model
    output        viewport.Model
    history       []string
    histIdx       int
    outputContent string  // accumulated output for viewport
}
```

- `textinput.New()` with `Prompt: "sql> "`, `CharLimit: 4096`
- History navigation: intercept Up/Down before delegating to textinput
- Enter: extract `input.Value()`, emit `replSubmitMsg`, call `input.SetValue("")`
- `replResultMsg` handler: append to `outputContent`, `output.SetContent()`, `output.GotoBottom()`
- View: viewport stacked above input line via `lipgloss.JoinVertical`
- Focus/Blur: managed by `App.setFocus()` calling `input.Focus()`/`input.Blur()`

### Step 9: Page viewer with Glamour + viewport

**`page_view.go`** — Render markdown through glamour, display in `viewport.Model`.

```go
type ContentModel struct {
    pageTitle, pageType, rawContent string
    viewport viewport.Model
}
```

- `SetPage(title, content, pageType string, width int)`: render header + glamour markdown → set viewport content
- `renderMarkdown()` helper already exists in `internal/renderer/tui/text.go` — add a local one to `page_view.go` since that package is unexported and in a different package
- `Update` delegates to `viewport.Update`, returns `tea.Cmd`
- `View` returns `viewport.View()`

### Step 10: Table browser with `bubbles/v2/table`

**`table_browser.go`** — Show actual row data in a scrollable table.

```go
type TableBrowserModel struct {
    tableName string
    rowCount  int
    table     btable.Model
    loading   bool
    err       string
}
```

New message type and async loader in `app.go`:
```go
type tableDataMsg struct {
    tableName string
    columns   []btable.Column
    rows      []btable.Row
    err       error
}

func (a *App) loadTableData(tableName string) tea.Cmd {
    // SELECT * FROM "tableName" LIMIT 1000 via a.engine.Execute
    // Convert engine.ResultSet → btable columns/rows
    // Auto-size column widths: max(len(header), max(len(cells))) clamped [8, 40]
}
```

- `SetLoading(name)`: show loading state
- `SetData(cols, rows, err)`: create `table.Model` with data and styles
- `Update` delegates to `table.Update`, returns `tea.Cmd`
- `handleSidebarSelect` for tables: set loading, return `a.loadTableData(name)` as `tea.Cmd`

### Step 11: Query editor with viewport

**`query_editor.go`** — Add `viewport.Model` for scrollable SQL + output display.

- `SetQuery` renders title + SQL + output into viewport content
- `Update` delegates to viewport
- `View` returns `viewport.View()`

### Step 12: Adaptive layout

**`layout.go`** — Responsive panel sizing:

```go
const (
    replHeightNormal   = 6
    replHeightExpanded = 14   // when REPL is focused
    collapseThreshold  = 60   // sidebar hidden below this width
)
```

New `computeLayout()`:
- `a.focus == focusREPL` → use `replHeightExpanded`
- `a.width < collapseThreshold` → `sidebarWidth = 0`, skip sidebar render
- Otherwise: existing 20% / 20-40px clamped logic

Status bar: replace hard-coded hint string with `a.helpView.ShortHelpView(a.keys.ShortHelp())`.

### Step 13: App integration

**`app.go`** — Wire everything together.

Struct additions:
```go
keys     KeyMap
helpView help.Model
```

Key changes:
- `NewApp`: initialize `DefaultKeyMap()`, `help.New()`, pass initial dimensions to sub-components
- `Init()`: return `textinput.Blink` if REPL is focused (cursor animation)
- `View()`: return `tea.NewView(string)` instead of `string`
- New `setFocus(f focus)` method: blur/focus textinput, enable/disable context-specific keybindings
- `cycleFocus()` and `toggleREPL()` call `setFocus()`
- `WindowSizeMsg` handler: propagate dimensions to all sub-components via `SetWidth`/`SetHeight`/`SetSize`
- `handleKeyMsg`: receive `tea.KeyPressMsg`, all panel delegations capture and return `tea.Cmd` from sub-component updates
- Handle new `tableDataMsg` in `Update()`
- `handleSidebarSelect`: extract item from `list.SelectedItem().(sidebarItem)`, handle table selection as async data load
- `renderHelp()`: pass `keys` and `helpView` to the help renderer

### Step 14: Update page_wizard.go and save.go

- `tea.KeyMsg` → `tea.KeyPressMsg` in Update methods
- Import path changes

### Step 15: Update tests

- **`app_test.go`**: Assert new fields (`keys`, `helpView`), test `setFocus` blur/focus, test `tableDataMsg` handling. Update `tea.KeyMsg` constructors → `tea.KeyPressMsg`. Check `View()` returns `tea.View`.
- **`sidebar_test.go`**: Update for `(SidebarModel, tea.Cmd)` return type, assert via `list.SelectedItem()`, test filtering. Update key constructors.
- **`repl_test.go`**: Assert via `input.Value()` instead of raw `input` field, test viewport output accumulation. Update key constructors.

### Step 16: Update renderer/tui imports

Update `internal/renderer/tui/{chart,detail,form,metric}.go` lipgloss import paths from `github.com/charmbracelet/lipgloss` to `charm.land/lipgloss/v2`.

## Verification

1. `go build ./...` — compiles clean
2. `go test ./...` — all tests pass
3. Manual smoke tests:
   - Launch studio → sidebar shows items with section headers
   - Type `/` in sidebar → filter mode activates
   - Select a table → actual row data loads in scrollable table
   - Select a page → markdown renders with glamour styling in scrollable viewport
   - Focus REPL (backtick) → REPL panel expands, cursor blinks
   - Type SQL, press Enter → results appear in scrollable output viewport
   - Resize terminal → layout adapts (sidebar collapses on narrow, REPL expands on focus)
   - Press `?` → help overlay shows grouped keybindings
