# CLAUDE.md

Guidance for Claude Code (claude.ai/code) when working in this repository.

## Overview

Vibe is an **agentic desktop workspace for working with data where it already lives** — DataGrip/DBeaver-shaped, with an AI agent, live notebooks, skills and A2UI artifacts, all exposed to external agents over MCP.

- **Connections** keep the data where it is (local DuckDB, MotherDuck, Postgres, MySQL, SQLite, S3/GCS/R2, Azure, DuckLake, Iceberg/Delta). They are attached on first use into one in-memory **DuckDB hub** per workspace.
- A **workspace** is a folder (optionally a git repo): `vibe.yaml`, `notebooks/*.vnb`, `queries/*.sql`, `skills/<name>/SKILL.md`, `artifacts/<name>/` (data artifacts), `data/`. Machine-local state lives in `.vibe-local/` (git-ignored).
- **Credentials** live in the OS keychain or env vars, never in the folder.
- **`.vibe`** is an exchange **bundle** (zip, optional age passphrase) of notebooks, queries, skills and connection definitions. It never holds data or credentials. Older data-holding SQLCipher `.vibe` files are not opened (vibe-app#110 was dropped).

**Read first:** `docs/direction.md` (north star; §9 "As delivered" lists what changed from the plan in vibe-app#105; the earlier Access-parity and page-system removals are recorded in `docs/archive/` and git history) and `docs/README.md` (index of every doc). Shipped surface reference: `docs/reference/`. Next direction: `docs/plans/answers-you-can-check.md` (umbrella #250, epics #241-#249; receipts, verifier, semantic layer). Closed epics: vibe-app#105, #162; cleanup survey: vibe-app#137 (`docs/reviews/cleanup-2026-10/`).

## Tech stack

- **Go 1.27+**, `CGO_ENABLED=1` (DuckDB bindings). Cobra + Viper for the CLI and config.
- **Engine:** `duckdb-go` (`github.com/duckdb/duckdb-go/v2`, embedded DuckDB 1.5) is the single query/CRUD engine. There is no SQLite or SQLCipher dependency (removed in vibe-app#137): tests use DuckDB, FileStores in `t.TempDir()`, or the store `Mock`s.
- **Desktop GUI:** Wails v2 + **Vue 3** (`studio/`). UI uses SchoolHouse (`studio/frontend/src/schoolhouse/`), themed through `--vibe-*` semantic roles (`docs/reference/themes.md`). Editor is CodeMirror. A2UI charts are hand-drawn CSS/SVG.
- **Agent:** `internal/llm` on `charmbracelet/fantasy` (providers: `docs/reference/llm-config.md`); tools, sandbox and approvals in `internal/agents`; MCP in `internal/mcp` and `internal/hub` (`docs/reference/mcp.md`).
- **TUI:** BubbleTea + Lipgloss (`vibe studio` without `--gui`): SQL REPL, read-only table browser, notebook list.
- **Logging:** `log/slog`. **Tests:** `testing` + testify.

## Build commands

Automation is **Taskfile.yml**, not a Makefile.

```bash
task              # default: lint + test + build
task build        # build the vibe binary (./cmd/vibe; also `go install github.com/SandwichLabs/vibe/cmd/vibe@latest`)
task test         # Go + Vitest tests (alias: task t); task test:frontend is Vitest only
task lint         # golangci-lint (task lint:fix to auto-fix)
task typecheck    # vue-tsc over studio/frontend (`npm run typecheck`); part of `task lint`, blocking in CI, 0 errors
task studio:dev   # Wails dev server (NOT bare `wails dev`: needs -tags webkit2_41,duckdb_arrow)
task build:studio # Studio desktop app (build:studio:linux on Linux); stamps the version, then restores wails.json
scripts/gen-icons.sh  # regenerate appicon.png + Linux icons from studio/build/icon/vibe.svg
go test -v ./internal/workspace/   # one package
ast-grep scan -c tools/ast-grep/sgconfig.yml cmd internal studio  # architecture rules (CI-blocking at error severity), incl. ts-window-go-outside-binding-layer
```

**Release**: `task release` prints the procedure; a release is just pushing a `v*` tag (`-rc`/`-test` tags are prereleases; every release is a draft until a human publishes it). See `docs/packaging.md`, "v0.1.0 unsigned pipeline, as built".

**CI** (`.github/workflows/ci.yml`): every PR runs the Linux `test` job (vet, `xvfb-run go test`, golangci-lint, vitest, frontend build, shellcheck) and a Linux amd64/arm64 build smoke on ubuntu-22.04 (CLI round trip + `wails build`). macOS runs only on push to main and `workflow_dispatch` (10x minutes). vue-tsc (`npm run typecheck`) and govulncheck (#41) are blocking, and build-smoke also checks `AppApi` arities against the regenerated bindings (`check-arity.mjs`). A govulncheck finding means bumping the module or the Go patch release (go.mod's `go` line is the toolchain CI and release use, now 1.27.3). Actions are pinned to commit SHAs; the golangci-lint version is pinned in the workflow, so bump it with the local one.

Keep the Wails CLI at the `github.com/wailsapp/wails/v2` version in go.mod (v2.16.0; CI and release pin it as `WAILS_VERSION`). Wails v2.16 loads Go 1.27, so the old `WAILS_GOTOOLCHAIN` pin (vibe-app#92) is gone.

CLI: `vibe workspace init`, `query` (`-e` edits in $EDITOR), `lsp` (`lsp snapshot`), `import`, `export`, `conn`, `artifact` (`artifact list`, mirrors the Library's artifact kinds), `config` (`config llm` mirrors Settings → App's provider picker; keys stay on `config llm-key`; `config updates` mirrors the About > Updates toggle), `update` (check, then install after confirmation), `studio` (`--gui` for desktop), `mcp serve`. `init` aliases `workspace init`. Old container commands (`info`, `verify`, `compact`, `rekey`, `dump`, `passphrase`) are hidden stubs that point to `vibe workspace init`.

## Architecture

```
cmd/vibe/, cmd/   main (cmd/vibe) and the Cobra commands (workspace, query, import, export, conn, artifact, config, bundle, lsp, mcp, studio, repl (hidden until the Studio console ships, #259))
studio/              Wails app (one bound App): app.go (core) + app_{about,agent,artifacts,artifact_views,complete,import_export,lifecycle,llm,repl,
                     notebooks,prefs,queries,skills,tables,update}.go (bindings by area), artifacts{,_edit}.go + artifact_starters.go (the New dialog's starter files by kind; the logic behind the data artifact bindings: list, open, run, edit, save, delete), viewserver{,_page}.go + artifact_view_host.go + artifact_page_host.go (the /vibe-view/ middleware and per-grant tokens for script views and static HTML pages), pagehost{,_bridge,_files,_launch_unix,_gate_linux,_gate_darwin}.go + pagecaps{,_state,_download,_ask,_app}.go (the `state`, `download` and `ask` capabilities) + pagehelper{,_unix}.go + pagewindow_{cgo.go,linux.c,darwin.m} + app_pagehost.go (page windows of scripted HTML pages: the manager and bridge in Studio, helper mode `--view-host`, the Linux bubblewrap network gate, the native WebKitGTK/WKWebView view), agent_turn.go (the in-flight chat turn and its cancel), repl_host.go + repl_pump.go (the Data Console's sessions, frame pump and backpressure behind the `Repl*` bindings),
                     workspace_{launch,recents,open,info,create,settings,conflict,mcp}.go, signin.go (AWS device sign-in flows: StartSignIn/CancelSignIn; ssodev.go and the `ssofake`-tagged ssofake.go are the dev-build seam),
                     connections_{crud,sync,catalog,creds,deps,probe,extensions}.go, secrets.go, bundle.go,
                     dotenv.go, templates.go; every exported App method is a thin binding (<= 25
                     statements counting nested ones; no file/process/SQL work, no filestore write, no
                     dot import or I/O alias: `TestExportedAppMethodsAreThinBindings`
                     and the `thin-binding` ast-grep rule); frontend/ is Vue 3 (SQL completion is `lib/sqlComplete.ts`, the one CodeMirror source, used by `components/SqlEditor.vue` (the Query Explorer's editor) and notebook SQL cells; all Go calls go through src/lib/bindings.ts: typed `App` over AppApi in env.d.ts; generated wailsjs/go is gitignored; views: Launcher, Dashboard, Library (the one list of notebooks, live views, pages and chart scripts, with the New type picker)/Notebook/Artifact,
                     TableBrowser, QueryEditor (Grid and Console tabs; `ReplTerminal.vue` is xterm.js), ConnectionEditor, Skills, Import, Settings, Agent)
internal/
  workspace/         the folder model: vibe.yaml, scopes (user/shared/local), discovery, init, watcher;
                     ConnectionStates (one typed status for CLI + Studio), TestCredential (the Test guard);
                     ApplySettings (the "only tighten" rules), ResolveConflict (Keep mine / Reload / Save as
                     copy over the notebook, query and skill stores), CreateNew, GitInfo (reads .git/HEAD, runs no git)
  connections/       connection type registry and attach SQL
  connname/          the connection-name rule (catalog alias), a dependency-free leaf
  session/           the DuckDB hub for a workspace; connection lifecycle and status
  sandbox/           the three sandbox tiers: Mode, Parse, Rank/Tighter (leaf; shared by workspace policy and agents)
  secrets/           keychain + env credential store, plus pluggable sign-in methods (`MethodProvider`: aws-sso) (write-only across every boundary)
  awssso/            AWS IAM Identity Center sign-in in Go: SSO profile loading, the shared token cache, role credentials, device-flow login (leaf: AWS SDK + stdlib only; `workspace.NewResolver` wires it as the `aws-sso:PROFILE` sign-in; Studio's banner and `vibe conn credentials sign-in` run the device flow, `aws sso login` shares its token cache)
  dotenv/            workspace .env parser and per-person allow/deny (hash-keyed allow, path-keyed deny)
  trustfile/         the allow-by-path-and-hash, deny-by-path decision store (leaf; JSON in the user config dir, never the workspace or a bundle); used by dotenv (dotenv.json) and by custom artifact views (views.json)
  agents/            ToolDef registry, sandbox tiers, approval queue, SQL allow-list policy, JSON-RPC handler (handler*.go by tool family)
  skills/            SKILL.md store (FileStore, Mock, embedded built-ins) + the skills service (LoadSkills, SaveUserSkillAt, DeleteSkill, WriteImportSkill)
  llm/               in-app agent runner (fantasy), task mode, grounding
  editor/            $VISUAL/$EDITOR hand-off for the TUI REPL (ctrl+o, \e) and `vibe query --edit`; scratch buffers in .vibe-local/edit/
  lsp/               `vibe lsp`: stdio Language Server (completion, hover) over a catalog snapshot (.vibe-local/catalog.json) the TUI, Studio or `vibe lsp snapshot` writes (the server itself opens no database and writes no file); `Live` is the in-process catalog Studio completes from (rebuilt on `App.catalogChanged`, saved-snapshot fallback marked stale, capped, never connects a connection or runs SQL) and it writes the snapshot too
  repl/              the data console engine (#255, #258): a `Session` over an `io.Writer` and byte input (Studio's console tab in R2, hidden `vibe repl` in a terminal), one pinned hub connection (`session.Conn`), the `x/term` line editor, `lsp.Split`, the editor's gates (`usersql.CheckInteractive`), `engine.ExecuteOn` (the one run path, plus an `AfterRun` hook for the surface), duckbox/box/line/csv/json/markdown renderers with every value through `engine.TerminalText`, the closed dot-command set, filtered machine-local history in `.vibe-local/repl/history`; no Wails, no shell, no PTY, no subprocess; the agent never reads it
  mcp/, hub/         MCP server (SSE, stdio) and which process owns a workspace's hub (Studio vs `vibe mcp serve`); `hub.Open` is the one open path (workspace, hub lock, DuckDB session, engine)
  notebooks/, queries/  file-backed stores (notebooks.Store, queries.Store), same shape as skills.Store
  artifacts/         data artifacts (#162): the manifest model, strict decode, validation and `Bind` (names and values to SQL + bound params), plus the artifacts.Store (FileStore, Mock) for `artifacts/<name>/{artifact.yaml,view.a2ui.json | view.js}` under one two-file revision, `ViewHash` (the trust hash of a custom view), and the `view: html` page file set (`html/**`, the save-time template vet `VetPage`, `PageHash`, and `RenderHTMLPage`, the bounded renderer of static pages, `docs/plans/html-artifacts.md`) with the closed template function map in the leaf `artifacts/pagefuncs`; structure only, SQL vetting lives in `internal/agents`
  pagehost/          the page window's wire protocol (leaf): JSON-lines messages between Studio and the `--view-host` helper, shape-checked and size-capped, no field naming an artifact; the injected `vibe` API script (with the `state`, `download` and `ask` objects); the limiters
  pagestate/         the `state` capability's store (leaf): `.vibe-local/artifacts/<name>/state.json`, keyed by the name Studio gives it, capped, debounced, compare-and-write through filestore
  a2ui/              component schema + validator (Go validates, Vue renders); `ValidateView` + `ViewComponents` = the context-aware data artifact view schema (controls, pivot, `query`/`drill`; refused by `Validate`)
  bundle/            .vibe bundle v2 export/import, optional age encryption; ContentsOf (what an export offers), ReadNotebookFile
  crud/, importer/, engine/, usersql/  datasheet CRUD + table paging + the one QBE filter/WHERE builder, CSV/xlsx import, query exec (the one `QuoteIdent` is `session.QuoteIdent`), the connection tree (`engine.ListCatalog`), courtesy SQL checks
  filestore/, filelock/, storageurl/, storageglob/, jsonenc/, prefs/, config/, templates/, tui/, testsupport/
                     (storageglob = s3/gcs/r2 wildcard expansion: DuckDB's parser finds the pattern literals, the hub's private lister lists them, the SQL is rewritten to explicit key lists before it runs, #138; testsupport = test fixtures: miniotest (a throwaway MinIO, integration tag only) and ssofake (a loopback AWS IAM Identity Center for tests, `tools/ssofake` and the `ssofake` dev build); jsonenc = the one JSON encoder setup: HTML escaping off, plus Quote; prefs.Recents = the launcher's recent list)
  selfupdate/        release check + install.sh-driven apply (opt-in, confirmed; manifest-based install detection; Service wires prefs); `vibe update`, Studio's About > Updates
  buildinfo/         version/commit/date + Go and DuckDB versions for Settings > About (`GetBuildInfo`)
  docsref/           test keeping docs/reference/ in sync with the a2ui registry, templates and vibe.yaml keys
editors/            Neovim plugin, Helix and eglot setup for `vibe lsp` (`docs/reference/editor.md`)
legal.go            repo-root package embedding LICENSE, NOTICE and the generated THIRD_PARTY_NOTICES.md (`task notices`)
docs/                see docs/README.md
```

## Key concepts

**Workspace and files.** Files are the source of truth; writes are compare-and-write and a watcher picks up outside changes (git, editors, other processes). Notebook outputs are never persisted. A committed `vibe.yaml` can only *tighten* the agent's limits; looser per-person settings live outside the folder. A workspace `.env` is loaded only after the person allows it (direnv-style: allow is keyed by path + content hash, deny by path; decisions in the user config dir), into a per-workspace overlay read by `env:` sign-ins, never `os.Setenv`.

**Connections.** Read-only by default, connect on first use, with a visible status. The connection name is the catalog alias in SQL (`prod_pg.public.orders`). Metadata-driven formats (DuckLake, Iceberg, Delta) may only be defined in your own scopes, never in a committed `vibe.yaml`. MotherDuck cannot be attached read-only.

**Notebooks (vibe-app#66).** JSON documents of `sql` / `markdown` / `a2ui` cells (schemaVersion 2; a cell may name a `connection`). Every data cell re-runs on open. The agent authors cells in **task mode**: `preview_query` before a terminal `write_cell` or `need_more_info`.

**Skills (vibe-app#62).** `SKILL.md` documents (`name`, `description` + markdown). The prompt advertises name and description; the agent fetches bodies with `read_skill`. Built-ins are embedded. Table-bound skills load only when the table has rows.

**Data artifacts (#162, plan `docs/plans/data-artifacts.md`).** A parameterised, live view stored as files: `artifacts/<name>/artifact.yaml` (typed parameters with defaults and *named* queries) plus `view.a2ui.json`. It stores questions, never answers. `artifacts.Bind` turns (query name, raw values) into the manifest's SQL text unchanged plus bound params; a value never becomes SQL text. The store refuses rather than repairs (strict YAML, byte caps before decode, no anchors/aliases), and one `Rev` covers both files. The SQL policy runs in `internal/agents` at save and again at every run, in every tier, so a hand-edited, git-pulled or bundled artifact is held to it too. No tool runs an artifact for the agent (Studio runs it for the person). Level 2 is the **script view** (`view: script`, `view.js`): it shows as source until the person allows it (path + `ViewHash`, decisions in `views.json` via `internal/trustfile`; the agent can never allow), then runs in a terminable Web Worker served by `studio/viewserver.go` under a per-grant token with a deny-all CSP, gets rows only through `RunArtifactViewQuery`, and returns an SVG (shown as an inert `<img>`) or an A2UI page Go validates. A static **HTML page** (`view: html`, `html/` templates, P3) is allowed the same way (`PageHash`), rendered in Go (`artifacts.RenderHTMLPage`, bounded by a work budget, an output cap and a deadline, queries only through `RunArtifactQuery`) and shown in a `sandbox=""` frame from `/vibe-view/<token>/page` under a per-artifact token and a script-less CSP, with Studio's page narrowed to `frame-src <origin>/vibe-view/` (emitted at build time); scripted pages and capabilities are below (`docs/plans/html-artifacts.md`).

**Custom artifact views (`view: script`, #159).** An artifact may name `view.js` instead of `view.a2ui.json`; the code is someone else's or the agent's, so it shows as source and runs nothing until the person allows it. Allow is keyed by the folder's canonical path plus `artifacts.ViewHash` (version-tagged, length-prefixed over `artifact.yaml` and the view files), kept in `views.json` beside `dotenv.json` (`internal/trustfile`), and refused if the files changed since they were shown. No agent tool or MCP method can allow, deny or revoke. The worker that runs an allowed view is step 9.

**Scripted HTML pages (`view: html` with script, #162 step P4).** An allowed scripted page opens only from Studio, in a native window owned by a helper process (`vibe-studio --view-host`, the same binary; `internal/pagehost` is the pipe protocol). The helper gets no workspace, credential or store: Studio renders the page, sends its hashed files over two pipes, pins the artifact by name, and answers `vibe.run` through the same gates as every artifact query. Linux starts it only inside bubblewrap (no network, an allow-listed filesystem view, a tree kill); macOS uses CSP, a navigation policy, a content rule list and a dead proxy, with one accepted, disclosed leak (#189). If the gate cannot be built there is no window. A page gets only the capabilities its manifest declares (`state` local only, `download` through the native save dialog behind the helper's own record of a physical input, and a dialog throttle, `ask` as one tool-less completion behind its own toggle), each a Studio method behind the pipe, never an agent tool. **The allow prompt (P6)** lists the files, the queries with their connections, each declared capability in plain words (`ask` names the configured provider), the macOS network disclosure and a never-ask-for-a-credential line (carried in `ArtifactViewState`, text in `artifacts/pageprompt.go` shared with `vibe artifact view status`); Settings > Workspace > Pages lists decisions, the per-page `ask` toggle and the recent model use. `write_artifact` takes `html_files` (text only) and `get_artifact` returns them; no tool renders, runs or allows a page, and every agent edit changes the hash. A bundle carries a page as `artifacts/<name>/html/<path>` entries, vetted before any write, and never a decision. Details: `docs/reference/artifacts.md`.

**A2UI artifacts (vibe-app#67).** `{ title, components[] }`, twenty component types (the original thirteen plus the KINETIC charts `areaChart`, `columnChart`, `ringChart`, `radarChart`, `sankeyChart`, `heatmap`, `streamChart`), closed color enum, unknown fields rejected. `stat` / `table` / every chart take inline data or a live `sql` binding; every chart takes a closed `motion` object (easing preset, speed, stagger, spring tuning), animated in `a2ui/useMotion.ts` and off under reduced motion.

**Agent policy.** One tool registry, consumed by the in-app runner and by MCP. Sandbox tiers `read-only`, `read-write`, `full`. A write needs all three: the tier allows it, the connection is writable, and the person approves (always asked for non-local connections; headless MCP refuses them). SQL is an allow-list parsed before `EXPLAIN` binds it (`docs/reference/agent-policy.md`). Guardrails (vibe-app#70): grounding check, empty-result explanation, tool-result budget.

## Conventions

- **`internal/` not `pkg/`**; Cobra + Viper; `log/slog` passed by dependency injection; services created in `cmd/vibe/main.go`/`cmd/` and passed down, no globals.
- **Complexity is linted**: `gocognit` (min 30) runs in `golangci-lint`; only the named waivers in `.golangci.yml` (tokenizers, wire-format decoders) may exceed it. Split a long function into named steps instead of adding a waiver.
- **Wrap errors** with context: `fmt.Errorf("failed to open workspace: %w", err)`.
- **Interfaces for testability**; a `mock.go` only when a test needs it. Co-located tests with testify. Functional options for complex constructors.
- **One tool surface**: new agent capabilities are `ToolDef`s in `internal/agents`, never bespoke `fantasy` tools in `internal/llm`.
- **Model output never reaches a style attribute**: A2UI colors are a closed enum; validators reject unknown fields.
- **No hex in stylesheets**: use `--vibe-*` roles (or bridged `--sh-*`); translucent variants via `color-mix()`. Themes are validated by `theme/schema.ts` and must pass the AA contrast spec.
- **Thin views**: a `.vue` file is at most 600 lines and 150 script lines. A view wires composables (`composables/use*.ts`, one per job: `useNotebookSave`, `useTableCrud`, …) and renders; the conflict banner flow is `useConflict`, popup-menu keys `useMenuKeys`, the A2UI live `sql` binding `a2ui/useLiveQuery`. `studio/frontend/scripts/check-vue-size.mjs` enforces it in CI.
- **Focus is visible**: `theme/bridge.css` owns the `:focus-visible` ring; never `outline: none` without it. Ad-hoc modals use `composables/useModal.ts`.
- **QA in a real browser**: Rodney against the Wails-bound dev server at `:34115` (see `docs/contributing/rodney.md`); Vitest specs are supplementary, never the acceptance criterion. The older Rodney verification protocol is in `docs/archive/parity-remediation-plan.md` §1.
- **Docs move with code**: keep `docs/reference/` and the architecture tree above current; `internal/docsref` fails when they drift.
- **Releases** are `.github/workflows/release.yml`: push a `v*` tag and it builds natively on four runners, stamps the version with ldflags, and creates a draft release. No GoReleaser.

## Design principles

- **Trust rules:** never store a credential in a config file, a `.vibe` bundle, a log line, an error shown to the UI or agent, or anything the agent can read. Credentials are write-only across the Wails bridge and MCP.
- **Data stays where it lives**: Vibe reaches data in its connections; it doesn't copy it into its own format.
- **The workspace is the app**: a folder of plain, diff-friendly files you can version, review and share via git.
- **Read-only until asked**: agent writes need the tier, a writable connection, and approval.
- **Single-user first**: sharing is via shared backends and git; multi-user mesh is deferred.
- **One tool surface**: the built-in agent and MCP clients see the same tools with the same gating.
- **AI as collaborator, not dependency**: everything works without AI.
- **Live, not frozen**: notebooks and artifacts store questions (SQL), not answers.
- **Progressive disclosure**: skills advertise a line and load on demand; connections connect on first use.
