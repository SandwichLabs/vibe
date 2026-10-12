# Architecture survey (#54): findings

**Repo:** SandwichLabs/vibe @ 0445cc1 · **Date:** 2026-10-11 · read-only survey.

**Inputs:** CLAUDE.md (the intended design) and the #137 survey.

**Artefacts in this folder:**
- `outline-{go,ts,vue}.jsonl`: `ast-grep outline --items all --json=stream`, with the Vue `<script>` blocks extracted to `vue-scripts/`.
- `inventory-go.txt` and `inventory-fe.txt`.
- `imports.txt`.
- `deadcode.txt` (`-tags webkit2_41,duckdb_arrow ./cmd/vibe ./studio`) and `deadcode-test.txt` (`-test ./...`).
- `dupl.txt` (`-t 80`) and `dupl-t50.txt`.
- `gocognit.txt` (`-over 25`).
- `golangci.txt`, from golangci-lint 2.13.1 with `golangci-arch.yml` (dupl, unparam, gocognit, funlen, errorlint, wrapcheck, gochecknoglobals, contextcheck, forbidigo).
- `rules/`, `sgconfig.yml` (the new rules plus the #137 rules), `scan-*.jsonl` and `scan-all.jsonl` (497 hits).
- `bindings.md`, `frontend.md` and `window-go-hits.txt`.
- `prior-status.md`.

Severity is high, med or low. Effort is S, M or L. "Risk" means risk to behaviour.

## 1. Summary

**Layering is sound.** No `internal/*` package imports `cmd` or `studio`, in production or test code, and the graph is an acyclic DAG of 10 levels.

**The problems are about where logic lives:**
- **The CLI and Studio implement the same workflows separately, and the copies have drifted:**
  - workspace open (C1, 4 copies);
  - connection status display (C2, 3 copies);
  - connection test credentials and error text (C3);
  - scope parse and labels (C4);
  - COPY export (C5);
  - table paging and filtering (C6).
- **Studio `App` is a god struct.** It has 118 bindings and about 70 private helpers behind 5 mutexes. `studio/workspace.go` (1,721 lines) and `studio/connections.go` (1,514) are the largest Go files (P1, #137 O2).
- **Packages are misplaced or overloaded:**
  - `internal/llm/skills` is unused by `llm` (L1);
  - `internal/agents` has 4 concerns in about 7.2k lines (P2);
  - `workspace.OpenSession` has cognitive complexity 104 and branches on connection type (P3).
- **The frontend has no binding layer.** There are 143 direct `window.go` accesses (F1), and the fingerprint logic disagrees with Go (F2).

**#137:** 21 closed, 15 open, 9 partial, and 2 dropped or not applicable. U1 is corrected to partial.

**New findings:** 36 in Go and the repo, and 13 in the frontend. They are organised into 11 slices.

## 2. Import-graph layering

These are the internal edges from `go list`. A package's level is 1 + the deepest level it imports.

| Level | Packages |
|---|---|
| 0 | a2ui, docsref, dotenv, filelock, filestore, jsonenc, prefs, storageurl, usersql |
| 1 | llm/skills, notebooks, secrets |
| 2 | config, session, templates |
| 3 | connections, engine, session/sessiontest |
| 4 | crud, importer, queries |
| 5 | agents, workspace |
| 6 | bundle, llm, mcp, tui |
| 7 | hub |
| 8 | cmd, studio |
| 9 | cmd/vibe |

**Fan-in:** session 13, engine 8, filestore/notebooks/secrets 7, agents 6.

**PASS:** there is no `internal → cmd/studio` edge (checked across Imports, TestImports and XTestImports), and there are no cycles.

**Odd edges, which are not inversions:**

- **L1 (med).** `internal/llm/skills` is imported by agents, workspace, bundle and studio, but **not by `internal/llm`**. The skills service lives in a third place: `agents/skills.go` (795 lines: `LoadSkills`, `SaveUserSkillAt`, `DeleteSkill`, `WriteImportSkill`).
  - Move: create `internal/skills` from the store plus that service. The `skills` → `session` edge is legal.
  - M · risk none.
- **L2 (low, O7).** `engine` is partly a shim over `session`:
  - `engine/quote.go:9` `QuoteIdent` just delegates;
  - `engine/table.go:57` `TableSQL` is wrapped again by `crud/crud.go:31` `tableSQL`;
  - `importer` uses both `engine.QuoteIdent` (32) and `session.QuoteIdent` (4).
  - Move: delete the shims and keep `engine` as the query-execution package (Manager, ScanRows, Normalize, params, autonumber).
  - S-M, about 70 sites · none.
- **L3 (low).** `queries`, a file store, imports `engine` and `session` only for the QBE builder `filter.go`.
  - Move: put `filter.go` in `crud` (or `internal/qbe`) next to C6's builder.
  - S · none.
- **L4 (low).** There are two per-user config homes:
  - `config.LegacyConfigPath()` (`~/.vibe.yaml`, still the live llm settings; `config/config.go:185`);
  - `prefs.DefaultDir()` (`os.UserConfigDir()/vibe`; `prefs.go:161`).
  - `workspace` imports `prefs` only for `DefaultDir`.
  - Move: rename to `LLMConfigPath`, or document.
  - S.

## 3. #137 open items

The evidence is in `prior-status.md`. The closing commits were 99a91ad, e6cb97e, ceb3bcc and 5fd6a6a.

| Id | Status | Now |
|---|---|---|
| U1 | **partial** (corrected) | `studio/app_tables.go:329` `buildFilterWhere` (parameterized) still re-implements the WHERE loop of `queries/filter.go:95` `buildFilterWhereInline`. Only the FROM is shared. (C6) |
| U2 | partial | 9 doubled-quote sites: `studio/app_import_export.go:183,231`, `usersql/usersql.go:66`, `queries/filter.go:145,147`, `importer/source.go:226`, `importer/xlsx.go:21,23`. 66 Sprintf SQL strings (was 75). |
| U3 | open | 4 slug helpers: `filestore.go:442,455`, `hub/claudeconfig.go:76`, `agents/skills.go:578`, `studio/workspace.go:1165`. |
| U4 | open | Non-atomic writes at `studio/app_notebooks.go:228` and `studio/bundle.go:312`. A second atomic writer is at `config/yamledit.go:165`. |
| U5 | open, grew | 23 `EvalSymlinks` sites in 14 files (was 20 in 9). There are 3 identical containment clones (D2'). |
| U7 | partial | The `engine/quote.go` shim and 2 `jsonQuote`s (`a2ui/schema.go:182`, `agents/skills.go:777`) remain. New: the `llm.DefaultModelFor`/`defaultModelFor` shims (`llm/runner.go:84,86`). |
| U8 | open | `classifyTestError` (`studio/connections.go:1302`) has 6 `err.Error()` matches. The CLI equivalent is `isLockMessage` (`cmd/wsopen.go:120`). |
| U9 | open | 34 `information_schema`/`duckdb_*` sites in 17 files. |
| U11 | partial | `ConfirmDialog.vue:61-69` (F13). |
| U12 | partial | `AgentModelSettings.vue:121,185,208,223,241` (F9). |
| E2 | open | Deep refs: `TableBrowserView.vue:58`, `QueryEditorView.vue:31`. |
| O2 | open | P1. |
| O4 | open | 2 lexers (`usersql/tokens.go:28`, `agents/sql_lex.go:56`) both feed the policy (`sql_classify.go:275`). There is no cross-check test. |
| O7 | open | L2. |
| O9 | open | `NotebookView.vue`: 1,066 script lines and 48 functions (F10). |
| D4 | partial | `hub.RequestRelease` (`hub/release.go:24`) is called only from tests. |
| D5 / xlsx | partial | The 3 xlsx bindings (`app_import_export.go:219-266`) have no UI caller (C5). |
| D6 | open | The generated `wailsjs/go` (3,190 lines) is committed and unused (F11). |
| M1 | partial | `env.d.ts` has all 118 bindings, synced by hand. vue-tsc is report-only and there is no typecheck script. |
| M4 | partial | The CLAUDE.md tree misses the `app_*.go` split, `studio/dotenv.go`, `internal/dotenv` and `internal/jsonenc` (H3). |

**Confirmed closed:**
- D2: deadcode with `-test` reports 0.
- E3 and E4: only 4 internal unparam hits remain (K5).
- M2.

## 4. New findings (Go)

### A. CLI and Studio duplication

**C1 (high): the workspace open sequence is implemented 4 times.**
- **The sequence:** `workspace.Open`, then hub lock, then `OpenSession(redactor, forceReadOnly)`, then `engine.NewManager`.
- **Where:**
  - `cmd/wsopen.go:138` and `:171`;
  - `studio/workspace.go:715` (`openLocked`, 86 lines);
  - `internal/hub/serve.go:213-228`;
  - `cmd/workspace.go:155` as a partial fifth.
- **Drift:**
  - only Studio repairs a missing id (`ErrMissingID` → `EnsureID`, `studio/workspace.go:730-735`);
  - only Studio does take-over;
  - only the CLI prints warnings and prompts for `.env`;
  - the close-on-error order differs.
- **Move:** add `hub.Open(ctx, dir, OpenOptions{App, Mode ReadWrite|ReadOnly|TakeOver|RemoteOnly, Logger, Resolver, Redactor, ReleaseHandler}) (*Opened{WS,Sess,Eng,Hold,Holder,Issues,ReadOnly}, error)`. The adapters keep only presentation.
- **Effort and risk:** M · med (lock ordering). Covered by the cmd, studio and hub tests, plus Rodney QA of open, read-only and take-over.

**C2 (high): connection display status is computed 3 times, with different words.**
- **Where:**
  - `cmd/workspace.go:239` `buildInfo` reports an issue as "unavailable" and detects a DuckDB lock by **message text** (`isLockMessage`, `cmd/wsopen.go:120`);
  - `cmd/conn.go:321` `runConnList` (cognitive complexity 37) reports "error";
  - `studio/workspace.go:1065` `connectionInfosLocked` (cognitive complexity 31) reports "error", rewrites the detail with `classifyTestError`, and parses the server change back out of the text with `serverChangedRE`.
- **Effect:** `vibe workspace info` and `vibe conn list` disagree, and `session.Status.LockedByPID` goes unused by the CLI.
- **Move:** add `workspace.ConnectionStates(ws, sess, issues) []ConnectionState` built from typed fields, with a typed `ServerChanged{Old,New}` on the needs-credentials status.
- **Effort and risk:** M · med (the CLI status word changes; update tests). Rodney QA of the rail.

**C3 (med): connection test differs between the surfaces.**
- **Studio:**
  - `testBaseCredential` (`studio/connections.go:1253`) applies the server-changed guard;
  - a field-by-field overlay of typed credentials (`:1090-1104`);
  - `classifyTestError` (`:1302`, 8 `strings.Contains`).
- **CLI:** `runConnTest` (`cmd/conn.go:434`) uses `workspace.ConnectionCredential` and prints the raw status.
- **Move:**
  - classification into `connections` (`TestResult.Code` and `Message`);
  - `workspace.TestCredential(...)`;
  - `secrets.Credential.Overlay`.
- **Effort and risk:** M · med and security-sensitive. Move the guard tests along with the code.

**C4 (med): scope parse and labels are duplicated.**
- **Where:**
  - `parseScope` at `cmd/conn.go:73` and `studio/connections.go:176`;
  - 3 label vocabularies:
    - `studio/connections.go:261`: "Team / This computer / All my workspaces";
    - `bundle/import.go:208`: "team / this computer / all your workspaces";
    - `workspace/conn_crud.go:205`: "shared / local (this computer) / user (all your workspaces)";
  - `studio scopeDef` (`:185`).
- **Move:** `workspace.ParseScope`, `Scope.Label()` and `Scope.Defs`.
- **Effort and risk:** S · low. Unifying the labels changes text, so do it in a separate commit.

**C5 (med): the COPY export is built twice.**
- **The two versions:**
  - `cmd/export.go:151` uses `QuoteLiteral`, `RequireSelect` and a qualified ref;
  - `studio/app_import_export.go:182` `buildCopySQL` uses a hand-doubled quote, and `:231` builds a hand-quoted, **unqualified** `` SELECT * FROM "%s" ``.
- **Note:** only the 3 xlsx bindings, which have no UI caller, use the Studio version.
- **Move:** either delete them (plus the docs line `import-office.md:136`), or share an `engine.ExportCopy`.
- **Effort and risk:** S · none if deleted.

**C6 (low): table browsing SQL lives in the adapters.**
- **Where:**
  - `studio/app_tables.go:160-200` and `:270-300` (count, page, sort, filter), plus the second WHERE builder at `:329`;
  - `tui/tables.go:66` runs an N+1 `COUNT(*)` per table, and `:151` uses `LIMIT 1000`.
- **Move:** `crud.Page(ctx, db, ref, PageRequest)` with one WHERE builder that supports both param and inline modes. The TUI should use `estimated_size`.
- **Effort and risk:** M · low-med. Needs TableBrowser QA.

**C7 (low): two terminal sanitizers.** `cmd/query.go:281` (NULL shown as "") and `tui/repl.go:289` (NULL shown as "NULL").
- **Move:** one `engine.TerminalText`.
- **Effort:** S.

**C8 (low): extension SQL is duplicated** in `cmd/workspace.go:298-317` and `studio/connections.go:1378-1435`.
- **Move:** `session.InstalledExtensions` and `session.EngineVersion`.
- **Effort:** S.

**C9 (low): a third statement classifier.** `studio/connections.go:1476` `isReadOnlyStatement` is a prefix guess. The TUI comments at `tui/app.go:302-326` call the same approach a bug. `agents.IsReadOnlyStatement` exists.
- **Move:** use it, or document why the guess is enough.
- **Effort and risk:** S · refresh events change.

**C10 (low): `connSummary` is cloned** in `bundle/import.go:220` and `studio/bundle.go:495`.
- **Move:** export it.
- **Effort:** S.

### B. Misplaced code, packages with several jobs, god files

**P1 (high): Studio `App` is a god struct.**
- **Size:** 118 bindings in 15 files, about 70 private methods, and 5 mutexes (`studio/app.go:29-108`).
- **`workspace.go`:** 1,721 lines, 62 functions and 26 bindings, covering launch, recents, git, open and close, watcher, info, create, settings, conflict and MCP.
- **`connections.go`:** 1,514 lines, 49 functions and 20 bindings.
- **Logic that belongs in `internal/`:**
  - `resolve{Notebook,Query,Skill}Conflict` (`workspace.go:1459-1624`, 3 near-copies);
  - recents kept in `prefs.Extra` (`:418-493`);
  - `gitBranch` (`:624`);
  - `SetWorkspaceSettings` (`:1367`, cognitive complexity 35);
  - `ListCatalog` (`connections.go:656`, cognitive complexity 35);
  - plus C1–C4 and C6.
- **Move:**
  - **(a)** move the logic out: `prefs.Recents`, `workspace.GitInfo`, a store-level `ResolveConflict` (which also fixes F3), and `workspace.ApplySettings`;
  - **(b)** a mechanical file split into `workspace_{launch,recents,open,info,create,settings,conflict,mcp}.go` and `connections_{crud,sync,catalog,creds,test,extensions}.go`.
  - Keep one bound `App`. Splitting it into several bound structs would rename every JS path, so that waits until after F1.
- **Effort and risk:** M+S · (a) med per item, (b) none.

**P2 (med): `agents` does 4 jobs in about 7.2k lines across 25 files.**
- **The jobs:**
  - the tool handler (`handler.go`: 1,437 lines, 34 methods);
  - the SQL policy analyzer (about 3.2k lines in `sql_*.go` and `policy_handler.go`);
  - the skills service (795);
  - notebook task (726);
  - plus the JSON-RPC server.
- **Move:**
  - `internal/agents/sqlpolicy`, as a pure move (`docsref` uses `Policy*`);
  - skills to `internal/skills`;
  - split `handler.go` by tool family.
- **Effort and risk:** M · low, but the Opus review must confirm the security code moved verbatim.

**P3 (med): `workspace.OpenSession` is the most complex function in the repo.**
- **Where:** `workspace/session.go:76`, 172 lines, **cognitive complexity 104**.
- **Problem:** inline branches for motherduck, duckdb and ducklake (token, folder creation, read-only rules, data_path).
- **Move:** a `Preparer` capability in `session/registry.go`, implemented in `connections/*`, the same way `CredentialedType` and `HubTokenType` work.
- **Effort and risk:** M · med. Integration tests exist.

**P4 (low): three file stores repeat the same skeleton.**
- **Where:** notebooks (249), queries (333) and skills (294) each have `FileStoreOption`, `WithObserver`, `NewFileStore`, `rel`, `remember`/`lastSeen` and a `seen` map. dupl matches `notebooks/file_store.go:121` with `queries/file_store.go:199`, and the mocks are cloned.
- **Move:** a generic `filestore.Docs[T]`.
- **Effort and risk:** M · low (`store_contract_test`).

**P5 (low): 20 open-coded "find connection by name" loops.**
- **Where:** 11 in `studio/connections.go`, `cmd/conn.go` ×2, `cmd/wsopen.go:249`, `bundle/import.go:201`, `connections/test.go`, and `workspace/{conn_crud,connect,spec}.go`. The same pattern repeats over `sess.Connections()` (`cmd/conn.go:565`).
- **Move:** `Workspace.Connection(name)` and `Session.Status(name)`.
- **Effort:** S.

**P6 (low): interfaces.**
- **Fat interfaces defined by their provider:** `session.Session` (31 methods) and `workspace.Workspace` (37), with one implementation each.
- **Eight overlapping one-method DB interfaces:** `crud.RowQuerier`/`TxExecer`, `engine.RowQuerier`/`Querier`, `session.Queryer`/`Querier`/`Execer`, `agents.queryDB`.
- **Move:** fold the small ones together and leave the big ones.
- **Effort:** S.

### C. Conventions

**K1 (med): context is not passed through.**
- **Rule counts:** 49 hits for `context.Background()` in request paths, and 20 contextcheck hits.
- **Where:**
  - `agents` skills APIs take no ctx and create `Background()` 9 times (`skills.go:83-745`);
  - Studio `app_notebooks` (8) and `app_queries` (7), including `RunQuery`'s `ExecuteCappedIn(context.Background(), …)` at `:109`;
  - `secrets.go` (7);
  - `bundle/import.go:242,499` drops the ctx it was given.
- **Move:** add ctx parameters, and use `a.ctxOrBackground()` in Studio.
- **Effort and risk:** S-M · low.

**K2 (low): error handling.**
- **Where:**
  - 6 `fmt.Errorf` calls format an error with `%v`: `a2ui/schema.go:418,424`, `config/llmsettings.go:163`, `config/llmkey.go:137`, `agents/sql_classify.go:231`, `cmd/conn.go:516`;
  - 6 `==` sentinel comparisons: `mcp/server.go:152,232`, `mcp/proxy.go:127`, `agents/server.go:176`, `engine/autonumber.go:117`, `importer/import.go:390`.
- **Move:** use `%w` and `errors.Is`. Ignore wrapcheck's 227 hits, which are not a project convention.
- **Effort:** S.

**K3 (low): logging and globals.**
- **Where:**
  - 4 direct `slog.Default()` calls bypass the injected logger: `queries/file_store.go:192`, `agents/skills.go:154`, `llm/skills/file_store.go:158`, `prefs/prefs.go:169`;
  - `cmd/root.go:14-18` has globals `logger`, `cliRedactor` and `cfgFile`;
  - flag-value globals at `cmd/config.go:27-28` and `cmd/config_llm.go:72`.
- **Why it matters:** this contradicts "no globals".
- **Move:** inject a logger, and use a `cliDeps` struct with RunE closures.
- **Effort:** S-M.

**K4 (low): environment reads outside config and secrets.**
- **Where:** `workspace/connect.go:24`, `workspace/interpolate.go:16`, `session/motherduck.go:31`, `connections/test.go:293`, `cmd/bundle.go:199`, `studio/jsc_linux.go:15`.
- **Assessment:** all are justified except the `${ENV}` interpolation lookup.
- **Move:** route that lookup through `secrets.EnvStore`.
- **Effort:** S.

**K5 (low): unparam.** 4 internal constant-argument helpers: `agents/budget.go:44`, `llm/reasoning.go:33`, `llm/task.go:45,430`.
- **Move:** inline the constants. Leave the binding signatures.
- **Effort:** S.

### D. Dead code, clones, complexity

**D1' (low): production code reached only from tests** (deadcode without `-test`).
- **Where:**
  - `llm.Runner.Chat` (`runner.go:457`);
  - `App.setInitialArg` (`studio/workspace.go:376`);
  - `bundleStore.has` (`studio/bundle.go:68`);
  - `skills.Overwrite` (`store.go:78`);
  - `session.ResolveStatus` (`types.go:34`);
  - `workspace.OpenFrom` (`workspace.go:241`);
  - `filelock.Acquire`/`Probe` (`filelock_unix.go:61,154`);
  - `hub.RequestRelease`.
- **Fine as they are:** the other entries are `With*` option seams and Mocks.
- **Move:** delete these, or move them to `export_test.go`.
- **Effort:** S.

**D2' (med): path containment is cloned.**
- **Where:** `filestore/filestore.go:172` `within`, `agents/sql_paths.go:79` `inside` and `workspace/session.go:324` `isWithin` are byte-identical (rule `go-path-containment-clone` and dupl).
- **Move:** export one `Within`, as step 1 of U5.
- **Effort and risk:** S then M · security code, needs Opus review.

**D3' (low): intra-package clones (dupl -t 80).**
- **Where:**
  - `connections/mysql.go:39-59` with `postgres.go:33-53`, and `mysql.go:106-124` with `postgres.go:104-122`;
  - `templates.go:166-214` with `:283-334`;
  - `llm/runner.go:530-560` with `:721-742`;
  - `agents/tools.go:98-109` with `:348-359`;
  - `cmd/import.go:220-233` with `:235-248`;
  - `engine/columns.go:60` with `session/schema.go:290` (at `-t 50`).
- **Move:** a `netDBType` helper for the mysql/postgres pair.
- **Effort:** S.

**Z1 (info): gocognit over 25 has 56 hits.**
- **Leave alone** (parsers and security analyzers): `lexStatements` 81, `qualifiers` 70, `Coerce` 64, `tokensTouchSecrets` 63, `screenTokens` 63, the `usersql` lexers 62.
- **Actionable:**
  - `OpenSession` 104 (P3);
  - adapter functions that shrink with C1–C6: `runConnList` 37, `SetWorkspaceSettings` 35, `ListCatalog` 35, `runTUI` 34, `connectionInfosLocked` 31, `writeResult` 40, `runExport` 39.
- **funlen:** one hit, `agents/approval.go:170`.

### E. Repo hygiene

- **H1 (low).** `studio/studio/frontend/dist/index.html` is tracked although `.gitignore` excludes `studio/studio/`. Move: `git rm` it. S.
- **H2 (low).** `.golangci.yml` excludes the non-existent `cmd/vibe-viewer/frontend`. Move: drop it. S.
- **H3 (low).** The CLAUDE.md tree has drifted (M4). Move: update it with each move. Optionally add a docsref check that lists the `internal/*` dirs. S.

## 5. New findings (frontend)

The detail is in `frontend.md` and `bindings.md`.

**F1 (med): no binding layer.**
- **Where:** 143 `window.go` accesses in 33 files (rule `ts-window-go-outside-binding-layer`). Stale feature probes at `AppearanceSettings.vue:57`, `AiCellDialog.vue:73` and `App.vue:42`.
- **Move:** add `src/lib/bindings.ts` as a typed App, migrate, then gate it in CI.
- **Effort and risk:** M · low.

**F2 (med): the fingerprint check disagrees with Go.**
- **Problem:** `ConnectionEditorView.vue:142-157` compares only `FingerprintFields`, which Go never reads. `workspace.Fingerprint` (`fingerprint.go:19`) also hashes region and every option.
- **Effect:** editing region or a non-`sslmode` Postgres option invalidates the sign-in without a prompt.
- **Move:** Go owns the comparison through a binding. Delete the TypeScript version.
- **Effort and risk:** S-M · med (more prompts; this fixes the gap). Rodney QA.

**F3 (med): the conflict UI is copied 3 times and has drifted.**
- **Where:** `SkillsView.vue:39-41,158`, `QueryEditorView.vue:47-58,171`, `NotebookView.vue:110-114,964`.
- **Problem:** the Skills "Keep mine" sends no `rev` (`SkillsView.vue:170-173`). All three detect a conflict by the string prefix `'conflict:'`.
- **Move:** `useConflict`, plus a typed conflict error and a store-level `ResolveConflict`.
- **Effort and risk:** M · low-med. The skills rev fix changes behaviour, so it goes in its own commit.

**F4–F9 and F13 (low, S each, risk low):**
- F4: the agent transcript is duplicated between AgentView and AgentPanel.
- F5: the A2UI live-query code appears 4 times.
- F6: the BundleImportDialog form duplicates the new-workspace form, including `slugify`.
- F7: 3 menu keyboard handlers.
- F8: the credential `KEY` map appears twice.
- F9: `AgentModelSettings` builds error text by hand.
- F13: `ConfirmDialog` does not use `useModal`.

**F10 (med): NotebookView is still large.** 1,066 script lines (O9). TableBrowserView (555) and ConnectionEditorView (516) mix view state with orchestration.
- **Move:** composables.
- **Effort and risk:** L · med.

**F11 (low): generated bindings are committed and the types are unchecked.** `wailsjs/go` is unused, and `env.d.ts` is unchecked.
- **Move:** use or gitignore the generated code, add a typecheck script, and make vue-tsc blocking.
- **Effort:** S.

**F12 (low): bindings with no real caller.**
- **Where:** `RenameNotebook` is dead. `ExecuteQuery` and `SetAgentConfig` are called only from tests. The xlsx bindings are covered by C5.
- **Effort:** S.

## 6. Fine as it is

**Layering:**
- one-way dependencies and no cycles;
- the level-0 leaf packages;
- a thin `cmd/vibe`.

**Interfaces defined by their consumers:**
- `agents.WorkspaceAccess` is satisfied by `workspace.NewAgentAccess`, and `workspace` does not import `agents`;
- `tui` defines its own interfaces;
- `session`'s capability interfaces are implemented by `connections`.

**Model adapters:**
- `.env` handling (`internal/workspace/dotenv.go`, thin `studio/dotenv.go` and `cmd/workspace_env.go`);
- `ws.SetCredentials`, used by both surfaces;
- the shared `connections.Test`, `bundle`, `importer` and passphrase handling.

**Hygiene:**
- `deadcode -test` is clean;
- every `regexp.MustCompile` is package-level;
- `os.Getenv` is confined, apart from K4;
- `gitBranch` deliberately runs no git;
- the `sync.Once` classifier DB (`sql_classify.go:205`) is deliberate.

**SQL built by concatenation (25 hits):** the ATTACH and secret DDL operands all go through `QuoteIdent`/`QuoteLiteral`. The `importer` concatenates the constant `stageTable` (`source.go:35`). `approval.go:281` appends a WHERE that the classifier has already screened.

**Frontend:**
- status mapping is centralised in `lib/connections.ts`;
- `errorText` is used in 29 files;
- one TS SQL string, and it is quoted;
- `notebookMerge` is factored out;
- `useModal` is used in 15 places.

## 7. Cleanup slices

Every slice is independently shippable and preserves behaviour unless marked **(behaviour)**.

**Gates for every slice:**
- lint, tests and CI green;
- Opus adversarial review;
- Rodney QA for anything Studio-facing.

| # | Slice | Contents | Effort |
|---|---|---|---|
| 1 | Hygiene and dead code | H1, H2, H3, D1', F12, C5 decision, U7 shims, K5 | S |
| 2 | Small shared helpers | C4 (ParseScope; labels unchanged), P5, C7, C8, C10, D2' step 1, U3 (with table tests first), U4 | S |
| 3 | Context, errors, logging | K1, K2, K3, typed `ErrConflict` (start of U8) | S-M |
| 4 | One workspace open path | C1, `hub.Open` | M |
| 5 | Shared connection state and test | C2, C3, F2 (F2 is a **behaviour** commit) | M |
| 6 | Table reads and filters to `internal/` | C6, C9, L3, the remaining Studio U2 sites | M |
| 7 | Pure package moves | L1, P2, L2, L4; update the CLAUDE.md tree | M |
| 8 | Studio files once their logic has moved | P1(a) remainder (including the F3 skills rev fix, **behaviour**), then the P1(b) split | M+S |
| 9 | Frontend binding layer and dedup | F1, F11, F3, F4–F9, F13 | M |
| 10 | Heavier follow-ups | P3, F10/O9, P4, rest of U5, O4, U9, E2, P6 | L |
| 11 | Guard rails | Commit these rules to the repo `sgconfig.yml` and CI as their slice lands: `go-sql-literal-in-adapter`, `go-path-containment-clone`, `go-errorf-err-without-w`, `go-err-eq-compare`, `go-context-background-in-request-path` (scoped), `ts-window-go-outside-binding-layer`. Add a `go list` check that `internal/**` never imports `cmd` or `studio`. | S |

**Why this order:**
- Slices 1–3 are low risk and shrink the code the later slices move.
- Slices 4–6 remove the CLI/Studio duplication, which is the highest-weight rubric item.
- Slice 7 renames packages afterwards, so no code is moved twice.
- Slice 8 splits files only once they are thin.
- Slice 9 needs slice 8's typed conflict API.
- Slice 10 is optional for "clean", except perhaps P3.

**Rule note:** ast-grep resolves `files:` and `ignores:` globs against the sgconfig's directory. The adapter-scoped rules are therefore exact only when the rules sit at the repo root, as they would once committed. The survey ran each rule with `ast-grep scan -r rules/<id>.yml cmd internal studio` from the repo root.

*Later note: with ast-grep 0.45.3 the globs resolve against the working directory, so the rules now live in `tools/ast-grep/` and are run from the repo root (see `tools/ast-grep/sgconfig.yml`).*
