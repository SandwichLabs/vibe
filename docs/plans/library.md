# Library: notebooks and artifacts in one place

Status: **built (2026-10-23, steps L1-L5).** See *As built* at the end. The owner asked to "have notebooks as a type of artifact instead of a separate notebooks and artifacts menu; a little design so the selection of the type is easy when adding one manually." A Fable judge reviewed the first proposal and the owner accepted its recommendations; they are decisions L1-L9 below. This is a Studio UI change only. It builds on epic #162 (`docs/plans/data-artifacts.md`, *the base plan*, and `docs/plans/html-artifacts.md`, *the pages plan*).

## Goal

One rail entry, **Library**, lists every document a person opens to read answers: notebooks, live views, pages and chart scripts. **New** asks *what kind* first, with the kinds side by side and described in plain words, then asks for a name in the form that kind needs. Each kind keeps its own storage, its own view and its own rules; the Library is the one place to find and start them.

## Non-goals

- **No storage change.** Notebooks stay `notebooks/*.slnb`; artifacts stay `artifacts/<name>/`. No change to `internal/notebooks`, `internal/artifacts`, the stores, the watcher or `ResolveConflict`.
- **No bundle, MCP tool, agent tool or CLI change.** `slaw artifact list` keeps listing artifacts only (L2). `create_dashboard` keeps its name and behaviour unless the owner decides otherwise (open question 1).
- **No search beyond a name filter** over the loaded list. No full-text search over cells, SQL or page files.
- **No new delete or rename paths.** A notebook deletes from its row, as today; an artifact from its Source tab, as today.
- **No change to trust.** A page or chart script from a starter is undecided like any other and opens on its allow prompt (open question 3).

## Grounding (what is there)

