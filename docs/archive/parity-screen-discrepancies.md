> **SUPERSEDED (2026-09-22).** The mockup set this audit compares against belongs to the Access-parity direction, which is superseded.
> See [`../direction.md`](../direction.md). Kept for history; do not plan new work from it.

# Screen Parity Discrepancy Catalog

**Date:** 2026-05-31
**Scope:** Each in-scope hi-fi mockup screen (`tmp/design/hifi/`) compared against its shipped Vue 3 implementation (`studio/frontend/src/`).
**Method:** Per-screen audit of mockup JSX (treated as visual/UX spec — *not* a framework decision) vs. the real Vue view + SchoolHouse tokens. No code was changed.

> Out of scope (deferred to v1.1 per the parity plan): Onboarding/Node-Setup, Node Manager, Conflict Resolution. Not audited here.

## Severity legend
- **High** — a core feature, section, or screen concept is missing or fundamentally different.
- **Medium** — a notable layout or interaction gap.
- **Low** — cosmetic / design-token / copy mismatch.

---

## Cross-cutting finding: the Agent is invisible

The single biggest gap spans multiple screens. The **Agent Chat Panel is entirely absent from the frontend** — no view, no route, no nav entry, no slide-in panel, no streaming tool-use display, no pin-context. The Go bindings (`SendAgentQuery`, `GetAgentStatus`, `AgentResult`) exist and are **orphaned** — no `.vue` file calls them.

Because the agent is the product's "wizard layer," its absence also guts the agent-driven flows the mockups build screens around:
- Schema Builder's conversational table proposal + streaming ER diagram
- Form Builder's "Ask agent" / "Apply with agent" per-field affordance
- Switchboard's "Have agent suggest layout"
- Report View's "Ask the agent → Modify this report"
- Query Explorer's "Ask agent" toolbar button

**Severity: High.** This is the top item to resolve.

---

## 1. Studio Home / Dashboard — `DashboardView.vue`

The shipped dashboard is a thin wrapper around a server-rendered `_dashboard` page (one live table count + a table-name list) plus a hardcoded Pages/Tables/Queries shortcut grid. The mockup is a multi-app builder home.

**High**
- **Stat tiles row absent** — Mockup: 4 KPI tiles (Apps, Records·7d w/ delta, Nodes online, Agent runs·24h). Impl: a single `{metric: Tables}` count.
- **"My Apps" section absent** — Mockup: grid of striped app cards (status badge, "edited {time}", click-to-open / right-click-to-edit). Impl: no apps concept on the dashboard.
- **"Recent activity" feed absent** — Mockup: cross-app activity list. Impl: none.
- **"Start from a template" rail absent** — Mockup: 4 starter templates with "Use" buttons. Impl: none.
- **Empty-state hero absent** — Mockup: `StudioHomeEmpty` ("No apps yet. Describe your business…" + quick-start templates). Impl: no empty-state branch.

**Medium**
- "Open template gallery" + "Create new app" header actions → Impl has "Import CSV" + "New page" instead.
- No create-app / template-instantiation flow anywhere on the screen.

**Low**
- Title copy: mockup personalizes ("Welcome back, James."); impl shows file title or literal "Dashboard".
- Quick-actions grid (Pages/Tables/Queries) is net-new, not in the mockup.
- `.stats`/`.stat` tokens ship in `kit.css` but are never instantiated.

---

## 2. Schema Builder — `SchemaDesignerView.vue` + `RelationshipsView.vue`

Effectively unbuilt relative to the mockup. The mockup is one conversational screen (chat ↔ live ER diagram); the impl is two form-based DDL editors. The mechanical substrate (column DDL, field metadata, FK declaration) exists, but none of the three headline concepts do.

**High**
- **Conversational agent chat pane** — not implemented at all (`amsg`/`bot` CSS exists, used by zero components).
- **Live ER diagram** — not implemented. Impl shows fields as a flat HTML table; relationships as a vertical list. No canvas, no node positioning, no connector arrows.
- **Chat composer** ("Add details… or paste an old spreadsheet schema", Attach CSV / Send) — absent.
- **Streaming schema-generation sequence** (tables materialize frame-by-frame w/ tool chips) — absent.
- **NL → schema generation loop** (describe → agent proposes → confirm → build) — absent; user manually runs DDL forms.
- **Split-pane (chat | diagram) layout** — impl is a tri-pane field editor; completely different.

**Medium**
- "Proposed tables · click to edit" agent card — absent.
- Inline clarifying-question quick replies — absent.
- "Discard" / "Looks good — build it" draft→commit lifecycle — absent (DDL mutates live schema immediately).
- Friendly field-type vocabulary (`ID`/`Link`/`Pick`) — impl exposes raw SQL types only.
- Schema work split across two routes (`/schema`, `/relationships`) vs. one unified screen.

