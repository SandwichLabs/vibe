# Architecture rubric for SandwichLabs/vibe (#54)

**Judge:** Fable (architecture judge). **Written:** 2026-10-11, round 0, before any findings were read.
**Scope:** the Go module `github.com/SandwichLabs/vibe` (`cmd/`, `cmd/vibe/`, `internal/...`, `studio/`) and the Vue frontend in `studio/frontend/src/`.
**Inputs used to write this:** `CLAUDE.md`, `docs/direction.md`, `docs/README.md`, `go list ./...`, the directory tree, `.golangci.yml`, the text of issue #54. No source file was audited. Package line counts were glanced at only to know the order of magnitude of the codebase (roughly 50k Go LOC, ~30 packages); no threshold below was derived from them.

The bar is **clean, idiomatic Go** for a pre-1.0 desktop app written by one person. Idiomatic Go includes simplicity: a flat `internal/` of cohesive packages, concrete types by default, small interfaces at the consumer, errors wrapped with context, and no ceremony that does not pay for itself in this codebase.

---

## How to read a criterion

Each criterion has: what clean means **here**; how to measure it (objective where possible; every measure must be reproducible by a command or an ast-grep rule the surveyor commits under `docs/reviews/arch-2026-10/rules/`); the pass/fail threshold; a score 0–3; and whether it is **blocking** or **advisory**.

Scoring, uniform across criteria:

| Score | Meaning |
|---|---|
| **3** | Passes the threshold with no exceptions, or every exception has a one-line justification in the findings doc that I accept. |
| **2** | Passes the threshold; residual items are below the fail bar and each is listed. |
| **1** | Fails the threshold, but the remaining work is enumerated, bounded, and no single item changes a public boundary. |
| **0** | Fails, and the failure is structural (a boundary is wrong, not merely a file too long). |

Thresholds are stated as **pass** (score ≥ 2) and **fail** (score ≤ 1). When a measure needs a tool that is not installed (`dupl`, `deadcode`, `gocognit`), the surveyor installs it via `go run <module>@<pinned version>`; "I couldn't run it" is not a pass.

---

## The criteria

### C1. Dependencies point inward (layering) — BLOCKING

**Clean here.** `cmd/`, `cmd/vibe/` and `studio/` are adapters over `internal/`. Nothing under `internal/` imports `cmd`, `cmd/vibe` or `studio`. `cmd/` does not import `studio`, and `studio` does not import `cmd`, except for one explicitly allowed edge: the `studio` subcommand in `cmd/` may launch the Wails app (that is the only `cmd → studio` import allowed, and it must be a single call site). Within `internal/`, there is no import cycle (the compiler guarantees that), and the dependency direction matches the architecture tree in `CLAUDE.md`:

- Leaf-ish utility packages (`filestore`, `filelock`, `storageurl`, `jsonenc`, `dotenv`, `usersql`, `config`, `prefs`, `templates`, `secrets`, `a2ui`) import nothing else in `internal/` except each other and, where justified, `workspace` for types.
- Domain packages (`workspace`, `connections`, `session`, `engine`, `crud`, `importer`, `notebooks`, `queries`, `bundle`, `llm/skills`) do not import the agent/MCP/UI layer (`agents`, `llm`, `mcp`, `hub`, `tui`).
- `agents` is the **one tool surface**; `llm` and `mcp` consume `agents`. `agents` never imports `llm` or `mcp`. `hub` may import `mcp` (it decides who serves) but `mcp` never imports `hub`'s process-ownership logic back in (if it does, the two are one package and should be merged or re-split).
- `tui` is an adapter like `studio`: it imports domain packages, is imported only by `cmd`.

**Measure.** `go list -deps -f '{{.ImportPath}}' ./internal/... | grep -E 'vibe/(cmd|studio)'` must be empty. For the intra-`internal` rules, a committed script (`rules/layering.sh`) that runs `go list -f '{{.ImportPath}} {{join .Imports " "}}' ./...` and asserts the allow-list above. The allow-list file is part of the deliverable and runs in CI.