- **Rail.** `components/AppShell.vue:40-43` lists Notebooks (`/notebooks`, `layers`), Queries, Artifacts (`/artifacts`, `barChart`), Skills.
- **Routes.** `router/index.ts`: `/notebooks` → `NotebooksView` (179 lines; list, Import, an inline New modal, hover delete, `?new=1` opens the modal), `/notebooks/:id` → `NotebookView`, `/artifacts` → `ArtifactsView` (78 lines; list via `useArtifacts`, `NewArtifactDialog`), `/artifacts/:name` → `ArtifactView`.
- **Hard-coded paths** to rewrite: `AgentPanel.vue:21` and `AgentView.vue:16` (`/notebooks/${id}`), `useNotebookEditor.ts:52,144` (back to `/notebooks`), `ArtifactView.vue:58,65` (back to `/artifacts`), `DashboardView.vue:74,135` (`/notebooks?new=1`, `/notebooks`).
- **Create.** `App.CreateNotebook(title)` (the store assigns the id; an empty title is refused in the view, not in Go; the store clamps a title to 200 characters, `maxTitleRunes`, rather than refusing it). `App.NewArtifact(name)` → `newArtifact` in `studio/artifacts_edit.go:175` writes one A2UI starter (two date parameters, one query, a `dateRange` and a `stat`); the name rule is `^[a-z0-9][a-z0-9_-]{0,63}$` (`validArtifactName`, mirrored in `NewArtifactDialog.vue`).
- **Kinds in a list.** `ArtifactSummary.View` is `artifacts.ViewLabel`: `a2ui`, `script`, `html (static)` or `html (scripted)` (the struct's comment still says "a2ui or script"; fix it in passing); a broken artifact carries `Error`. `notebooks.Summary` has `id`, `title`, `cellCount`, `updatedAt` and `error` (a `.slnb` that failed to parse still lists, with the file stem as title). Both timestamps are RFC 3339.
- **Counts.** Home reads `GetDashboard` (`DashboardData`, `studio/app_queries.go:413`: notebooks, queries, skills, tables; a broken notebook counts as a notebook). `WorkspaceCounts` (`studio/workspace_info.go:46`) rides on `WorkspaceInfo.counts` but no frontend code reads it. `LauncherView.vue:150,168,176` says "notebooks, saved queries and skills".
- **Agent.** `slaw:open-agent` (a window event; `AppShell.vue` listens at line 98 and `openAgentFromEvent` only sets `agentOpen`) carries no payload; `AgentPanel.vue` owns `draft` and only `submit()` calls `send`. In chat, the agent makes an artifact with `write_artifact` (`view`, `view_source` or `html_files`); it makes a notebook only through `create_dashboard`, which appends an A2UI cell (`internal/agents/tools.go:389`). `write_cell` is task-mode only and runs from a notebook's AI cell dialog (`AiCellDialog.vue`; `useAiCellDialog.openCreate()` takes no text today). No frontend code decides whether a model is set up; a failed turn gets a hint from `GetLLMSettings` after the fact (`useAgentChat.errorHint`).
- **Allow wording.** A page or script view shows as source until the person allows it. The trust strip (`ArtifactViewTrust.vue`) says **Not allowed yet** and the button is **Allow**; the reference sections are *Custom views: `view: script`* and *HTML pages: `view: html`* in `docs/reference/artifacts.md`. Nothing in the UI says "asks before it runs".

## Decisions

### L1. Unify the UI, keep storage separate

The Library is a view over two stores, not a new store. Base plan D1 says why a notebook is not an artifact on disk, and nothing here changes it: a notebook is coerced on load, an artifact is refused on load; a notebook autosaves on every cell edit, an artifact's script or page is trusted by a content hash that must not move on unrelated edits; notebooks have no parameter scope. So "a notebook is a type of artifact" is true in the UI (one list, one New, one type picker) and false in the folder. Docs say "library item" for the union and keep "artifact" for `artifacts/<name>/`.

### L2. The rail entry is "Library"

Not "Artifacts". Two reasons. **CLI mirrors UI**: if the merged list were called Artifacts, `slaw artifact list` would have to list notebooks too, which is a CLI change and blurs the word. Called Library, `slaw artifact list` mirrors the Library's artifact kinds (live views, pages, chart scripts), and the docs say so. **The word keeps its meaning**: "artifact" stays the folder, the manifest and the agent tool family.

**Saved queries and skills stay their own rail entries.** A saved query is a SQL file you edit and run in the Query Editor; a skill is instructions for the agent. Neither is a document you open to read answers, and both have their own editors. The rail becomes: Home, Library, Queries, Skills (plus the connection rail, Agent, Import, Settings as today). Icon: `layers` (the notebook icon today); `barChart` stays the live-view type icon.

### L3. Type names people see

| Type | Is | Card line (picker) | Icon | Badge |
|---|---|---|---|---|
| **Notebook** | `notebooks/<id>.slnb` | SQL, notes and charts in cells you run top to bottom. | `layers` | |
| **Live view** | artifact, `view: a2ui` | A view with controls (dates, filters) over named queries. Built from Slaw's components. | `barChart` | |
| **Page** | artifact, `view: html` | Your own HTML bound to named queries. Starts static; tick *starts with a script* for interactivity in its own window. | `fileText` | *starts with a script*: "Runs after you allow it" |
| **Chart script** (Advanced) | artifact, `view: script` | A `view.js` that draws an SVG or a component page from query rows. | `pieChart` | "Runs after you allow it" |

All four icons exist in `src/icons.ts`. The badge wording matches the trust strip's **Not allowed yet** / **Allow** (Grounding), so the picker and the thing it opens say the same thing.

- **Not "Dashboard"** for the A2UI kind: the agent's `create_dashboard` appends an A2UI *cell to a notebook*, so "Dashboard" would name two different things (open question 1).
- **One Page card, not two.** Static vs scripted is derived from the files at save (pages plan H1), never declared; a scripted page is a page that has a `.js` file or a `<script>`. The checkbox only picks the starter. The list shows *Page* with a small `script` tag when `ViewLabel` says `html (scripted)`.
- **Chart script under "Advanced".** Scripted pages cover most of what `view: script` does (pages plan, view-kind table); it stays reachable for in-Studio custom drawing, but not as a peer card.
- A broken artifact whose kind can't be read shows type **Artifact** and its error; a broken notebook (`Summary.error`) shows type **Notebook**, the file stem as title, and its error. Both count under their chip (a broken artifact under *All* only), as the stores count them today.

### L4. Routes

| New | Shows | Old paths that redirect |
|---|---|---|
| `/library` | `LibraryView`; `?type=notebook\|live\|page\|script`, `?new=` (`1` or a type) | `/notebooks` → `/library?type=notebook`; `/notebooks?new=1` → `/library?new=notebook`; `/artifacts` → `/library` |
| `/library/notebook/:id` | `NotebookView` | `/notebooks/:id` |
| `/library/artifact/:name` | `ArtifactView` | `/artifacts/:name` |

Redirects are router `redirect` entries in **function form** (`redirect: to => ({ path: '/library', query: {...} })`): a string redirect drops the original query, and `/notebooks?new=1` has to become `?new=notebook`, so each redirect maps `to.query` and `to.params` itself. Deep links (`#/notebooks/<id>` typed or bookmarked) and doc links keep working; Studio keeps no last-route memory, so there is nothing else to migrate. Once the dialog is open, `LibraryView` replaces the URL without `?new=` (as `NotebooksView` does today), so Back never reopens it. `AppShell.isActive` uses `startsWith`, so `/library/...` lights the rail entry. Route names become `library`, `library-notebook`, `library-artifact`; `AgentPanel`'s "pin this screen" text follows the names. Every hard-coded path in *Grounding* moves to one helper, `lib/libraryRoutes.ts` (`notebookPath(id)`, `artifactPath(name)`, `libraryPath(type?)`), so a later move is one file.

### L5. The Library list

- **One merged list** from `ListNotebooks` and `ListArtifacts`, loaded together; either failing shows its error above the rows and keeps the other's rows.
- **Columns:** type (icon + name, L3), title (an artifact's `title` or `name`; a notebook's title), a detail line (notebook: *N cells*; artifact: `name` · description), changed (relative, full time in `title`), and *Needs fixing* for a broken item.
- **Filter chips:** All · Notebooks · Live views · Pages, each with its count. **Chart scripts** appears as a fifth chip only when the workspace has at least one (it is Advanced in the picker, so an empty chip would advertise it). The chip is `?type=`, so Back keeps it.
- **Name filter:** a text box, case-insensitive substring over title and name/id, client-side. Placeholder "Filter by name". `/` focuses it when no field has focus.
- **Sort:** recently changed first (`updatedAt` / `updated_at`), ties by title; a toggle offers Name A-Z. A broken item sorts by its file time like any other.
- **Open** goes to the type's route (L4). Notebook rows keep the hover delete; header keeps **Import notebook** (only notebooks import from a file today) beside **New**.
- **Empty states** use the picker's words: an empty workspace says "Nothing in the library yet. **New** makes a notebook, a live view or a page, or ask the agent to build one." An empty filter says "No pages yet" with **New page** (opens the picker on that card). No "add `view.a2ui.json`" text in the UI; the file layout lives in the reference.
- **Refresh** on the existing notebook and artifact change events (`useArtifacts` already follows the watcher; `NotebooksView` reloads on create, delete and import).

### L6. The New dialog

**Step 1, type.** Three cards in a row (Notebook, Live view, Page) and an **Advanced** disclosure holding Chart script. The cards are one radio group (`role="radiogroup"`, roving `tabindex`): arrow keys move the selection, Space selects, Enter goes to step 2 (as does **Next**); the opener's `?new=<type>` preselects, Notebook otherwise. Initial focus is the selected card (`useModalKeys`'s `initialFocus`), so from Home it is Enter, a title, Enter. The Page card's *starts with a script* checkbox is its **own control after the group in tab order**, shown only while Page is selected (a control inside a roving radio is not reachable by keyboard); the mock draws it under the cards. Opening Advanced puts Chart script into the same group as a fourth radio. Below the cards, a row **"Or describe it and let the agent build it"** (L7), last in tab order so the common path is not interrupted.

