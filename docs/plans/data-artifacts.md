# Data artifacts: parameterised, live views stored as files

Status: **level 1 delivered 2026-10-17 (steps 1-6b and 10); level 2 not built.** Epic #162; steps 1-10 are #151-#161 (step 6a #156, 6b #157, spike #158). Builds on vibe-app#67 (A2UI), vibe-app#66 (notebooks, task mode), vibe-app#116 (the agent SQL policy), vibe-app#118 (`.vibe` bundle v2), vibe-app#168 (the `.env` allow model) and #138 (storage hardening). Level 1 (steps 1-6b, and the step 10 end-to-end and docs sweep) is delivered; see *As built (level 1)*. Level 2 (steps 8-9) is **conditional on the spike in step 7**: the Linux half ran 2026-10-17 (see *Spike* at the end). The iframe design is dropped; a worker reshape passed on Linux and waits on the macOS run.

## Goal

A **data artifact** is a computation plus a view, stored as plain files in the workspace:

```
artifacts/<name>/
├── artifact.yaml        title, typed parameters with defaults, NAMED queries over those parameters
├── view.a2ui.json       level 1: a declarative A2UI view (closed schema, validated)
├── view.js              level 2 (conditional, the RESHAPE of D10): a worker view returning SVG or A2UI
└── html/                HTML artifact (proposal, docs/plans/html-artifacts.md): a template page, optionally scripted
```

1. **Live, not frozen.** An artifact stores questions (SQL and parameters), never answers. Every query runs when the artifact opens and again when a parameter changes.
2. **Compute stays in the engine.** Each named query is SQL vetted by the agent SQL policy when the agent saves it and again on every run. At run time only bound parameter values vary. A value is never spliced into SQL text. A view, of either level, can only ask "run query *by_region* with *from = 2026-01-01*"; it can never send SQL.
3. **One tool surface.** The agent authors artifacts with a `write_artifact` ToolDef dispatched by `agents.Handler`, so MCP clients get the same tool with the same gates. The CLI mirrors only what Studio's UI does.
4. **Level 1 view.** A2UI grows parameter controls (select, date range, number, text) that re-run the bound queries, a `pivot` (crosstab) component and drill-down. The schema stays closed and validated; model output never reaches a style attribute.
5. **Level 2 view (conditional).** A custom HTML/JS view, served from its own asset path with a CSP `sandbox` header, no network, no `window.go`, vetted chart libraries bundled same-origin, and one bridge: `run(queryName, params)`. A custom view that arrived by git or a bundle shows as source until the person allows it (path + content hash, decisions outside the folder, like `.env`).
6. **HTML artifacts (proposal, 2026-10-18).** `docs/plans/html-artifacts.md` adds a further view kind, a Go `html/template` page bound to the same named queries. A static page renders in Studio in a script-less frame; a scripted page with declared capabilities (`state`, `download`, `ask`) opens in a page window Vibe owns. It has its own spike (step P1) and generalises step 8's trust to a file set.

## Non-goals

- Storing results, snapshots or caches of query output in the folder or a bundle.
- Writes. Artifact queries are read-only in every sandbox tier (the same gate as an A2UI `sql` binding today).
- A Python or server-side compute plane; scheduled refresh; email or PDF delivery.
- Free-form JS that reaches Studio's DOM, Wails bindings, the network, or any SQL other than the manifest's named queries.
- Replacing notebooks. A notebook stays the place to explore; an artifact is the place to publish a parameterised answer.
- A new CLI for agents (`vibe artifact write` or `run`). Agents use MCP (memory: "CLI mirrors UI").
- Windows (as for the rest of Studio).

## What is there today (grounding)

- **A2UI is a closed schema in Go.** `internal/a2ui/schema.go` holds `Components` (20 types, `schema.go:144-165`), the closed `ColorTokens` enum (`:30`), `DataBound` (`:34-37`), `maxComponents = 200` (`:65`) and `Validate(raw)` (`:523`), which rejects unknown fields and reports JSON-path-prefixed errors that go straight back to the model. It has no notion of parameters or named queries: a data-bound component carries its own `sql` and optional `connection` (`tableData`, `:115-125`). `prompt.go:17-32` panics at init when `typeOrder` and `Components` disagree.
- **Bindings are vetted only when they run.** `present_data` and `create_dashboard` validate the page shape and save it without looking at its SQL (`internal/agents/handler_a2ui.go:19-25`, `notebook_cells.go:124-148`). Only `add_query_cell` classifies its SQL at save (`notebook_cells.go:61-90`). At render, every `sql` binding and every notebook sql cell runs through `App.RunA2UIQueryIn` (`studio/app_queries.go:210-240`) → `Handler.RunReadOnlySQLIn` (`internal/agents/handler_query.go:60-127`): wildcard expansion, `classifyIn` on a connection pinned to the binding's catalog, `checkSQLClassReadOnly` whatever the tier, a `LIMIT 501` push-down, a 500-row cap (`a2uiQueryRowCap`, `app_queries.go:159`) and a 30 s timeout (`:165`). `RunA2UIQueryIn` passes `nil` params (`app_queries.go:240`); the handler already accepts them. Studio runs it on a `ModeFull` handler (`readOnlyRunner`, `app_queries.go:251-261`) because `checkSQLClassReadOnly` (`sql_classify.go:396`) is the gate. A run-time denial is one fixed message (`ErrReadOnlyBindingDenied`, `sql_classify.go:391`); the policy's reason is only logged at debug level (`handler_query.go:90-97`).
- **Parameters already exist for agent SQL.** `run_query` takes `params` (`handler_query.go:131-133`). `engine.BindParams` (`internal/engine/params.go:29-89`) rewrites `$name` to `?` and builds an ordered args slice; names must match `^[A-Za-z0-9_]+$`; a missing value is an error. It is a **regex over the raw text** (`params.go:14`), so a `$name` inside a string literal or a `$$...$$` body is rewritten too. It emits one `?` and one arg per *occurrence* (its comment says per unique name; the code is per occurrence). **duckdb-go silently ignores extra args** (checked on 1.5.6: `SELECT '?'` with one arg returns `?`), so `'$x'` does not fail: it quietly becomes the literal `'?'`. Too few args is an error. `analyzeSQL` classifies the bound form (`internal/agents/sql_policy.go:112-121`) and `planCheck` runs `EXPLAIN` with the args (`sql_plan.go:37-41`). The policy refuses a parameter as a file or URL ("Not a literal: ... parameters", `docs/reference/agent-policy.md`, *Files and URLs*), so a parameter can never choose what is read.
- **Saved queries already declare parameters.** `queries.Param{Name, Type, Required, Default}` (`internal/queries/model.go:39-45`), stored in the `-- vibe:` header line (`docs/reference/workspaces.md`, *Queries*).
- **The PIVOT statement is refused** by the policy (it expands into several statements; agent-policy.md, *Limits worth knowing*). `FROM t PIVOT (... IN (...))` is a normal read.
- **The Vue renderer.** `A2UIPage.vue` builds the parent→children map and provides an abort signal; `A2UIComponent.vue` dispatches by type. `useLiveQuery.ts` runs the `sql` **once, in `onMounted`** (`useLiveQuery.ts:21-35`): nothing re-runs when an input changes. `runLiveQuery` (`a2ui/query.ts:96`) queues every live query through one priority pool (4 slots, 1 reserved for `'high'`). It is SQL-shaped (it calls `RunA2UIQuery`/`RunA2UIQueryIn` itself), and an abort only skips a queued query or drops a late result: the Go call is not cancelled and holds its slot until it returns (up to 30 s). Charts take colours from theme roles `chart.1`…`chart.6`, never from the page.
- **Stores.** Notebooks are one JSON file each (`notebooks/<id>.vnb`), coerced on load (`Coerce`, `internal/notebooks/model.go:261`): cells over 256 KB are truncated (`clampSource`, `:439`), more than 200 live components per notebook are **demoted to static** (`:49-66`, `demoteExcessDataBound` at `:494`), and an `a2ui` cell whose page fails `a2ui.Validate` is **dropped** with a warning (`:394-397`). Queries are `queries/<id>.sql`. Skills are a **folder per item**, `skills/<name>/SKILL.md` (`internal/skills/file_store.go:80`), listed with `filestore.ListSubdirs`. All three are compare-and-write through `internal/filestore` (`WriteAtomic`, `Read`, `ValidID` at `filestore.go:493`). The watcher classifies `notebooks/*.vnb`, `queries/*.sql` and `skills/<name>/SKILL.md` (`internal/workspace/watcher.go:190-197`, `:253-267`) and adds a watch on a newly created skill folder (`:338-370`); `filestore.ListSubdirs` skips symlinked folders (`DirEntry.IsDir`); `tempDirs` lists the same folders (`workspace.go:551-557`). `ResolveConflict` covers the three stores (`internal/workspace/conflict.go:72-85`).
- **The agent's view of the workspace.** `agents.WorkspaceAccess` has `Notebooks()`, `Queries()`, `Skills()` (`internal/agents/handler.go:25-37`); optional capabilities are separate interfaces checked by type assertion (`DataDirProvider`, `ProtectedPathsProvider`, `:39-51`). Tools are one registry (`internal/agents/tools.go`); `content`-category tools (`add_query_cell`, `create_dashboard`, `create_or_update_skill`) are refused in `read-only` (`sandbox.go:102-107`). MCP advertises `ListToolsForMode(ModeMCP)` and a test refuses registration outside the registry (`internal/mcp/tool_surface_test.go`). Dispatch is one switch (`handler.go:445-522`).
- **Bundles.** `internal/bundle` has a closed set of entry kinds (`bundle.go:71-77`: notebook, query, skill, connections) and entry patterns; every document is parsed by its store's rules before any write; caps are 10 MB per entry, 50 MB total, 2,000 entries (`bundle.go:29-39`). Agents, llm and mcp may not import it (workspaces.md, *Sharing without git*).
- **The `.env` trust model.** `dotenv.Decisions` (`internal/dotenv/decisions.go`) stores allow (absolute path + sha256 of content) and deny (path) in `dotenv.json` in the user config directory beside `local.yaml` (`internal/workspace/dotenv.go:20`, `:69`). Its error texts name `.env`, but the shape is generic.
- **Asset serving in Studio.** `studio/main.go:61-63` sets only `AssetServer.Assets` (the embedded `frontend/dist`); there is no `Handler` or `Middleware`, and the frontend has **no CSP at all** (`studio/frontend/index.html`). In Wails v2.16:
  - `Middleware` wraps the asset handler in production (`pkg/assetserver/assethandler.go:74`) and the Vite proxy in dev (`assethandler_external.go:79`). A `Handler` is only reached after a 404, which Vite's SPA fallback does not give for HTML.
  - Wails **injects `/wails/runtime.js` and `/wails/ipc.js`** into any `200 text/html` response whose path ends in `/` or `/index.html` (`pkg/assetserver/assetserver.go:139-183`, `:247-255`), including responses from our middleware.
  - **Binding calls from a sub-frame.** On macOS a binding message from any frame other than the main frame is dropped (`internal/frontend/desktop/darwin/frontend.go:171-176`). On Linux the origin check uses the **web view's** URI, not the sending frame's (`internal/frontend/desktop/linux/window.c:50-71`, `frontend.go:258-272`). So if WebKitGTK exposes `window.webkit.messageHandlers.external` to a sub-frame, that frame can call every bound `App` method, the query editor's unrestricted `RunQuery` included (`ExecuteQuery` is a test-only helper, `export_test.go:26`). On Linux every message from that handler, not only binding calls, takes this one check, so the runtime's `BrowserOpenURL` (opens the system browser: an exfiltration channel) is reachable too. Hiding `window.go` is not enough.
  - **Self-navigation to Studio's own page.** A sandboxed frame that navigates itself to `/` or `/index.html` gets Studio's `index.html` **with `ipc.js` injected** (the rule above). The frame stays sandboxed (opaque origin), but on Linux the injected IPC would pass the web-view-URI check. On macOS the only user script runs in every frame (`forMainFrameOnly:false`, `WailsContext.m:293-297`) but only sets a context-menu flag.
  - **No navigation hook.** Linux registers no `decide-policy` handler. On macOS `WailsContext` is the `WKNavigationDelegate` but implements only `didFinishNavigation` (`darwin/WailsContext.m:315`, `:509`). Neither exposes the native web view to the app.
