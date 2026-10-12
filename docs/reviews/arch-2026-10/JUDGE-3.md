# Judge pass 3 — SandwichLabs/vibe @ 0fe14d6 (main, after slices 7–10)

**Judge:** Fable. **Date:** 2026-10-12. **Rubric:** unchanged. **Measured on:** the `judge-main` worktree at 0fe14d6 with `studio/frontend/dist` built so the Go tooling could load `studio`. `golangci-lint run ./...`: 0 issues. `go test ./studio -run Thin` and `./internal/docsref`: pass. PR #74's checks passed; the `main` push run was still in progress when I measured.

## Verdict: **NOT CLEAN — by two small items, both S.** Total **32 / 36.**

Every blocking criterion is ≥ 2 **except C5**, which is held at 1 by one file over the hard 1000-line bar (`internal/session/session.go`, 1205 lines). Clean-condition §4 is also short one guard: there is no rule protecting C3 (one tool surface). Nothing else stands between this code and "clean". Once the two items below land I will re-run `scripts/check-filesize.sh` (with the session.go waiver removed) and the new rule, and rule clean; the final summary doc is drafted at the bottom of this file so it can be committed on that same pass.

---

## 1. Scores

| # | Criterion | Blk | J1 | J2 | **J3** | Evidence on 0fe14d6 |
|---|---|---|---|---|---|---|
| C1 | Layering | B | 3 | 3 | **3** | `go list -deps` and Test/XTest imports: no `internal → cmd/studio`; no domain package imports `agents/llm/mcp/hub/tui`; `scripts/check-layering.sh` → `layering: ok`, in CI. |
| C2 | CLI mirrors UI | B | 1 | 2 | **3** | One `hub.Open`, one `ConnectionStates`/`TestCredential`, one `ParseScope`, one WHERE builder; the TUI now pages through `crud.Page` (`tui/tables.go:180`) and the `COUNT(*)` fallback is gone; `dupl -t 80` across `cmd`/`studio`: 0; `go-sql-literal-in-adapter`, `go-text-sniffing-in-adapter`, `go-find-connection-by-name-loop` at `error`, 0 hits. |
| C3 | One tool surface | B | 3 | 3 | **3** | Unchanged in code (`llm` adapts `agents.ToolDef`s; `mcp` registers from the registry; one `IsReadOnlyStatement`). `internal/sandbox` is a tier-definition leaf, not a second policy. **No guard rule yet** (§3, item 2). |
| C4 | Cohesion / tree | B | 1 | 1 | **3** | `internal/skills` holds store + service; `internal/llm/skills` and `agents/skills.go` gone; `engine/quote.go` gone (0 `engine.QuoteIdent` users); `queries` imports neither `engine` nor `session`; new leaves `connname`, `sandbox` documented; `docsref/tree_test.go` asserts every `internal/*` dir is in the CLAUDE.md tree (passes). Largest `agents` file is now 699 lines. |
| C5 | God files / receivers | B | 1 | 1 | **1** | Functions > 100 lines: 5, all on the §3 waiver list (`Coerce`, `unpack`, `lexStatements`, `validateDecoded`, `castExpr`). gocognit > 30 outside tests: 14, all waived (lexers, `analyzer.*`, `qualifiers`, `Coerce`, `unpack`, `checkValue`, `validateDecoded`, `usersql`); `resolveRef` was split; the linter runs at 30 with those named exclusions. `.vue` > 600 lines: 0; views > 150 script lines: 0; `check-vue-size.mjs` in CI. Bindings: 112, **all ≤ 15 top-level statements** (max 15), no `os`/`exec`/`sql`/`filestore` calls (rule at `error`). **Fails only on files:** `internal/session/session.go` 1205 (over the 1000 hard bar) and `cmd/conn.go` 825, both waived in `scripts/filesize-waivers.txt`. I do not accept those waivers (§2). |
| C6 | Interfaces | A | 2 | 2 | **2** | Unchanged. |
| C7 | Dead code | B | 1 | 2 | **3** | `check-deadcode.sh` clean (45 allow-listed: `With*`, `Mock.*`, 4 `Policy*` for docsref, `UnhardenedForTests`); `check-bindings.sh` exit 0; knip (`check:exports`) in CI; `wailsjs/go` untracked; `check-arity.mjs` in CI. |
| C8 | Errors / logging | B | 1 | 2 | **3** | `slog.Default().X(`: 0; `%v err`: 0; `== ErrX`: 0; `go-err-message-match`: 0 (was 5); all four rules at `error`; no secret-bearing log/error argument; typed `ErrConflict`, one parser in `lib/errors.ts`. |
| C9 | Tests | A | 3 | 3 | **3** | Unchanged. |
| C10 | Frontend thin views | B | 1 | 1 | **2** | `window.go` is read in exactly one file, `src/lib/bindings.ts` (rule at `error`); 0 thick views; 0 `.vue` > 600; hex outside tokens: 0; Go rules in TS: none (fingerprint is Go-owned via `useConnectionForm.ts:142-143`); `vue-tsc` blocking. Residuals (listed, below the fail bar): `outline: none` with no `:focus-visible` replacement at `TableBrowserView.vue:337` (cell editor input; its 2px border is the only focus cue) and an inline-style fallback textarea at `QueryEditorView.vue:111`. |
| C11 | Wiring | A | 3 | 3 | **3** | Unchanged. |
| C12 | Public shape | A | 1 | 3 | **3** | Only `cmd/vibe` and `studio` (both `package main`) lack a package comment (≤ 3). Lint clean; 2 TODOs. |

