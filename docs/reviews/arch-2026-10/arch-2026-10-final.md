# Vibe architecture review (#54): final scores

**Verdict: clean**, ruled by the architecture judge (Fable) on 2026-10-12 at `0e89fbd` (slice 11, PR #75).

The rubric (`RUBRIC.md`) was written on 2026-10-11 before any finding was read and was not changed through the four judge passes. The surveyor's findings are in `FINDINGS.md`; the judge passes are `JUDGE-1.md` (0445cc1, before cleanup), `JUDGE-2.md` (1cfac9a, after slices 1–6 and the F2/F3 bug fixes), `JUDGE-3.md` (0fe14d6, after slices 7–10) and `JUDGE-4.md` (0e89fbd, after slice 11). Cleanup PRs: #58, #59 (bug fixes), #60, #61, #63, #65, #66, #68, #69, #70, #72, #73, #74, #75.

## Scores (0–3 per criterion)

| # | Criterion | Blocking | J1 | J2 | J3 | J4 |
|---|---|---|---|---|---|---|
| C1 | Dependencies point inward (`internal/` never imports `cmd` or `studio`) | yes | 3 | 3 | 3 | 3 |
| C2 | CLI and Studio share logic through `internal/` (CLI mirrors UI) | yes | 1 | 2 | 3 | 3 |
| C3 | One tool surface for agents (`internal/agents`) | yes | 3 | 3 | 3 | 3 |
| C4 | Package cohesion matches the documented tree | yes | 1 | 1 | 3 | 3 |
| C5 | No god files, no god receivers | yes | 1 | 1 | 1 | 3 |
| C6 | Interfaces are consumer-defined and earn their keep | no | 2 | 2 | 2 | 2 |
| C7 | No dead code | yes | 1 | 2 | 3 | 3 |
| C8 | Errors and logging follow the conventions | yes | 1 | 2 | 3 | 3 |
| C9 | Test seams and test shape | no | 3 | 3 | 3 | 3 |
| C10 | Frontend: views thin over composables and lib | yes | 1 | 1 | 2 | 3 |
| C11 | Construction and wiring happen at the edges | no | 3 | 3 | 3 | 3 |
| C12 | Public shape is tidy for an open-source reader | no | 1 | 1 | 3 | 3 |
| | **Total / 36** | | **21** | **26** | **32** | **34** |

**Clean condition** (from `RUBRIC.md`): every blocking criterion ≥ 2; no criterion at 0; total ≥ 28; the guard rules committed and failing CI; the CLAUDE.md architecture tree accurate; every waiver written down. All six hold at J4.

The two points left are on C6 (advisory): `session.Session` (31 methods) and `workspace.Workspace` (37) are fat provider-side interfaces with one implementation each. They are test seams with mocks behind them, and the rubric's "What I will NOT require" says not to split interfaces for their own sake. No further work is asked.

## Waivers in force

Each waiver names what it covers and why; the rubric allows a waiver only with a justification the judge accepted.

- **Functions over 100 lines or gocognit 30.** Tokenizers, single-format decoders/validators and type-mapping switches that have their own table tests, where splitting would hide the state machine: `agents.lexStatements`, `agents.tokensTouchSecrets`, `(*analyzer).screenTokens`, `(*analyzer).walk`, `(*analyzer).ddl`, `(*analyzer).dml`, `(*analyzer).tableFunc`, `usersql.Tokens`, `usersql.statements`, `session.qualifiers`, `notebooks.Coerce`, `bundle.unpack`, `a2ui.validateDecoded`, `a2ui.checkValue`, `importer.castExpr`. Named in `.golangci.yml` under `exclusions.rules`; adding one needs a one-line reason there. Every other function is under both bars.
- **deadcode allow-list** (`scripts/deadcode-allow.txt`): functional-option `With*` seams; `Mock.*` test doubles; the four `agents.Policy*` lists consumed by `internal/docsref`; `session.UnhardenedForTests`, consumed by the non-test `session/sessiontest` helper package. The "other" class is capped at five entries.
- **Thin bindings.** The rubric's bar is ≤ 15 top-level statements and no I/O in an exported `App` method (a binding may do adapter plumbing: locks, `requireWorkspace`, one `internal/` call, result shaping, an event). Enforced as ≤ 25 nested statements (`studio/thin_binding_test.go`) plus the `thin-binding` ast-grep rule that forbids `os`, `exec`, `database/sql` and `filestore` I/O in a binding. The 25 cap is not to be raised.
- **File size.** 800 lines per non-test Go file, 600 lines / 150 script lines per `.vue`. `scripts/filesize-waivers.txt` is a growth-capped waiver mechanism for future use; it is empty as of J4.
- **P3, `OpenSession` preparers.** The per-connection-type preparation step lives in `internal/workspace` as a consumer-defined `preparer` interface with an unexported map, not as a capability on the `connections` type, because `connections` is a lower layer and must not learn workspace concepts (hub token, data dirs, read-only rules, `SessionIssue`). Covered by `session_prepare_test.go`.
- **Conflict errors across the Wails bridge.** The bridge transports errors as strings, so `filestore.ErrConflict` is recognised by one central function in `src/lib/errors.ts`; views call it and never match the marker themselves.
- **`cmd/` globals.** Cobra command and flag variables in `cmd/root.go` and the command files are the wiring edge where services are created; they are not package-level singletons in `internal/`.

## Guard rails in CI

All of these run in `.github/workflows/ci.yml` and fail the PR.

| Guard | Protects | Where |
|---|---|---|
| `scripts/check-layering.sh` | C1: `internal/` never imports `cmd` or `studio`; the intra-`internal` allow-list | CI "Layering" |
| `scripts/check-deadcode.sh` + `scripts/deadcode-allow.txt` | C7 | CI "deadcode" |
| `scripts/check-bindings.sh` | C7: every exported `App` method has a non-test frontend caller | CI "Wails binding usage" |
| `scripts/check-filesize.sh` + `scripts/filesize-waivers.txt` | C5: 800 lines per non-test Go file | CI "File size" |
| `studio/thin_binding_test.go` | C5: ≤ 25 nested statements per binding | CI "Thin bindings" / `go test` |
| `internal/mcp/tool_surface_test.go` | C3: every MCP tool is built from `agents.ListTools*` | `go test` |
| `internal/docsref` (incl. `tree_test.go`) | C4: every `internal/*` directory is in the CLAUDE.md tree; docs match code | `go test` |
| `ast-grep scan -c tools/ast-grep/sgconfig.yml cmd internal studio` (rules in `tools/ast-grep/rules/`) at `severity: error` | C2 `go-sql-literal-in-adapter`, `go-text-sniffing-in-adapter`, `go-find-connection-by-name-loop`, `go-local-parse-scope`, `go-open-session-outside-hub-open`, `go-open-session-in-defaultsteps-elsewhere`; C3 `go-fantasy-tool-outside-agents`; C5 `thin-binding`, `studio-no-dot-import`; C8 `go-err-eq-compare`, `go-errorf-err-without-w`, `go-slog-default-call`; D2' `go-path-containment-clone`; C10 `ts-window-go-outside-binding-layer` | CI "ast-grep rules" |
| ast-grep rules at `info`/`warning` (report-only) | `go-context-background-in-request-path`, `go-exec-command-in-adapter`, `go-getenv-outside-config`, `go-mutable-package-var`, `go-slog-default-or-log`, `go-sql-concat`, `go-err-message-match` | same step, non-blocking |
| golangci-lint with `gocognit` at 30 and the named waivers | C5 | CI "golangci-lint" |
| `knip` (`npm run check:exports`) | C7: no unused frontend exports | CI "Frontend unused exports" |
| `scripts/check-vue-size.mjs` (`npm run check:vue-size`) | C5/C10: 600 lines / 150 script lines per `.vue` | CI "Frontend view size" |
| `vue-tsc --noEmit` (blocking) | C10: typed bindings | CI "typecheck" |
| `scripts/check-arity.mjs` | C10: `AppApi` matches the generated Wails bindings | CI "AppApi matches the generated bindings" |

## Changing the bar

`RUBRIC.md` may change only by a dated entry in its changelog with a written reason, and never to pass a failing criterion. A new waiver needs its justification written next to the mechanism that grants it (the golangci exclusion, the deadcode allow-list, the filesize waiver file).
