# Code mode: proposal

Status: **proposal**, not built; the engine choice is now part of the compute decision spike #248 (2026-10-26); reviewed 2026-10-16 (§13: spike Starlark and SQL result handles before committing to Monty). The research is in [spike-code-mode-findings.md](spike-code-mode-findings.md). This document says how code mode works in Assay and which rules it must obey. It names the code that it changes.

This document uses ASD-STE100 Simplified Technical English. Descriptive sentences have a maximum of 25 words. Instructions have a maximum of 20 words. Function names, file names and code are in `code` font and are technical names.

## 1. Summary

Code mode adds one agent tool, `run_code`. The model writes a short Python program. A Monty sandbox runs the program in a separate worker process. The program calls the existing Assay tools as functions of an `assay` module. Each function call goes through `agents.Handler.Handle`, the same path as a direct tool call. Thus every gate, approval and redaction stays the same.

Only the final value of the program goes back to the model. Query rows, schemas and intermediate results stay in the sandbox.

Code mode is optional. It is off by default. When it is off, or when the sandbox is not available, the agent works as it does now.

## 2. Why Assay needs it

Assay has approximately 30 tools. The tool definitions are not a large cost. The large costs are:

1. **Rows in the context.** `run_query` returns up to 15,000 characters of rows on each call. A comparison over three connections puts three results into the context.
2. **Model turns.** A chat turn has a maximum of 8 tool steps (`maxSteps` in `internal/llm/runner.go`). A loop over 20 tables needs 20 steps.
3. **Arithmetic in the model.** The model must join, sort and count rows that it reads as text. The grounding guardrail (#70) exists because the model sometimes gets this wrong.

In code mode, the program does the loop, the join and the arithmetic. The model reads one small result.

**Example: the model writes this program for "Which connections have tables with no rows?"**

```python
import assay

empty = []
for conn in assay.list_connections()["connections"]:
    if conn["status"] not in ("ok", "not-connected"):
        continue
    # introspect_schema returns names that run_query can use as they are.
    for table in assay.introspect_schema(connection=conn["name"])["tables"]:
        rows = assay.run_query(sql="SELECT count(*) AS n FROM " + table)["rows"]
        if rows[0]["n"] == 0:
            empty.append(table)
{"empty_tables": empty, "count": len(empty)}
```

Without code mode, this question needs one tool step for each table.

## 3. The rules

Code mode must obey all the Assay design principles. These rules are mandatory.

| # | Rule | How code mode obeys it |
|---|---|---|
| C1 | **One tool surface.** | `run_code` is a `ToolDef` in `internal/agents`. The `assay` module functions are made from `ListToolsForMode`. There is no second list of functions. |
| C2 | **Same gates.** | Each `assay.<tool>()` call is one `agents.Request` to `Handler.Handle`. The tier, the writable connection, the approval, the SQL policy and the table allow-list apply to each call. |
| C3 | **Credentials stay out.** | The worker starts with an empty environment. No host function returns a credential. The sandbox has no file, network or environment access. |
| C4 | **Read-only until asked.** | A write in a program needs the same three gates as a direct write. The person sees an approval card for each gated write. |
| C5 | **Everything works without AI, and without code mode.** | When `run_code` is not available, the agent uses the normal tools. No other feature needs code mode. |
| C6 | **Data stays where it lives.** | The sandbox is local. No program, row or snapshot goes to a remote service. |
| C7 | **The person can see what happened.** | Studio shows each inner tool call as a step under the `run_code` call. The audit log records each inner call. |

## 4. Architecture

```
model ──run_code(code)──▶ agents.Handler.Handle            (gates for run_code itself)
                              │
                              ▼
                       codemode.Runner ──────────────▶ monty worker process
                              ▲      typed frames         (no env, no files,
                              │      over stdio            no network)
                              │
          assay.run_query(...) │ host-function suspension
                              ▼
                     agents.Handler.Handle              (gates for each inner call)
                              │
                              ▼
                     session / DuckDB hub ── credentials are attached here only
```

### 4.1 New package `internal/codemode`

The package has three parts:

- **`Engine`**: an interface. It runs one program with a `Host` and limits, and returns a `Result`. The first implementation uses `internal/montyclient`, a Go client for the Monty worker that Assay owns ([monty-client.md](monty-client.md)). A `starlark-go` implementation is the fallback (findings §5).
- **`Host`**: an interface with one method, `Call(ctx, tool string, kwargs map[string]any) (any, error)`. The production `Host` sends each call to the agent handler.
- **`Stubs`**: a function that makes Python type stubs (`.pyi`) from `ToolDef` values. `ParamsToJSONSchema` is the model for this function.

The package does not import `internal/llm`. It imports `internal/agents` only for `ToolDef`, `Request` and `Response`. Add the package to the `CLAUDE.md` architecture tree, because `internal/docsref` (`tree_test.go`) checks the tree.

### 4.2 The `run_code` tool

| Field | Value |
|---|---|
| `Name` | `run_code` |
| `Category` | `introspect` |
| `Modes` | chat and MCP. Not task mode (see §9). |
| `Params` | `code` (string, required): the program. |

The category is `introspect`, so every tier can run a program. The program itself has no authority. Each inner call has its own category check, so a `read-only` tier still refuses `assay.create_record()`.

The tool description contains:

1. The Python subset rules in five lines (no `class` inheritance, no `match`, no third-party packages, only the `assay` module and the Monty standard library).
2. The stub of each `assay` function: the name, the keyword parameters with types, and the first sentence of the tool description.
3. The return rule: the value of the last expression is the result.

The stubs come from `ToolDef` values at run time, so they cannot be different from the tools.

### 4.3 The `assay` module

The `assay` module is a Monty host module (`external_modules`). It has one function for each tool in `ListToolsForMode(outerMode)`, with these exceptions:

- `run_code` itself. A program cannot start a program.
- The task-mode terminal tools (`write_cell`, `need_more_info`). They end a model turn.
- `present_data` and `propose_schema`. They show content to the person. The model calls them directly with the program result.

Functions take keyword arguments only. The argument names are the `ParamDef` names. Each function returns the `Result` of the `Response` as a Python value.

**Error mapping.** An error `Response` becomes a Python exception in the sandbox. The program can catch it.

| `Response` error code | Python exception |
|---|---|
| `ErrPermission` (-32000) | `PermissionError` |
| `ErrApprovalNeeded` (-32001) | `PermissionError` (the message says that nothing changed) |
| `ErrInvalidParams` (-32602) | `TypeError` |
| `ErrMethodNotFound` (-32601) | `NameError` |
| Other | `RuntimeError` |

The message text is the redacted text that `NewErrorResponse` makes. Code mode does not add a second redaction path.

## 5. Gates and policy

### 5.1 Each inner call is a normal call

The `Host` makes a new `agents.Request` with `Method` set to the tool name and `Params` set to the keyword arguments. It sends the request to the same handler that received `run_code`. Thus:

- `IsModeVisible`, `validateParams`, `sb.Check` and the table allow-list run for each call.
- `run_query` classifies each statement with `analyzeSQL`, then runs `checkSQLClass` and `writeGates`.
- A write that needs approval calls `awaitApproval`. A headless `assay mcp serve` returns `ErrApprovalNeeded`, as now.
- The sandbox ceiling on the context (`WithSandboxCeiling`) goes to each inner call, because the inner call uses the same `ctx`.

**Do not** call `dispatch` or a `handleX` function directly from code mode. A test must make sure that `internal/codemode` calls only `Handle` (an `ast-grep` rule is a good fit).

### 5.2 The Studio lock

`studio/app_agent.go` `agentToolHandler.Handle` holds `a.mu.RLock` for the full duration of one tool call. If `run_code` holds that lock while the program runs, each inner call takes `RLock` again on the same goroutine path. A writer that waits on `a.mu` between the two locks causes a deadlock.

Do this:

1. Make `agentToolHandler.Handle` not hold `a.mu` for `run_code`. Check the workspace identity once at the start.
2. Send each inner call through `agentToolHandler.Handle`. Each inner call then takes the lock, checks the workspace identity and refreshes the policy, as a direct call does.
3. If the workspace closes during a program, the next inner call fails. The engine then stops the program.

This also makes a settings change apply to the next inner call. This is the same behavior as a direct call.

### 5.3 Approval

**Phase 1 (blocking).** A gated write blocks inside `awaitApproval`, as it does now. The Studio approver waits up to 5 minutes (`defaultApprovalTimeout`). The Monty worker waits in its suspension during this time. Monty does not count host waits against `max_feed_duration` or `max_turn_duration`. The client's worker timer runs only while the parent waits for the child, so it does not stop the worker during this wait ([monty-client.md](monty-client.md) §4.5).

The approval card must show that the write comes from a program. Add these fields to the card:

- The text "From a code-mode program".
- The source line of the call (Monty gives the position of each suspension).
- The connections that the program read before this write.

The last field applies a lesson from Muse "tainted egress" (findings §2.6). A program that read `prod_pg` and now writes to a shared `s3` connection can move private data. The person must see that before approval.

**Phase 3 (durable).** Use Monty snapshots. At a gated write, the engine calls `FeedStart`, stops at the call, and dumps the snapshot. `run_code` returns "waiting for approval" to the model and ends the program step. The approval card stays open. When the person approves, Assay loads the snapshot, runs the write and resumes the program. Then a Studio restart does not lose the run. See §7.3 for the snapshot rules.

### 5.4 What code mode must not change

- `run_code` must not make the tier, the approval rule or the SQL policy less strict.
- A committed `assay.yaml` can turn code mode off for the workspace. It cannot turn it on for a person who has it off (the "only tighten" rule in `workspace.ApplySettings`).
- `run_code` must not add a `fantasy` tool in `internal/llm`. It is a `ToolDef` (CLAUDE.md, "One tool surface").

## 6. Results, budgets and guardrails

### 6.1 Rows inside the program

At this time, `handleRunQuery` cuts rows to `MaxQueryRows` (10,000) without a flag. Then `budgetRows` cuts the result to 15,000 characters for the model context. A program needs the full rows, not the model budget.

Make these two changes:

1. Add a context value that tells `handleRunQuery` "the caller is a program". With this value, skip `budgetRows`, but keep `MaxQueryRows`.
2. When `MaxQueryRows` cuts rows, always set `truncated` in the result. In a program, raise a `RuntimeError` for a truncated result. A partial result gives an incorrect count with no warning. (mcp-data-platform uses the same rule, findings §2.5.)

### 6.2 The program result

The `run_code` result is a JSON object:

| Field | Content |
|---|---|
| `result` | The value of the last expression, after `budgetRows`-style budgeting to 15,000 characters. |
| `output` | The `print` output, cut to 8 KiB. |
| `calls` | One entry for each inner call: `tool`, `ok`, `ms`, and `connection` when one applies. No arguments and no rows. |
| `error` | The Python error type, the message and the line, when the program failed. |

The `calls` list lets the model see what the program did. It is also the audit summary.

### 6.3 Guardrails

- **Grounding (#70).** `maybeGroundText` counts a successful `run_query` or `preview_query`. A successful `run_code` with at least one successful inner `run_query` must count too. `wroteThisTurn` must count an inner write.
- **Empty-result note.** `explainEmptyResult` runs inside `handleRunQuery`, so a program gets the `note` field. The program can return it.
- **UI events.** `notifyAgentToolChange` runs for each inner call through `agentToolHandler.Handle`, so the Studio views refresh after an inner write.

## 7. The sandbox

### 7.1 Limits

| Limit | Value | Where |
|---|---|---|
| Execution time for one program | 30 s | Monty `max_feed_duration` |
| Wall time for one program, without approval waits | 120 s | `context.WithTimeout` on the `run_code` context |
| Memory | 256 MiB | Monty `max_memory` |
| Inner tool calls | 200 | `max_suspensions`, counted by the client |
| Recursion depth | 100 | Monty `max_recursion_depth` |
| Source size | 64 KiB | `run_code` parameter check |
| `print` output | 8 KiB | Cap in the client's `Print` handler |
| Workers in the pool | 2 | `internal/montyclient` pool |

The wall-time context also goes to each inner call. At this time `run_query` has no statement timeout. Thus the program deadline is the first limit on a slow inner query. When a limit stops a program, discard the session (findings §4.2).

### 7.2 The worker binary

[monty-client.md](monty-client.md) §5 gives the full rules. In summary:

- The release workflow builds the worker from a pinned Monty tag. It installs it as `assay-monty` next to the `assay` binary on each of the four release runners.
- Assay never downloads the worker at run time.
- Verify the SHA-256 of the binary when the pool starts. Use a digest that the build stamps in with `ldflags`.
- Add the Rust crates of the worker to `THIRD_PARTY_NOTICES.md` (`task notices`).
- If the binary is not there, Assay does not show `run_code`. Settings shows "Code mode is not available on this install".
- macOS: sign the helper with the app when the signed release comes (epic #4). Until then, measure the Gatekeeper effect on an unsigned helper in the spike.

### 7.3 Snapshots (phase 3 only)

A snapshot contains the program state. This can include rows. Obey these rules:

1. Keep snapshots in memory while Studio runs.
2. If a snapshot must survive a restart, write it to the user cache directory with mode `0600`. Never write it into the workspace folder or `.assay-local/`.
3. Sign each snapshot with an HMAC. Keep the key in the OS keychain through `internal/secrets`. Monty does not authenticate snapshots.
4. Store the Monty version with the snapshot. Discard a snapshot from a different version.
5. Delete the snapshot after it resumes, after the person declines, or after the approval expires.

## 8. Settings

| Setting | Values | Default | Scope |
|---|---|---|---|
| Studio, Settings → Assistant: "Code mode" | on, off | off | Per person (user config) |
| `assay.yaml` `agent.code_mode` | `off` | not set | Workspace. It can only turn code mode off. |
| `assay config code-mode` | `on`, `off` | off | CLI mirror of the Studio setting |

Document the `assay.yaml` key in `docs/reference/workspaces.md` between the `<!-- keys:NAME -->` markers. `internal/docsref` checks those tables.

## 9. Scope

**In scope**

- Chat mode in Studio.
- MCP (`assay mcp serve` and the Studio MCP server). External agents get the largest gain, because they do many tool calls.

**Out of scope for now**

- Task mode (notebook cell authoring). Task mode needs `preview_query` and then `write_cell`. A program does not make that loop shorter.
- Programs that a person saves, or that run on a schedule. They are a different feature with a different trust model (a saved program runs without the model and without a person).
- Skills that contain programs. A later proposal can add `skills/<name>/*.py` helpers that `run_code` imports.
- Network access from a program. There is no `fetch`. Data comes only through connections.

## 10. Phases

| Phase | Content | Done when |
|---|---|---|
| P0, spike | Build a minimum `internal/montyclient` (protocol 6, `import assay`, one host call) against Pydantic's worker build. Build the worker from source on the four release runners and measure size and time. Test Gatekeeper with an unsigned helper. Write findings. | A findings note in `docs/plans/`. |
| P1, reads | `internal/codemode`, the `run_code` tool, read tools only in the `assay` module, the setting, the binary in the release. Chat and MCP. | The parity test (§11) passes for all read tools. |
| P2, writes | Add the write tools. Blocking approval with the new card fields (§5.3). Fix the `MaxQueryRows` flag (§6.1). | The parity test passes for all tools and tiers. |
| P3, durable approval | Snapshots (§7.3). | A program that waits for approval survives a Studio restart. |
| P4, fallback | `starlark-go` engine behind the same `Engine` interface, only if P0 finds a platform without the worker. | Optional. |

## 11. Tests

1. **Gate parity.** For each tool and each tier, a direct call and an `assay.<tool>()` call in a program give the same allow, deny or ask result. Use the fixtures of `sandbox_parity_test.go` and `policy_fixture_test.go`.
2. **No ambient access.** A program that tries `open`, `os.environ`, `os.getenv`, `import socket` or `import subprocess` fails. The worker environment is empty.
3. **No credential leak.** Attach a connection with a known secret. Run programs that call each tool and print each result and each error. The secret is not in the output (use the same check as `error_redaction_test.go`).
4. **Limits.** An infinite loop, a large allocation, deep recursion and 201 inner calls each stop the program with a clear error. The Assay process continues.
5. **Crash isolation.** Stop the worker process during a program. `run_code` returns an error, and the next `run_code` works.
6. **Lock.** Close the workspace during a program. The next inner call fails, and there is no deadlock (`go test -race`).
7. **Headless MCP.** A program that writes to a non-local connection gets `PermissionError` with the existing message, and nothing changes.
8. **QA in a real browser.** Use Rodney against the Wails dev server (`docs/contributing/rodney.md`). Make sure that the inner steps and the approval card show correctly.

## 12. Open questions

1. **Language.** Python (Monty) is the proposal. JavaScript (`qjs`) is the alternative. Python fits Assay users and skills better. Decide before P1.
2. **Monty maintenance.** Decided: Assay owns its Go client and builds the worker itself ([monty-client.md](monty-client.md)). Open: do we offer our client back to the Monty project as a Go binding?
3. **Default.** Code mode is off by default in this proposal. After P2, do we turn it on by default for MCP clients only?
4. **Approval scope.** Muse grants one-time, session, task and time-bounded approvals. Assay approves one call. Do we add "approve all writes from this program to this connection"?
5. **Data-flow display.** §5.3 shows the connections that a program read before a write. Do we also block such a write in the `read-write` tier when the source is non-local and the target is shared?

## 13. Review (2026-10-16)

The security design stands: every `assay.<tool>()` call is one `Handler.Handle` request, credentials never enter the sandbox, the gate parity test is the acceptance test, the Studio lock change (§5.2) and the truncation flag (§6.1) are needed whatever engine is used.

The engine choice is reopened:

1. **The gain is smaller for Assay than in the sources.** The cited savings come from many-tool API servers. Assay has about 30 tools, and `run_query` already spans every attached connection: the §2 example is one query over `duckdb_tables()`. The real gains are loops over different tools and keeping rows out of the context.
2. **Monty is a large standing cost.** About 12 MB more per CLI tarball (29 MB today), a Rust toolchain in the release (about 18 minutes cold), a ~1,900-line client to own, a monthly upgrade, and a second executable for Gatekeeper.
3. **Its unique benefits are phase 3 snapshots and crash isolation.** Phases 1 and 2 already block a goroutine for approval. Data reaches a program only through capped tools.
4. **Small local models write restricted dialects poorly.** Assay's default provider is local.

Next step is a spike (#149), not P0 of this plan: run the common multi-step agent tasks through (a) a `starlark-go` `run_code` prototype behind the §4.1 `Engine` interface and (b) a `run_query` variant that keeps its result in a scratch table and returns a summary and a handle the model can query. Compare tokens and first-try success with the local model and with Claude. Go to Monty only if the numbers show Starlark's limits hurt. Phase 3 is dropped for now.