**Low**
- FK arrow color semantics (teal solid = FK, marigold dashed = optional) — no arrows at all.
- Versioned breadcrumb / "Schema · v0.3" — absent.
- Conversational/business copy → impl is technical/CRUD copy.

---

## 3. Form Builder — `FormView.vue`

`FormView.vue` is a runtime form **player**, not the **authoring** tool the mockup specifies. The three-pane builder CSS (`.split-page`, `.col-pane`, `.field-row`, `.seg`, `.ask-agent`) ships but is unused.

**High**
- **Properties panel absent** — no per-field editor (Label, Placeholder, Required, Visibility rule, Validation).
- **"Ask agent" field-edit affordance absent** ("Make this hide when status is Closed" → Apply with agent).
- **Field palette / draggable field list absent** — the left column is a list of *forms*, not fields; no "Add field".
- **Archetype selector absent** (Columnar / Continuous / Datasheet / Master-Detail) — archetype is read-only caption text; only a fixed columnar+subform layout renders.
- **Three-pane builder layout** → impl is two-pane runtime (form-list | bound record).
- **No field-selection model** — can't click a field to edit it.
- **Validation & visibility rules not authorable** (server-side write-back only).

**Medium**
- "Preview in App Mode" + "Publish" buttons — absent.
- "Unsaved" dirty-state badge — absent.
- Live-preview status row ("pixel-accurate to App Mode · Updates in 60ms") — absent.
- Required-marker + help-text in preview — absent.
- No add/remove field or drag-to-reorder.

**Low**
- Builder CSS vocabulary shipped but unused; `--sh-coral` required-asterisk + dashed-teal selection outline never applied.
- Subform heading shows raw table name (`parts_used · subform`) vs. caption ("Parts used").
- "Add row" vs. mockup "Add part"; no computed Total column.
- Impl adds Access-faithful record-nav/Find/MsgBox the mockup doesn't show (divergent, not wrong).

---

## 4. Switchboard / App Shell Builder — `AppShellView.vue`

A functional list-based nav editor, not the specced spatial builder. `.sw-canvas`/`.sw-node` ship but are unreferenced.

**High**
- **No visual canvas of screen tiles** — impl is a flat `<ul>` of single-line rows; no tiles, no connector arrows.
- **No "Add to shell" source palette** (Forms/Reports/Dashboards grouped w/ icons) — impl has one `<select>` dropdown.
- **No "Advanced · access rules" section** — the screen's headline feature (per-node/role visibility matrix) is absent from UI, the `NavItem` model, and the `SaveNav` payload.
- **No selected-item inspector** (Display name / Icon / Sort order) — editing is inline label-only.

**Medium**
- "Preview as Maria", "Publish shell", "Have agent suggest layout" actions — absent.
- Three-pane layout (palette / canvas / inspector) → impl is a single centered column.
- Home/start node not visually distinguished from children.
- Drag-to-add + spatial placement → impl uses dropdown-add + up/down reorder buttons.

**Low**
- Per-item meaningful icons → impl hardcodes one `apps` icon for every tile.
- Type/subtitle metadata ("Form · master-detail") dropped; raw `action_kind` shown instead.
- 8-item-per-page cap surfaced as a blocking notice the mockup doesn't depict.
- Breadcrumb / "App Shell" framing / canvas caption — absent.

---

## 5. App Mode + Mode Toggle — `AppShell.vue` + `useAppMode.ts`

Builder tools **are** correctly hidden in App Mode (router guard + `v-if="!isAppMode"`), and return-to-Studio is passphrase-gated (exceeds the mockup). But the App-Mode *chrome* is essentially the Studio chrome reskinned by one class that doesn't match the CSS selectors — the white/marigold locked shell (`.topbar.app` → `.brand-block`/`.tabstrip`/`.lockpill`) is styled in `hifi.css` but never rendered.

