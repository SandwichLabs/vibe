# CLAUDE.md

Guidance for Claude Code (claude.ai/code) when working in this repository.

## Overview

Vibe is an **agentic desktop workspace for working with data where it already lives** — DataGrip/DBeaver-shaped, with an AI agent, live notebooks, skills and A2UI artifacts, all exposed to external agents over MCP.

- **Connections** keep the data where it is (local DuckDB, MotherDuck, Postgres, MySQL, SQLite, S3/GCS/R2, Azure, DuckLake, Iceberg/Delta). They are attached on first use into one in-memory **DuckDB hub** per workspace.
- A **workspace** is a folder (optionally a git repo): `vibe.yaml`, `notebooks/*.vnb`, `queries/*.sql`, `skills/<name>/SKILL.md`, `artifacts/<name>/` (data artifacts), `data/`. Machine-local state lives in `.vibe-local/` (git-ignored).
- **Credentials** live in the OS keychain or env vars, never in the folder.
- **`.vibe`** is an exchange **bundle** (zip, optional age passphrase) of notebooks, queries, skills and connection definitions. It never holds data or credentials. Older data-holding SQLCipher `.vibe` files are not opened (vibe-app#110 was dropped).

**Read first:** `docs/direction.md` (north star; §9 "As delivered" lists what changed from the plan in vibe-app#105; the earlier Access-parity and page-system removals are recorded in `docs/archive/` and git history) and `docs/README.md` (index of every doc). Shipped surface reference: `docs/reference/`. Closed epic: vibe-app#105; cleanup survey: vibe-app#137 (`docs/reviews/cleanup-2026-10/`).

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
task docs         # regenerate this file's package map from the Go package comments
task build:studio # Studio desktop app (build:studio:linux on Linux); stamps the version, then restores wails.json
scripts/gen-icons.sh  # regenerate appicon.png + Linux icons from studio/build/icon/vibe.svg
go test -v ./internal/workspace/   # one package
ast-grep scan -c tools/ast-grep/sgconfig.yml cmd internal studio  # architecture rules (CI-blocking at error severity), incl. ts-window-go-outside-binding-layer
```

**Release**: `task release` prints the procedure; a release is just pushing a `v*` tag (`-rc`/`-test` tags are prereleases; every release is a draft until a human publishes it). See `docs/packaging.md`, "v0.1.0 unsigned pipeline, as built".

**CI** (`.github/workflows/ci.yml`): every PR runs the Linux `test` job (vet, `xvfb-run go test`, golangci-lint, vitest, frontend build, shellcheck) and a Linux amd64/arm64 build smoke on ubuntu-22.04 (CLI round trip + `wails build`). macOS runs only on push to main and `workflow_dispatch` (10x minutes). vue-tsc (`npm run typecheck`) and govulncheck (#41) are blocking, and build-smoke also checks `AppApi` arities against the regenerated bindings (`check-arity.mjs`). A govulncheck finding means bumping the module or the Go patch release (go.mod's `go` line is the toolchain CI and release use, now 1.27.2). Actions are pinned to commit SHAs; the golangci-lint version is pinned in the workflow, so bump it with the local one.

Keep the Wails CLI at the `github.com/wailsapp/wails/v2` version in go.mod (v2.16.0; CI and release pin it as `WAILS_VERSION`). Wails v2.16 loads Go 1.27, so the old `WAILS_GOTOOLCHAIN` pin (vibe-app#92) is gone.

CLI: `vibe workspace init`, `query` (`-e` edits in $EDITOR), `lsp` (`lsp snapshot`), `import`, `export`, `conn`, `artifact` (`artifact list`, mirrors the Artifacts list), `config` (`config llm` mirrors Settings → App's provider picker; keys stay on `config llm-key`; `config updates` mirrors the About > Updates toggle), `update` (check, then install after confirmation), `studio` (`--gui` for desktop), `mcp serve`. `init` aliases `workspace init`. Old container commands (`info`, `verify`, `compact`, `rekey`, `dump`, `passphrase`) are hidden stubs that point to `vibe workspace init`.

## Architecture

One line per Go package, **generated from its package comment** (`task docs`); `internal/docsref` fails when the list is stale or a package has no comment. To change a line, edit the package comment (in `doc.go` or the file named for the package) and run `task docs`. `go doc ./internal/<pkg>` prints the full comment: the entry points, invariants and who calls the package.

<!-- package-map:start -->
- `legal.go` (module root) — embeds the licence files (LICENSE, NOTICE and the generated THIRD_PARTY_NOTICES.md, `task notices`) so the binaries can show them in Studio's Settings > About without reading from disk.
- `cmd/` — holds the Cobra command tree for the vibe CLI (workspace, query, import, export, conn, artifact, config, lsp, mcp, studio, update).
- `cmd/vibe/` — the Vibe command-line entry point; it wires the cmd tree.
- `internal/a2ui/` — implements the A2UI page schema: the closed, validated contract an agent uses to hand a chart/table/stat page back to the Studio UI.
- `internal/agents/` — the one tool surface for the built-in agent and MCP clients: the ToolDef registry, sandbox tiers, the approval queue, the SQL allow-list policy and the JSON-RPC handler that dispatches tool calls.
- `internal/artifacts/` — the file-backed store of data artifacts: a parameterised, live view stored as plain files under artifacts/<name>/.
- `internal/awssso/` — signs a person in to AWS with an IAM Identity Center (SSO) profile from their ~/.aws/config, in Go, and hands back short-lived role credentials.
- `internal/buildinfo/` — describes the running build for Settings > About and bug reports: the ldflags-stamped version, commit and date, plus the Go and DuckDB versions it was built with.
- `internal/bundle/` — reads and writes `.vibe` bundles, format version 2 (#118, plan section 4.6): a zip of a workspace's notebooks, queries, skills and (optionally) connection definitions, with a manifest that carries a SHA-256 for every entry.
- `internal/config/` — loads Vibe's app-level settings (Viper) and the LLM provider settings, including provider defaults and the keychain-backed API key lookup.
- `internal/connections/` — the connection type registry (plan §4.2, #115): what each kind of connection needs, how its secret and attach SQL are emitted, and how it is tested.
- `internal/connname/` — holds the connection-name rule (the name is the catalog alias in SQL).
- `internal/crud/` — provides the transactional, type-coercing write-back core behind Studio's (Wails) datasheet bindings, with table paging (page.go) and the one QBE filter-to-WHERE builder (filter.go).
- `internal/docsref/` — the test-only check that keeps the docs honest against the code; there is no production behavior here.
- `internal/dotenv/` — reads a workspace's .env file and remembers, per person, whether it may be used (direnv-style allow and deny).
- `internal/editor/` — opens SQL in the person's own editor ($VISUAL, then $EDITOR) for the TUI REPL and `vibe query --edit`, the way git and psql's \e do.
- `internal/engine/` — runs SQL against a DuckDB connection and normalises the result: column metadata, typed values, parameters, row caps and table reads.
- `internal/filelock/` — the crash-safe, non-blocking, exclusive advisory file lock behind a workspace's hub lock (#112, internal/hub).
- `internal/filestore/` — the shared plumbing under the file-backed notebook, query and skill stores (#109, plan §4.1): safe paths that never leave the workspace, atomic writes (temp file + fsync + rename in the same directory), and compare-and-write.
- `internal/hub/` — decides which process owns a workspace's DuckDB hub, and lets the others find and reach it (#112, plan §4.4).
- `internal/importer/` — provides backend helpers for reading external data files (CSV, Excel) into a workspace connection's DuckDB session: the import wizard's inspect/preview and its create/replace/append/upsert write path (#82).
- `internal/jsonenc/` — the one JSON encoder configuration for the files and strings Vibe writes: HTML escaping off (Encode), so < > & stay readable and diffs stay small.
- `internal/llm/` — runs Vibe's in-app agent on the charmbracelet/fantasy framework: chat, notebook task mode (task.go) and the grounding guardrails, against the configured provider (Anthropic, OpenAI, Gemini, Ollama, Cloudflare or a local OpenAI-compatible server; docs/reference/llm-config.md).
- `internal/lsp/` — `vibe lsp`: a Language Server Protocol server over stdio that completes and describes a workspace's connections, schemas, tables, views and columns in any editor with an LSP client (Neovim, Helix, VS Code, Zed, Emacs).
- `internal/mcp/` — serves a workspace's agent tools to external agents over the Model Context Protocol (SSE and stdio) and proxies to the process that owns the workspace hub.
- `internal/notebooks/` — implements the Notebook contract (#66 part A): a title plus an ordered list of cells of three types (sql, markdown, a2ui), stored one per file in a workspace (notebooks/<id>.vnb, see file_format.go).
- `internal/prefs/` — stores app-global user preferences for Vibe Studio — the settings that belong to the person, not to any one workspace (theme, appearance).
- `internal/queries/` — the file-backed store of saved SQL queries (queries/*.sql) and the filter model the table browser shares with them.
- `internal/sandbox/` — the one definition of the agent's three sandbox tiers: their names, the strict parser for a tier string and the restrictiveness order.
- `internal/secrets/` — keeps credentials out of files, logs and the agent's reach (plan §4.3, #114).
- `internal/selfupdate/` — tells a person that a newer Vibe release exists and, only when they confirm, upgrades by running that release's own install.sh.
- `internal/session/` — the DuckDB "hub" for a workspace (plan §4.4): one DuckDB instance whose attached catalogs are the user's connections, with catalog-aware introspection, cancellable statements, a schema cache and open-time hardening that is read back and fails closed.
- `internal/session/sessiontest/` — builds Sessions for the tests of packages that depend on one.
- `internal/skills/` — Vibe's SKILL.md layer (#62): the built-in skills embedded in the binary, the file-backed store of a workspace's skills/<name>/SKILL.md (FileStore, Mock), and the service the agent and Studio share (LoadSkills, SaveUserSkillAt, DeleteSkill, WriteImportSkill).
- `internal/storageglob/` — expands wildcards in S3, GCS and R2 URLs for the SQL Vibe runs (#138 follow-up).
- `internal/storageurl/` — the one place that decides what an object-storage URL is: which schemes count, what a plain URL looks like, whether one lies inside a scope, and whether an Azure URL names the account's own endpoint.
- `internal/templates/` — holds the built-in workspace templates that "new workspace" and the importer scaffold from.
- `internal/testsupport/miniotest/` — the integration tests' S3-compatible store: a throwaway MinIO in a container, and the lake the #118 end-to-end tests read from it.
- `internal/testsupport/ssofake/` — a loopback IAM Identity Center for tests and for the ssofake dev build (tools/ssofake): the OIDC device-authorization endpoints and the portal that turns an access token into role credentials.
- `internal/trustfile/` — remembers one person's allow and deny decisions about things that arrive in a workspace folder and could act on their behalf: a workspace .env (internal/dotenv, dotenv.json) and a custom artifact view (views.json).
- `internal/tui/` — Vibe Studio's terminal interface (#69): a SQL REPL, a read-only table browser, and a notebook list.
- `internal/usersql/` — holds the courtesy checks the CLI and the TUI run on SQL a person types, before it reaches the session.
- `internal/workspace/` — the folder-shaped unit of a Vibe project (plan §4.1): a directory with a committed vibe.yaml marker, notebooks, queries and skills as plain files, and connection definitions merged from three scopes.
- `studio/` — the Vibe desktop app: a Wails v2 shell whose bound App exposes the workspace, connections, notebooks and agent to the Vue frontend.
- `tools/ssofake/` — a loopback AWS IAM Identity Center for driving Vibe Studio's AWS sign-in by hand or from a script (task studio:dev:ssofake, Rodney QA).
<!-- package-map:end -->

**Studio** (`studio/`, one bound `App`). Files are split by area:

- `app.go` (core) and `app_<area>.go`, the bindings by area: about, agent, artifacts, artifact_views, import_export, lifecycle, llm, notebooks, prefs, queries, skills, tables, update.
- `workspace_<step>.go` (launch, recents, open, info, create, settings, conflict, mcp) and `connections_<step>.go` (crud, sync, catalog, creds, deps, probe, extensions), plus `secrets.go`, `bundle.go`, `dotenv.go`, `templates.go`.
- `artifacts.go` and `artifacts_edit.go`: the logic behind the data artifact bindings (list, open, run, edit, save, delete). `viewserver.go` and `artifact_view_host.go`: the `/vibe-view/` middleware and per-grant tokens for script views.
- `agent_turn.go`: the in-flight chat turn and its cancel. `signin.go`: AWS device sign-in (`StartSignIn`/`CancelSignIn`); `ssodev.go` and the `ssofake`-tagged `ssofake.go` are the dev-build seam.
- **Thin bindings:** every exported `App` method is at most 25 statements (nested ones counted), does no file, process or SQL work and no filestore write, and uses no dot import or I/O alias (`TestExportedAppMethodsAreThinBindings` and the `thin-binding` ast-grep rule).
- `frontend/` is Vue 3. Every Go call goes through `src/lib/bindings.ts` (a typed `App` over `AppApi` in `env.d.ts`; the generated `wailsjs/go` is gitignored). Views: Launcher, Dashboard, Notebooks/Notebook, TableBrowser, QueryEditor, ConnectionEditor, Skills, Artifacts/Artifact, Import, Settings, Agent.

**Not Go:** `editors/` (Neovim plugin, Helix and eglot setup for `vibe lsp`, `docs/reference/editor.md`), `tools/` (the ast-grep architecture rules, QA video scripts), `scripts/` (build, packaging and CI checks such as `check-layering.sh`), `docs/` (see `docs/README.md`).

## Key concepts

**Workspace and files.** Files are the source of truth; writes are compare-and-write and a watcher picks up outside changes (git, editors, other processes). Notebook outputs are never persisted. A committed `vibe.yaml` can only *tighten* the agent's limits; looser per-person settings live outside the folder. A workspace `.env` is loaded only after the person allows it (direnv-style: allow is keyed by path + content hash, deny by path; decisions in the user config dir), into a per-workspace overlay read by `env:` sign-ins, never `os.Setenv`.

**Connections.** Read-only by default, connect on first use, with a visible status. The connection name is the catalog alias in SQL (`prod_pg.public.orders`). Metadata-driven formats (DuckLake, Iceberg, Delta) may only be defined in your own scopes, never in a committed `vibe.yaml`. MotherDuck cannot be attached read-only.

**Notebooks (vibe-app#66).** JSON documents of `sql` / `markdown` / `a2ui` cells (schemaVersion 2; a cell may name a `connection`). Every data cell re-runs on open. The agent authors cells in **task mode**: `preview_query` before a terminal `write_cell` or `need_more_info`.

**Skills (vibe-app#62).** `SKILL.md` documents (`name`, `description` + markdown). The prompt advertises name and description; the agent fetches bodies with `read_skill`. Built-ins are embedded. Table-bound skills load only when the table has rows.

**Data artifacts (#162, plan `docs/plans/data-artifacts.md`).** A parameterised, live view stored as files: `artifacts/<name>/artifact.yaml` (typed parameters with defaults and *named* queries) plus `view.a2ui.json`. It stores questions, never answers. `artifacts.Bind` turns (query name, raw values) into the manifest's SQL text unchanged plus bound params; a value never becomes SQL text. The store refuses rather than repairs (strict YAML, byte caps before decode, no anchors/aliases), and one `Rev` covers both files. The SQL policy runs in `internal/agents` at save and again at every run, in every tier, so a hand-edited, git-pulled or bundled artifact is held to it too. No tool runs an artifact for the agent (Studio runs it for the person). Level 2 is the **script view** (`view: script`, `view.js`): it shows as source until the person allows it (path + `ViewHash`, decisions in `views.json` via `internal/trustfile`; the agent can never allow), then runs in a terminable Web Worker served by `studio/viewserver.go` under a per-grant token with a deny-all CSP, gets rows only through `RunArtifactViewQuery`, and returns an SVG (shown as an inert `<img>`) or an A2UI page Go validates. HTML pages are a proposal (`docs/plans/html-artifacts.md`).

**Custom artifact views (`view: script`, #159).** An artifact may name `view.js` instead of `view.a2ui.json`; the code is someone else's or the agent's, so it shows as source and runs nothing until the person allows it. Allow is keyed by the folder's canonical path plus `artifacts.ViewHash` (version-tagged, length-prefixed over `artifact.yaml` and the view files), kept in `views.json` beside `dotenv.json` (`internal/trustfile`), and refused if the files changed since they were shown. No agent tool or MCP method can allow, deny or revoke. The worker that runs an allowed view is step 9.

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
- **Docs move with code**: keep `docs/reference/` current, and describe a package in its Go package comment (the package map above is generated from it by `task docs`); `internal/docsref` fails when either drifts.
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