**Step 2, name.** The form follows the type:

| Type | Field | Rule (checked here for the message; Go checks again) | "Creates …" line | Binding |
|---|---|---|---|---|
| Notebook | Title | non-empty (the store clamps it to 200 characters; the field says so past 200) | Creates a notebook in `notebooks/` | `CreateNotebook(title)` |
| Live view, Page, Chart script | Name | `^[a-z0-9][a-z0-9_-]{0,63}$` | Creates `artifacts/<name>/` with a working example | `NewArtifact(name, kind)` |

Back returns to step 1 with the choice kept; Escape closes (`useModalKeys`). On success the dialog routes to the new item. A name in use is refused by Go and shown inline.

**Starters** (`NewArtifact(name, kind)`; `kind` is `a2ui` (also `""`), `html`, `html-script`, `script`; anything else is refused). All share the A2UI starter's manifest (two date parameters, one `span` query) so the kinds read alike:

- `a2ui`: today's starter, unchanged.
- `html`: `html/index.html.tmpl` with a heading, `{{ param }}` dates and a table over `{{ query "span" }}`, plus a small `html/assets/style.css` linked by a relative `assets/` href (pages plan H2). Derives *static*.
- `html-script`: the static page plus `html/app.js` that calls `slaw.run("span", …)` and updates a cell. Derives *scripted*; no capabilities declared.
- `script`: `view.js` whose `draw` returns a one-bar SVG from `span`.

