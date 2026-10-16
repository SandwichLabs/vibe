# Code mode: research findings

Research for a proposed "code mode" for the Vibe agent. No product code. The proposal that uses these findings is [code-mode.md](code-mode.md). Research date: 2026-10-16.

This document uses ASD-STE100 Simplified Technical English. Descriptive sentences have a maximum of 25 words. Instructions have a maximum of 20 words. Product names, function names and code are in `code` font and are technical names.

Each claim has a label:

- **[docs: URL]**: The vendor documentation or a published article says this. We did not run it.
- **[source]**: We read this in the source or the README of the project.
- **[vibe]**: This is a fact about the Vibe code in this repository.

## 1. The question

At this time, the agent calls one tool in each model turn. The tool result goes back into the model context. Then the model selects the next tool.

In code mode, the model writes one short program. A sandbox runs the program. The program calls the same Vibe tools as functions. Only the final result goes back to the model.

We must answer three questions:

1. Which sandbox technology can a Go process use safely on macOS and Linux?
2. How do the Vibe gates (tier, writable connection, approval, SQL policy) apply to a call from a program?
3. How do we keep credentials out of the sandbox?

## 2. The pattern in other products

### 2.1 Cloudflare Code Mode

Cloudflare converts MCP tools into a TypeScript API. The model writes TypeScript against that API. A V8 isolate in a Cloudflare Worker runs the code. [docs: https://blog.cloudflare.com/code-mode/]

The server version gives the model only two tools: `search` (find operations in an API description) and `execute` (run code against the API). [docs: https://blog.cloudflare.com/code-mode-mcp/]

### 2.2 Anthropic programmatic tool calling

A tool definition can have `allowed_callers: ["code_execution_20260120"]`. Then Claude can call the tool from code that runs in the hosted code execution tool. Each such call comes back to the client as a `tool_use` block with a `caller` field. [docs: https://platform.claude.com/docs/en/agents-and-tools/tool-use/programmatic-tool-calling]

Anthropic reports one workflow that went from 150,000 tokens to 2,000 tokens. [docs: https://www.anthropic.com/engineering/code-execution-with-mcp]

This feature is specific to one provider, and the code runs on Anthropic servers. Vibe supports many providers (`internal/llm`), so it cannot be the only path. [vibe]

### 2.3 Bifrost (Maxim AI), a Go gateway

Bifrost is an MCP gateway in Go. Its Code Mode gives the model four meta-tools: `listToolFiles`, `readToolFile`, `getToolDocs` and `executeToolCode`. The model reads Python stub files for the tools it needs. Then it writes Python. A Starlark interpreter runs it. [docs: https://www.getmaxim.ai/articles/code-execution-with-mcp-how-code-mode-cuts-agent-token-costs-by-90/]

The gate rules are important for Vibe:

- The three read meta-tools always run without approval.
- `executeToolCode` runs without approval only when every tool that the script calls is on the auto-run list.
- Each tool call in a script is a separate audit entry.

Bifrost measured 58% fewer input tokens at 96 tools and 93% fewer at 508 tools. The pass rate stayed at 100%. An independent test with one MCP server measured 78% fewer input tokens, but 7% more latency. [docs: same URL]

### 2.4 Kubernetes MCP server (Go)

The `containers/kubernetes-mcp-server` project added one `evaluate_script` tool. It compared five interpreters that embed in Go: `goja`, Starlark, `yaegi`, `gopher-lua` and Tengo. It selected `goja` because models write JavaScript well and `goja` exposes Go objects by reflection. The issue says that `goja` has no memory limit. [source: https://github.com/containers/kubernetes-mcp-server/issues/710]

### 2.5 mcp-data-platform (Go)

This project embeds `starlark-go` for scripts that agents write. Each host function calls the MCP server through an in-memory MCP session. Thus a script gets the same authentication, authorization, rate limits and audit as a direct tool call. [docs: https://imti.co/starlark-agents/]

Other useful details from that article:

- `while` and recursion are off. `SetMaxExecutionSteps` limits CPU. A goroutine calls `Thread.Cancel` when the context ends.
- SQL parameters are bound by the host. The script never makes a SQL string by concatenation.
- A truncated query result is an error, not a partial result.
- A lexical check before the run finds Python habits that Starlark does not support, for example `import`, `try` and f-strings.
- Neither `starlark-go` nor `goja` has a hard memory limit.

### 2.6 Meta Muse

Muse is a personal agent from Meta. Its harness runs in a `systemd-nspawn` container (the "runtime cell"). Security services run outside that cell. [docs: https://research.meta.ai/blog/security-and-safety-for-ai-agents-our-approach-with-muse]

The parts that apply to Vibe:

| Muse part | What it does | Vibe equivalent |
|---|---|---|
| Sentinel | The only authority for connector actions and network egress. The agent proposes. Sentinel decides "allow", "deny" or "ask". | `internal/agents` policy and approval queue [vibe] |
| `hatch-authd` | Stores credentials. Gives "surrogate" tokens to the cell. Sentinel puts the real credential in at the network boundary. | `internal/secrets`, write-only across every boundary [vibe] |
| `privsep` workers | Connector code runs outside the cell with a per-worker credential allow-list. The CLI in the cell only sends typed arguments over a Unix socket. | Host functions that run in the Vibe process, not in the sandbox |
| Approvals as capabilities | An approval is bound to one connector and one use. It can be one-time, session, task, time-bounded or permanent. | The approval card [vibe] |
| Tainted egress | A process that reads user data loses auto-allow for network requests. | Not in Vibe. See [code-mode.md](code-mode.md) §8. |

Muse is a Linux VM product. Vibe is a desktop app on macOS and Linux. We can use the Muse architecture, but not its Linux mechanisms (eBPF, `systemd-nspawn`, `SO_PEERCRED`).

The most important Muse rule for code mode: **the code that holds a credential never runs in the sandbox.** The sandbox only sends typed requests to a component that it cannot change.

## 3. What Vibe needs from a sandbox

These requirements come from `CLAUDE.md` and from the code. [vibe]

| # | Requirement | Reason |
|---|---|---|
| R1 | Works in a Go 1.27 process with `CGO_ENABLED=1`, on Linux amd64 and arm64 (glibc 2.35 baseline) and macOS arm64 (14.0 or later) and amd64 (13.0 or later). | Release builds on four native runners (`release.yml`). Vibe does not support Windows at this time. |
| R2 | No ambient access to files, network, environment variables or processes. | Credentials and data must stay inside Vibe gates. |
| R3 | Limits on CPU time, wall time and memory. | A model can write an infinite loop. |
| R4 | A crash in the sandbox does not stop Studio or `vibe mcp serve`. | Studio holds the DuckDB hub for the workspace. |
| R5 | The script can stop at a tool call and wait for the person to approve. | Writes to non-local connections always need approval. |
| R6 | Models write the language well. | The model must write correct code on the first try. |
| R7 | No runtime download from the network. | `privacy.md`: nothing goes to Sandwich Labs, and installs must be reproducible. |
| R8 | Small binary size and a licence compatible with Vibe. | `THIRD_PARTY_NOTICES.md` and the release tarballs. |

## 4. Candidate technologies

### 4.1 Summary

| Technology | Language | Isolation | Crash isolation | Memory limit | Pause and resume | Go integration |
|---|---|---|---|---|---|---|
| Monty with a Go client (`monty-go`, or our own) | Python subset | Language-level | Yes (worker subprocess) | Yes | Yes (snapshots) | Pure Go, plus a worker binary |
| Monty with `gomonty` | Python subset | Language-level | No (in process) | Yes | Yes | `purego`, bundled shared library |
| `starlark-go` | Starlark (Python dialect) | Language-level (hermetic) | No (in process) | No | No | Pure Go |
| `goja` | JavaScript (ES5.1, most of ES6) | Language-level (by omission) | No (in process) | No | No | Pure Go |
| `qjs` (QuickJS in `wazero`) | JavaScript (ES2023) | WebAssembly | Partial (traps) | Yes (linear memory) | No | Pure Go |
| CPython for WASI in `wazero` | Python | WebAssembly plus WASI | Partial (traps) | Yes | No | Pure Go, plus a large `.wasm` file |
| `yaegi` | Go | Package allow-list | No | No | No | Pure Go |
| Container or microVM | Any | OS or hypervisor | Yes | Yes | Whole memory only | External runtime |
| Hosted (Anthropic, Cloudflare) | Python or TypeScript | Provider | Yes | Yes | No | Network API |

### 4.2 Pydantic Monty

Monty is a Python interpreter in Rust. It is for code that a model writes. The licence is MIT. [source: https://github.com/pydantic/monty]

**Security model.** Monty is a language-level sandbox. The interpreter has no operation that touches the host. File, environment and network access exist only through three opt-in mechanisms: host functions, host objects and mounts. [docs: https://pydantic.dev/docs/monty/concepts/security/]

**Worker subprocess.** Since Monty 1.0, the Python and JavaScript bindings run every session in a `monty` worker subprocess. A panic stops the worker, not the host. The worker starts with an empty environment. The parent treats each frame from the worker as untrusted input. [docs: same URL]

**Host functions.** When the code reads a name that it did not define, execution stops. The host runs the function and resumes execution with the result. A host function runs with the full authority of the host process. Thus the host must validate each argument. [docs: https://pydantic.dev/docs/monty/concepts/host-functions/]

**Snapshots.** `feed_start` stops at each host call. `dump()` serializes the interpreter, with the paused call stack, to a few kilobytes. A new session can load the snapshot and resume, also on a different day. The documentation names "approval gates" as a use: stop at a sensitive call, store the snapshot, and resume after a person approves. [docs: https://pydantic.dev/docs/monty/concepts/snapshots/]

Monty does not authenticate snapshots. Load only snapshots that the same host made, from trusted storage. [docs: same URL]

**Limits.** `max_memory`, `max_feed_duration_secs`, `max_turn_duration_secs`, `max_suspensions` and a host-side `request_timeout`. The duration clocks stop while the host runs a host function. After a memory or time limit stops a feed, discard the session. [docs: https://pydantic.dev/docs/monty/concepts/security/]

**Type checking.** Monty has a type checker. The host can supply `.pyi` stubs for its host functions. Monty then rejects a call with incorrect types before it runs. [docs: https://pydantic.dev/articles/pydantic-monty]

**Language subset.** Monty supports functions, closures, simple classes, `@dataclass`, comprehensions, `try`/`except`, f-strings, `async`/`await` with `asyncio.gather`, and a part of the standard library (`json`, `re`, `datetime`, `math`, `collections`, `itertools` and others). It does not support class inheritance, `match`, generators, user exception classes or third-party packages. [docs: https://pydantic.dev/docs/monty/limitations/]

**Speed.** A new session from a pool takes approximately 0.8 ms. A new pool takes approximately 5 ms. A Docker container takes approximately 195 ms. [docs: https://pydantic.dev/docs/monty/reference/alternatives/]

**Go bindings.** Pydantic does not maintain Go bindings. Its README links community bindings. [source: https://github.com/pydantic/monty/issues/289]

- `CamdenClark/monty-go` (MIT): pure Go. It uses the same subprocess protocol as the official TypeScript binding. Version v0.7.0 supports Monty 1.1 (protocol 5), released 2026-10-14. It has typed host functions by reflection, `monty.Async`, snapshots (`FeedStart`, `Dump`, `LoadSnapshot`), resource limits, type checking stubs and print callbacks. It has no file mounts. File access is denied unless the host handles the OS call. The supported targets are macOS arm64 and x64, Linux arm64 and x64 (glibc), and Windows x64. [source: https://github.com/CamdenClark/monty-go]
- `ewhauser/gomonty` (MIT): an older binding. It loads a bundled Rust shared library with `purego` and runs Monty in process. The last release was in March 2026, before the subprocess model. [source: https://github.com/ewhauser/gomonty]

`monty-go` can download the worker from Pydantic at run time with a pinned SHA-256. Vibe must not use that path (R7). The release must put the pinned `monty` binary in the tarball.

### 4.3 starlark-go

Starlark is a Python dialect from Bazel. It is hermetic: no clock, no randomness, no files and no network. A script can only use the functions that the host gives it. The licence is BSD-3-Clause. [docs: https://imti.co/starlark-agents/]

- `Thread.SetMaxExecutionSteps` limits CPU. `Thread.Cancel` stops a run from another goroutine. [docs: https://pkg.go.dev/go.starlark.net/starlark]
- It has no memory limit.
- It has no `try`/`except`, no classes and no f-strings. `while` and recursion are options in `syntax.FileOptions`.
- It runs in the Vibe process. A Go panic in a builtin can stop the process unless the host recovers it.
- It has no snapshot. An approval must block the goroutine, or the script must stop and run again.

Bifrost and mcp-data-platform use it in production for this pattern (§2.3, §2.5).

### 4.4 goja and qjs

`goja` is a JavaScript engine in pure Go. It has `Runtime.Interrupt` for time limits. It has no memory limit. A security research group published a sandbox escape (an out-of-bounds bug to remote code execution) in a product that used `goja`. [docs: https://www.slcyber.io/research/out-of-bounds-out-of-sandbox-rce-goja]

`qjs` runs QuickJS-NG, compiled to WebAssembly, in `wazero`. It is pure Go and supports ES2023. WebAssembly gives a hard memory limit. [source: https://github.com/fastschema/qjs]

Both are possible. But Vibe is a data tool, and its users and skills already use Python and SQL. A Python subset is a better fit than JavaScript.

### 4.5 CPython for WASI in wazero

This option gives almost full Python with a WebAssembly memory limit. A new sandbox takes approximately 16 ms with a precompiled module, and ten commands take approximately 180 ms. The standard library is approximately 54 MB unpacked. It has no snapshot. [docs: https://pydantic.dev/docs/monty/reference/alternatives/]

The size and the start time are too large for a desktop app tarball.

### 4.6 Containers, microVMs and hosted services

These give the strongest isolation and full Python. But Docker is not on every desktop, and a hosted service sends data off the machine. Both are against the Vibe principles "data stays where it lives" and "single-user first". [vibe]

## 5. Findings

1. **The pattern is proven.** Four independent products use it. The gain increases with the number of tools and with the size of intermediate results. For Vibe, the large intermediate results are query rows.
2. **The security boundary is the host function, not the interpreter.** Every source says this. A host function that takes SQL is a database primitive. Thus each host function must call the same `ToolDef` handler, through the same gates, as a direct tool call.
3. **Credentials never enter the sandbox.** This is already true for Vibe tools. A host function gets a connection name, not a secret. The worker process starts with an empty environment.
4. **Approval needs pause and resume.** Only Monty gives a serializable pause at a host call. With Starlark or `goja`, an approval blocks a goroutine, and a Studio restart loses the run.
5. **Monty, with a Go client that Vibe owns, is the best fit.** It meets R1 to R6 and R8. It meets R7 when Vibe bundles the worker binary. [monty-client.md](monty-client.md) says what the client needs. It gives crash isolation, memory limits and snapshots. No other candidate gives all three.
6. **`starlark-go` is the fallback.** It is pure Go and proven for this use. Use it if the Monty worker binary cannot ship on a platform, or if Monty stops its maintenance.

## 6. Risks

| Risk | Effect | Mitigation |
|---|---|---|
| `monty-go` is a community project with one maintainer. | The binding can fall behind Monty releases. | Do not depend on it. Vibe writes and owns its own client ([monty-client.md](monty-client.md)). Keep `starlark-go` as the fallback behind the same interface. |
| Monty releases change the snapshot format. | Old snapshots do not load. | Keep snapshots only while a run waits for approval. Store the worker version with the snapshot. Discard snapshots after an upgrade. |
| The worker binary adds size and a new executable to the release. | Larger tarball. macOS Gatekeeper can block an unsigned helper. | Measure the size. Sign and notarize the helper with the app in the signed release (epic #77). |
| A future Vibe target (for example Windows) has no Monty worker build. | Code mode is not available there. | Code mode is optional. The normal tool path always works. |
| The model writes Python that the subset does not support. | Extra turns. | Supply type stubs. Return Monty errors in the tool result. State the subset in the tool description. |
| A host function becomes a new path around a gate. | A policy bypass. | Each host function calls the existing dispatcher. A test proves that each gate has the same result for a direct call and a code call. |

## 7. Sources

- Cloudflare, "Code Mode: the better way to use MCP": https://blog.cloudflare.com/code-mode/
- Cloudflare, "Code Mode: give agents an entire API in 1,000 tokens": https://blog.cloudflare.com/code-mode-mcp/
- Anthropic, programmatic tool calling: https://platform.claude.com/docs/en/agents-and-tools/tool-use/programmatic-tool-calling
- Anthropic, "Code execution with MCP": https://www.anthropic.com/engineering/code-execution-with-mcp
- Maxim AI, Bifrost Code Mode: https://www.getmaxim.ai/articles/code-execution-with-mcp-how-code-mode-cuts-agent-token-costs-by-90/
- Meta AI, "How We Built Safety Into Muse": https://research.meta.ai/blog/security-and-safety-for-ai-agents-our-approach-with-muse
- Pydantic Monty repository: https://github.com/pydantic/monty
- Monty security model: https://pydantic.dev/docs/monty/concepts/security/
- Monty host functions: https://pydantic.dev/docs/monty/concepts/host-functions/
- Monty snapshots: https://pydantic.dev/docs/monty/concepts/snapshots/
- Monty limitations: https://pydantic.dev/docs/monty/limitations/
- Monty comparison to alternatives: https://pydantic.dev/docs/monty/reference/alternatives/
- `monty-go`: https://github.com/CamdenClark/monty-go
- `gomonty`: https://github.com/ewhauser/gomonty
- "Don't Sandbox Python. Embed Starlark.": https://imti.co/starlark-agents/
- Kubernetes MCP server, issue #710: https://github.com/containers/kubernetes-mcp-server/issues/710
- `qjs`: https://github.com/fastschema/qjs
- `goja` sandbox escape research: https://www.slcyber.io/research/out-of-bounds-out-of-sandbox-rce-goja
- ASD-STE100 Issue 9: https://www.asd-ste100.org/assets/files/ASD-STE100_ISSUE9.pdf
