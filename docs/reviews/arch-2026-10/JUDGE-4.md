# Judge pass 4 — SandwichLabs/vibe @ 0e89fbd (main, after slice 11)

**Judge:** Fable. **Date:** 2026-10-12. **Rubric:** unchanged through all four passes. **Measured on:** the `judge-main` worktree at 0e89fbd with `studio/frontend/dist` built. PR #75's checks passed; the `main` push run was in progress when I measured.

## Verdict: **CLEAN. 34 / 36.**

Every clean-condition clause holds on this commit:

1. Every blocking criterion (C1, C2, C3, C4, C5, C7, C8, C10) scores ≥ 2. ✓
2. No criterion scores 0. ✓
3. Total 34 ≥ 28. ✓
4. The guard rules are committed and fail CI: `check-layering.sh`, `check-deadcode.sh`, `check-bindings.sh`, `check-filesize.sh`, `thin_binding_test.go`, `tool_surface_test.go`, 14 ast-grep rules at `error`, gocognit at 30 in golangci, the docsref tree test, knip, `check-vue-size.mjs`, blocking `vue-tsc`, `check-arity.mjs`. ✓
5. CLAUDE.md's architecture tree matches `ls internal` (`internal/docsref` passes). ✓
6. Every waiver is written down with its justification (final summary, "Waivers in force"). ✓

## Verification of the two items

| Item | Claim | Verified |
|---|---|---|
| 1 File split | `session.go` 1205 → 512 (+`session_connect.go` 492, `session_status.go` 227); `conn.go` 825 → 610 (+`conn_prompt.go` 97, `conn_creds.go` 140); pure moves; waiver file empty | Line counts as claimed; `scripts/check-filesize.sh` → `filesize: ok (limit 800 lines, 0 waived)`; no non-test Go file over 800; the diff is 956 insertions / 908 deletions across 6 files, consistent with a move plus file headers; `go test ./internal/session/... ./cmd/...` green via the full suite in CI. I did not re-run the go/ast declaration comparison myself; the line arithmetic and green tests are enough for a pure move. |
| 2 C3 guard | `go-fantasy-tool-outside-agents` at `error`; `internal/mcp/tool_surface_test.go` | Rule present at `severity: error`; `ast-grep scan cmd internal studio` exit 0. A scratch file in `internal/hub` constructing `fantasy.NewAgentTool` was caught (1 hit) and removed. `TestMCPToolsComeFromTheAgentsRegistry` exists and `go test ./internal/mcp` passes. |
| Advisories | focus ring on the fallback textarea; `outline: none` sites gone; main package docs; preparer test | 0 `outline: none` outside `bridge.css`/`schoolhouse`/`connections.css`; `cmd/vibe` and `studio` have `Command …` package docs (I grepped for the wrong prefix in J3; the claim stands); `TestWithPreparerReplacesTheStepForAType` and `TestMotherDuckPreparerNeverAttachesReadOnly` exist. |
| Watcher test | debounce window 40 → 300 ms | Diff confirmed: the test uses `ws.Watch(WithDebounce(300ms))` with a comment on CI stalls; production code untouched. Acceptable; it is a test-timing fix, not a behaviour change. |

## Scores

| # | Criterion | Blk | J1 | J2 | J3 | **J4** | Evidence on 0e89fbd |
|---|---|---|---|---|---|---|---|
| C1 | Layering | B | 3 | 3 | 3 | **3** | `check-layering.sh` ok; `go list` outer and test-import checks empty. |
| C2 | CLI mirrors UI | B | 1 | 2 | 3 | **3** | Unchanged from J3. |
| C3 | One tool surface | B | 3 | 3 | 3 | **3** | Code unchanged; now guarded by the rule and the MCP test. |
| C4 | Cohesion / tree | B | 1 | 1 | 3 | **3** | docsref tree test passes; package set unchanged from J3. |
| C5 | God files / receivers | B | 1 | 1 | 1 | **3** | No non-test Go file > 800 (0 waived); no `.vue` > 600; functions > 100 lines: 5, all waived; gocognit > 30 outside tests: 13, all on the waiver list (`Coerce` dropped under 30); 112 bindings ≤ 15 top-level statements, no I/O. |
| C6 | Interfaces | A | 2 | 2 | 2 | **2** | Unchanged (fat `session.Session`/`workspace.Workspace`, advisory). |
| C7 | Dead code | B | 1 | 2 | 3 | **3** | deadcode clean (45 allow-listed), bindings ok, knip in CI. |
| C8 | Errors / logging | B | 1 | 2 | 3 | **3** | All four rules at `error`, 0 hits; lint 0 issues. |
| C9 | Tests | A | 3 | 3 | 3 | **3** | Unchanged. |
| C10 | Frontend thin views | B | 1 | 1 | 2 | **3** | `window.go` read only in `lib/bindings.ts`; 0 thick views; 0 `.vue` > 600; 0 hex; 0 Go rules in TS; the two `outline: none` residuals are gone. |
| C11 | Wiring | A | 3 | 3 | 3 | **3** | Unchanged. |
| C12 | Public shape | A | 1 | 3 | 3 | **3** | Every package documented; lint clean. |

**Total 34 / 36** (21 → 26 → 32 → 34). Two advisory points left on C6, which I would not spend effort on (see "What I will NOT require" in the rubric: a fat interface with a mock behind it is a test seam, not a defect).

## Files for `docs/reviews/arch-2026-10/`

`RUBRIC.md`, `JUDGE-1.md`, `JUDGE-2.md`, `JUDGE-3.md`, `JUDGE-4.md`, `arch-2026-10-final.md`. (`FINDINGS.md` is the surveyor's and is already there.) In the judge files, the only scratch references are the `$A/…` paths in the headers and the "Files for" line in JUDGE-1; JUDGE-3 §4's draft summary is superseded by `arch-2026-10-final.md` and can be dropped from the committed copy or left as history.