**Trust badge.** The ticked *starts with a script* checkbox and the Chart script card show "Runs after you allow it" (L3). Step 2's "Creates" line adds "It opens as source with an **Allow** button" for those kinds, so the strip the person lands on is the one they were told about.

### L7. "Describe it and let the agent build it"

The row opens a describe step for the type selected in step 1 (shown as a line, "A live view · Change" goes back; no second type choice), a text box, and **Ask the agent**. Routing by type, so each kind lands on the tool that makes it:

- **Notebook**: needs a title (the field is shown). Studio creates the notebook with `CreateNotebook`, opens it and opens its AI cell dialog with the description filled in (task mode: `preview_query`, then `write_cell`). `useAiCellDialog.openCreate` gains an optional `description`; the hand-off is a one-shot `usePendingAsk` composable (set before the route push, read and cleared by `NotebookView` on mount, cleared on workspace close), not the URL.
- **Live view, Page, Chart script**: opens the assistant panel with the draft filled in and **not sent**, e.g. "Build a live view (a data artifact with an A2UI view): <description>", "… an HTML page (view: html) with script …", "… a chart script (view: script) …". The person reads and sends it; the agent uses `write_artifact` and the data-artifacts skill. `slaw:open-agent` gains an optional `detail: { draft }`; `AppShell` passes it to `AgentPanel`, which sets `draft` only when it is empty or unchanged from a previous prefill, and never while a turn is in flight (`sending`). This adds no tool and no binding: the prefill is text in the same box the person types in, so the one-tool-surface rule is untouched.
- **No model set up**: nothing new. Studio has no "is a model configured" signal in the frontend, and a failed turn already gets a Settings hint (`useAgentChat.errorHint`). The row is always enabled; the panel says what is missing when Send fails. Adding a readiness probe for one row is more than this plan needs.

### L8. Home and launcher

- `DashboardData` gains `artifact_counts: {live, page, script, broken}` from one `listArtifacts` (a broken artifact counts once, under `broken`); `WorkspaceCounts` is left alone (nothing in the frontend reads it). The Go change is counting only, in a helper beside `listArtifacts` so `GetDashboard` stays a thin binding; no new binding.
- Home's count tiles become **Library**: Notebooks · Live views · Pages (Chart scripts only when non-zero), each opening `/library?type=…`. The Notebooks card becomes a **Library** card. **New notebook** stays the primary button and opens the picker with Notebook preselected (`/library?new=notebook`), so Enter, a title, Enter is the old flow.
- Launcher copy: "Your notebooks, live views and pages, saved queries and skills are kept in it as ordinary files"; the Notebooks feature card becomes **Library** ("Notebooks, live views and pages that re-run on open. Answers are never frozen.").

### L9. Thin views

`LibraryView.vue` wires `useLibrary` (load both lists, merge, type mapping, chips with counts, name filter, sort) and renders. `NewItemDialog.vue` wires `useNewItem` (step, type, name rules per type, submit routing, the describe hand-off) and renders `TypePicker.vue` (the cards and Advanced). Both stay under 600 lines and 150 script lines (`check-vue-size.mjs`). `NewArtifactDialog.vue` and `NotebooksView`'s inline modal are deleted; `NotebooksView.vue` and `ArtifactsView.vue` are deleted once redirects land. No Go call outside `lib/bindings.ts`.

## Mocks

The picker, step 1 (Page focused, script ticked):

