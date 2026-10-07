> **SUPERSEDED (2026-09-22).** Vibe is no longer targeting Microsoft Access application parity. Forms, subforms, field meta, relationships UI, switchboard, App Mode and macros are being removed (#68, #69); Pages become Notebooks (#66).
> See [`../direction.md`](../direction.md). Kept for history; do not plan new work from it.

# MS Access Parity — Phased Implementation Plan

**Target:** Bring Vibe to Microsoft Access 97 feature parity as a **local-first Go desktop app**.
**Architecture commitment:** Keep the existing **Wails v2 (Go backend + Vue frontend) Studio + BubbleTea TUI** stack. DuckDB single-engine. `.vibe` = encrypted SQLCipher container + DuckDB blob.
**Deferred to v1.1:** Tailscale mesh / multi-user / sync (see §7).
**Decisions locked with stakeholder:**
- **App Mode (the locked-down end-user experience) ships as a runtime *mode toggle inside the Studio app*** — not a separate Viewer binary. One binary, lockdown enforced at runtime via capability gating.
- **First milestone = Forms & CRUD write-back** (Phase 1).

**Design reference:** UX wireframes + hi-fi mockups for all 11 MVP screens live in `tmp/design/` (see §9 for the inventory and the mockup → phase map).

---

## 1. Reconciling the three source docs

| Doc | What it is | How this plan treats it |
|---|---|---|
| `vibe-studio-system-design.md` | Aspirational v0.1 reframe: browser PWA + DuckDB-Wasm/OPFS + **Tailscale mesh** + llama.cpp | Source of the *Access-parity ambition* and the AI-as-Wizard vision. Its **PWA/mesh/networking** layer is **v1.1**, not now. We keep the Go desktop stack. |
| `PROJECT.md` / `PLAN.md` / `CLAUDE.md` | The real product spec — Go/Wails desktop, `.vibe` format | The architecture of record. Build phases 0–6 are largely **done** (see survey, §2). |
| `EVIDENCE_PARITY_PLAN.md` | Focused plan for the *reporting/dashboard* half (charts, filters, layout, formatting) | **Folded into Phase 3** of this plan as the "live dashboards" stream. Not duplicated. |
| `ms-access-ux-research.txt` / `Access 97…` | The parity target | Defines the pillars in §3. |

**The central insight:** Access 97 was a **data-application builder**, not a report tool. Vibe's *read/report* half is built; its *interactive data-app* half is the gap.

---

## 2. Current state (from codebase survey)

**Working today:**
- Container lifecycle, SQLCipher v4 encryption, DuckDB blob materialize/checkpoint — `internal/container/`
- Page parse → execute → render pipeline with directives `{table} {chart} {metric} {detail} {form} {if} {row}` — `internal/pages/`, `internal/renderer/html|tui/`
- DuckDB **single-engine** query execution (SQLite code is legacy/unused) — `internal/engine/`
- Wails bindings: file ops, page CRUD, table CRUD methods (`InsertRow`/`UpdateRow`/`DeleteRow`), schema (`CreateTable`/`DropTable`/`DescribeTable`), query exec, CSV import + CSV/JSON export — `studio/app.go`
- Vue views: Dashboard, Pages, PageEditor, **TableBrowser**, QueryEditor, Import, Settings, Unlock — `studio/frontend/src/views/`
- AI agent (JSON-RPC, 20+ tools, sandbox modes, approval queue) + MCP server — `internal/agents/`, `internal/mcp/`
- BubbleTea TUI (sidebar/content/REPL, page+table+query mgmt) — `internal/tui/`

**Partial / absent (the parity gap):**
- ⚠️ Form **write-back** incomplete (POST handler exists, doesn't run INSERT/UPDATE); no inline record-edit UI
- ❌ Relationships / FKs / referential integrity; field properties (caption, default, validation, input mask, lookup)
- ❌ Form archetypes (continuous, datasheet-edit, **master-detail/subform**), bound controls (combo w/ hidden PK), record nav bar
- ❌ Visual querying (Filter-by-Form / Filter-by-Selection)
- ❌ Banded **reports** (group headers/footers, subtotals, print/PDF)
- ❌ **Switchboard / App-Shell navigation**, dual-mode lockdown, startup config
- ❌ **Macros / automation** (command-button actions, AutoExec, events)
- ❌ Excel (xlsx) import/export, "Analyze in Excel"

---

## 3. Access 97 pillar → gap map

| Access 97 pillar | Vibe today | This plan |
|---|---|---|
| Tables + relationships + referential integrity | CreateTable/DropTable, basic types | **Phase 2** |
| Datasheet view (edit, sort, filter, freeze) | Paginated read + insert/delete | **Phase 1** (inline edit) + **Phase 2** (QBE/freeze) |
| Forms: columnar / continuous / datasheet / **subform** | `{form}` renders, write-back broken | **Phase 1** |
| Bound controls (combo/hidden PK, lookup, checkbox, ControlTip) | text-field rendering only | **Phase 1** |
| Visual queries (Filter-by-Form/Selection, QBE) | SQL editor + saved queries | **Phase 2** |
| Banded reports + grouping + subtotals + print | dashboards/charts only | **Phase 3** |
| Switchboard / hub-and-spoke nav / dual-mode lockdown | none | **Phase 4** |
| Wizards (Table/Form/Report/Button) | **AI agent** (create_table/page/record…) | cross-cutting — agent = the Wizard layer |
| Macros / AutoExec / command buttons / events | none | **Phase 5** |
| Excel bridge (Import Spreadsheet, Office Links) | CSV only | **Phase 6** |

---

## 4. Architecture decisions (cross-cutting)

1. **DuckDB single-engine, confirmed.** Matches as-built and the Evidence plan. CRUD runs on DuckDB. Aligns with Access's "single-user first" principle. **Risk to validate early:** DuckDB FK-constraint maturity and per-row UPDATE latency at SMB scale (P1/P2 spike).
2. **App Mode = capability-gated runtime mode in Studio**, per decision. Lockdown is enforced in `studio/app.go` (deny schema/SQL/page-edit methods when `mode=app`) and by forcing the agent sandbox to `read-only`/`read-write`. Weaker boundary than a separate process — acceptable for a single-user local-first tool.
3. **AI agent is the Wizard layer** across every phase (Table/Form/Report/Switchboard/Macro wizards), reusing the existing `internal/agents` tool surface + approval queue. This is how we honor the system-design doc's "AI replaces the wizards" vision without abandoning the desktop stack.
4. **Presentation/validation metadata lives in a new `_vibe_field_meta` table.** DuckDB column metadata is thin; Access-style field properties (human captions over `tbl_` names — the Leszynski convention, per `CLAUDE.md`), defaults, validation rules, input masks, lookup sources, and control types are stored alongside.
5. **New `_vibe_*` tables** (all additive migrations in `internal/container/schema.go`): `_vibe_field_meta`, `_vibe_relationships`, `_vibe_forms`, `_vibe_reports`, `_vibe_nav`, `_vibe_macros`. Reuse `_vibe_pages.page_type='form'` and the stubbed `_vibe_components` where natural.

---

## 5. Phased plan (toward v1.0)

### Phase 1 — Forms & CRUD write-back  ⭐ first milestone
*Goal: build a real data-entry application — the heart of Access.*

- **1.1 Complete the form submission pipeline.** Wire the POST handler → `InsertRow`/`UpdateRow`; `on_submit: refresh | redirect | noop`; type coercion, `required`/validation, server-returned error feedback. Files: `internal/server/handlers.go`, `internal/pages/executor.go`, `studio/app.go`, `internal/renderer/html/form.go`.
- **1.2 Inline datasheet editing.** Edit-in-place + add-row in `TableBrowserView.vue` → `UpdateRow`/`InsertRow`; optimistic refresh; delete already works. Files: `studio/frontend/src/views/TableBrowserView.vue`.
- **1.3 Field-metadata layer.** New `_vibe_field_meta` + `GetFieldMeta`/`SetFieldMeta` Wails methods: caption, control type, default, required, format code, validation rule, input mask, lookup SQL. Files: `internal/container/schema.go`, `studio/app.go`, new `internal/schema/fieldmeta.go`.
- **1.4 Bound controls.** Combo box with display-vs-hidden-value (the Access "show 'Visa', store 2" pattern), lookup-from-query, checkbox, date picker, ControlTip/help. Render in `{form}` and datasheet. Files: `internal/renderer/html/form.go`, new `internal/renderer/html/controls.go`.
- **1.5 Form archetypes.** Columnar single-record **+ record-navigation bar** (first/prev/next/last/new/save/delete, Find Record); continuous (tabular inline-edit); datasheet (reuse 1.2); **master-detail/subform** (FK-linked auto-filter). Files: `internal/pages/parser.go` (subform directive), `internal/renderer/html/form.go`, new `internal/renderer/html/subform.go`, `studio/frontend/src/views/FormView.vue`.
- **1.6 Form definitions + agent Form Wizard.** Store form specs in `_vibe_forms`; add agent tool `create_form`. Files: `internal/agents/tools.go`, `internal/container/schema.go`.

**Acceptance:** Build an order-entry form (order header + line-item subform); insert/edit/delete records end-to-end from both the form and the datasheet; combo box stores FK while showing the label.

---

### Phase 2 — Relational schema & visual querying
*Goal: the "Tables" + "Queries" pillars — relational integrity and SQL-free retrieval.*

- **2.1 Relationships & referential integrity.** `_vibe_relationships`; emit DuckDB FK constraints + cascade options; relationship-diagram view. Files: `internal/container/schema.go`, `studio/app.go` (`AddForeignKey`, `ListRelationships`), new `studio/frontend/src/views/RelationshipsView.vue`.
- **2.2 Schema designer.** Field-property editor (type, default, required, validation, input mask, index) + `AlterTable`/`AddColumn`/`AddIndex`. Files: `studio/app.go`, `internal/container/`, new `studio/frontend/src/views/SchemaDesignerView.vue`.
- **2.3 Datasheet UX parity.** Freeze columns, resize/reorder, **Filter-by-Selection** (click a value → filter), **Filter-by-Form** (QBE generating AND/OR `WHERE`, wildcards, `IS NULL`), saved filters, record-count bar. Files: `TableBrowserView.vue`, `studio/app.go` (filter-spec → SQL).
- **2.4 Visual query builder (QBE grid).** Join/criteria builder → SQL on top of existing saved-query registry; agent = "Query Wizard." Files: new `studio/frontend/src/views/QueryBuilderView.vue`, `internal/queries/registry.go`.

**Acceptance:** customers↔orders↔order_items with enforced FKs; multi-criteria Filter-by-Form search without typing SQL; save it as a named query.

---

### Phase 3 — Reports & dashboards
*Goal: banded reporting + fold in the Evidence dashboard parity work.*

- **3.1 Fold in `EVIDENCE_PARITY_PLAN.md`.** Execute its WUs (value formatting, variables, block layout/tabs/callout, conditional eval, `{value}`, enhanced metric/table, multi-series/stacked charts, input filters, editor component toolbar, CSS theme). This is the *live dashboard* stream.
- **3.2 Banded report model.** `_vibe_reports`: report/page/group header-footer + detail bands; group levels; aggregate expressions (`=Sum/Count/Avg` over group/all); sort/group. Files: `internal/container/schema.go`, new `internal/reports/`.
- **3.3 Banded renderer + print.** Extend `internal/renderer/html` with band layout, print CSS, pagination, page numbers/dates. Files: new `internal/renderer/html/report.go`.
- **3.4 Print/PDF & Office Links.** Print-to-PDF (Wails/headless), export report → PDF/HTML/xlsx, one-click "Analyze in Excel/Sheets." Files: `studio/app.go`.

**Acceptance:** a grouped "sales by rep" report with subtotals + grand total, print-to-PDF; a filterable live dashboard page.

---

### Phase 4 — App Shell, Switchboard & dual-mode
*Goal: turn a `.vibe` into a deployable, locked-down end-user application.*

- **4.1 App Mode / Studio Mode toggle** in the Studio Wails app; persist default in `_vibe_meta` (`default_mode`). Files: `studio/app.go`, `studio/frontend/src/router/`, new `studio/frontend/src/views/AppShellView.vue`.
- **4.2 Switchboard / nav generator.** `_vibe_nav` hierarchical menu (≤8 items/page, nested drill-down per Access constraint); buttons → open form/report/page or run macro. Files: `internal/container/schema.go`, `studio/app.go`, `AppShellView.vue`.
- **4.3 Startup & lockdown.** `_vibe_meta`: `startup_object`, `app_title`; in App Mode hide schema/SQL/agent-write; **capability gating** denies privileged `studio/app.go` methods; Studio-Mode unlock = Access's Shift-to-bypass. Files: `studio/app.go` (gating middleware), `internal/agents/sandbox.go`.
- **4.4 Agent Switchboard Wizard.** Agent generates nav structure + button bindings. Files: `internal/agents/tools.go`.

**Acceptance:** flip a finished `.vibe` to App Mode → user sees only the switchboard + permitted forms/reports and cannot reach schema, SQL, or destructive agent tools.

---

### Phase 5 — Automation & macros
*Goal: the VBA/Macro-Builder layer — behavior behind buttons and events.*

- **5.1 Declarative action engine.** `_vibe_macros`; actions: `OpenForm`, `OpenReport`, `GoToRecord`, `FindRecord`, `RunQuery`/`RunSQL`, `SetValue`, `SetFilter`, `MsgBox`, `Export`, `EmailObject`, `RunMacro`, with `If/Then` conditions. Files: new `internal/automation/`.
- **5.2 Command-button control.** `{button: label, action: <macro>}` bound to forms/switchboard (Access's Command Button Wizard). Files: `internal/pages/parser.go`, `internal/renderer/html/controls.go`.
- **5.3 AutoExec & events.** Startup macro; form/record event hooks (`on_open`, `on_load`, `before_insert`, `after_update`). Files: `internal/automation/`, `studio/app.go`.
- **5.4 Agent automation layer.** Natural-language → macro generation (the system-design THOUGHT/ACTION/OBSERVATION loop), bounded by the existing sandbox + approval queue. Files: `internal/agents/`.

**Acceptance:** a command button that opens a filtered form; an AutoExec that opens the switchboard maximized on file open.

---

### Phase 6 — Office bridge, templates & polish → **v1.0**

- **6.1 Excel import wizard.** xlsx via DuckDB `excel` extension; header detection, field-type mapping, append-vs-create, sheet/range selection (Access's Import Spreadsheet Wizard). Files: `studio/app.go` (`ImportSpreadsheet`), `ImportView.vue`.
- **6.2 Export / Office Links.** xlsx + PDF export, "Analyze in Excel."
- **6.3 Starter templates.** Access-style sample apps (contacts, inventory, order-entry, training-registrations) as `.vibe` templates; `vibe init --from <template>`. Files: `internal/container/examples.go`, `cmd/init.go`.
- **6.4 Hardening.** Encrypted DuckDB temp VFS, memory zeroing, crash/orphan recovery, large-data perf, docs (page/form/report/macro reference).

**Acceptance:** import a messy spreadsheet into a relational app; ship ≥3 templates; cut **v1.0**.

---

## 6. Sequencing & dependencies

```
P1 Forms/CRUD ⭐ ──┬─────────────► P4 App Shell / Switchboard ──► P5 Macros ──► P6 Office/Polish (v1.0)
                   │                         ▲                        ▲
P2 Schema/Querying ┴─────────────────────────┘                        │
P3 Reports (incl. Evidence parity) ───────────────────────────────────┘
```
- **P1 first** (decision). P2 can start in parallel once 1.3 (field metadata) lands.
- **P3** is largely independent (consumes P1 data) and can run alongside P2 — it absorbs the existing Evidence parity plan.
- **P4** needs forms (P1) + reports (P3) to have things to navigate to.
- **P5** needs P1/P4 (buttons target forms/reports/nav). **P6** is the closing pass.

---

## 7. v1.1 — Tailscale mesh & multi-user (explicitly deferred)

The system-design doc's networking ambition, deferred per decision:
- Host/worker **delta sync**; **optimistic concurrency** (`_version` + `_updated_at`, 409-on-conflict, merge UI); offline write-queue with replay.
- Tailscale node setup wizard; `tailscale serve` unified origin; **identity-aware ACLs**; multi-site subnet routers.
- Per-node / per-identity App-Mode permissions (Access's `.mdw` workgroup successor).

These layer on top of v1.0 without reworking it: the CRUD path (P1) and capability gating (P4) are the integration seams.

---

## 8. Top risks

| Risk | Mitigation | Phase |
|---|---|---|
| DuckDB as transactional CRUD store (FK maturity, per-row UPDATE latency) | Spike early; constrain to single-user; consider write-batching | P1/P2 |
| App-Mode lockdown is in-process (not a separate binary) | Capability gating in `app.go` + forced agent sandbox; document the boundary | P4 |
| Form/report spec sprawl (many new `_vibe_*` tables) | Keep specs as versioned JSON; reuse `_vibe_components`/`_vibe_pages` where possible | all |
| Scope creep vs. true Access parity | Each phase has a concrete acceptance app (order-entry, grouped report, locked switchboard) | all |

---

## 9. Design mockups (`tmp/design/`)

A full design pass exists for the 11 MVP screens (the "Vibe Studio — Request for Design" brief, `tmp/design/uploads/vibe-studio-design-brief.md`), delivered as two rendered React canvases — one lo-fi, one hi-fi:

| Artifact | Open this | Component sources |
|---|---|---|
| **Wireframes** (lo-fi, structure-first) | `tmp/design/Vibe Studio Wireframes.html` | `wf-*.jsx` + `wireframe.css` |
| **Hi-fi mockups** (full SchoolHouse system) | `tmp/design/Vibe Studio Mockups.html` | `hifi/hifi-*.jsx` + `hifi/hifi.css` |

Both render the same 11 screens on a `DesignCanvas` (`design-canvas.jsx`); `main.jsx` is the artboard registry/layout. Personas the screens serve: **Maria** (data entry), **James** (citizen dev), **Priya** (studio dev).

> **The mockups are React/JSX only as a rendering convenience — this is NOT a framework decision.** React was just the quickest way to lay out interactive artboards on a canvas. The Studio frontend stays **Vue 3 + Wails v2** (per `CLAUDE.md`); these screens are translated into the existing Vue + SchoolHouse stack, not ported to React. Treat the `.jsx` files as visual/UX specs, not as code to adopt.

**Design-system note (matches the as-built Studio).** The hi-fi set uses **SchoolHouse** (Fraunces display / Inter body / JetBrains Mono labels, warm paper, load-bearing borders + hard-offset shadows) — the same system the Studio frontend already adopted. Three colors carry *meaning* system-wide: **Teal = Studio Mode**, **Marigold = App Mode**, **Coral = Attention** (errors/conflicts — explicitly *not* destructive-red).

### Mockup → phase map

| # | Screen (artboard ids) | Maps to | Notes |
|---|---|---|---|
| 1 | **Onboarding · Node Setup Wizard** (`onb-*`, `err-*`) | **v1.1** (mesh) | Tailscale join + node URL is deferred mesh work. The **AI model select** (local vs. remote API tabs, hi-fi `1.3a/1.3b`) and **storage path** steps are the v1.0-relevant slices. Error states: Tailscale-missing / low-RAM / storage-denied. |
| 2 | **Studio Home / Dashboard** (`home-pop`, `home-empty`) | cross-cutting → **P6** | Studio shell: app card grid, recent activity, node-status widget, quick-start templates. Populated + empty states. |
| 3 | **Schema Builder** (`schema-main`, `stream-1..3`) | **P2** + agent Wizard | Split-pane conversational chat ↔ live ER diagram. The 3-frame `StreamingFrame` sequence shows tables appearing as the agent narrates — the **agent-as-Wizard** pattern (§4.3). |
| 4 | **Form Builder** (`form-mainpage`) | **P1** ⭐ | Field list · live preview · properties panel. Renders the **Master-Detail / subform** archetype — the core P1 acceptance pattern. |
| 5 | **App Shell Builder · Switchboard** (`sw-main`) | **P4** | Canvas of screens; access rules tucked under "advanced". The Access Switchboard Manager homage. |
| 6 | **App Mode · Maria's view** (`app-desktop`, `app-mobile`) | **P4** | Locked tab bar, no sidebar, 44px hit targets, marigold accent. Desktop Master-Detail form + phone PWA (PWA is Phase-4/v1.1 horizon). |
| M | **Mode toggle · annotated flow** (`mode-flow`) | **P4** | Studio ↔ App transition on one HVAC app — the dual-mode capability gating made visible. |
| 7 | **Agent Chat Panel** (`agent-panel`) | cross-cutting (Wizard) | Slide-in right panel, streaming tool-use log (collapsible for trust/debug), pin-context. |
| 8 | **Query Explorer** (`query-main`) | exists today + **P2** | SQL editor + sticky results + schema tree (mostly built); P2 adds visual querying / QBE around it. |
| 9 | **Node Manager** (`nodes-main`) | **v1.1** (mesh) | Hub-and-spoke topology + node inspector. Deferred with multi-user. |
| 10 | **Conflict Resolution** (`conf-all`, `conf-field`) | **v1.1** (mesh) | Plain-language merge dialog, apply-all + field-by-field paths. Deferred (optimistic-concurrency UI, §7). |
| 11 | **Report View** (`report-main`) | **P3** | Live DuckDB filters, chart ↔ table toggle, "Edit SQL" Studio-only. Absorbs the Evidence parity stream. |

**Takeaway for sequencing:** the mockups that drive **v1.0** are #4 (P1), #3/#8 (P2), #11 (P3), and #5/#6/M (P4) — plus the cross-cutting agent panel (#7) and Studio home (#2). Screens #1 (Tailscale steps), #9, and #10 are **v1.1** mesh/multi-user surfaces (§7) and should not pull scope into v1.0.
