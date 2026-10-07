> **SUPERSEDED (2026-09-22).** The screens this plan remediates (Form Builder, Switchboard, Schema Builder, App Mode) are being removed (#68). §1's Rodney verification protocol is still the QA standard and is referenced from CLAUDE.md.
> See [`../direction.md`](../direction.md). Kept for history; do not plan new work from it.

# Parity Remediation Plan

**Date:** 2026-05-31
**Input:** `docs/parity-screen-discrepancies.md` (per-screen mockup-vs-implementation audit).
**Goal:** Close the screen-parity gaps — and, crucially, fix the *process* that let unbuilt screens pass as "done."

---

## 0. Root cause (read this first)

The first parity pass produced views that **passed `*.spec.ts` (Vitest) but were never rendered in a real browser against the real backend.** Vitest mounts components in jsdom with mocked Wails bindings — a form-player can "pass tests" while the *actual* screen is missing its palette, properties panel, agent affordances, and live data. Many mockup CSS classes (`.split-page`, `.sw-canvas`, `.schema-tree`, `.ask-agent`, `.topbar.app`) were even shipped into `schoolhouse/*.css` but never wired to markup — invisible to a jsdom test, obvious in a browser.

**The fix is a hard verification gate:** every screen's acceptance is proven with **Rodney (headless Chrome) driving the full Wails-bound dev server at `http://localhost:34115`** — real Go bindings, real DuckDB, real `.vibe` data, real rendered pixels. Vitest specs are *supplementary* (logic/edge cases), never the acceptance criterion. **A screen is not done until it has been opened in the browser, screenshotted, and the screenshot visually matches the mockup.**

---

## 1. Verification protocol (mandatory for every workstream)

### 1.0 Agentic features — local LLM server
Agent-driven flows (W0, and the "Ask agent" affordances in W2/W3/W4/W6) are testable end-to-end against a **local LLM server at `http://localhost:8080`**. Point the `.vibe` agent config at it and exercise real `SendAgentQuery` round-trips — assert that an actual model response renders, not just that the binding was called. If `:8080` is unreachable, treat the agent test as **blocked/skipped and say so explicitly** in the PR — do not stub a fake response to make the gate pass.

### 1.1 Bring up the real stack — once per session
```bash
# 1. Start the Wails dev server (NOT bare `wails dev` — needs the build tags).
#    Runs wails dev -tags webkit2_41,duckdb_arrow; Go bindings serve at :34115.
task studio:dev            # run in background; wrapper may report exit 0 while it keeps serving

# 2. Confirm it's actually serving the Go-bound app (NOT the bare :5173 frontend):
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:34115/     # expect 200

# 3. Start headless Chrome:
rodney start
rodney open http://localhost:34115/
rodney waitload
```

**Rule: always drive `http://localhost:34115` (Wails-bound), never `http://localhost:5173`.** Port 5173 is the bare Vite frontend with **no Go bindings** — `window.go.main.App.*` is undefined there. The whole point is to exercise the real backend. If `window.go?.main?.App` is undefined, you are on the wrong port — stop and fix the harness, do not mock.

### 1.2 Get past Unlock into a populated app (CLI init can't be scripted headlessly)
`./vibe init` prompts for a passphrase over TTY and has no `--passphrase` flag, so seed via the binding instead. Semicolons break `rodney js` — **use the comma operator**, never `;`.
```bash
# Create + open a fresh .vibe in one step (emits file:opened → Vue leaves Unlock):
rodney js 'window.go.main.App.CreateFile("/tmp/parity-qa.vibe","test-pass","HVAC Ops","QA").then(r=>"ok")'

# Seed realistic relational data so screens have something to render:
rodney js 'window.go.main.App.ImportCSV("/path/customers.csv","customers").then(r=>"ok")'
# ...plus the binding(s) each screen needs (CreatePage, SaveQuery, form/report/nav bindings, etc.)
```
Build a small reusable seed fixture (an HVAC-Ops-shaped dataset: customers, service_calls, technicians, equipment, parts_used) so every screen renders against the *same* data the mockups depict.

### 1.3 Per-screen acceptance loop (the gate)
For **each** screen below, the implementing agent must:
1. Navigate to the hash route (`rodney open http://localhost:34115/#/<route>` → `rodney waitload` → `rodney waitstable`).
2. **Assert structure in the live DOM** with `rodney exists` / `rodney count` / `rodney visible` / `rodney text` — e.g. the mockup's named regions actually exist and are *visible* (`rodney visible '.schema-tree'`, `rodney count '.sw-node'`, etc.). `exists` is not enough — use `visible`, because the failure mode here is "class shipped, never rendered."
3. Drive the real interactions the mockup specifies (`rodney click`, `input`, `select`, `submit`) and assert the resulting state via bindings (`GetTableData`, `GetReportSQL`, …) — prove write-back/round-trips against real DuckDB.
4. **Screenshot** the screen (`rodney screenshot qa-screenshots/<screen>.png`, and `rodney screenshot-el <sel>` for key regions) and **visually compare to the mockup** (open the mockup HTML in a second tab/page if useful). Note residual deltas.
5. Only then write/adjust supplementary Vitest specs for logic not provable through the DOM.

`qa-screenshots/` and `.vibe` files are gitignored — keep them out of commits.

### 1.4 Definition of Done (per screen)
- [ ] Renders at its route on `:34115` with no console errors (`rodney js 'window.__errors||"none"'` if an error hook is added; otherwise check the page renders without the error toast).
- [ ] Every **High** and **Medium** mockup region is present **and visible** in the live DOM (proven by `rodney visible`/`count`, not by CSS existing).
- [ ] The screen's core interactions round-trip through real bindings (data persists / re-renders).
- [ ] A committed-to-the-PR screenshot exists and visibly matches the mockup layout.
- [ ] Supplementary Vitest specs pass **and** `task test` (Go) is green.

> Anti-pattern to reject in review: a PR whose only evidence is passing `*.spec.ts`. No screenshot + no `rodney visible` assertions = not done.

---

## 2. Workstream sequence

Ordered by dependency and impact. **W0 is a prerequisite for W2/W3/W4/W6/W7** because those screens embed agent affordances.

### W0 — Agent Chat Panel (foundation, unblocks 5 screens) · Highest priority
**Gap:** Panel is entirely absent; `SendAgentQuery`/`GetAgentStatus`/`AgentResult` bindings are orphaned.
**Build:**
- A slide-in right-docked panel component (`components/AgentPanel.vue`) with open/close, mounted in `AppShell.vue` so it overlays any screen.
- Wire `SendAgentQuery` + `GetAgentStatus`; render the conversation, tool-use steps, and a pin-context affordance (pin current table/query/record).
- Trigger: nav/toolbar button + (stretch) ⌘K/⌘J shortcut.
- If the binding returns a single `AgentResult` (no streaming), render request→response now and leave a clear seam for streaming; do **not** fake streaming.
**Rodney acceptance:** with the local LLM at `:8080` reachable (§1.0), open panel from ≥2 screens; submit a query; assert a **real model response** node renders (`rodney visible '.agent-panel .agent-response'`) — not just that the binding fired; pin a context chip and assert it persists; screenshot.

### W1 — App Mode chrome (locked shell) · High
**Gap:** White/marigold locked chrome (`.topbar.app` → `.brand-block`/`.tabstrip`/`.lockpill`) is styled but never rendered; sidebar gutter persists; logo stays teal. (Tool-hiding logic is already correct — keep it.)
**Build:** Render the actual `.topbar.app` markup in App Mode (brand-block + locked tab strip + lock pill + role line); collapse the 240px grid (no sidebar) via the `.app-mode` root class; define a real `.btn-app-primary` (marigold) variant; enforce 44/52px hit targets on App-Mode controls and inputs; marigold logo mark.
**Rodney acceptance:** toggle into App Mode; assert `rodney visible '.topbar.app .tabstrip'` and `.lockpill`; assert the sidebar is **gone** (`rodney visible '.sidebar'` → false / count 0); assert builder routes still blocked; screenshot both modes side-by-side.

### W2 — Form Builder (authoring tool) · High
**Gap:** `FormView.vue` is a runtime *player*; the 3-pane builder is missing entirely.
**Build:** New builder surface (`split-page`: field palette | live preview | properties panel) consuming the already-shipped builder CSS. Field selection model; archetype `Seg` (Columnar/Continuous/Datasheet/Master-Detail) that actually re-renders the preview; properties (Label/Placeholder/Required/Visibility/Validation); "Ask agent" per-field box → W0; Unsaved badge; Publish. Keep the runtime player reachable (it's the App-Mode form renderer) but separate it from the builder route.
**Rodney acceptance:** select a field → assert properties panel reflects it; switch archetype → assert preview DOM changes; edit a label + Publish → reopen and assert it persisted via bindings; screenshot the 3-pane layout.

### W3 — Schema Builder (conversational + ER diagram) · High
**Gap:** Effectively unbuilt — two DDL forms instead of one chat↔diagram screen.
**Build:** Split-pane (chat | live ER diagram). Chat composer → agent proposes tables (W0); render proposed tables as draggable diagram cards on a `.diagram-canvas` with FK connector arrows (teal solid = FK, marigold dashed = optional); draft→commit ("Looks good — build it") lifecycle; friendly field-type vocabulary (ID/Text/Link/Pick/Date/Number) mapping to SQL types. Reuse existing DDL bindings under the hood. (Streaming animation is a stretch; static reveal is acceptable for v1 if real.)
**Rodney acceptance:** type a prompt → assert proposed-table cards render on canvas (`rodney count '.sw-node, .tbl-card'`); assert FK arrows render; commit → assert tables exist via `ListTables`; screenshot.

### W4 — Switchboard (spatial builder) · High
**Gap:** Flat `<ul>` list, not a canvas; no inspector; **access-rules feature absent from UI + `NavItem` model + `SaveNav` payload.**
**Build:** 3-pane (source palette grouped by Forms/Reports/Dashboards | `.sw-canvas` of `.sw-node` tiles with connector arrows + distinct home node | selected-item inspector: Display name/Icon/Sort order + "Advanced · access rules" per-node/role matrix). **Extend `NavItem` + `SaveNav` to persist `visible_to`/access rules** (backend change — add Go field + migration). Per-item icons; "Preview as Maria"; "Publish shell".
**Rodney acceptance:** add an item from palette → assert a `.sw-node` appears; open inspector → set access rule → save → reopen and assert it persisted; assert home node is visually distinct; screenshot.

### W5 — Query Explorer · Medium
**Gap:** No schema tree, no export, no run metadata; left rail is saved-queries not the schema tree.
**Build:** Left schema tree (tables w/ row counts, expandable columns, PK/FK badges) via `ListTables`/`DescribeTable` — consume the shipped `.schema-tree` CSS; CSV/Parquet export buttons; run-status metadata (success badge, DuckDB engine label, last-run time); "Ask agent" button → W0. **Fix stale dual-engine copy** (`[engine=sqlite]` → DuckDB-only). Decide whether to keep the saved-query rail (move it, don't drop it). (Syntax highlighting/CodeMirror is a stretch.)
**Rodney acceptance:** assert `rodney visible '.schema-tree'` with ≥1 table node; expand a table → assert columns render; run a query → assert results + metadata; click export → assert file written; screenshot.

### W6 — Report View · Medium
**Gap:** Single-series chart, no KPI tiles, generic single-column filters, either/or canvas.
**Build:** Stacked multi-series Chart.js chart + legend; KPI/detail tiles column; side-by-side grid (chart + tiles/agent, then full-width detail table); faceted filters (range/category/status) carded; "Run now" + last-run metadata; "Ask the agent → Modify this report" → W0. **Fix:** "Edit SQL" should actually edit (or relabel to "View SQL") — currently read-only despite the label; keep the `isStudio` gating. Per-series token palette instead of hard-coded `#2F8F87`.
**Rodney acceptance:** assert chart + tiles render simultaneously; apply a faceted filter → assert chart + table both update; as Studio user assert Edit-SQL present, as App user assert absent; screenshot.

### W7 — Dashboard / Studio Home · Medium
**Gap:** Thin wrapper around a seeded page; missing stat tiles, My Apps, activity feed, templates, empty state.
**Decision (locked):** the Dashboard is a **literal multi-app launcher of recent `.vibe` files on disk** — the "My Apps" cards open recent `.vibe`s (not objects inside the current file). This is a bigger architectural change: it needs a recent-files store (a small index outside any single `.vibe`, e.g. an app-data JSON), and Studio Home renders *before/around* a file being open. Treat the recent-files list + open-on-click as the core of W7.
**Build:** Recent-`.vibe` "My Apps" grid (open via `OpenFile`/file picker, with last-opened metadata); stat tiles row (counts via bindings, scoped per-app once one is open); recent-activity feed (if an activity source exists; otherwise scope down and note it); template rail + empty-state hero ("No apps yet…"). Personalized greeting. **Note:** because this changes when the dashboard renders relative to Unlock, confirm the routing/lifecycle implications before building.
**Rodney acceptance:** assert `rodney count '.stat'` == expected; assert app cards + template rail render; force empty state (fresh file) → assert hero renders; screenshot both states.

---

## 3. Backend changes implied (not just frontend)
- **W4:** `NavItem` struct + `_vibe_nav` schema + `SaveNav`/`ListNav` must carry access rules (`visible_to` / per-role). Additive migration.
- **W0:** confirm `SendAgentQuery` shape; if streaming is wanted, a streaming transport/event is a follow-up (don't fake it).
- **W6:** report definitions may need multi-series/grouping metadata to drive a stacked chart and KPI tiles.
- **W5:** export bindings for CSV/Parquet if not already present.

Each backend change keeps the existing Go test discipline (co-located `_test.go`, real `.vibe` + DuckDB integration tests). Backend tests are still required — the Rodney gate is *in addition to*, not *instead of*, Go integration tests.

---

## 4. Sequencing & batching
**Execution model (locked): one workstream at a time, reviewed before the next starts.** The agent completes a single workstream on its own branch, passes its Rodney gate, opens a PR **with screenshots attached**, and **stops for the user's review** before beginning the next. No parallel worktrees — serial, reviewed, branch-per-workstream.

Order:
1. **W0** first (unblocks the agent affordances in W2/W3/W4/W6/W7). → review.
2. Then the High screens in order **W1 → W2 → W3 → W4**, each its own branch + Rodney gate + review.
3. Then Medium **W5 → W6 → W7**.
4. Finally sweep the **Low/cosmetic** items (token namespacing `--color-*`→`--sh-*`, copy fixes, breadcrumbs, sticky result bar) as a polish pass.

Do **not** squash multiple workstreams into one opaque diff — that's part of what hid the gaps the first time.

---

## 5. Guardrails (what review will reject)
- ❌ Evidence is only passing `*.spec.ts`. → Require `rodney visible` assertions + a screenshot.
- ❌ Verified against `:5173` (no Go bindings) or with mocked bindings. → Must be `:34115`.
- ❌ CSS class added but not rendered (the original failure mode). → `rodney visible`, not `rodney exists` on a styled-but-empty node.
- ❌ Faked streaming / faked data. → Real bindings, real DuckDB, real seed fixture.
- ❌ "Done" without a mockup-vs-screenshot visual diff noted in the PR.