```
┌─ New ────────────────────────────────────────────────────────────┐
│  ┌──────────────────┐ ┌──────────────────┐ ┏━━━━━━━━━━━━━━━━━━┓  │
│  │ ▤ Notebook       │ │ ▥ Live view      │ ┃ ▢ Page           ┃  │
│  │ SQL, notes and   │ │ A view with      │ ┃ Your own HTML    ┃  │
│  │ charts in cells  │ │ controls over    │ ┃ bound to named   ┃  │
│  │ you run top to   │ │ named queries.   │ ┃ queries.         ┃  │
│  │ bottom.          │ │                  │ ┃                  ┃  │
│  └──────────────────┘ └──────────────────┘ ┗━━━━━━━━━━━━━━━━━━┛  │
│  [x] Starts with a script   ⚑ Runs after you allow it            │
│  ▸ Advanced                                                      │
│ ──────────────────────────────────────────────────────────────── │
│  ✦ Or describe it and let the agent build it                  →  │
│                                     ←/→ choose · Enter next      │
│                                          [ Cancel ]  [ Next ]    │
└──────────────────────────────────────────────────────────────────┘
```

Step 2 for a page:

```
┌─ New page ───────────────────────────────────────────────────────┐
│  Name                                                            │
│  [ weekly-sales                                              ]   │
│  Creates artifacts/weekly-sales/ with a working example.         │
│  It opens as source with an Allow button.                        │
│                                [ Back ]  [ Cancel ]  [ Create ]  │
└──────────────────────────────────────────────────────────────────┘
```

The Library list:

```
 Library                                   [ Import notebook ] [ + New ]
 ( All 9 ) ( Notebooks 4 ) ( Live views 3 ) ( Pages 2 )   [ Filter by name ]
                                                     Sort: Recently changed ▾
 ┌──────────────────────────────────────────────────────────────────────┐
 │ ▥ Live view  Weekly sales      weekly-sales · by region   2 min ago  │
 │ ▤ Notebook   Churn questions   12 cells                   1 h ago    │
 │ ▢ Page       Board summary     board · script             yesterday  │
 │ ▤ Notebook   Untitled notebook 0 cells                    3 days ago │
 │ ! Artifact   orders-pivot      artifact.yaml: unknown field "vew"    │
 │                                                       Needs fixing   │
 └──────────────────────────────────────────────────────────────────────┘
```

## Steps (sequential; each a Sonnet implementation, then an Opus adversarial review; commit only after APPROVE)