**Pass:** zero violations of the outer rule; ≤ 2 intra-`internal` violations each with a written justification I accept. **Fail:** any `internal → cmd|studio` import, or ≥ 3 intra-`internal` violations, or a violation that the justification papers over ("it was convenient").

### C2. The CLI and Studio share logic through `internal/` (CLI mirrors UI) — BLOCKING

**Clean here.** Every operation that both `vibe <cmd>` and a Studio binding perform (open workspace, init, import, export, bundle in/out, connection add/test/list, config llm, mcp config, query) is implemented **once** in an `internal/` package; `cmd/` and `studio/` each do argument/flag/JSON marshalling plus a call. Neither adapter contains a business rule the other has to re-implement (validation of a connection definition, bundle encryption choice, `.env` allow/deny semantics, the three-gate write rule, redaction of secrets, etc.).

**Measure.**
1. `dupl -t 60 ./cmd ./studio` (token threshold 60) across the two adapters: report every clone pair whose two halves are in different adapters.
2. An ast-grep rule (`rules/adapter-logic.yml`) that flags, in `cmd/*.go` and `studio/*.go` (non-test), function bodies longer than **40 statements** that contain no call into `internal/` — these are adapters doing work that belongs below them.
3. The surveyor lists each pair (CLI command, Studio binding) that performs the same user-visible operation and names the shared `internal/` function. A pair with no shared function is a violation.

**Pass:** zero cross-adapter dupl clones ≥ 60 tokens; zero paired operations without a shared `internal/` implementation; ≤ 3 adapter functions over the 40-statement bar. **Fail:** any business rule implemented twice, or ≥ 4 oversized adapter functions.

### C3. One tool surface for agents — BLOCKING

**Clean here.** Every capability the agent has is a `ToolDef` registered in `internal/agents`. The in-app runner (`internal/llm`) and the MCP server (`internal/mcp`) both consume the same registry and apply the same gating (tier, writable connection, approval). There are no bespoke `fantasy` tools in `internal/llm`, no MCP-only tools in `internal/mcp` that bypass the registry, and no Studio binding that gives the agent a capability the registry does not know about. The SQL allow-list policy is evaluated in one place.

