# Cleanup survey — findings (#137)

**Date:** 2026-10-06 · **After:** epic #105 (PRs #121–#136)
**Method:**
- ast-grep 0.45.3 with the rules in [`rules/`](rules/). Re-run with `ast-grep scan -c docs/reviews/cleanup-2026-10/sgconfig.yml .`
- `deadcode` (with and without `-test`).
- golangci-lint with extra linters (dupl, unparam, prealloc, gocyclo, gocognit, funlen).
- A Wails-binding usage cross-check, and Vue script extraction.

**Clean already:**
- `regexp.MustCompile` is always package-level, and there is no `defer` in a loop.
- All 83 `v-for` are keyed, and there are no unused Vue components.
- There are no deep watchers.
- Events and copy use "workspace".
- `internal/container` and `engineAgentAdapter` are gone.

Severity: high / medium / low. Effort: S / M / L. Proposed changes preserve behaviour unless noted.

## 1. Dead code

| Id | Sev | Finding | Proposed change | Effort |
|---|---|---|---|---|
| D1 | high | Test-only legacy `_vibe_*` stores (~690 lines): `notebooks/storage.go`, `notebooks/sqlite_store.go`, `llm/skills/sqlite_store.go`, `queries/registry.go`, `engine/sqlite.go`. They have 0 non-test callers. | Delete them and port the tests to FileStore/Mock. | M |
| D2 | high | Dead even with `-test`:<br>- `engine/crud_spike.go` and `relationship_spike.go`, `crud/fieldmeta.go`<br>- `agents.Policy{…}` (`sql_names.go:261-281`), `agents.MetaPolicy`<br>- `session.PrimaryKeyColumns`, `WithDefaultTimeout`<br>- `workspace.UserConfigPath`, `WithSessionResolver`, `WithEnv`<br>- `storageurl.IsURL`, `filelock.UnlinkAt`, `hub.WithMCPSandbox`, `connections.Lookup`, `bundle.Default.*` | Delete. | S |
| D3 | medium | Field-meta plumbing is always empty: `studio/app.go:1290` returns an empty map, and nothing writes `_vibe_field_meta`. About 160 lines remain in `crud.go:100-260`. | Drop `FieldMeta` and the `metas` parameter. | M |
| D4 | medium | Test-only production helpers (`session.Wrap`, `UnhardenedForTests`, `PolicySource`/`PolicyFrom`, `SkillsPromptBlock`, `SaveUserSkill`, `hub.RequestRelease`, …). | Keep the real test seams and move them to `export_test.go`; delete the obsolete ones. | M |
| D5 | medium | 18 of 125 `App` bindings have no frontend caller:<br>- `AddColumn`, `AddIndex`, `AlterColumnType`, `DropColumn`<br>- `AgentChat`, `SendAgentQuery`, `GetAgentStatus`<br>- `AnalyzeQueryInExcel`, `ExportQueryXLSX`, `ExportTable{XLSX,CSV,JSON}`<br>- `ListCatalogTables`, `ListTableStats` (N+1 `COUNT(*)`), `PreviewFilterSQL`, `SaveQuery`<br>- `ChooseBundleSaveFile`, `SwitchWorkspace` | Delete them, together with the Go tests that only exist for them. | M |
| D6 | medium | The generated `wailsjs/go/**` (~3.5k lines) is committed but unused. The frontend uses `window.go…` plus the hand-written `env.d.ts`, and only imports `wailsjs/runtime`. | Gitignore `src/wailsjs/go/`. | S |
| D7 | low | Unused CSS classes (`.btn-danger`, `.success-toast`, `.sdot-sr`, `.dim-behind`, `.stepper`, `.themecard`, `.tree-plan`). | Delete them after grepping for dynamic class names. | S |
| D8 | medium | Agent-visible text still describes the container (`agents/help.go`, `tools.go:80,171,193`, `llm/runner.go:131`). | Rewrite it in workspace terms. This changes what the model sees. | S |
| D9 | low | Stale root files: `PLAN.md`, `PROJECT.md`, `motherduck-research.md`, `.claude/plan.md`, and the old landing screenshots. | Archive (I2). | S |

## 2. Duplication