- **Gates that will bind every step.** `TestExportedAppMethodsAreThinBindings` (≤ 25 statements, no I/O; `studio/thin_binding_test.go:22`) and the `thin-binding` ast-grep rule; `check-vue-size.mjs` (600 lines, 150 script lines); `gocognit` (min 30); `scripts/check-layering.sh` (every new `internal/` package must be added to `layer_of`; domain packages may not import `agents`); `internal/docsref` (`TestClaudeMDTreeMatchesInternal`; every A2UI component in `docs/reference/notebooks.md`'s table, `docsref_test.go:81-193`); `ts-window-go-outside-binding-layer`; `check-arity.mjs` for new `AppApi` methods.
- **A docsref bug to fix on the way.** `TestNotebooksDocComponentTableMatchesA2UIRegistry`'s reverse check is a no-op: `assert.NotNil(t, a2ui.Components[name])` (`docsref_test.go:174`) is always true for a struct map value, and its row regex also matches the *Motion* table (`easing`, `speed`, `stagger`). A stale row is never caught.
- **Rodney is Chrome.** Live QA runs headless Chrome against `:34115` (`docs/contributing/rodney.md`), not WebKitGTK or WKWebView. It cannot accept any level-2 isolation property.
- **Naming.** `direction.md` §3.7 calls A2UI pages "Artifacts (A2UI)". This plan calls the new folder items **data artifacts** and keeps "A2UI page" for a notebook cell or a chat reply.

## Decisions

### D1. A new top-level store, `artifacts/<name>/`, not a richer notebook cell

Artifacts are a fourth store beside notebooks, queries and skills, shaped like skills (a folder per item). Reasons from the code:

- **Refuse, don't repair.** A notebook is coerced on load: oversized sources are truncated and live components past the budget are demoted to static (`notebooks/model.go:306-385`). An artifact's queries are vetted and (at level 2) its view is trusted by hash, so what runs must be exactly what was checked. A broken artifact lists with its error, as a broken query file does, and does not run.
- **The trust key must be stable.** Allow is keyed by content hash. A `.vnb` changes on every autosaved edit to any cell, so a custom view inside a notebook would ask again after every keystroke elsewhere. A separate `view.html` (and `artifact.yaml`) changes only when the artifact does.
- **Parameters need a scope.** Notebook cells run with `nil` params (`app_queries.go:240`) and share nothing. Notebook-level parameters would change the meaning of every existing cell.
- **Level 2 needs a non-JSON file and a URL.** A folder per item gives both. Skills already prove the folder shape through the store, watcher and bundle.
- **Reuse, not a second system:** the A2UI validator and renderer, the `queries.Param` field names, `filestore`, the watcher, `ResolveConflict`, the bundle importer, `RunReadOnlySQLIn`.

An `artifact` notebook cell (embed by name) is in [Later](#later).

### D2. The manifest

```yaml
# artifacts/regional-sales/artifact.yaml
version: 1
title: Regional sales
description: Revenue by region, with drill-down to customers.
parameters:
  - name: from
    type: date
    default: today-90d
    label: From
  - name: to
    type: date
    default: today
  - name: region
    type: enum
    options_query: regions
    default: ""
queries:
  - name: regions
    connection: prod_pg
    sql: SELECT DISTINCT region FROM sales.orders ORDER BY 1
  - name: by_region
    connection: prod_pg
    sql: |
      SELECT region, sum(amount) AS revenue
      FROM sales.orders
      WHERE order_date BETWEEN $from AND $to
      GROUP BY ALL ORDER BY revenue DESC
  - name: customers
    connection: prod_pg
    sql: |
      SELECT customer, sum(amount) AS revenue
      FROM sales.orders
      WHERE order_date BETWEEN $from AND $to AND ($region = '' OR region = $region)
      GROUP BY ALL ORDER BY revenue DESC LIMIT 50
view: a2ui            # a2ui | html (html only if level 2 ships)
```

- **Strict decode.** `yaml.v3` with `KnownFields(true)` (as `workspace/config.go:331-332`); an unknown key is an error, so nothing credential-shaped can hide in an extra field. No `${ENV}` interpolation anywhere.
- **Names.** The folder name is the artifact name and must pass `filestore.ValidID`. Parameter and query names match `engine.ValidateParamName`, **start with a letter** (so a positional `$1` can never be a declared parameter) and are unique.
- **Parameter types** (closed): `text` (with `max_length`, default and cap 1,000), `number` and `integer` (optional `min`/`max`), `date`, `boolean`, `enum` (static `options:` of up to 200 strings, or `options_query:` naming a query whose first column supplies the options; that query may not use the parameter it feeds, and the default need not be one of its rows: the select shows it first, so `""` reads as "all"). **Every parameter has a default**, so an artifact opens without input and the save-time vet can bind every query.
- **Date defaults** may be a literal `YYYY-MM-DD` or one of a closed set, resolved in Go on the local clock at run time: `today`, `today-<n>d` (1-3660), `start-of-month`, `start-of-year`. A frozen literal default would make "the last 90 days" stale.
- **Queries.** Up to 20 per artifact, `sql` up to 64 KB each, an optional `connection` (validated like a notebook cell's, `policy_handler.go:232-256`). Every `$name` in a query must be a declared parameter. Which parameters a query uses is **derived** from its SQL, not declared, so the two cannot disagree.
- **Caps.** `artifact.yaml` 256 KB, `view.a2ui.json` 1 MB, `view.html` 1 MB (level 2), and the A2UI page cap of 200 components.

### D3. Binding: names and values cross the bridge, never SQL

`internal/artifacts` turns `(manifest, queryName, rawValues, now)` into `(sql, connection, params)`:

- An unknown query or parameter name, a value of the wrong type, a `text` value over its length, a number outside `min`/`max`, a `date` that is not a real `YYYY-MM-DD`, or an `enum` value outside its **static** options is refused. Values missing from the call take the default.
- Go types: `text`/`enum` → `string`, `number` → `float64`, `integer` → `int64`, `boolean` → `bool`, `date` → `string` in ISO form. Values arrive over the bridge as JSON, so an `integer` must be integral and within ±2^53 before it becomes `int64`.
- **Checked on DuckDB 1.5.6 (duckdb-go v2.10506.0, 2026-10-16; answers open question 1).** An ISO string bound to `?` takes `DATE` from a comparison: `d BETWEEN ? AND ?`, `d >= ?` and `d = ?` all work, and DuckDB itself refuses `'not-a-date'` and `'2026-02-30'`. It does **not** in a function or arithmetic context: `? - INTERVAL 1 DAY` and `date_trunc('month', ?)` are binder errors ("could not choose a best candidate"), and `typeof(?)` is `VARCHAR`. So `date` stays a string, and the skill and `artifacts.md` say to write `$from::DATE` outside a plain comparison; the save-time vet (D5) binds defaults, so the agent sees the binder error and fixes it. `time.Time` is **not** the fallback: duckdb-go binds it as `TIMESTAMP WITH TIME ZONE`, and `date_trunc('month', ?)` then shifts by the local zone. `LIMIT ?` and `LIMIT $1` accept a bound `int64` (also `float64` 2.0 and the string `'3'`); a negative value is a binder error; the push-down's outer `LIMIT 501` still caps a huge one. A text value such as `'5 OR 1=1'` bound against an integer column is a conversion error, never SQL. `ORDER BY ?` is refused by DuckDB ("Parameter not supported"), so a parameter cannot choose a sort column.
- The run path is `Handler.RunArtifactQuery(ctx, name, query, values)` in `internal/agents`. It loads the artifact **from the store** (never from the caller), re-validates the query's `connection` (as `RunA2UIQueryIn` does with `session.ValidateName`), repeats D5's two lexer checks (cheap, no I/O: a hand-edited file never passed the save-time vet, and duckdb-go would otherwise quietly turn `'$x'` into `'?'`), binds the values, and calls `RunReadOnlySQLIn(ctx, connection, sql, params, a2uiQueryRowCap)`. Expansion, classification, the read-only gate, the row cap and the timeout are the existing ones; there is no second execution path. Studio's binding, the level-2 bridge and any later MCP tool all call this one method.
- **The re-check at run is the real gate.** Files arrive by git, editors and bundles without passing through the agent, so the save-time vet (D5) is for good errors, not for safety. An artifact query is held to the agent's read policy **whoever wrote it**, as notebook sql cells already are (they run through `RunA2UIQueryIn`). Because `classifyIn` runs with the actual values, the plan check (`EXPLAIN`) at run sees what will run, not the defaults. A run-time denial shows the fixed `ErrReadOnlyBindingDenied` text (grounding); the reason is what the Source tab's vet warnings (step 6a) are for.

### D4. Layering: structure in `internal/artifacts`, SQL vetting in `internal/agents`

`internal/artifacts` is a domain package (added to `layer_of`): the manifest model, strict decode, structural validation, binding, `FileStore`, `Mock`. It cannot import `internal/agents` (`check-layering.sh` rule 3), so it cannot run the SQL policy. SQL vetting lives in `internal/agents/artifact_vet.go`. The agent's handler reaches the store through an optional `ArtifactsProvider` interface (as `DataDirProvider`), so the `WorkspaceAccess` mocks in `agents_test.go` and `bundle.Target` do not have to change.

### D5. Vetting at save

`Handler.VetArtifact(ctx, m)` runs on every query, with defaults bound: `validConnection` → `connectReferenced` → `expand` → `classifyIn` with the params → `checkSQLClassReadOnly`. Two extra checks, on the policy's own lexer (`sql_lex.go`):

- every `$name` occurrence is a parameter token, not text inside a string literal, `E'...'`, a quoted identifier or `$$...$$` (`engine.BindParams` would rewrite those too, and duckdb-go ignores the extra arg, so it would not fail; grounding); and the SQL has no bare `?` or positional `$1` of its own (it would take another parameter's arg);
- a parameter is never the argument of `query(...)`, `query_table(...)` or a reader (the policy already refuses a parameter as a path; this gives a clearer error at save).

`write_artifact` refuses on any vet failure, with the policy's reason, so the model can correct. Studio's **Save** of a person's own edit (step 6a) saves on structural success and shows vet failures as warnings: a person must be able to save while a source is offline, and the run-time gate is the same either way.

### D6. A2UI in an artifact view: a context-aware validator

- `a2ui.ValidateView(raw, ViewContext{Params, Queries})` validates an artifact view. `a2ui.Validate(raw)` (chat, notebooks, `create_dashboard`) is unchanged and **refuses** the new components and fields: "`select` is only allowed in a data artifact view". (A notebook `a2ui` cell carrying one is therefore dropped by `Coerce` with that warning, as any invalid page is today.)
- **A separate registry.** The view-only types live in `a2ui.ViewComponents` with their own order and init-time check, not in `Components`/`typeOrder`. So the chat `<a2ui>` prompt never lists them (the `data-artifacts` skill documents them, progressive disclosure), and every existing `range a2ui.Components` (docsref, Vitest parity, `DataBound`) is unchanged. `ViewContext` is plain names and types defined in `a2ui`, so `a2ui` imports nothing new.
- **In a view, data comes only from named queries.** Every data-bound component takes `query: "<name>"` instead of `sql`; `sql` and `connection` are refused in a view (the connection belongs to the query). Inline data stays allowed. `query` must name a manifest query.
- **Controls** (new types; each names its parameter, the manifest holds the semantics):
  - `select` `{param, label?}` for `enum` and `boolean` parameters (options from the manifest, static or from `options_query`);
  - `dateRange` `{start, end, label?}`, both `date` parameters;
  - `numberInput` `{param, label?}` for `number`/`integer`;
  - `textInput` `{param, label?}` for `text`.
  A control whose parameter is missing or of the wrong type is a validation error. Typing is debounced (400 ms); every change re-runs **only the queries that use that parameter**.
- **`pivot`** `{query, rows: [col…], columns: col, value: col, label?, drill?}` lays out a **long-form** result (one row per row-key × column-key, already aggregated with `GROUP BY` in the query) as a crosstab. Compute stays in the engine: the component never aggregates, sums or totals. Two rows for the same cell are a render error that names the cell and says to group by those columns. Totals come from the SQL (`GROUP BY ROLLUP`) and are shown as returned. The 500-row cap is the cell cap; a truncated result is labelled.
- **Drill-down** `drill: {param, column}` on `table`, `barChart`, `columnChart` and `pivot` rows: a click sets `param` to the clicked row's `column` value, coerced and validated as any value (D3), which re-runs the dependent queries. The control of that parameter shows the value and a **Reset** to the default. `column` is checked against the result at render (columns are only known after a run).
- No new colour or style field. Controls and the pivot use SchoolHouse components and `--vibe-*` roles; nothing from the page reaches a `style` attribute.
- `ValidateView` takes the same `raw` shapes as `Validate` and returns the same JSON-path error format, so `write_artifact` feeds errors back to the model unchanged.

### D7. Studio level 1

- Routes `#/artifacts` (list) and `#/artifacts/:name` (view). Thin views: `ArtifactsView.vue`, `ArtifactView.vue`.
- Composables (one job each): `useArtifacts` (list, watcher refresh), `useArtifact` (load one, errors), `useArtifactParams` (values, defaults, reset, debounce), `useArtifactRuns` (one run per *(query, values used by that query)*, shared by every component bound to it, through the existing pool (`runPooled`, step 4) with an `AbortSignal` per parameter state, so a stale result is dropped).
- The A2UI renderer gets an optional injected **artifact runtime** (`provide('a2uiArtifact', …)`). A data-bound component with `query` reads its result from the runtime; one with `sql` keeps `useLiveQuery`. Notebooks and chat never provide the runtime, so their behaviour does not change.
- Bindings (thin): `ListArtifacts`, `GetArtifact(name)` (manifest, view, validation errors, rev), `RunArtifactQuery(name, query, values)` → the existing `A2UIQueryResult`.

### D8. Sharing and the CLI

- **Bundle**: a new kind `artifact` with entries `artifacts/<name>/artifact.yaml` and `artifacts/<name>/view.a2ui.json` (and `view.html` if level 2 ships), parsed by `artifacts.Decode` / `ValidateView` before any write, written through the store. Selection gets `Artifacts []string`; `ContentsOf` lists them.
- **CLI mirrors the UI only.** `vibe artifact list` mirrors the list view (name, title, view kind, error). If level 2 ships, `vibe artifact view status|allow|deny|revoke <name>` mirrors the trust prompt and Settings, as `vibe workspace env` does. There is no `vibe artifact run` or `write`.

### D9. Level 2, as agreed (subject to step 7)

- **Serving.** A Wails `Middleware` (not `Handler`, see grounding) serves `/vibe-view/<token>/<artifact>/view.html` and `/vibe-view/lib/...`. `<token>` is random per Studio process. The path never ends in `/` or `index.html`, so Wails never injects its runtime; a request under `/vibe-view/` that does is answered `400`, not `404` (Wails swaps a 404 on an injection-matching path for its own default HTML page, `assetserver.go:174-175`). Every response carries:
  `Content-Security-Policy: sandbox allow-scripts; default-src 'none'; script-src <studio-origin>/vibe-view/lib/ 'unsafe-inline'; style-src <studio-origin>/vibe-view/lib/ 'unsafe-inline'; img-src data: blob:; font-src <studio-origin>/vibe-view/lib/; connect-src 'none'; frame-src 'none'; worker-src 'none'; form-action 'none'; base-uri 'none'`
  plus `X-Content-Type-Options: nosniff` and `Cache-Control: no-store`. The iframe also gets `sandbox="allow-scripts"` (no `allow-same-origin`, `allow-popups`, `allow-forms`, `allow-top-navigation`). `'unsafe-inline'` is acceptable only because the whole document is the untrusted, opaque-origin view.
- **Serve what was hashed.** The middleware reads the file once, hashes it with `artifact.yaml`, compares with the allowed hash, and serves those bytes. A file changed after the allow gets a "changed since you allowed it" page.
- **Libraries.** A short, pinned list from npm (for example `d3` modules and `vega` + `vega-lite`), vendored into the embedded FS by the frontend build, licences in `THIRD_PARTY_NOTICES.md` (`task notices`). No CDN.
- **The bridge.** At load the host posts one handshake to the frame carrying a `MessagePort` and the resolved `--vibe-*` theme roles (values only, already validated by `theme/schema.ts`). All later traffic uses that port, never `postMessage(…, '*')`: if the frame ever navigated, the new document would not hold the port, so query results cannot follow it. Messages are `{id, type: 'run', query, params}` only, shape-checked and size-capped in TS, rate-limited (4 in flight, 60 a minute), and passed to `RunArtifactQuery`, which re-validates against the manifest.
- **Trust like `.env`.** A custom view is shown **as source** (read-only CodeMirror) until the person allows it. Allow is keyed by the artifact folder's absolute path plus `ViewHash`: sha256 over the **length-prefixed** `artifact.yaml` and `view.html` (or the hash of the two per-file hashes), never over their plain concatenation, which would let bytes move across the boundary (a `<script>` in a trailing YAML comment at allow time, moved into `view.html` later) with the same hash. Deny by path. When the watcher reports a change to an artifact whose custom view is showing, the host tears the view down and re-checks, so a frame loaded before the change cannot keep running against the new manifest. Decisions live in `views.json` beside `dotenv.json` in the user config directory, never in the folder or a bundle. This applies to views the agent wrote too: an agent's output is model output.
- The decision store is extracted from `internal/dotenv` into a small leaf, `internal/trustfile` (generic noun, same file format), and `dotenv` uses it. One implementation of "allow by path + hash, deny by path".

### D10. Level 2: what the spike must settle (go/no-go)

Three known risks and one found while grounding this plan. Step 7 measures them in the **real Wails window** on Linux (WebKitGTK 4.1, `webkit2_41`) and macOS (WKWebView), never in Rodney's Chrome.

| | Risk | Pass means |
|---|---|---|
| (a) | **Navigation exfiltration.** A sandboxed frame can still set its own `location` to an external URL (with data in the query string); CSP `navigate-to` is gone; Wails exposes no navigation policy (grounding). | Every vector (`location =`, `location.replace`, `<a>` click, `<meta http-equiv=refresh>`, `window.open`, `top.location`, form submit, `data:`/`blob:` navigation) is blocked on both platforms, and a loopback listener standing in for the outside sees **no request**. Candidates: (a1) a CSP on Studio's own page (meta tag in `index.html`) with `frame-src <studio-origin>/vibe-view/`, which governs navigations inside child frames; (a2) a native `decide-policy` handler on Linux via cgo, found through the GTK widget tree. macOS has no app-side hook without patching Wails (swizzling `WailsContext` is rejected), so on macOS only (a1) can pass. |
| (b) | **Freeze.** `while(true){}` in a same-process iframe stalls Studio's UI thread (WebKit has no site isolation for these frames). | Studio stays responsive and the view can be stopped within 2 s. Expected to **fail** for an iframe. Then measure the alternative: the view's code runs in a dedicated **Web Worker** that the host can `terminate()` on a missed heartbeat. |
| (c) | **The CSP `sandbox` header** from Wails' asset handler (custom scheme on both platforms). | In the frame: `origin === 'null'`, `document.cookie` throws, `parent.document` throws, `fetch`/XHR/WebSocket/`EventSource`/`<img src=http…>`/`new Worker` blocked, `'self'` and the lib path resolve as written. For the worker shape: the worker's **own** response CSP is enforced (`fetch`, `importScripts`, WebSocket, nested worker all blocked). |
| (d) | **Wails IPC from a sub-frame** (found in grounding). On Linux, the binding message handler checks the web view's URI, not the frame's. | `window.webkit?.messageHandlers?.external` is undefined in the frame, or a raw binding message posted from the frame never reaches Go (logged with a harmless method), and neither does a runtime message (`BrowserOpenURL` to the loopback listener). The frame navigating **itself to `/` and `/index.html`** (Studio's page with `ipc.js` injected) is blocked, or the injected IPC from there never reaches Go. In a worker, `self.webkit` is undefined. |

**Outcomes:**

- **GO as designed (iframe)** only if (a), (b), (c) and (d) all pass on **both** platforms.
- **RESHAPE to a worker view** if (b) or (d) fails for the iframe but the worker passes (c) and (d) on both platforms. Level 2 becomes "custom drawing, not custom UI": the file is `view.js`, run in a terminable dedicated Worker, receiving rows only through `run()`, and returning either an **SVG document**, shown as `<img src="blob:…">` (an SVG image runs no script and loads nothing, which the spike also checks), or an **A2UI page**, validated by Go with `ValidateView` before it renders. Parameter controls and drill-down stay level-1 components around it. The bundled libraries must work without a DOM (d3 path generators, `vega` `toSVG()`).
- **DROP level 2** if the worker cannot be held to `connect-src 'none'` on either platform, or (a) cannot be closed for the iframe and the worker reshape also fails. Artifacts stay at level 1, the plan records the result in `direction.md` §9, and missing views become new A2UI components instead.
- A platform the spike could not be run on (no Mac to hand) counts as a fail.

## Steps (sequential; each a Sonnet implementation, then an Opus adversarial review; commit only after APPROVE)

**Gates for every step:** `task lint` (golangci-lint with gocognit, and `vue-tsc`), `go test ./...`, `task test:frontend` (Vitest), `node studio/frontend/scripts/check-vue-size.mjs`, `ast-grep scan -c tools/ast-grep/sgconfig.yml cmd internal studio`, `scripts/check-layering.sh`, and `go test ./internal/docsref`. Every step that changes the UI adds **Rodney live QA** against `task studio:dev` at `:34115`, light and dark, with screenshots in `qa-screenshots/`. New `AppApi` methods go through `src/lib/bindings.ts` and `env.d.ts` (CI's `check-arity.mjs`).

### Level 1

#### Step 1. `internal/artifacts`: the manifest, the store and binding (Go only)

- `internal/artifacts`: `Manifest`, `Param`, `Query` (field names aligned with `queries.Param`), `Decode` (strict YAML, D2), `Validate` (names, types, defaults, caps, every `$name` declared, derived `Uses` per query), `Bind(m, query, values, now) (sql, connection string, params map[string]any, err error)` (D3), `FileStore` (`artifacts/<name>/artifact.yaml` and `view.a2ui.json`; `List` returns broken items with `Error`, like queries; compare-and-write with `Rev`; `Put`, `Get`, `Delete`), `Mock`, the store contract test shared by both.
- `engine.ParamNames(sql)`: the names `BindParams` would bind, exported from `params.go` so the two cannot drift.
- **One revision for two files.** `Rev` is a hash over both files (length-prefixed, as `ViewHash`); `Put` checks it against both files on disk before writing either, writes `view.a2ui.json` then `artifact.yaml`, each with `WriteAtomic`. A crash between the two leaves a pair that fails validation (a view naming a missing query) and lists with its error; it never runs half-checked. Both files are read before `Validate`, and the pair is what `Get` returns.
- Workspace wiring: `Workspace.Artifacts()`, `AgentAccess.Artifacts()`, watcher kind `artifact` (`artifacts/<name>/<file>`; `addTree` and the create-a-folder path at `watcher.go:338-370` watch the subfolders, as for skills), `tempDirs`. (`ResolveConflict` kind `artifact` moves to step 6a, where Studio first edits an artifact.)
- `scripts/check-layering.sh`: `internal/artifacts` in the domain layer. `CLAUDE.md` tree and *Key concepts*: one line each. `docs/README.md` indexes this plan (committed with this step).
- **Tests:** strict decode refuses unknown keys, duplicate names, a `$name` with no parameter, a parameter with no default, each cap; every type's coercion and refusal; date defaults on a fixed clock (`today-90d` across a month and a leap day); **on a real DuckDB 1.5.6**, pin the D3 results as regression tests (ISO string in `BETWEEN` compares as `DATE`; `$from - INTERVAL 1 DAY` is a binder error and `$from::DATE - INTERVAL 1 DAY` works; `LIMIT $n` takes an `int64`), so a DuckDB bump that changes them fails loudly; a parameter named `1` refused; the store contract on `FileStore` and `Mock`; watcher classifies `artifacts/x/artifact.yaml` and ignores temp files.
- **Review first:** values reaching SQL text by any path (`Bind` must only build `params`); `..`, upper case, Windows device names and symlinked folders as artifact names; a manifest that passes `Validate` but makes `BindParams` fail (a `$name` inside a literal: acceptable here, caught in step 3); YAML anchors and aliases used to blow up size (cap the bytes before decode); `List` following a symlink out of the workspace (`ListSubdirs` skips symlinked folders; `Get` must also refuse a symlinked folder or file, since it is reached by name); the two-file rev (a write that checks one file's rev only).

#### Step 2. A2UI view schema: controls, `query` binding, `pivot`, `drill` (Go only)

- `internal/a2ui`: `ValidateView(raw, ViewContext)` (D6). New components `select`, `dateRange`, `numberInput`, `textInput`, `pivot` in `ViewComponents` (D6, a separate registry with its own init check); new fields `query` and `drill`. `Validate` refuses them with "only in a data artifact view". The chat `<a2ui>` prompt and `typeOrder` do not change: the five types are documented in the `data-artifacts` skill (step 3), so `present_data` and `create_dashboard` are not tempted.
- `internal/artifacts.Validate` calls `a2ui.ValidateView` with the manifest's parameters and queries.
- `internal/docsref`: fix the reverse check (`_, ok := a2ui.Components[name]`) and scope the row regex to the *Component types* section; check `ViewComponents` against a separate "Artifact view components" table the same way, both directions. `docs/reference/notebooks.md`: that table, marked "data artifact views only". New `docs/reference/artifacts.md` (manifest, parameter types, date defaults, view rules, controls, pivot, drill), linked from `docs/reference/README.md`.
- **Tests:** every new type valid and invalid in `ValidateView`; each refused by `Validate` (and so by `present_data`, `create_dashboard`, and dropped with a warning by notebook `Coerce`); the chat `<a2ui>` prompt text unchanged (golden); `sql` and `connection` refused in a view; `query` naming a missing query; a control on a missing or wrongly typed parameter; `dateRange` over a non-date; `drill.param` undeclared; JSON-path error text stable; the docsref test now fails on a stale row (prove it with a temporary bad row in a test fixture, not the real doc).
- **Review first:** any path where a view field reaches a style attribute or a class name built from input (`pivot` column names, `label`); a page valid under `Validate` that changes meaning under `ValidateView` or the reverse; whether a notebook saved before this step still loads (`Coerce` uses `Validate`).

#### Step 3. Agent and MCP tools: `write_artifact`, `list_artifacts`, `get_artifact` (Go only)

- `internal/agents`: `ArtifactsProvider`; `artifact_vet.go` (`VetArtifact`, D5, with the lexer checks); `RunArtifactQuery` (D3); handlers in `handler_artifacts.go`; ToolDefs in `tools.go`:
  - `write_artifact` (category `content`): `name`, `manifest` (object), `view` (object, the A2UI page), optional `rev`. Structural validation, then vet, then compare-and-write. Refused in `read-only` like the other content tools.
  - `list_artifacts`, `get_artifact` (category `introspect`): manifest and view as data, never results; long SQL truncated as `get_notebook` does.
- A built-in skill `skills/data-artifacts/SKILL.md` (embedded): the manifest schema, the view rules and one worked example. The tool description stays one paragraph and points to `read_skill` (progressive disclosure).
- `docs/reference/agent-policy.md`: `write_artifact` in the content-tools row; "artifact queries are read-only in every tier and are vetted at save and at every run". `docs/reference/mcp.md`: the three tools.
- **Tests:** a write that vets clean lands as files; each D5 refusal (a write statement, `read_csv(?)` from a parameter, `$x` inside `'...'`, `"..."` and `$$...$$`, a bare `?` or `$1`, `query($q)`, an unknown connection, a storage connection with no catalog); a file edited on disk to `'$x'` in a literal is refused **at run** too (the run repeats the lexer checks); `read-only` tier refuses the write; `RunArtifactQuery` with a value that would be an injection as text (`'; DROP TABLE x; --`) returns zero rows and changes nothing; an unknown query or parameter refused; a file edited on disk to a write statement after save is refused at run (the run-time gate); MCP lists exactly the new tools (`tool_surface_test.go` unchanged); task mode does not see them (`TaskAllowedToolNames` unchanged).
- **Review first:** vet and run disagreeing (vetted on one connection, run on another; vetted with defaults, run with a value that changes the plan, such as a huge `LIMIT $n`); a value that makes the policy's `EXPLAIN` itself do I/O; the rev check (agent overwriting a person's concurrent edit); error text leaking a path, a DSN or a credential (the redactor must see it); `get_artifact` as a way to read files outside `artifacts/`; any tool that hands an artifact's **results** to the agent (none may in this plan: the agent writes artifacts but never reads their output, so an artifact cannot widen what the agent reads; see *Later*).

#### Step 4. Studio: the artifact list and view with parameter controls

- Bindings in `studio/app_artifacts.go` (each ≤ 25 statements): `ListArtifacts`, `GetArtifact`, `RunArtifactQuery` (timeout and row cap as `RunA2UIQueryIn`).
- The pool: split `runLiveQuery` into a generic `runPooled(task, signal, priority)` (slot accounting and abort as today) plus the existing SQL wrapper, so artifact runs share the one pool without a second one. `useArtifactRuns` keeps **at most one queued run per query**: a newer value replaces a queued, not-yet-started run, since an aborted in-flight Go call still holds its slot (grounding).
- Frontend (D7): routes; `ArtifactsView.vue`, `ArtifactView.vue`; `useArtifacts`, `useArtifact`, `useArtifactParams`, `useArtifactRuns`; `a2ui/useBoundQuery.ts` (reads from the injected runtime); `A2UIControl.vue` for the four controls (SchoolHouse inputs, `--vibe-*` roles, visible focus); the existing data-bound components accept `query` through `useBoundQuery`. A nav entry for Artifacts. The list refreshes on `workspace:changed` with kind `artifact`. A broken artifact opens to its errors, not a blank page.
- **Tests (Vitest):** `useArtifactRuns` runs a shared query once for two components, re-runs only dependants of a changed parameter, drops a stale result after a fast second change; debounce; Reset restores the default; a view with a `sql` component never reaches `RunArtifactQuery` (it cannot validate, but the renderer must not crash).
- **Rodney:** create a workspace with `data/local.duckdb`; write one artifact's two files on disk (the watcher path; there is no save binding until step 6a) and one through `write_artifact` on Studio's MCP server; open it, change the date range and the select, see only the dependent charts reload, break the YAML on disk and see the error, fix it and see it recover.
- **Review first:** `RunArtifactQuery` called with a name from another workspace after a switch; a burst of parameter changes starving the shared pool (the user's own Run must still win, `'high'` lane); values from the URL or the DOM reaching the binding unvalidated; any `v-html` or built `class`/`style` in the new components (labels, option rows from `options_query`, error text are text interpolation only); thin-binding and size limits.

#### Step 5. Studio: `pivot` and drill-down

- `A2UIPivot.vue` (layout only: row headers, column headers, cells, a "truncated" note) and a pure `a2ui/pivot.ts` (long form → grid, duplicate-cell detection, stable header order: order of first appearance, which the SQL controls).
- Drill on `table`, `barChart`, `columnChart` and `pivot` rows: click and keyboard (Enter on a focused row or bar); the parameter's control shows the value and Reset.
- **Tests:** `pivot.ts` on ragged input, a duplicate cell, `NULL` keys (shown as "(blank)", ROLLUP rows as returned), 500 cells; drill sets a coerced value and a value the parameter refuses shows an inline error and runs nothing.
- **Rodney:** pivot of region × month; click a region bar, see the customers table filter; Reset; keyboard-only drill.
- **Review first:** a drill value that is not in a static enum; drill on a column that is not in the result; a pivot whose column header text is long or contains markup (rendered as text only); accessibility of the grid (table semantics, focus).

#### Step 6a. Studio editing and conflicts

- Studio **Source** tab on `ArtifactView`: CodeMirror for `artifact.yaml` and `view.a2ui.json`, **Save** through `SaveArtifact(name, manifest, view, rev)` (structural errors refuse; vet failures are warnings, D5, with the policy's reason, which a run-time denial does not show), the conflict banner through `useConflict` and `ResolveConflict` kind `artifact` in `internal/workspace/conflict.go` (Reload, Keep mine, Save as copy to `<name>-copy` as skills do, over the two-file rev of step 1). **New artifact** and **Delete** (with confirm). The Source tab is its own component and composable (`useArtifactSource`) so `ArtifactView.vue` stays inside the 600/150-line limit.
- **Tests:** Save with a stale rev conflicts (either file changed); each `ResolveConflict` action; a structural error refuses, a vet failure saves with warnings; Delete removes both files and the folder.
- **Rodney:** edit the date default in Source, save, see the view update; a concurrent edit on disk shows the conflict banner and each action does what it says.
- **Review first:** Save writing two files non-atomically (a crash between them: what does the watcher show?); `SaveArtifact` thin (the vet call lives in a non-binding helper); a name from the route reaching the store unvalidated.

#### Step 6b. Bundles and `vibe artifact list`

- `internal/bundle`: kind `artifact`, the two entry patterns, `Selection.Artifacts`, `ContentsOf`, import through `artifacts.Decode` and `ValidateView` and the store (rename, overwrite with rev, skip, as notebooks). `bundle.Target` gains `Artifacts()` (its test doubles change; `bundle` may not import `agents`, so an import is structural only and the run-time gate is the safety, D3). Studio's export and import dialogs list artifacts. `docs/reference/workspaces.md`: layout, file format, bundle format and entry list.
- `cmd/artifact.go`: `vibe artifact list` (mirrors the list view: name, title, view kind, error).
- **Tests:** bundle round trip byte-identical; a bundle with `artifacts/../x`, a nested path, an unknown file in an artifact folder, only one of the two files, or a manifest that fails `Decode` is refused whole; the CLI output for a valid and a broken artifact.
- **Rodney:** export and import a bundle with an artifact; the imported artifact runs.
- **Review first:** the bundle entry rules (zip-slip, case folding on macOS, a `view.html` sneaking in before level 2 exists); an imported artifact with a hostile query (it imports, and is refused at run); `vibe artifact list` printing more than the UI shows.

### Level 2 (conditional)

#### Step 7. Spike: isolation of a custom view in the real webview (not committed)

- A throwaway branch: a `Middleware` serving a spike page under `/vibe-view/spike/...` with the D9 CSP, an iframe host and a worker host, a loopback HTTP listener that logs every request, and a log line in Go for any binding call. Run in `task studio:dev` **inside the Wails window** on Linux (WebKitGTK 4.1) and on macOS (arm64; Intel if available).
- Measure every row of D10, each with a one-line result and the WebKit version. Also: does `<img src="blob:…svg">` with `<script>`, `<foreignObject>`, `<image href="http://127.0.0.1:…">` and CSS `url()` load or run anything; does `frame-src` in a meta CSP on Studio's page stop a child frame's self-navigation; does adding that CSP break anything in Studio (dev server, Vite HMR, CodeMirror, A2UI motion).
- **Output:** a "Spike: custom view isolation (run YYYY-MM-DD)" section appended to this plan (Verified / Not verified, as `storage-setup-and-sso.md` does), and the go / reshape / drop decision of D10 recorded in the plan's status and on the epic. The owner signs off before step 8.
- **Review first:** that every "blocked" result was observed at the loopback listener or in Go's log, not inferred from a console error; that macOS was actually run.

#### Step 8. Trust for custom views (only on GO or RESHAPE)

- `internal/trustfile` (leaf, domain layer): `Decisions` extracted from `internal/dotenv` (same JSON, generic error texts); `dotenv` uses it with no behaviour change (its tests unchanged).
- `internal/artifacts`: the custom view file (`view.html`, or `view.js` on RESHAPE), its cap, `ViewHash(manifestBytes, viewBytes)`. Bundle entry pattern for it. `write_artifact` gains `view_source` (string). `views.json` beside `dotenv.json`; `workspace` exposes `ArtifactViewState(name)` and `Allow/Deny/Revoke`.
- Studio: a custom view shows **as source** with **Allow** / **Deny** and a plain line ("This view runs code written by someone else or by the agent. It cannot reach the network or your files, and can only run this artifact's queries."). Settings > Workspace lists decisions with Revoke. `vibe artifact view status|allow|deny|revoke <name>`.
- **Tests:** allow then any byte change to either file → *changed*; bytes moved from the end of `artifact.yaml` to the start of the view → *changed* (the length prefix); deny holds across edits; a cloned folder at another path is undecided; decisions never written inside the workspace or a bundle; `dotenv` tests pass unchanged.
- **Rodney:** an agent-written custom view shows as source; Allow; edit on disk → asks again.
- **Review first:** TOCTOU between the hash shown at Allow and the bytes later served; path canonicalisation (symlinked workspace, case-insensitive macOS paths) making two folders share a decision; the agent able to allow its own view (it must not: no tool, no MCP method).

#### Step 9. The view host and the bridge (only on GO or RESHAPE)

- `studio/viewserver.go` (non-binding helper) wired as `AssetServer.Middleware` in `studio/main.go`: the `/vibe-view/` routes and headers of D9; serves only allowed bytes; lib files from the embedded FS. The frontend build vendors the pinned libraries into `frontend/dist/vibe-view/lib/`; `task notices` regenerated.
- `components/CustomView.vue` and `composables/useViewBridge.ts`: iframe or worker per the spike; the `MessagePort` handshake; the message schema, caps and rate limit; the watchdog (terminate on a missed heartbeat, show "This view stopped responding" with Reload); theme roles in the handshake.
- **Tests:** Go: headers on every `/vibe-view/` response, no runtime injection (path rules), 404 for a non-allowed or changed view, no directory listing, no path traversal into the workspace. Vitest: the bridge refuses SQL-shaped and oversized messages, unknown types, a 61st call in a minute; results only on the port.
- **Live QA in the Wails window on both platforms** (not only Rodney): the spike's hostile page rerun as an allowed view; a `while(true){}` view is stopped and Studio stays usable; a chart from a bundled library renders with theme colours.
- **Review first:** any response under `/vibe-view/` without the CSP; `postMessage` with `'*'` carrying data; a view reaching `RunArtifactQuery` for another artifact's queries; `ts-window-go-outside-binding-layer` still clean; the Linux IPC path of D10 (d) re-tested on the shipped build.

### Step 10. End to end and docs sweep

- Go integration (`internal/hub`): `hub.Open` → `write_artifact` over the handler → `RunArtifactQuery` with each parameter type → a hostile value (text that would be SQL, a date that is not one, an enum outside its options) → a file edited on disk to a write statement and to `read_text('/etc/passwd')` → refused at run in every tier, MCP included. Bundle export → import into a fresh workspace → the artifact runs.
- Needle audit as #118 step 7: a connection password and an `.env` value never appear in an artifact error, `get_artifact`, `list_artifacts`, the bridge, a log line or a bundle.
- Docs: `direction.md` §3.7 and §9 ("data artifacts" as delivered, and the level-2 outcome), `docs/reference/artifacts.md` complete, `workspaces.md` (layout, bundle), `mcp.md`, `agent-policy.md`, `notebooks.md` (pointer from A2UI to artifact views), `docs/README.md`, this plan's status and an "as built" note. `CLAUDE.md` *Key concepts*: a "Data artifacts" paragraph.
- **Rodney:** a captioned walkthrough (memory: rodney screencast recipe) of authoring by agent, parameters, pivot, drill, and (if built) an allowed custom view.

## Security: what the adversarial reviewer should hit (all steps)

- **SQL text from a value.** Every path from a control, a drill click, the bridge or MCP to `BindParams` must carry values only. Search for string building around `sql` in `internal/artifacts`, `handler_artifacts.go` and the bindings.
- **Vet/run drift.** The query is vetted on a pinned connection with defaults and run with other values; anything value-dependent in the plan (a reader, `query()`, a macro taking a parameter) must be refused at save and still refused at run.
- **Files from outside.** A manifest edited by git or by hand to a write, a secret function, a file read or a `$name` inside a literal must be refused at run (the fixed read-only denial; the reason in the debug log and in the Source tab's vet), never run because "it was vetted".
- **Values and the statement.** A bound value cannot change the statement's class (prepared parameters; DuckDB refuses `ORDER BY ?`, and a value of the wrong shape is a conversion error, D3). The places a value could still choose *what is read* (a reader, `query()`, `query_table()`) are refused by the policy whatever the value, at save and at run.
- **A2UI context confusion.** A view-only component smuggled into a notebook cell, a chat reply or `create_dashboard`; a `sql` field smuggled into a view.
- **Level 2.** Everything in D10; the served bytes equal the allowed bytes; no runtime injection; no data on `'*'`; the watchdog cannot be starved; a custom view cannot run another artifact's queries or more than 500 rows per call.
- **Trust.** The agent cannot allow a view; a bundle or a clone cannot carry a decision; a path rename re-asks.

## Later

- An `artifact` notebook cell that embeds an artifact by name with fixed parameter values.
- Parameters in the route query (`#/artifacts/sales?region=West`) and drill-through from one artifact to another (needs the prefill rules of `storage-setup-and-sso.md` step 3: visible, validated, never auto-run on a crafted link from a notebook).
- Multi-select parameters (list binding), relative date ranges as one parameter.
- A per-artifact row cap above 500 for custom views, with a hard ceiling.
- Saved queries as artifact queries by reference (`from: queries/<id>`), now that both share `Param`.
- An MCP `run_artifact` tool, if MCP clients want an artifact's data without re-writing its SQL. It must run on the **agent's** handler and tier with the tool-result budget, not Studio's `ModeFull` read-only runner, so an artifact never becomes a way to read what the agent's own `run_query` could not.
- HTML/PDF export of an artifact at its current parameters (with #1, notebook export).

## Open questions and assumptions

1. **DuckDB parameter typing. Answered 2026-10-16** by a throwaway program on DuckDB 1.5.6 (results in D3): an ISO string takes `DATE` in a comparison but not in a function or arithmetic (`$from::DATE` there), `LIMIT $n` takes a bound integer, and `time.Time` is not a safe fallback (it binds as `TIMESTAMPTZ`). Step 1 pins these as regression tests.
2. **Level 2 depends on step 7.** Linux result (2026-10-17): the iframe fails (a), (b) and, decisively, (d); the worker passed. Expect (b) to fail for the iframe. The reshape to a worker is the likely outcome; it narrows "custom HTML" to "custom drawing". The owner should confirm that the reshape is still worth building before step 8.
3. **A Mac for the spike.** Step 7 needs Zac's Mac (arm64 at least). Without it, level 2 is not built (D10). Still open: the owner runs the macOS half by hand (the spike patch and run notes are attached to #158).
4. **A CSP on Studio's own page** (candidate (a1)) is new for the whole app. If it breaks Vite HMR in dev, it may have to be production-only, which would make dev and release differ in a security property. Linux: a `frame-src 'self'` meta CSP blocked every navigation vector and did not break the dev server or HMR.
5. **Library list.** Which charting libraries, at which versions, and their licences. Proposal: d3 modules and vega + vega-lite (BSD-3), both DOM-free for the worker shape. Owner to confirm.
6. **Agent-authored views need an allow.** D9 treats them as untrusted. This adds a click after every agent edit. Accepted for now; revisit only if it proves painful, never by trusting model output.

## Spike: custom view isolation (run 2026-10-17)

Step 7 run on **Linux only** (WebKitGTK 4.1, reported WebKitGTK **2.54.1**;
in-webview UA `AppleWebKit/605.1.15 … Version/60.5 … wails.io`). macOS (WKWebView,
arm64) was **Not run** — no Mac to hand — which per D10 counts as a **fail**, so
level 2 cannot be GO or RESHAPE on this run alone. Throwaway branch
`spike/custom-view-isolation` (never merged/pushed); Wails itself was not
modified. Method: an `AssetServer.Middleware` served `/vibe-view/spike/…` with
the D9 CSP (+nosniff, +no-store, 400 on injection-match paths); the host logic
was injected into Studio's own page; the untrusted parts ran in a
`sandbox="allow-scripts"` iframe and a dedicated Worker. Ground truth is a
loopback listener on `127.0.0.1:39247` (any request logged = escape) and a
`SpikeProbe` Go binding that only logs (a line = IPC reached Go). "Blocked" below
means **no request arrived at the listener / no line in the Go log**, never a
console error. Logs saved to the scratchpad (`listener-run1.log`,
`goprobe-run1.log`, and `-run2-a1` for the candidate-a1 run).

### D10 rows

| Row | Linux (WebKitGTK 2.54.1) | macOS (WKWebView) |
|---|---|---|
| **(a) navigation exfiltration** | **Not verified (FAIL).** Of 9 vectors, `location.href`, `location.replace`, `<a>.click()` and `<meta refresh>` navigated the sandboxed frame to the loopback listener with query data (4 `/nav` HITs). `window.open`, `top.location`, GET form submit, `data:` and `blob:` navigation did not reach the listener (sandbox has no allow-popups / allow-top-navigation). The D9 `sandbox`+`connect-src 'none'` CSP does not govern a frame navigating itself. **Candidate (a1) verified:** adding `<meta http-equiv="Content-Security-Policy" content="frame-src 'self';">` to Studio's `index.html` blocked **all 9** vectors (0 `/nav` HITs) and did not break Studio dev (app booted, HMR connected, only frame-src constrained). | Not run = fail. a1 is the only candidate that can pass on mac (no app-side nav hook); must be confirmed there. |
| **(b) freeze** | **iframe Not verified (FAIL), worker Verified (PASS).** Worker `while(true)` was `terminate()`d after a missed heartbeat in **913 ms** and the host heartbeat never stopped → Studio stayed responsive. The iframe `while(true)` stalled the shared main thread: the host heartbeat stopped dead at `hb:85` and never resumed (unrecoverable; an iframe cannot be terminated). | Not run = fail. |
| **(c) CSP sandbox header** | **Frame: mostly Verified.** `origin === 'null'`; `parent.document` and `top.document` both throw; `fetch`, XHR, WebSocket, the `ws://localhost:34115/wails/ipc` dev socket, `EventSource`, `<img src=http…>` and (implicitly) `new Worker` all produced **no listener HIT** → `connect-src 'none'` held. **Deviation:** `document.cookie` did **not throw** — it returned `""` (empty, no access) rather than throwing as the row's wording expects. **Worker: Verified for the property that matters** — `connect-src 'none'` held (fetch rejected, `importScripts` throws, WebSocket/XHR/nested-worker no HIT); `self.webkit` undefined. **Deviation:** the worker's `self.origin` was the real `wails://wails.localhost:34115`, **not** opaque — the CSP `sandbox` directive does not give a dedicated worker an opaque origin in WebKitGTK (immaterial: a worker has no DOM/cookies/parent, cannot navigate). | Not run = fail. DROP if the worker cannot be held to `connect-src 'none'` on mac. |
| **(d) Wails IPC from a sub-frame** | **Not verified (FAIL) — the decisive result.** In the sandboxed opaque-origin frame `self.webkit.messageHandlers.external` is **defined**. A raw binding message `C{"name":"main.App.SpikeProbe",…}` posted from the frame **reached Go** (`SpikeProbe REACHED tag=frame-raw-ipc` in the Go log), and `BOL<url>` (runtime `BrowserOpenURL`) **launched the system browser** against the loopback listener (`/browseropen` HITs; a stray Firefox tab was left open as proof). Cause confirmed by source-reading: Wails' Linux handler (`internal/frontend/desktop/linux/window.c` `sendMessageToBackend` → `frontend.go` `startBindingsMessageProcessor`) validates `webkit_web_view_get_uri(webview)` — the **main** web view's URI — not the originating frame's, so the opaque-origin child passes the origin check. **Candidate a1 does not fix this** (it governs navigation, not the IPC channel): with the meta CSP in place the frame IPC still reached Go and still opened the browser. In the Worker, `self.webkit` is undefined (the worker cannot reach the IPC). | Not run = fail. Measure whether a sub-frame message is accepted by `userContentController:didReceiveScriptMessage` on the WKWebView. |

### Extra checks (Step 7)

- **Hostile SVG as `<img>` — Verified (inert).** `<img src="…hostile.svg">` with an embedded `<script>`, a `<foreignObject>` HTML subtree, an `<image href="http://127.0.0.1:…">` and a CSS `url(http://127.0.0.1:…)` rendered as an image and produced **zero** listener HITs (`/svg-script`, `/svg-image-href`, `/svg-foreignobject`, `/svg-css-url` all absent). Supports the RESHAPE path of rendering a worker-produced SVG as `<img src="blob:…">`.
- **meta `frame-src` stops child-frame self-navigation — Verified (Linux).** See row (a): all 9 vectors blocked.
- **Does the meta CSP break Studio — Verified not broken (Linux dev).** With only `frame-src` set (all other directives unconstrained), Studio booted, the bootstrap injected, HMR/Vite was live and the heartbeat ran normally. CodeMirror/A2UI-motion were not exercised cell-by-cell but nothing script/style/connect-related was constrained, so no breakage is expected; confirm on the shipped (non-dev) build and on mac, where WKWebView enforces meta CSP identically.
- **(a2) native `decide-policy` handler (Linux cgo)** — **Not attempted.** It was gated on (a1) failing; a1 passed, and (d) is an independent hard fail that no navigation hook addresses, so the iframe path is dead regardless.

### Provisional verdict (Linux only; macOS still required)

**DROP the iframe design; the worker RESHAPE is the only survivable shape, pending macOS.**

- The **iframe** shape fails three independent ways on Linux: (a) frame
  self-navigation exfiltrates (fixable only by the app-wide meta CSP a1, which
  mac cannot replace with anything stronger), (b) a `while(true)` freezes and
  cannot be recovered, and — decisively — (d) the sandboxed frame reaches the
  **full Wails binding IPC**: it can call any bound Go method and
  `BrowserOpenURL` to exfiltrate, because Wails checks the main view's URI, not
  the frame's. a1 does not touch (d). An iframe view is therefore not acceptable
  at any CSP.
- The **worker** shape passed every property measured on Linux: terminable
  (b), `connect-src 'none'` held (c), and no IPC channel exists (`self.webkit`
  undefined, d). Combined with the proven-inert SVG-as-`<img>` render path, the
  D10 "RESHAPE to a worker view" ("custom drawing, not custom UI": `view.js` in a
  terminable Worker returning an SVG document or a Go-validated A2UI page) is the
  recommended direction.
- This verdict is **provisional**: D10 requires both platforms, and macOS was
  not run (= fail). Before step 8, run `scratchpad/spike/MAC-RUN.md` on Zac's Mac
  and confirm, for the **worker** path, that `connect-src 'none'` holds and
  `self.webkit` is undefined under WKWebView (and that a1 blocks every nav vector
  there). Only then is RESHAPE a GO.

### Wails sub-frame IPC (filed as #174)

The Linux Wails binding-IPC path authorises a message by the **top web view's**
URI, so **any** sub-frame — including a `sandbox="allow-scripts"`, opaque-origin,
`connect-src 'none'` frame — can invoke every bound Go method and the runtime
`BrowserOpenURL`. This is broader than the known #173 (dev-socket `CheckOrigin`
always true): it affects the production custom-scheme IPC, not just the dev
websocket. It does not expose Studio today (Studio embeds no untrusted frames),
but it is the reason level-2 custom views cannot be iframes, and it is worth an
upstream report with the `main_frame`/`is_main_frame` check the handler lacks.

## As built (level 1)

Level 1 shipped in PRs #164 (step 1), #165 (2), #167 (3), #169 (4), #170 (5), #171 (6a), #172 (6b) and the step 10 sweep. Where the code differs from the plan above, or the plan was wrong:

- **Agent writes never replace a version they did not read.** `WithRev("")` is create-only in the artifacts store; to replace, `write_artifact` takes the `rev` from `get_artifact`. The store's last-seen revision is never a fallback. The same hole in `create_or_update_skill` is #166.
- **`keep_sql`.** `get_artifact` never returns part of a query over 4000 runes (`sql: null`, `sql_omitted`); `write_artifact` with `keep_sql: true` and a `rev` keeps the stored SQL byte for byte, and it is vetted again.
- **One vet path.** Save (`VetArtifact`) and run (`RunArtifactQuery`) share `checkArtifactStatement`, with lexer checks on the policy's own lexer and `readOnlyVerdict` extracted unchanged from `RunReadOnlySQLIn`. The save vet runs with every parameter at its default; the run vet classifies with the actual values.
- **A policy refusal does not refuse a person's save.** In Studio's Source tab a query the read policy refuses saves, with the policy's reason (paths scrubbed) as a warning; the run still refuses with the fixed text. Structural errors do refuse.
- **The save vet connects on first use.** A query on a connection that cannot connect (no sign-in, a changed server, offline) is refused at save, and at run, as "not attached". The vet connects a pending connection like any query, so `write_artifact` fails while a source is offline; Studio's Save turns this into a warning. The reason (for example "the server changed since you signed in") is visible only in the connection's status, not in the refusal.
- **The needle audit's limit.** The audit covers credentialed connections that fail before attaching: plain `go test` has no postgres or httpfs extension and no network, so no credentialed connection is attached and the paths where a password would travel (the secret, an ATTACH string, a driver error after a dial) are not exercised. An `integration`-tagged variant on `miniotest` with an S3 secret-key needle would cover them.
- **The renderer draws what Go validated.** `artifacts.RenderView` re-marshals the validated view; the raw file bytes are never rendered (Go and JS disagree on lone surrogates).
- **Run path details.** Studio's `RunArtifactQuery` binding has a 30 s timeout (`a2uiQueryTimeout`); the handler caps results at 500 rows; Studio runs artifact queries in the low lane with one in flight and one waiting per query. Errors from a run are not scrubbed of store paths, which is acceptable in Studio's own UI; a level-2 bridge would have to use fixed text.
- **Drill and pivot.** `drill` is valid on `table`, `barChart`, `columnChart` and `pivot` as planned, and refused without `query`; a refused drilled value shows a note naming it and runs nothing. A pivot never aggregates; duplicate cells are an error.
- **Two-file writes.** The view is written first, the manifest second, with the view rolled back if the manifest write conflicts. A crash between the two leaves a pair that fails validation and lists with its error.
- **Bundles.** Exactly the two entries per artifact; `view.html` and any other name refuse the bundle. Import is structural only (the bundle package cannot import the agent policy); a hostile query imports and is refused when it runs.
- **MCP.** There is no `run_artifact` tool (the *Later* item), so "refused at run, MCP included" in step 10 is tested as: over MCP in each tier a write statement or file read is refused at `write_artifact`, and no tool lists or runs results. The run itself is tested on `Handler.RunArtifactQuery` in each tier.
- **docsref** reverse check for A2UI components could never fail before step 2; fixed and scoped.
- **Step 10 UI polish** (carried from review): bar chart row label padding, column chart focus ring in dark mode, and the "RUNNING QUERY" note on re-run in the column chart.
- **Not done:** the optional captioned Rodney walkthrough video (cost); level 2 (steps 8-9), pending the macOS spike run (#158) and Wails sub-frame IPC (#174).
