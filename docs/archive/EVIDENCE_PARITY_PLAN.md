> **SUPERSEDED (2026-09-22).** The page-directive rendering model this plan extends is being replaced by Notebooks (#66) and A2UI artifacts (#67). Its shipped work units remain in the code until then; do not add directives.
> See [`../direction.md`](../direction.md). Kept for history; do not plan new work from it.

# Evidence Parity Plan

Goal: Bring Vibe's page/dashboard rendering to feature parity with Evidence's core authoring model. DuckDB-only engine. Markdown editor without autocomplete.

> **Scope & status note (2026-05):** This plan covers the **reporting/dashboard** half of
> Vibe and is **folded into Phase 3 of [`ms-access-parity-plan.md`](ms-access-parity-plan.md)**
> (the live roadmap). Two of its assumptions have since been settled in the codebase:
> 1. **Single-engine DuckDB is done** — the `engine=` option and SQLite query path were
>    removed; WU-1 below is essentially complete.
> 2. **The editor is CodeMirror, not OverType** — `PageEditorView.vue` uses a CodeMirror
>    instance via `studio/frontend/src/composables/useVibeEditor.ts`, themed with the
>    **SchoolHouse** design system. WU-12 is reframed accordingly (see its note).

## Current State (Vibe)

**Page syntax:** Markdown with YAML frontmatter, SQL fenced blocks (```` ```sql name ```` ), single-line YAML directives (`{table: source}`), Go template expansion (`{{.Results.name}}`).

**Directives implemented:** `{table}`, `{chart}`, `{metric}`, `{detail}`, `{form}` — all render server-side to static HTML via Chart.js.

**Gaps vs Evidence:**
- Single dataset per chart, no multi-series/stacking
- No layout components (row/stack grids)
- No interactive filters (dropdowns, sliders, toggles)
- No value formatting system (just basic currency/percent/number)
- No conditional rendering (AST parsed but not evaluated)
- No inline `{value}` component
- Charts limited to single color, no axis config, no reference lines
- Tables have no pagination, column formatting, or aggregation
- No variable/parameter system beyond Go templates

## Simplifications

- **DuckDB only** — SQLite engine option removed from parser/executor; all queries run on DuckDB (done).
- **No autocomplete** — CodeMirror markdown editor (SchoolHouse theme), no schema-aware suggestions.
- **No Markdoc syntax** — keep the existing `{directive}` YAML syntax rather than adopting `{% component %}`. It's simpler and already works.
- **No SQL files** — queries live in pages (existing `_vibe_queries` table is for saved queries in the query editor, not page-level reuse).
- **No access control / row-level security** — single-user app.
- **No publishing** — `.vibe` files are the distribution format.

## Component Mapping: Evidence -> Vibe

| Evidence Component | Vibe Equivalent | Status |
|---|---|---|
| SQL code blocks | ```` ```sql name ```` | Done (engine= option removed) |
| `{% bar_chart %}` | `{chart: source, type: bar}` | Needs: multi-series, stacking, axis config |
| `{% line_chart %}` | `{chart: source, type: line}` | Needs: same as bar |
| `{% area_chart %}` | `{chart: source, type: area}` | Needs: same as bar |
| `{% scatter_chart %}` | `{chart: source, type: scatter}` | Needs: same as bar |
| `{% pie_chart %}` | `{chart: source, type: pie}` | Needs: same as bar |
| `{% big_value %}` | `{metric: ...}` | Needs: comparison, sparkline, title |
| `{% value %}` | NEW `{value: ...}` | Not started |
| `{% table %}` | `{table: source}` | Needs: pagination, fmt, column config |
| `{% dropdown %}` | NEW `{dropdown: ...}` | Not started |
| `{% button_group %}` | NEW `{button_group: ...}` | Not started |
| `{% slider %}` | NEW `{slider: ...}` | Not started |
| `{% text_input %}` | NEW `{text_input: ...}` | Not started |
| `{% toggle %}` | NEW `{toggle: ...}` | Not started |
| `{% if %}` / `{% else %}` | `{if: expr}...{/if}` | AST exists, evaluator missing |
| `{% row %}` / `{% stack %}` | NEW `{row}...{/row}`, `{stack}...{/stack}` | Not started |
| `{% tabs %}` / `{% tab %}` | NEW `{tabs}...{/tabs}` | Not started |
| `{% callout %}` | NEW `{callout: ...}` | Not started |
| `{% accordion %}` | Skip for MVP | — |
| `{% histogram %}` | Skip for MVP | — |
| `{% heatmap %}` | Skip for MVP | — |
| `{% sankey_chart %}` | Skip for MVP | — |
| `{% funnel_chart %}` | Skip for MVP | — |
| `{% map %}` | Skip for MVP | — |
| `{% modal %}` | Skip for MVP | — |
| `{% download %}` | Skip for MVP | — |
| `{% detail %}` | `{detail: source}` | Done |
| `{% form %}` | `{form: table}` | Done (POST handler still needed) |
| Frontmatter variables | `{{.Params.key}}` | Partially done |
| Input-created variables | Not started | Requires reactive execution |
| `fmt` formatting | Basic formats only | Needs Evidence fmt codes |
| Reference lines/areas | Not started | Chart.js plugin |

---

## Work Units

### Phase 1: Foundation (engine + formatting + variables)

#### WU-1: DuckDB-Only Simplification — ✅ done
Dual-engine complexity removed. All SQL blocks run on DuckDB; the `engine=` parser option
is gone and the SQLite query path is unused. Listed here for completeness; no further work.

Files:
- `internal/pages/parser.go` — remove engine validation, always set `Engine: "duckdb"`
- `internal/pages/executor.go` — remove engine routing, always call DuckDB
- `internal/engine/engine.go` — simplify Manager to single engine
- `studio/app.go` — remove engine selector from query execution
- `studio/frontend/src/views/QueryEditorView.vue` — remove engine dropdown

#### WU-2: Value Formatting System
Implement Evidence-compatible `fmt` codes usable across all components.

New file: `internal/renderer/format.go`

Format codes to support:
- Numbers: `num0`, `num1`, `num2`, `num1k`, `num1m`, `num1b`
- Currency: `usd0`, `usd1`, `usd2`, `usd0k`, `usd1m` (+ eur, gbp)
- Percentages: `pct0`, `pct1`, `pct2`
- Dates: `shortdate`, `longdate`, `hms`, `hm`
- Identity: `id` (no formatting)

Integrate into existing metric/table/chart renderers via a `Format(value any, fmt string) string` function.

#### WU-3: Variable & Parameter System
Extend the existing Go template system to support Evidence-style variables.

- Frontmatter variables accessible as `{{.Vars.name}}` (already partially works via `{{.Params}}`)
- Input components (dropdowns, etc.) create named variables during execution
- Variables usable in SQL blocks via `{{var_name}}` substitution before query execution
- Conditional SQL: `[[ AND col = {{filter.selected}} ]]` — include clause only when variable is set

Files:
- `internal/pages/executor.go` — add variable registry, SQL template substitution
- `internal/pages/template.go` — extend template context with variables
- `internal/pages/ast.go` — add variable tracking to execution state

---

### Phase 2: Layout & Conditional Rendering

#### WU-4: Block Directives (Row, Stack, Tabs, Callout)
Add block-level directives that contain children, similar to `{if:}...{/if}`.

New syntax:
```
{row}
  {metric: label: "Revenue", sql: "SELECT sum(total) FROM orders"}
  {metric: label: "Orders", sql: "SELECT count(*) FROM orders"}
{/row}

{stack}
  ...content...
{/stack}

{tabs}
  {tab: "Overview"}
    ...content...
  {tab: "Details"}
    ...content...
{/tabs}

{callout: type: "info"}
  Important message here.
{/callout}
```

Files:
- `internal/pages/parser.go` — generalize block parsing (currently only `{if}` is a block)
- `internal/pages/ast.go` — add `BlockNode` type with `Type`, `Config`, `Children`
- `internal/renderer/html/layout.go` — new file for row/stack/tabs/callout rendering
- `internal/renderer/html/renderer.go` — wire up block rendering

#### WU-5: Conditional Rendering
Implement the `{if}` evaluator that was parsed but never executed.

Conditions to support:
- `{if: "query_name"}` — render if query returned rows (Evidence default)
- `{if: "query_name", condition: "no_rows"}` — render if query returned no rows
- `{if: "expr"}` — simple expression evaluation (e.g., `{{.Vars.filter}} == 'value'`)

Files:
- `internal/pages/executor.go` — evaluate conditionals during execution
- `internal/pages/ast.go` — already has `ConditionalNode`

---

### Phase 3: Enhanced Components

#### WU-6: Inline Value Component
New `{value}` directive for displaying a single formatted value inline.

```
{value: source, column: "total_sales", fmt: "usd1m"}
```

Or with inline SQL:
```
{value: sql: "SELECT sum(total) FROM orders", fmt: "usd1m"}
```

Files:
- `internal/renderer/types.go` — add `ValueConfig`
- `internal/renderer/html/value.go` — new renderer
- `internal/renderer/html/renderer.go` — wire up
- `internal/pages/executor.go` — handle `value` directive type

#### WU-7: Enhanced Metric (Big Value)
Upgrade `{metric}` to support Evidence's `big_value` features:
- `title` — label above the value
- `fmt` — use WU-2 format codes
- `comparison` — delta vs prior period (requires two-value result)
- `sparkline` — mini trend line (requires time-series result)

```
{metric: source, value: "total_sales", title: "Revenue", fmt: "usd1m", comparison: "prior_period"}
```

Files:
- `internal/renderer/types.go` — extend `MetricConfig`
- `internal/renderer/html/metric.go` — add comparison delta + sparkline rendering

#### WU-8: Enhanced Table
Upgrade `{table}` to support:
- `pageSize` — client-side pagination (already in config, not rendered)
- `fmt` — per-column formatting via column config map
- `title` / `subtitle` — header text
- `row_shading` — alternate row colors
- `wrap` — cell content wrapping
- `order` — default sort column
- `limit` — max rows

```
{table: source, pageSize: 20, title: "Orders", fmt: {total: "usd2", date: "shortdate"}}
```

Files:
- `internal/renderer/types.go` — extend `TableConfig`
- `internal/renderer/html/table.go` — add pagination JS, column formatting, title

#### WU-9: Enhanced Charts
Upgrade all chart types to support:
- **Multi-series:** `series` key to split data by a categorical column
- **Stacking:** `stacked: true` or `stacked: "100%"`
- **Axis config:** `x_title`, `y_title`, `y_range`, `x_fmt`, `y_fmt`
- **Multiple Y columns:** `y: [col1, col2]`
- **Colors:** `colors: [...]` palette, `series_colors: {name: color}`
- **Reference lines:** `ref_lines: [{value: 50000, label: "Target"}]`
- **Data labels:** `data_labels: true`
- **Legend:** `legend: true/false`
- **Horizontal bar:** `{chart: source, type: horizontal_bar}`

```
{chart: source, type: bar, x: "month", y: ["revenue", "cost"], stacked: true, y_fmt: "usd1m"}
```

Files:
- `internal/renderer/types.go` — extend `ChartConfig` significantly
- `internal/renderer/html/chart.go` — rewrite chart rendering for multi-dataset support
- Chart.js annotation plugin for reference lines

---

### Phase 4: Interactive Filters

#### WU-10: Dropdown Filter
New `{dropdown}` directive that creates a named variable from user selection.

```
{dropdown: id: "region_filter", source: "regions", column: "region", title: "Region"}
```

Renders as a `<select>` element. On change, re-executes downstream queries that reference `{{region_filter.selected}}` and re-renders dependent components.

**Reactivity model:** Since Vibe renders server-side HTML, interactivity requires either:
- **Option A: Full re-render** — dropdown change triggers a page re-render with the selected value as a parameter. Simpler, works with current architecture.
- **Option B: Client-side HTMX** — dropdown change fetches updated HTML fragments. More responsive, more complex.

Recommend **Option A** for MVP: dropdown selection triggers Go backend re-execution with filter values, full page HTML replaced.

Files:
- `internal/pages/ast.go` — add `InputNode` type
- `internal/pages/parser.go` — parse input directives
- `internal/pages/executor.go` — register input values as variables, inject into SQL
- `internal/renderer/html/dropdown.go` — render `<select>` with options from query
- `internal/renderer/html/input.go` — shared input rendering utilities
- `studio/app.go` — `RenderPageContent` accepts filter state map
- `studio/frontend/` — pass filter selections back to Go on change

#### WU-11: Additional Input Filters
Following the same pattern as WU-10:
- `{button_group: id, source, column}` — segmented buttons
- `{slider: id, min, max, step, default}` — numeric range
- `{text_input: id, placeholder}` — free-text filter
- `{toggle: id, default}` — boolean switch

Each creates a named variable usable in SQL and conditionals.

Files:
- `internal/renderer/html/button_group.go`
- `internal/renderer/html/slider.go`
- `internal/renderer/html/text_input.go`
- `internal/renderer/html/toggle.go`

---

### Phase 5: Frontend Integration

#### WU-12: Page Editor Component Toolbar

> **Reconciliation note:** The editor decision landed on **CodeMirror**, not OverType.
> `PageEditorView.vue` already runs a CodeMirror instance via the
> `studio/frontend/src/composables/useVibeEditor.ts` composable (themed with the
> **SchoolHouse** design system, with an `insertSnippet` seam for toolbar buttons).
> OverType is *not* a dependency. The remaining deliverable for this WU is therefore the
> **Vibe component-insertion toolbar** (the directive buttons in the table below), wired to
> that composable. The OverType-specific prose that follows is retained for historical
> context only — read "OverType" as "the CodeMirror editor" throughout.

The original plan (superseded) was to replace the plain `<textarea>` in PageEditorView with [OverType](https://github.com/panphora/overtype) — a lightweight WYSIWYG markdown editor that overlays an invisible textarea on a styled preview. This gives users live-rendered markdown (bold, italic, headings, lists) while editing, without the complexity of a full rich-text editor.

**Why OverType:**
- ~117KB, framework-agnostic, drops into Vue with a `ref` target
- Invisible textarea overlay = perfect 1:1 character mapping, no AST divergence
- Built-in toolbar is extensible with custom buttons via `toolbarButtons` array
- `getValue()`/`setValue()` for programmatic content access
- `onChange` callback for debounced preview re-rendering
- Supports `cave` (dark) and `solar` (light) themes, plus custom theme objects

**Installation:**
```bash
cd studio/frontend && npm install overtype
```

**Integration approach:**

Replace the raw `<textarea v-model="content">` with a `<div ref="editorEl">` mount point. Initialize OverType in `onMounted`, wire `onChange` to the existing debounced preview pipeline, and sync `getValue()`/`setValue()` with the Vue `content` ref.

```javascript
import OverType, { toolbarButtons } from 'overtype'

const editorEl = ref(null)
let editor = null

onMounted(() => {
  const [instance] = new OverType(editorEl.value, {
    value: content.value,
    theme: 'cave',
    toolbar: true,
    toolbarButtons: [
      // Standard markdown formatting
      toolbarButtons.bold,
      toolbarButtons.italic,
      toolbarButtons.code,
      toolbarButtons.link,
      toolbarButtons.separator,
      toolbarButtons.h1,
      toolbarButtons.h2,
      toolbarButtons.h3,
      toolbarButtons.separator,
      toolbarButtons.bulletList,
      toolbarButtons.orderedList,
      toolbarButtons.quote,
      toolbarButtons.separator,
      // === Vibe component insertion buttons ===
      ...vibeToolbarButtons
    ],
    onChange: (value) => {
      content.value = value
      // triggers existing watch → debouncedPreview()
    }
  })
  editor = instance
})
```

**Custom Vibe toolbar buttons** — each inserts a directive snippet at the cursor position:

| Button | Icon | Inserts |
|--------|------|---------|
| SQL Block | `<db>` | ` ```sql query_name\nSELECT ...\n``` ` |
| Bar Chart | `<bar-chart>` | `{chart: query_name, type: bar, x: "col", y: "col"}` |
| Line Chart | `<line-chart>` | `{chart: query_name, type: line, x: "col", y: "col"}` |
| Pie Chart | `<pie-chart>` | `{chart: query_name, type: pie, x: "col", y: "col"}` |
| Table | `<table>` | `{table: query_name}` |
| Metric | `<hash>` | `{metric: label: "Label", sql: "SELECT count(*) FROM table"}` |
| Value | `<type>` | `{value: query_name, column: "col", fmt: "num0"}` |
| Dropdown | `<filter>` | `{dropdown: id: "filter", source: query_name, column: "col"}` |
| Row Layout | `<columns>` | `{row}\n  ...\n{/row}` |
| Callout | `<info>` | `{callout: type: "info"}\n  ...\n{/callout}` |
| Conditional | `<branch>` | `{if: "query_name"}\n  ...\n{/if}` |

**Custom button implementation pattern:**
```javascript
const vibeToolbarButtons = [
  { name: 'separator' },
  {
    name: 'sql-block',
    icon: '<svg>...</svg>',  // or use a simple text icon
    title: 'Insert SQL query block',
    action: ({ editor, getValue }) => {
      // Insert at cursor via textarea selection API
      const ta = editor.querySelector('textarea')
      const pos = ta.selectionStart
      const val = getValue()
      const snippet = '```sql query_name\nSELECT * FROM table_name\n```\n'
      const newVal = val.slice(0, pos) + snippet + val.slice(pos)
      editor.setValue(newVal)
      // Position cursor inside the SQL block
    }
  },
  {
    name: 'bar-chart',
    icon: '<svg>...</svg>',
    title: 'Insert bar chart',
    action: ({ editor, getValue }) => {
      insertSnippet(editor, getValue,
        '{chart: query_name, type: bar, x: "column", y: "column"}\n')
    }
  },
  // ... etc for each component
]
```

**Shared `insertSnippet` helper:**
```javascript
function insertSnippet(editorEl, getValue, snippet) {
  const ta = editorEl.querySelector('textarea')
  const pos = ta.selectionStart
  const val = getValue()
  // Ensure blank line before directive
  const prefix = pos > 0 && val[pos-1] !== '\n' ? '\n' : ''
  const newVal = val.slice(0, pos) + prefix + snippet + val.slice(pos)
  // setValue triggers onChange → preview update
}
```

**Theme integration:**
The editor theme matches the Studio **SchoolHouse** design system and is already
implemented in `useVibeEditor.ts`. (The OverType theme object below is historical; the
live CodeMirror theme lives in that composable.)
```javascript
{
  theme: {
    name: 'vibe',
    colors: {
      bgPrimary: '#ffffff',
      bgSecondary: '#f5f5f0',
      text: '#1a1a1a',
      h1: '#1a1a1a',
      h2: '#1a1a1a',
      h3: '#1a1a1a',
      strong: '#1a1a1a',
      em: '#1a1a1a',
      link: '#2563eb',
      code: '#dc2626',
      codeBg: '#f5f5f0',
      blockquote: '#6b7280',
      hr: '#1a1a1a',
      syntaxMarker: '#9ca3af',
      cursor: '#1a1a1a',
      selection: '#fef08a'  // highlight matching the SchoolHouse accent
    }
  }
}
```

**Preserving split-pane layout:**
The left pane is the CodeMirror editor (`useVibeEditor.ts`); the toolbar sits above it. The right pane (server-rendered preview) stays the same — it shows the full executed output with charts, tables, and directives rendered.

**Filter state in preview:**
`RenderPageContent` already accepts a params map. When the rendered preview HTML contains filter elements (dropdowns, etc.), intercept their change events via `postMessage` or DOM event delegation, collect filter state, and re-call `RenderPageContent(pageID, filterState)`.

Files:
- `studio/frontend/src/composables/useVibeEditor.ts` — existing CodeMirror setup + SchoolHouse theme; expose an `insertSnippet(snippet)` helper for the toolbar
- `studio/frontend/src/views/PageEditorView.vue` — add the component-insertion toolbar above the CodeMirror editor
- `studio/frontend/src/components/VibeToolbarIcons.ts` — SVG icons for component buttons
- `studio/app.go` — `RenderPageContent` accepts params map for filter state

#### WU-13: Dashboard View as a Page
Replace the hardcoded `DashboardView.vue` with a special system page (`_dashboard`) that uses the same rendering pipeline. Users can customize their dashboard using the same directive syntax.

Default dashboard page content auto-generated on file creation with metrics, charts, and tables summarizing the container's data.

Files:
- `internal/container/examples.go` — default dashboard page content
- `studio/app.go` — `GetDashboard` renders the `_dashboard` page
- `studio/frontend/src/views/DashboardView.vue` — render HTML from Go instead of hardcoded stats

#### WU-14: CSS Theme & Styling
Create a cohesive default stylesheet for all rendered components, aligned with the Studio **SchoolHouse** design system (`studio/frontend/src/schoolhouse/`) and Evidence's clean look.

- Consistent spacing, typography, colors
- Responsive grid for `{row}` layout
- Card styling for metrics
- Clean table styles with pagination controls
- Chart container sizing
- Filter bar styling
- Dark mode support (matches the Studio SchoolHouse theme)

Files:
- `internal/renderer/html/styles.go` — embedded CSS (or separate .css file)
- `internal/renderer/html/renderer.go` — inject stylesheet into rendered output

---

## Execution Order & Dependencies

```
Phase 1 (Foundation)        Phase 2 (Layout)         Phase 3 (Components)       Phase 4 (Filters)        Phase 5 (Frontend)

WU-1 DuckDB-only ──────┐
                        │
WU-2 Formatting ────────┼── WU-4 Block directives    WU-6 Value ──┐
                        │                            WU-7 Metric ─┤
WU-3 Variables ─────────┘── WU-5 Conditionals        WU-8 Table ──┼── WU-10 Dropdown ──── WU-13 Dashboard
                                                     WU-9 Charts ─┘   WU-11 Inputs ────── WU-14 CSS Theme

WU-12 Editor Toolbar ──────────────────────────────────────────────────────────────────────┘
  (CodeMirror is in place; component-insertion buttons added incrementally as components land)
```

- Phase 1 has no internal dependencies, all 3 WUs can be done in parallel
- **WU-12 (editor toolbar) can start immediately** — the CodeMirror editor (`useVibeEditor.ts`) is already in place; this WU adds the component-insertion toolbar, with custom Vibe component buttons added incrementally as each component WU lands. No blocker.
- Phase 2 depends on Phase 1 (block parsing needs the simplified parser, conditionals need variables)
- Phase 3 depends on WU-2 (formatting) but can run parallel to Phase 2
- Phase 4 depends on WU-3 (variables) and Phase 3 components
- Phase 5 (dashboard + CSS) depends on everything else
- WU-12 toolbar buttons for filters (dropdown, slider, etc.) added after WU-10/WU-11 land

## Out of Scope (Future)

- Horizontal bar chart (can add as chart type later)
- Histogram, heatmap, sankey, funnel, polar charts
- Map component (requires Mapbox)
- Accordion, modal, details components
- Download/export button component
- Sparkline standalone component (only as metric add-on)
- Data source connectors (Vibe is self-contained)
- SQL autocomplete in editor
- Partials / reusable page fragments
- Print/PDF layout components
- Comparison selectors / date grain selectors
- Dimension grid component
- Commentary component