| Id | Sev | Finding | Proposed change | Effort |
|---|---|---|---|---|
| U1 | high | Exact clone: `studio/app.go:1865-1921` (`buildFilterWhereInline`, `sqlLiteral`, `FilterSpec`) duplicates `queries/filter.go:78-130`. | Use `queries.BuildFilterSQL`. | S |
| U2 | high | 11 hand-rolled quote sites (`go-sql-quote-doubled-quote`) duplicate `session.QuoteLiteral`. There are also 75 Sprintf-built SQL strings (`go-sql-sprintf-ident`). | Add a shared leaf quoting package, replace the 11 sites, and audit the 75 as a list. Needs security review. | M |
| U3 | medium | 5 slug helpers with different fallbacks. | One `Slug(s, fallback)`, with table tests first. | S |
| U4 | medium | 3 atomic writers. Two non-atomic `os.WriteFile` calls on user files (`studio/app.go:615`, `studio/bundle.go:314`). | One shared `WriteFileAtomic`. | S |
| U5 | medium | 20 `EvalSymlinks` containment sites in 9 files. | `internal/pathsafe`, done one package at a time. Security code, so it goes in a follow-up. | M |
| U6 | low | 3 `SetEscapeHTML(false)` encoders. | A shared helper. | S |
| U7 | low | Thin shims: `crud_writeback` aliases, `isValidIdentifier`, `engine/quote.go`, three `Redact` wrappers, two `jsonQuote`s. | Inline them, keeping one `jsonQuote`. | S |
| U8 | medium | Errors are classified by message text (`classifyTestError` has cyclomatic complexity 29, plus 5 other sites). | Typed errors, as a follow-up. | L |
| U9 | low | 35 `information_schema`/`duckdb_*` SQL sites. | Consolidate only the DuckDB-local ones. | M |
| U11 | low | `ConfirmDialog`, `AiCellDialog` and `TableBrowserView` hand-roll modal behaviour instead of using `useModal`. | Move them to `useModal`. | S |
| U12 | low | Raw `String(err)` in `DashboardView` and `useTheme`. | Use `errorText`. | S |

## 3. Inefficiency

There are no hot-path problems.

| Id | Sev | Finding | Proposed change |
|---|---|---|---|
| E1 | low | `ListTableStats` does N+1 `COUNT(*)` (dead, see D5). `AllTables` runs one query per schema. | Delete `ListTableStats`; leave the rest. |
| E2 | low | Deep `ref` on results of up to 5k rows. `TableBrowserView` mutates rows, so it may rely on deep reactivity. | `shallowRef`, with QA. Follow-up. |
| E3 | low | 5 `prealloc` hits. | Add capacities. |
| E4 | low | 13 `unparam` hits; 8 of them are internal. | Fix the internal ones and leave the Wails bindings. |

## 4. Organisation

The largest files are `studio/app.go` (3,058 lines), `studio/workspace.go` (1,670), `agents/sql_policy.go` (1,563), `agents/handler.go` (1,439), `studio/connections.go` (1,386), `NotebookView.vue` (1,304) and `session/session.go` (1,193).

The most complex functions (`OpenSession`, `lexStatements`, `qualifiers`, `Coerce`, `unpack`, the policy walkers) are mostly lexers, parsers and security checks. They are not for this pass.

| Id | Sev | Finding | Proposed change | Effort |
|---|---|---|---|---|
| O1 | high | `studio/app.go` mixes 9 concerns across 61 bindings. | Mechanical split into `app_lifecycle`, `app_skills`, `app_notebooks`, `app_tables`(+filter), `app_queries`, `app_agent`(+tool handler), `app_import_export`, `app_prefs`. | S-M |
| O2 | medium | `studio/workspace.go` and `studio/connections.go` each hold several concerns. | Split them the same way. | M |
| O3 | high | `agents/sql_classify.go` isn't legacy, just misnamed. It holds the catalog and plan plumbing that the policy calls. | Rename it (`sql_catalog.go` / `sql_plan.go`). Rename its test to `sql_policy_e2e_test.go`. Split `sql_policy.go` into SELECT and statement analyzers. | S |
| O4 | medium | There are two SQL tokenizers (`usersql.Tokens`, `agents/sql_lex.go`) that can drift. | Add a cross-check test, plus a follow-up. | M |
| O5 | medium | `agents.ContainerAccess` (40+ refs) has no container behind it any more. The `Meta*` / `PolicySource` / `config.MetaKeys` items are `_vibe_meta` remnants. | Rename to `WorkspaceAccess` and delete the remnants. | S |
| O6 | medium | Unused mocks: `session/mock.go`, `workspace/mock.go`, `bundle/mock.go`, and the store mocks used only by `workspace/mock.go`. Only `secrets.Mock` is really used. | Delete them, and change the convention to "mock.go when a test needs it". | S |
| O7 | low | `internal/engine` is a thin remnant. | Fold it into `session`? Follow-up. | M |
| O8 | low | Two things are called "hub": the `internal/hub` MCP owner and the `session` DuckDB hub. | Rename one. | S |
| O9 | medium | `NotebookView.vue` has a 1,068-line script and 58 functions. | Extract composables for save, sync and cells. These are data-loss paths, so it's a follow-up with QA. | M-L |