**Gates for every step:** `task lint` (golangci-lint with gocognit, `vue-tsc`), `go test ./...`, `task test:frontend`, `node studio/frontend/scripts/check-vue-size.mjs`, `ast-grep scan -c tools/ast-grep/sgconfig.yml cmd internal studio`, and `go test ./internal/docsref`. UI steps add **Rodney live QA** against `task studio:dev` at `:34115`, light and dark, screenshots in `qa-screenshots/` (`docs/contributing/rodney.md`); Vitest is supplementary. A changed `AppApi` signature updates `env.d.ts` and `lib/bindings.ts` (CI's `check-arity.mjs`).

#### Step L1. Go: starters by kind and artifact counts

- `NewArtifact(name, kind string)`; `newArtifact` picks the starter by kind (L6); an unknown kind is refused with the list of kinds. Starters are constants or embedded files under `studio/`, not in `internal/artifacts` (they are Studio's examples, not the format).
- `DashboardData.ArtifactCounts` (L8). The binding stays thin (counting lives in a helper beside `listArtifacts`). Fix the `ArtifactSummary.View` comment while there.
- The signature change is the one `AppApi` arity change in this plan: `env.d.ts`, `lib/bindings.ts` and the one caller (`NewArtifactDialog.vue`, until L3 replaces it) move together, or `check-arity.mjs` fails build-smoke.
- **Tests:** every starter decodes, validates and passes the save-time vet with no warnings against the `Mock` store and a `FileStore` in `t.TempDir()`; `html` derives `PageStatic` and `html-script` `PageScripted` (`PageInfo.Kind`); `script` has a `view.js` and passes `ViewHash`; an existing name and a bad kind are refused and nothing is written; a starter page is undecided in a fresh `views.json`; counts with a broken artifact; `TestExportedAppMethodsAreThinBindings` still passes.
- **Review first:** a starter that a later page rule refuses (the test must run the real vet, not a copy); a kind string reaching a path; the CSS starter breaking the pages plan's URL rules.

#### Step L2. Routes, redirects and the Library list

- `lib/libraryRoutes.ts`; router entries and redirects (L4); every hard-coded path moved to the helper; rail entry **Library** (L2).
- `composables/useLibrary.ts`, `views/LibraryView.vue` (L5). `NotebooksView` and `ArtifactsView` deleted with their specs (`ArtifactView.spec.ts` also names the old list route; `check-exports.mjs` fails on any export they leave orphaned); the New buttons temporarily open the existing two dialogs by type (the picker is L3). `LibraryView` handles `?new=` as `NotebooksView` does (open, then `router.replace` without it).
- **Tests (Vitest):** type mapping from `ViewLabel` strings and broken items of both kinds; chip counts and the Chart scripts chip appearing at one; name filter; both sorts; one list failing keeps the other; every old path redirects with its query string and `new=1` becomes `new=notebook` (a new `router/index.spec.ts`; there is none today).
- **Rodney:** a workspace with each type and a broken artifact; chips, filter, sort, opening each type, Back keeps the chip; `#/notebooks/<id>` and `#/artifacts/<name>` from the address bar land on the item; empty-state copy.
- **Review first:** a redirect loop or a lost `?new=1`; `useNotebookEditor`'s leave guard still firing on the new path; a title or error reaching markup other than by interpolation; the rail still lighting on `/library/notebook/...`.

#### Step L3. The New dialog and starters in the UI

- `components/TypePicker.vue`, `components/NewItemDialog.vue`, `composables/useNewItem.ts` (L6); `NewArtifactDialog.vue` and the notebook modal deleted; `?new=<type>` preselects.
- **Tests (Vitest):** keyboard (arrows move, Space selects, Enter advances, Tab reaches the checkbox and the describe row, Escape closes), initial focus on the preselected card, Advanced opening and Chart script reachable by keyboard, the checkbox shown only for Page and picking `html-script`, rules and "Creates" line per type, Back keeping the choice, the binding called with the right kind, a Go refusal shown inline.
- **Rodney:** create one of each by keyboard only; each lands on the right view; the page and chart script open as source with **Not allowed yet** and **Allow**; focus ring visible on every card (`theme/bridge.css`); axe-style check that the radiogroup and each radio have labels.
- **Review first:** focus trapped in the modal (`useModalKeys`); a double Create making two items; the dialog's 150-script-line cap.

#### Step L4. "Describe it" and the agent hand-off

- The describe step (L7); `slaw:open-agent` `detail.draft`; `AgentPanel` prefill rule; `useAiCellDialog.openCreate(description?)`; `usePendingAsk` and `NotebookView` opening the AI cell dialog with it. The data-artifacts skill text gains the three type names (Live view, Page, Chart script) beside the `view:` values so the agent recognises the prefilled drafts.
- **Tests:** the draft per type; an existing typed draft never overwritten; nothing prefilled while `sending`; the pending ask used once and cleared on workspace close; `AgentPanel.submit` is the only caller of `send` (a grep in the spec, so a prefill can never send).
- **Rodney** (local LLM, `docs/contributing/rodney.md`): describe a live view, send, and see it in the Library; describe a notebook and see the AI cell dialog open filled in; with no provider, Send fails with the existing Settings hint.
- **Review first:** anything sent without the person pressing Send; the description reaching markup or a URL; the panel's draft clobbered while a turn is running.

#### Step L5. Home, launcher and docs

- `DashboardView` Library tiles and card, New notebook → picker (L8); launcher copy.
- **Docs that move:** `docs/reference/artifacts.md` (*In Studio*: Library, routes, the New dialog and starters; *Agents, bundles and the CLI*: `slaw artifact list` mirrors the Library's artifact kinds), `docs/reference/notebooks.md` (where notebooks live in Studio), `docs/reference/workspaces.md` (the `slaw artifact list` row), `docs/quickstart.md` step "Choose **Library**, then **New**, pick **Notebook**…", `CLAUDE.md` (views list: Library replaces Notebooks and Artifacts; the CLI line's "mirrors the Artifacts list"), `docs/direction.md` §9 (one line), `README.md` (line 16's "notebooks, saved queries and skills" may stay; it names files, not menus), and the plan's own *As built*.
- **Rodney:** Home counts match the Library chips; New notebook from Home is Enter, a title, Enter; launcher text in both themes.
- **Review first:** a count disagreeing with the list (broken items, filtered kinds); docs still saying *Artifacts in the sidebar*.

## Risks

- **Muscle memory and links.** People and docs say "Notebooks". Redirects keep links working; the quickstart and reference move in L5. The Notebook chip is one click.
- **Two shapes in one list.** Notebooks and artifacts differ in naming (title vs slug), delete location and conflict handling. The list hides none of it: the detail line shows the slug, and delete stays where each kind has it today.
- **Home cost.** `ListArtifacts` reads every artifact folder, including page file sets, on each Home load. It is the same work the Artifacts list did; if Home slows on a large workspace, count from manifests only.
- **The word "artifact" in the agent's mouth.** The agent still says "artifact" (`write_artifact`, the skill). The type names appear in the prefilled drafts so the person and the agent meet in the middle; the skill text gains the three names in L4.
- **A starter that drifts from the rules.** Covered by L1's test running the real vet over every starter.

## Open questions (for the owner)

1. **`create_dashboard` vs "Live view".** The tool appends an A2UI *cell* to a notebook, while "Live view" is an artifact. Options: (a) keep the name and document "a dashboard cell" in the tool description and the notebooks reference; (b) rename to `add_chart_cell` (or `add_view_cell`) with the old name kept as an alias for a release, since renaming breaks MCP clients and saved prompts that name it; (c) keep it and add a sibling that writes a live view. This plan does not decide; nothing in L1-L5 depends on it. *Judge's recommendation:* (a). The two words meet only in the agent's mouth; one sentence in the tool description ("a dashboard cell in a notebook, not a live-view artifact; for that use `write_artifact`") costs nothing and breaks nobody. (c) already exists in effect: `write_artifact` is the sibling.
2. **Chart scripts chip.** Shown only when present (L5). Alternative: fold chart scripts into *Pages* with a `script view` tag. Chosen this way so the filter never hides a kind. *Recommendation:* as proposed; a chip that appears with its first item is the smallest honest rule.
3. **Allow on a starter.** A page or chart script made from Slaw's own starter opens as source with **Allow** (one click; it shows what Allow means). Auto-allowing the embedded starter bytes is possible (the person made it, the hash is known) but adds a second allow path; not proposed. *Recommendation:* keep the one path. The first thing a person edits is the starter, and the hash moves then anyway; the starter is the cheapest place to learn what the prompt is.
4. **Saved queries in the Library later?** Kept out (L2). If people ask, a *Queries* chip that opens the Query Editor is cheap. *Recommendation:* keep out at 0.3.0; revisit only on a request.

## As built

Built in five steps (L1 starters and counts, L2 list and routes, L3 the New dialog, L4 describe-it, L5 Home, launcher and docs), as planned except for these:

- **The script starter is `html/app.js`.** The `html-script` starter is the page starter plus an `html/app.js` that calls `slaw.run` again; `script` is a `view.js` that draws a one-bar SVG. All four kinds share one manifest body (two date parameters, one query).
- **The describe row sits last in tab order** under the picker, and acts on the type chosen in step 1. For a notebook it creates the notebook and opens its AI cell dialog with the description filled in (nothing runs until Generate); for the other types it opens the assistant panel with an unsent request naming the kind and `write_artifact`. It never auto-sends and never overwrites typed text; a draft that cannot be placed waits behind a "Use it" notice.
- **No auto-land of drafts.** An artifact the agent writes shows up in the Library through its `data:changed` reload; Studio does not navigate to it.
- **`useModalKeys` now traps Tab app-wide.** Every modal that passes a root keeps Tab and Shift+Tab inside it, handles nested modals and native radio groups; `AiCellDialog` uses it and `ConfirmDialog`'s destructive button gets a focus ring.
- **Home.** `DashboardData.artifact_counts` is counting only (a helper beside `listArtifacts`; no new binding). The Library tiles are Notebooks, Live views, Pages and (only when non-zero) Chart scripts, each opening `/library?type=...`; a broken artifact counts once under `broken`, which Home shows as a "needs fixing" line linking to the Library (the "All" chip is where broken artifacts list). **New notebook** opens `/library?new=notebook`.
- **`create_dashboard`** keeps its name (open question 1, option a); its description gained one sentence pointing to `write_artifact` for a live view.
- **Chart scripts chip** appears with the first item (open question 2); starters keep the single allow path (3); queries stay out of the Library (4).