**High**
- **Locked tab bar entirely absent** — no primary-task tab strip in App Mode (mockup's core navigation chrome).
- **Lock pill / session indicator absent** ("Locked · Mar 14 · 2:14 PM") — the signal of the locked-down state is gone.
- **`app-primary` (marigold) button variant not defined** — no `.btn-app-primary`; App-Mode buttons render teal or inline-marigold only.
- **Sidebar shell retained in App Mode** — spec says "No sidebar"; impl keeps the 240px grid gutter and just empties the aside. `.app-mode` root class has no CSS to collapse the grid.
- **Top bar is dark teal-ink, not white-with-marigold** — rendered markup uses `.brand`/`.topright`, so the white `.topbar.app` rules never apply; logo mark stays teal.

**Medium**
- ⌘⇧L mode-toggle shortcut — no keyboard handler (click-only).
- Toggle framed as buried sidebar button vs. mockup's prominent "Preview as Maria" control.
- App Mode lands on the switchboard button grid, not the bound data-entry form the mockup depicts (arguably Access-faithful, but divergent).
- User identity/role line ("Maria · Receptionist") absent; avatar hardcoded "VS".
- 44px/52px hit targets only honored on switchboard buttons, not on toggle/return buttons or form inputs.

**Low**
- Mobile App Mode (bottom tab bar) not implemented (likely out of desktop scope).
- Mode pill is a small marigold dot on a dark bar vs. spec's marigold-dominant chrome.
- Copy: "Exit App Mode (Studio Mode)" vs. preview-oriented framing.

---

## 6. Query Explorer — `QueryEditorView.vue`

Partially implemented: editor + saved-query list + basic results render. The shipped left rail is a saved-query list, not the mockup's schema tree.

**High**
- **Schema tree absent** — no left-rail table/column browser with PK/FK badges (`.schema-tree` CSS ships unused).
- **CSV / Parquet export controls absent** — only Save + Run exist.
- **"Ask agent" toolbar affordance absent.**

**Medium**
- Run-status metadata (success badge, DuckDB engine label, last-run timestamp) — absent (only row count + duration).
- Line-number gutter / syntax highlighting — plain `<textarea>` (note: CLAUDE.md claims CodeMirror; this view doesn't use it).
- Breadcrumb + filename + dirty-state badge — absent.
- Two-column (schema tree | editor) intent → schema rail replaced by query-list rail.

**Low**
- Result bar is not `position: sticky` — scrolls away.
- `status` column rendered as plain text vs. mockup's teal badge.
- Run button copy "Ctrl+Enter" vs. "⌘⏎"; engine label never shown.
- **Stale dual-engine copy** — empty state references `[engine=duckdb]` / `[engine=sqlite]`, contradicting single-engine reality.

---

## 7. Report View — `ReportView.vue`

Implements the three required mechanics — live DuckDB filters, chart↔table toggle, studio-dev-gated Edit-SQL — but diverges structurally. Mockup is a single-report dashboard; impl is a report-list browser with an either/or chart-or-table canvas.

**High**
- **Stacked/multi-series chart** — impl is single-series, non-stacked.
- **Detail / KPI tiles** ("Top part this month", "Trend +34%") — absent.
- **"Ask the agent → Modify this report" card** — absent.
- **Multi-pane grid** (chart + tiles/agent side-by-side, then full-width detail table) → impl shows *either* chart *or* table, never both; no tiles/agent column.
- **Faceted filters** (range / technician / status multi-select / category) → impl is one generic single column=value filter (at most one active).

**Medium**
- Breadcrumb + per-report title ("Parts usage · last 6 months") → impl static "Reports".
- "Last run · time · ms" metadata strip + "Run now" button — absent.
- Chart legend block — absent.
- Filters as carded labeled fields → impl bare native `<select>`s; no "Reset" button.
- Export: mockup "Export CSV" → impl "Print/PDF" + "Analyze in Excel" (neither is CSV).
- "Edit SQL" opens a **read-only** `<pre>`, not an editor, despite the label. (Role-gating itself is correct.)

**Low**
- Chart/Table toggle default + order differ (mockup Chart-first; impl Table-first).
- Hard-coded `#2F8F87` chart color bypasses subject tokens; no per-series palette.
- Token names `--color-*` / `--font-*` vs. SchoolHouse `--sh-*` — verify they alias.
- No double-click-to-open row drill-through; no row-count caption.
- Card/"stripe" framing absent around chart/table.

---

## Priority rollup

**Tier 1 — structural / headline concepts missing**
1. Agent Chat Panel (absent everywhere; bindings orphaned) — unblocks 5 other screens.
2. Schema Builder: conversational flow + live ER diagram (screen effectively unbuilt).
3. Form Builder: the authoring tool itself (current view is a runtime player).
4. Switchboard: visual canvas + access-rules section + inspector.
5. App Mode: locked tab-bar chrome + white/marigold shell + sidebar collapse.

**Tier 2 — significant gaps within otherwise-present screens**
- Dashboard: stat tiles, My Apps, activity, templates, empty state.
- Query Explorer: schema tree, export, run metadata.
- Report View: stacked multi-series chart, KPI tiles, faceted filters, side-by-side layout.

**Tier 3 — cosmetic / token / copy** (the Low items above), plus two correctness notes:
- Query Explorer stale dual-engine copy (`[engine=sqlite]`).
- Report View "Edit SQL" is read-only despite the label.

> Recurring theme: many mockup CSS classes (`.split-page`, `.sw-canvas`, `.schema-tree`, `.ask-agent`, `.topbar.app`) were ported into `schoolhouse/*.css` but never wired to markup — the design vocabulary shipped ahead of the views that consume it.