**Measure.**
- ast-grep: any call that constructs a `fantasy` tool (`fantasy.NewTool`, `fantasy.Tool{`, `agent.WithTools(` with an inline literal, or whatever the library's constructor is) outside `internal/agents` → violation.
- ast-grep: any `mcp` tool registration (`server.AddTool`, `AddTool(`, `mcp.NewTool(`) in `internal/mcp` or `internal/hub` whose handler does not dispatch through the `agents` registry/handler → violation.
- grep for a second SQL policy entry point: more than one function that decides "is this statement allowed" (names like `Allow`, `Policy`, `CheckSQL`, `isReadOnly`) outside `internal/agents` → violation.

**Pass:** zero violations. **Fail:** any violation. This is a trust property the product documents; it has no soft threshold.

### C4. Package cohesion matches the documented tree — BLOCKING

**Clean here.** Each `internal/` package has the one responsibility `CLAUDE.md`'s architecture tree gives it, and the tree is accurate. No package is a grab-bag (`util`, `common`, `helpers`, `misc`) and no responsibility lives in two packages (e.g. workspace discovery both in `workspace` and `cmd`; connection attach SQL both in `connections` and `session`; secret redaction in three places). Tiny packages that exist only to break a cycle are suspicious; the cycle usually means two packages are one.

**Measure.**
- For every package, the surveyor writes a one-sentence responsibility and lists exported symbols that do not serve it ("strays"). A package with > 3 strays or no single-sentence responsibility fails.
- Any package named `util|common|helpers|misc|shared` fails outright.
- The `CLAUDE.md` tree must list every package that exists and no package that does not (`internal/docsref` already enforces part of this; extend it or add a test).
- `go list -f '{{.ImportPath}} {{len .GoFiles}}' ./internal/...`: a package with exactly 1 non-test file and ≤ 50 LOC must justify its existence (a cycle-breaker is a smell; a genuinely reusable leaf such as `jsonenc` is fine if ≥ 2 consumers import it).

**Pass:** every package has a one-sentence responsibility, ≤ 3 strays each, tree accurate. **Fail:** any grab-bag package, any responsibility in two packages, or the tree out of date.

### C5. No god files, no god receivers — BLOCKING

**Clean here.** Go: no non-test, non-generated file over **800 lines**; no function over **100 lines** or with `gocognit > 30`; no single receiver type with more than **40 methods**, with one named exception: the Wails `App` struct in `studio/` is the binding receiver Wails requires, so it may carry many methods **provided** each binding method is a thin delegate (≤ 15 statements, no business logic, no SQL text, no file I/O other than via an `internal/` call) and the methods are grouped into topical files (`app_notebooks.go`, `app_queries.go`, …) each under the 800-line bar. Frontend: no `.vue` file over **600 lines** (template + script + style) and no `.ts` file over **500 lines**, generated `wailsjs/` excluded.

**Measure.** `wc -l` with the exclusions above; `gocognit -over 30 ./...`; a small Go test or script that counts methods per receiver (`go/ast` walk; committed under `rules/`); ast-grep rule `rules/thin-binding.yml` that flags `App` methods with > 15 statements or containing a string literal matching `(?i)^\s*(select|insert|update|delete|create|attach)\b`.

**Pass:** zero files over the size bars; ≤ 3 functions over 100 lines / gocognit 30, each named; `App` methods all thin. **Fail:** any file > 1000 lines; any `App` binding that contains business logic; > 3 oversized functions.

### C6. Interfaces are consumer-defined and earn their keep — ADVISORY

**Clean here.** An interface is declared in the package that **calls** it, is as small as that caller needs (1–4 methods typical), and exists for a reason: either a real second implementation ships, or it is a test seam that a test actually uses. Provider-side "interface for my own struct" with one implementation and no mock using it is noise and should be a concrete type. `mock.go` files exist only where a test imports them. Functional options are used for constructors with ≥ 3 optional knobs, not for everything.

**Measure.** For every exported interface in `internal/` and `studio/`: list (a) the package that declares it, (b) packages/types that implement it, (c) call sites that take it as a parameter or field. ast-grep can inventory declarations; a `go/types` script or `grep` finds implementers. An interface with exactly one implementer and no `_test.go` or `mock.go` user is a **vestigial interface**. An interface declared in the provider's package but used only from one consumer package is **misplaced** (the fix is to move it, unless two consumers share it — then a provider-side or neutral home is fine and idiomatic).

**Pass:** ≤ 5 vestigial interfaces, ≤ 5 misplaced interfaces, each listed. **Fail:** > 10 of either. (Advisory: I will not block the release on this, but a 0 score here combined with a 1 on C4 is a sign the boundaries are being faked with interfaces.)

### C7. No dead code — BLOCKING

**Clean here.** No unreachable Go functions, types or methods; no unused exported symbols that exist "for the future"; no Wails binding without a frontend caller; no frontend composable, lib function or component without an importer; no hidden-stub CLI commands beyond the documented container stubs (`info`, `verify`, `compact`, `rekey`, `dump`, `passphrase`), and those stubs are each ≤ 10 lines; no `.golangci.yml` exclusion paths that no longer exist (`cmd/vibe-viewer/frontend` is already a candidate); no leftover references to removed subsystems (`container`, `pages`, SQLCipher, forms, App Mode, macros) outside `docs/archive/`.

**Measure.**
- `deadcode -test ./...` (golang.org/x/tools/cmd/deadcode): every reported function is either removed or justified (e.g. reflect-reached Wails bindings are **not** justified by that alone: see next bullet).
- Binding cross-check: list every exported method on the Wails `App` receiver, and grep `studio/frontend/src` (excluding `wailsjs/`) for `App.<Name>(` or `<Name>(` imported from `wailsjs`. A binding with zero frontend callers and zero test-only reason is dead.
- Frontend: `npx knip` or `ts-prune`-equivalent (or an ast-grep rule listing exports plus a grep for imports) over `src/composables`, `src/lib`, `src/components`, `src/a2ui`.
- `grep -rnE 'container|SQLCipher|sqlcipher|MigratePages|App Mode|macro' --include='*.go' --include='*.ts' --include='*.vue' --include='*.md' . | grep -v docs/archive | grep -v _test.go` reviewed by hand; mentions in doc prose that explain the removal are fine, code references are not.

**Pass:** `deadcode` clean except an allow-list of ≤ 5 justified items; zero dead bindings; zero dead frontend exports; zero code references to removed subsystems. **Fail:** any dead binding, > 5 deadcode items, or any removed-subsystem code path still compiled.

### C8. Errors and logging follow the conventions — BLOCKING

**Clean here.** Errors are wrapped with context and `%w` (`fmt.Errorf("failed to open workspace: %w", err)`); errors are not swallowed (`_ = f()` on something that can fail is a violation unless commented); sentinel errors and `errors.Is/As` are used where callers branch on them; no `panic` on user-reachable paths; no `log.Printf`/`fmt.Println` as logging — `log/slog` only, injected (`*slog.Logger` passed in, never a package-level global logger or `slog.Default()` outside `main`/`cmd` wiring); no credential or DSN can reach a log line, an error string, a UI toast or the agent's context (the write-only trust rule). The frontend has one error path (`lib/errors.ts`) and views do not hand-roll error parsing.

**Measure.**
- ast-grep rule `rules/err-wrap.yml`: `fmt.Errorf` with `%v` of an `err` identifier, or a `return err` that crosses a package boundary from an exported function without wrapping, in `internal/` — count. (Bare `return err` inside the same package's private helpers is fine and idiomatic; the rule targets exported functions returning a foreign package's error unwrapped.)
- grep: `log\.(Print|Fatal|Panic)|fmt\.Print(ln|f)?\(` in non-`cmd`, non-`tui` Go code; `slog\.(Info|Warn|Error|Debug)\(` (package-level default logger) outside `cmd/vibe/main.go` and `cmd/`; `slog.Default()` outside `main`/`cmd`.
- grep for `panic(` outside tests and `init`-time registry validation.
- Secret hygiene: an ast-grep/grep inventory of every log call and `fmt.Errorf` whose arguments include an identifier matching `(?i)(password|token|secret|key|dsn|credential)`; each must either be a redacted/typed value or be removed. This overlaps the security review; here it is scored as an architecture property (redaction lives in one place, `internal/secrets`, and everyone goes through it).

**Pass:** ≤ 10 unwrapped boundary errors, zero package-level logging, zero stray `fmt.Print` in library code, zero secret-bearing log/error arguments. **Fail:** any secret-bearing log/error argument, or package-level logging in `internal/`, or > 25 unwrapped boundary errors.

### C9. Test seams and test shape — ADVISORY

**Clean here.** Tests are co-located, use `testing` + testify, use DuckDB in `t.TempDir()`, `FileStore`s or the store `Mock`s (no SQLite, no network). Every `internal/` package with logic has tests; `studio/` tests exercise bindings against real `internal/` code, not against each other. There are no test-only hooks exported from production code (`SetXForTest` is acceptable only in an `export_test.go`). Test helpers with ≥ 2 consumers live in a `*test` package (`session/sessiontest` is the existing pattern) rather than copy-pasted. The Vitest suite is supplementary; Rodney is the acceptance gate for Studio-facing changes, and the rubric does not count Vitest coverage as architecture.

**Measure.** `go list -f '{{.ImportPath}} {{len .TestGoFiles}}' ./internal/...` — packages with zero test files listed and justified (pure type packages or ≤ 50 LOC leaves are fine). `dupl -t 80` over `*_test.go` to find copy-pasted fixtures with ≥ 3 copies. grep for exported `ForTest|ForTesting` identifiers in non-`_test.go` files.

**Pass:** every package with > 100 LOC has tests; ≤ 3 fixture clone groups; zero production test hooks. **Fail:** > 3 packages > 100 LOC without tests, or production test hooks.

### C10. Frontend: views are thin over composables and lib — BLOCKING

**Clean here.** A view (`src/views/*.vue`) composes components and calls composables; it does not contain data-fetching logic, Wails calls, or business rules inline. Wails bindings are called from `src/lib/wails.ts` and composables only (one import surface), never directly from a `.vue` file. Components under `src/components/` are presentational or own one interaction; shared state lives in composables. Pure logic (merge, format, parse) lives in `src/lib/*.ts` with a `.spec.ts`. CSS follows the theming rule (no hex in stylesheets; `--vibe-*` or bridged `--sh-*`; `:focus-visible` ring owned by `theme/bridge.css`) — the theme rule is a convention check, scored here because it is a boundary (model output and ad-hoc colors never reach a style attribute).

**Measure.**
- ast-grep rule `rules/no-direct-wails-in-vue.yml`: any import from `wailsjs/` or any `window.go.` reference inside a `.vue` file → violation.
- ast-grep/grep: a `.vue` `<script>` block with > 150 lines of script, or > 3 `async` functions, is a thick view: list them.
- `grep -rnE '#[0-9a-fA-F]{3,8}\b' src --include='*.vue' --include='*.css'` excluding `theme/` and `schoolhouse/` tokens → violation count; `grep -rn 'outline: *none' src` without an adjacent `:focus-visible` rule → violation.
- Duplicated logic between TS and Go: the only Go↔TS shared shapes are the generated `wailsjs/go/models.ts` types. Any hand-maintained TS that re-implements a Go rule (SQL classification, connection validation, a2ui validation beyond "is it JSON", bundle format rules, `.env` allow/deny semantics) is a violation; the TS must call the binding instead. The surveyor lists candidate pairs by function name and purpose.

**Pass:** zero direct Wails calls in `.vue`; ≤ 3 thick views each listed; zero hex outside tokens; zero duplicated Go rules in TS. **Fail:** any duplicated Go rule in TS, or > 3 direct Wails calls in views, or any hex in a non-token stylesheet.

### C11. Construction and wiring happen at the edges — ADVISORY

**Clean here.** Services are constructed in `cmd/vibe/main.go`, `cmd/`, or `studio/main.go`/`app.go` startup, and passed down. No package-level mutable singletons (`var defaultX = New()` then used from everywhere), no `init()` that does I/O, no global registries mutated after startup except the deliberate `agents` tool registry and the `connections` type registry (both of which must be populated at startup, then read-only). Viper is read in `cmd/` and `internal/config`, not scattered through `internal/`.

**Measure.** grep `^var .* = New|^var .*Registry|^func init\(` across `internal/` and classify; grep `viper\.` outside `cmd/` and `internal/config`. ast-grep rule for `init()` bodies containing a call into `os.`, `net.`, or `sql.`.

**Pass:** ≤ 3 package-level mutable singletons, each justified; `viper` confined. **Fail:** > 5 singletons or `viper` reads in domain packages.

### C12. Public shape is tidy for an open-source reader — ADVISORY

**Clean here.** A new contributor can map `CLAUDE.md` → tree → packages in one read. Each `internal/` package has a package doc comment (one `// Package x ...` line is enough). Exported identifiers have doc comments where `golint`-era rules would ask for them on the main types and constructors (not on every method). No `TODO|FIXME|XXX|HACK` that references a closed issue or a removed subsystem. `go vet` and the committed `golangci-lint` config are clean, and the config has no stale paths.

**Measure.** `go list -f '{{.ImportPath}} {{.Doc}}' ./internal/...` lists packages with empty `Doc`. `grep -rnE 'TODO|FIXME|XXX|HACK'` classified. `golangci-lint run` zero issues.

**Pass:** ≤ 3 undocumented packages; lint clean; stale TODOs removed. **Fail:** lint not clean on `main`.

---

## The "clean" condition (exact)

I will declare the architecture **clean** when, on a single commit on `main` with CI green, **all** of the following hold:

1. **Every blocking criterion (C1, C2, C3, C4, C5, C7, C8, C10) scores ≥ 2.**
2. **No criterion scores 0**, blocking or advisory.
3. **The sum over all 12 criteria is ≥ 28 of 36** (mean ≥ 2.33).
4. **The measurement rules are committed and run in CI**: the layering allow-list (C1), the ast-grep rules for C2, C3, C5, C8 and C10, and the binding cross-check (C7). A rule that only lives in a findings doc does not count. "Run in CI" means a failing rule fails the PR.
5. **`CLAUDE.md`'s architecture tree matches the result** and `internal/docsref` passes.
6. **Every item I waived** in reaching a score is written in the findings doc with the one-line justification, so a later reader can see the bar was not lowered silently.

If any one of these is false the verdict is **not clean**, and the round's output is the ordered list of slices needed to make it true.

---

## What I will NOT require

This is a pre-1.0 desktop app by one person. The following are over-engineering here and I will reject findings that propose them, or score them neutrally at best:

- **Hexagonal / clean-architecture ceremony**: `domain/`, `usecase/`, `ports/`, `adapters/` directories; a "repository" interface in front of every store; DTO types that mirror domain types one-for-one. Flat `internal/` with cohesive packages is the idiom.
- **Interfaces with a single implementation that are not test seams.** C6 counts these as noise, not as cleanliness. Concrete types are fine.
- **Package splits for their own sake.** A 3,000-line package with one responsibility is fine. Splitting `workspace` into six packages because it is "big" is not a finding unless it has more than one responsibility (C4).
- **Dependency-injection frameworks**, wire/fx/dig. Constructor arguments and functional options are enough.
- **A mock for every interface.** Only where a test uses it.
- **Microservice-style boundaries inside one binary**: event buses, message types between packages, "modules" with their own `main`.
- **Generics for their own sake**, or removing a bit of repetition when the generic version is harder to read.
- **Blanket doc comments on every exported method**, exhaustive `godoc` polish, or `stylecheck`/`revive` style passes on existing code. Package-level docs and main types are enough (C12).
- **A Vitest coverage number.** Rodney is the acceptance gate; specs are supplementary.
- **Rewriting `studio/app.go`'s binding receiver into many structs.** Wails binds a struct; one `App` with thin methods in topical files is the right shape (C5). Finding "125 bindings" by itself is not a finding; "bindings contain logic" is.
- **Removing the TUI, the `legacy.go` stubs, or Viper** on taste. They are documented product decisions.
- **Perfect dupl zero.** Two similar 30-token blocks in two Cobra commands is normal Go.
- **Renaming for consistency's sake** when the names already follow the tree.

---

## How this rubric may change

- The rubric may change **only** with a written justification appended to a "Changelog" section at the bottom of this file, dated, naming the criterion and the reason.
- Acceptable reasons: a measure turned out to be un-runnable or to measure the wrong thing (then the measure changes, not the bar); a criterion was ambiguous and two readers scored it differently (then it is clarified in the direction that is stricter or equally strict); the product's documented intent changed (`docs/direction.md` or `CLAUDE.md` changed first, with an owner decision).
- **Never** acceptable: loosening a threshold, demoting a blocking criterion to advisory, or changing the "clean" condition **in order to pass a criterion that is currently failing**. If a failing criterion is genuinely wrong for this codebase, the change is made in a round where that criterion is not the deciding one, and the justification says why it would have been wrong even if the code passed.
- The waived-items list (clean condition §6) is the only per-finding flexibility, and every waiver is itself written down.

## Changelog

- 2026-10-11 — round 0. Rubric written before any findings were read.