**Total 32 / 36** (J1 21 → J2 26 → J3 32). Blocking below 2: **C5 only.**

---

## 2. Rulings requested

**Thin-binding bar (15 statements vs the committed ≤ 25 nested).** My rubric said "≤ 15 statements" without saying top-level or nested. I rule it means **top-level statements of the method body**: counting nested statements would penalise idiomatic `if err != nil { return … }` pairs and the lock/defer prologue, which are exactly the adapter plumbing a binding is allowed to do. Measured with a `go/ast` walk: all 112 bindings are ≤ 15 top-level (max 15: `PreviewBundleImport`, `RunQuery`); 24 exceed 15 when nested statements are counted, and one (`PreviewBundleImport`, 26 by my count, 25 by the test's) sits at the committed limit. I read `RunQuery` and `ImportFile` (the two longest by lines, 42–43): lock choice, `requireWorkspace`, one `internal/` call, result shaping, an event — adapter work, no business rule. **The committed test (≤ 25 nested) plus the `thin-binding` ast-grep rule (no I/O in a binding) meets my bar; no binding needs to change.** The 25-nested cap must not be raised.

**P3 deviation (preparer map in `workspace`, not a capability on the `connections` type).** **Accepted, and preferred.** `connections` is level 3 and `workspace` level 5; a capability on the type would have needed the preparer's inputs (hub token, data dirs, read-only rules, `SessionIssue`) pushed down into `connections`, i.e. workspace concepts leaking into a leaf. A consumer-defined `preparer` interface with an unexported per-type map inside the package that orchestrates `OpenSession` is the idiomatic shape, and `OpenSession` dropped from gocognit 104 to under 30 with no per-type `switch`. The only thing I ask is that a new connection type that needs preparation fails loudly if it is missing from `defaultPreparers` (a test that every registered type with `HubTokenType`/data-dir needs has a preparer, if that is not already there).

**File-size waivers.** `scripts/filesize-waivers.txt` waives `session.go` (1205) with "a split by concern is a mechanical move that was not part of the Studio slice" and `conn.go` (825) with "over the limit by 25 lines". Both are scheduling reasons, not architectural ones, and the rubric only lets me accept a waiver whose justification I agree with. `session.go` is also over the **hard** 1000-line fail bar, which no waiver covers. The split the waiver itself calls "mechanical" is the fix: `hub` is one type and one lock, so moving its methods into `session_connect.go` (Register/Attach/Connect/connectLocked/readyForConnect/Preflight), `session_status.go` (setStatus*/Status/Connections/ConnectionInfos/Detach) and `session_query.go` (Query/Exec/Conn/pin/acquire) changes nothing but file boundaries. `conn.go` likewise: move the prompts or the `test` subcommand into their own file. The growth-cap mechanism in the waiver file is good and should stay for future use; it just should be empty on the day I rule clean.

---

## 3. Remaining items (exact, with acceptance bars)

| # | Item | Effort | Acceptance bar |
|---|---|---|---|
| 1 | **Split `internal/session/session.go` and `cmd/conn.go` under 800 lines** (mechanical, by concern, no behaviour change). Remove both lines from `scripts/filesize-waivers.txt`. | S | `scripts/check-filesize.sh` → `filesize: ok (limit 800 lines, 0 waived)`; `find . -name '*.go' ! -name '*_test.go' \| xargs wc -l \| awk '$1>800'` lists nothing but `total`; `go test ./internal/session/... ./cmd/...` green. Raises **C5 → 3** (every other C5 measure already passes with accepted waivers). |
| 2 | **Commit the C3 guard** (clean-condition §4 asks for a rule per blocking criterion; C3 has none). An ast-grep rule at `severity: error`, `go-fantasy-tool-outside-agents`: any `fantasy.AgentTool`/`fantasy.NewAgentTool`/`fantasy.NewTool` **construction** (not the adapter types in `internal/llm` that wrap an `agents.ToolDef`) outside `internal/agents` and `internal/llm`'s two adapter files; plus a second rule or a Go test asserting that every `mcp.NewTool(`/`AddTool(` in `internal/mcp` and `internal/hub` is fed from `agents.ListTools*`. Run in CI via the existing `ast-grep scan` step. | S | `ast-grep scan cmd internal studio` exit 0 with the new rule at `error`; a deliberate violation in a scratch file is caught. Satisfies clean-condition §4 for C3. |
| — | Advisory, not required: the two `outline: none` sites (C10 residual); a one-line `// Package main` on `cmd/vibe` and `studio`; the `preparer` coverage test above. | S | — |

After items 1–2: projected **C5 = 3, total 34 / 36**, all blocking ≥ 2, no criterion at 0, rules in CI, tree accurate, waivers written → **clean**.

---

## 4. Draft of the final summary (to commit as `docs/reviews/arch-2026-10/arch-2026-10-final.md` on the clean pass)

### Vibe architecture review (#54): final scores

Rubric: `RUBRIC.md` (written 2026-10-11 before any finding was read; unchanged through four passes). Judge passes: J1 (0445cc1, before cleanup), J2 (1cfac9a, after slices 1–6), J3 (0fe14d6, after slices 7–10), J4 (`<sha>`, after the file-size closeout and the C3 guard).

| # | Criterion | Blocking | J1 | J2 | J3 | J4 |
|---|---|---|---|---|---|---|
| C1 | Dependencies point inward | yes | 3 | 3 | 3 | 3 |
| C2 | CLI and Studio share logic through `internal/` | yes | 1 | 2 | 3 | 3 |
| C3 | One tool surface for agents | yes | 3 | 3 | 3 | 3 |
| C4 | Package cohesion matches the documented tree | yes | 1 | 1 | 3 | 3 |
| C5 | No god files, no god receivers | yes | 1 | 1 | 1 | 3* |
| C6 | Consumer-defined interfaces that earn their keep | no | 2 | 2 | 2 | 2 |
| C7 | No dead code | yes | 1 | 2 | 3 | 3 |
| C8 | Errors and logging conventions | yes | 1 | 2 | 3 | 3 |
| C9 | Test seams and shape | no | 3 | 3 | 3 | 3 |
| C10 | Frontend: views thin over composables/lib | yes | 1 | 1 | 2 | 2 |
| C11 | Construction and wiring at the edges | no | 3 | 3 | 3 | 3 |
| C12 | Public shape | no | 1 | 1 | 3 | 3 |
| | **Total / 36** | | **21** | **26** | **32** | **34*** |

\* projected; confirmed on the J4 pass.

**Clean condition** (RUBRIC.md): every blocking criterion ≥ 2, no criterion at 0, total ≥ 28, the guard rules committed and failing CI, CLAUDE.md tree accurate (`docsref` enforces it), every waiver written down. Met at J4.

**Waivers in force (each with its justification):**
- *Functions over 100 lines / gocognit 30* — tokenizers, single-format decoders/validators and type-mapping switches with their own table tests, where a split would hide the state machine: `agents.lexStatements`, `agents.tokensTouchSecrets`, `(*analyzer).screenTokens/walk/ddl/dml/tableFunc`, `usersql.Tokens`, `usersql.statements`, `session.qualifiers`, `notebooks.Coerce`, `bundle.unpack`, `a2ui.validateDecoded`, `a2ui.checkValue`, `importer.castExpr`. Named in `.golangci.yml` `exclusions.rules`; adding one requires a one-line reason there.
- *deadcode allow-list* (`scripts/deadcode-allow.txt`): functional-option `With*` seams; `Mock.*` test doubles; the four `agents.Policy*` lists (consumed by `internal/docsref`); `session.UnhardenedForTests` (consumed by the non-test `sessiontest` helper package). At most five "other" entries.
- *Thin bindings*: the bar is ≤ 15 top-level statements and no I/O in an exported `App` method; enforced as ≤ 25 nested statements (`studio/thin_binding_test.go`) plus the `thin-binding` ast-grep rule. The 25 cap is not to be raised.
- *P3*: `OpenSession`'s per-type preparers live in `workspace` (consumer-defined interface, unexported map) rather than on the `connections` type, because `connections` is a lower layer and must not learn workspace concepts.
- *Single string-marker parser* for `ErrConflict` in `lib/errors.ts`: the Wails bridge transports errors as strings; one central recogniser satisfies "views do not hand-roll error parsing".
- *Advisory residuals left open*: fat provider-side `session.Session`/`workspace.Workspace` interfaces (C6); two `outline: none` sites (C10); no package comment on the two `package main`s (C12).

**Guard rails in CI** (`.github/workflows/ci.yml`), all blocking:
- `scripts/check-layering.sh` — `internal/` never imports `cmd` or `studio`; intra-`internal` allow-list.
- `scripts/check-deadcode.sh` with `scripts/deadcode-allow.txt`.
- `scripts/check-bindings.sh` — every exported `App` method has a non-test frontend caller.
- `scripts/check-filesize.sh` with `scripts/filesize-waivers.txt` (growth-capped; empty at J4) — 800 lines per non-test Go file.
- `studio/thin_binding_test.go` — ≤ 25 nested statements per binding.
- `ast-grep scan cmd internal studio` with `sgconfig.yml` at the root; rules at `severity: error`: `go-err-eq-compare`, `go-errorf-err-without-w`, `go-slog-default-call`, `go-find-connection-by-name-loop`, `go-local-parse-scope`, `go-open-session-outside-hub-open`, `go-open-session-in-defaultsteps-elsewhere`, `go-path-containment-clone`, `go-sql-literal-in-adapter`, `go-text-sniffing-in-adapter`, `studio-no-dot-import`, `thin-binding`, `ts-window-go-outside-binding-layer`, and (J4) `go-fantasy-tool-outside-agents`. Report-only (`info`/`warning`): `go-context-background-in-request-path`, `go-exec-command-in-adapter`, `go-getenv-outside-config`, `go-mutable-package-var`, `go-slog-default-or-log`, `go-sql-concat`, `go-err-message-match`.
- golangci-lint with `gocognit` at 30 and the named waivers.
- `internal/docsref` tree test — every `internal/*` directory appears in the CLAUDE.md architecture tree.
- Frontend: `knip` (`check:exports`), `check-vue-size.mjs` (600 lines / 150 script lines), `vue-tsc` (blocking), `check-arity.mjs` (AppApi matches the generated bindings).

**Changing the bar later**: only by a dated entry in `RUBRIC.md`'s changelog with a written reason, never to pass a failing criterion.