## 5. Docs information architecture

| Id | Sev | Finding | Proposed change |
|---|---|---|---|
| I1 | high | There's no `docs/README.md`. `mcp.md` and `llm-config.md` sit outside `reference/`. | Add a docs index and move both into `reference/`. |
| I2 | high | Access-parity and early-design docs (~2.3k lines) are still under `docs/`, and stale plan files are at the root. | Move them to `docs/archive/`. |
| I3 | medium | The UX-panel review is 27 MB of screenshots for a closed epic. | Move it to `docs/archive/reviews/`. |
| I4 | low | The design mockups for #106 are complete. | Index them from `docs/README.md`. |
| I5 | low | Reference docs restate the same rules, and 11 lines still say container, SQLCipher or `_vibe_`. | One owner doc per rule, linked from the others; fix the stale lines. |
| I6 | low | `CLAUDE.md` is 238 lines, much of it removal history. | Trim it to about 150 and point to `direction.md`. |

## Review verdicts (Opus, 2026-10-06) and the plan

An Opus review spot-checked every "dead" claim. Five claims were corrected before any change:
- `PolicyFrom` and `PolicySource` are used in production (`cmd/workspace.go:285`).
- `PolicyReaderFunctions`, `PolicyTableFunctions`, `PolicyCalls` and `PolicyPragmas` feed the docs-drift test.
- O6 must keep the store Mocks, because they are D1's port target.
- The `SaveQuery` and `SwitchWorkspace` tests are retargeted (to `SaveQueryOn` and `OpenWorkspace`), not deleted. `ChooseBundleSaveFile` becomes private.
- U1 must keep Studio's qualified `FROM tref.SQL()`, so the fix shares the WHERE builder only.

Survey findings the review added:
- **M1:** `env.d.ts` doesn't match the Go bindings (20 are undeclared) and there is no `vue-tsc` check.
- **M2:** `hasWailsRuntime` is defined 9 times.
- **M3:** after D1, `go-sqlcipher` is used only by tests.
- **M4:** CLAUDE.md is factually stale.

| PR | Contents |
|---|---|
| 1. Docs and agent text (first, so CLAUDE.md is accurate for later work) | I1 (including the `AgentModelSettings` link), I2 + D9, I4, I5, I6 (a correctness rewrite), and D8 as its own model-visible commit |
| 2. Internal dead code and test port | D1 plus removing `go-sqlcipher`; D2/D4 as corrected; O6 as corrected; the O5 remnants; U6; non-Studio U7; E3; internal E4 |
| 3. Studio tidy | D5 safe deletes plus retargeted tests; D3; U1 as changed; Studio U7; U12; D7; M2; syncing `env.d.ts` (M1) |
| 4. Pure moves and renames (last) | O1 (split `app.go`), O3 as narrowed (move the catalog and plan helpers out; keep `sql_classify.go`), and the O5 rename `ContainerAccess` to `WorkspaceAccess` |

**Dropped:** I3 (moving files in git doesn't shrink the repo) and O8.

**Deferred to follow-up issues:** U2, U5, U8, U3, U4 (atomic export writes), U9, U11, E2, O2, O4, O7, O9, D6 + M1 (typed bindings or `vue-tsc`), and the xlsx export bindings (wire them up or delete them), and refreshing the landing screenshots (`01-unlock.png`, `03-pages.png` show retired UI and are still referenced by `landing/`) along with the landing page. The xlsx bindings are kept for now, since they are Studio's only xlsx path and `import-office.md` documents them.
