# Monty in Vibe: our own Go client and worker build

Status: **proposal**, not built. It belongs to [code-mode.md](code-mode.md). It says what Vibe must build and maintain to run Monty without a dependency on the community Go binding `monty-go`. Research date: 2026-10-16.

This document uses ASD-STE100 Simplified Technical English. Descriptive sentences have a maximum of 25 words. Instructions have a maximum of 20 words. Function names, file names and code are in `code` font and are technical names.

Labels: **[source]** means that we read the Monty or `monty-go` source at the commit named in §11. **[measured]** means that we ran it in this research. **[vibe]** means a fact about this repository.

## 1. Decision

Vibe owns two parts:

1. **The Go client** (the "parent"). It starts the worker, speaks the wire protocol, converts values, answers host calls and enforces the limits that the parent owns. This replaces `monty-go`.
2. **The worker binary** (`monty`). Vibe builds it from a pinned Monty source tag in its own release workflow.

Vibe does not depend on `CamdenClark/monty-go`. Vibe also does not fork it. §3 gives the reasons.

## 2. What the protocol is

Pydantic designed the worker protocol for parents in any language. [source: `crates/monty-proto/README.md`]

| Property | Value |
|---|---|
| Transport | The child's stdin (parent requests) and stdout (child events). stderr is free-form diagnostics. |
| Framing | A 4-byte little-endian length, then the protobuf bytes. Maximum frame: 256 MiB. |
| Schema | One file, `proto/monty/v1/monty.proto`, approximately 1,100 lines and 68 messages and enums. It imports nothing. |
| Flow | Strict alternation. The parent writes one `ParentRequest`. The child replies with zero or more `Print` events, then one turn-ending event. There is no multiplexing. |
| Versions | The parent declares `Configure.protocol_version`. On the Monty `main` branch, the child serves versions 5 to 6. Version 6 added the `import` and `import_stubs` OS calls. There is no negotiation. A child that rejects a version sends `FatalError` and stops. |
| Trust | The parent must treat each child frame as untrusted. Monty's Rust parent uses a decode budget of 1 GiB for each frame and validates each arena index. |

`monty-go` v0.7.0 speaks protocol 5. [source] Thus a program cannot use `import vibe` through `monty-go`, because protocol 5 has no `import` call. Code mode needs `import vibe` ([code-mode.md](code-mode.md) §4.3). This is the first reason for our own client.

## 3. Why not fork `monty-go`

| Fact about `monty-go` | Effect for Vibe |
|---|---|
| It encodes each protobuf field by hand with a literal tag number (`protowire.AppendTag(nil, 5, ...)` in `wire.go` and `arena.go`). It does not generate code from `monty.proto`. [source] | A schema change upstream does not fail at compile time. It fails at run time, or it gives incorrect values. |
| It speaks protocol 5. [source] | No `import vibe`. We must add protocol 6 in any case. |
| Approximately 4,300 lines of library code, with an installer (393 lines), reflection adapters for Go functions (`host.go`, 445 lines), class values and complex numbers. [source] | Vibe needs none of these. Vibe sends `map[string]any` to `Handler.Handle` and gets JSON-shaped values back. |
| The installer downloads the worker from npm at run time. [source] | This is against `docs/privacy.md` and reproducible installs. We delete it. |
| One maintainer, 17 commits, first release 2026-08-24. [source] | A fork does not give us a community. It gives us a copy to maintain. |

A fork keeps the hand-written encoding, which is the largest risk. A new client generates its codec from `monty.proto`. Then each protocol change is a compile error or a test failure in our CI.

We use `monty-go` as a reference. It is MIT. If we copy code from it (for example a fuzz target), keep its copyright line in the file and in `THIRD_PARTY_NOTICES.md`.

## 4. The Go client

### 4.1 Package

Make a new leaf package, `internal/montyclient`. It imports only the standard library, `google.golang.org/protobuf` (already an indirect dependency of Vibe, v1.36.12 [vibe]) and `log/slog`. It does not import `internal/agents`. `internal/codemode` uses it. Add it to the `CLAUDE.md` architecture tree.

### 4.2 Parts and size

| Part | What it does | Estimate (Go lines, without tests) |
|---|---|---|
| Codec | Generated code from the vendored `monty.proto` (`protoc-gen-go`). Checked in. | Generated |
| Framing | Read and write length-prefixed frames. Refuse a frame over the cap before allocation. | 80 |
| Arena | Convert Go values to and from `monty.v1.Arena`. Validate each child index (lower than the holder, post-order). Charge each allocation to a decode budget. | 400 |
| Worker | Start the process with `exec.Cmd`, an empty `Env`, and stdin and stdout pipes. Copy stderr to `slog` with a size cap. Stop the process on a protocol error. | 200 |
| Session | The state machine: `Configure`, `Feed`, the suspension loop, `ResumeCall`, `ResumeNameLookup`, `AbortFeed`, `Reset`, `Shutdown`. | 500 |
| OS calls | Answer each `OsCall` arm (§4.4). | 200 |
| Limits | Parent-side limits (§4.5). | 150 |
| Pool | A small pool of workers. Replace a worker that stops. Send `Reset` before the next checkout. | 250 |
| Snapshots | `Dump` and `Load`, for phase 3 of code mode only. | 100 |
| **Total** | | **approximately 1,900** |

### 4.3 Values

Vibe tool results are JSON-shaped. [vibe] Thus the client supports only these arena node kinds as input: `none`, `bool`, `int`, `float`, `str`, `list`, `dict`. It also sends one `module` node with `function` nodes for `import vibe` (§4.4). Before a host function result goes into the arena, pass it through `encoding/json` (with `internal/jsonenc`). This changes `time.Time`, `big.Int` and DuckDB types into strings and numbers that Vibe already uses for the model.

As output, the client also accepts `tuple`, `set`, `frozenset`, `bytes`, `date`, `datetime`, `big_int`, `repr` and `cycle`. It converts each one to a JSON-compatible value (a tuple becomes a list, a date becomes an ISO 8601 string, and a `cycle` becomes the string `"<cycle>"`). Any other node kind is a protocol error.

### 4.4 OS calls

The program can try to read files, environment variables or the clock. The worker sends each try to the parent as an `OsCall`. The client answers as follows.

| `OsCall` arm | Answer | Result in the program |
|---|---|---|
| All `Path.*`, `open`, `mkdir`, `rename` | `not_handled` | `PermissionError` |
| `getenv`, `get_environ` | `not_handled` | `RuntimeError` |
| `urandom` | `not_handled` | `RuntimeError` |
| `date_today`, `date_time_now`, `time` | Not sent. Set `OsPolicy` to the child clock in UTC. | The real time |
| `sleep`, `async_sleep`, `system_sleep`, `async_system_sleep` | Not sent for `time.sleep` with `sleep` set to `zero`. | Returns at once |
| `import` | For the name `vibe`: a `ModuleNode` with one `Function` node for each tool, named `vibe.<tool>`. A call then arrives as a `FunctionCall` with that name. For all other names: `not_found`. | `import vibe` works. Other imports raise `ModuleNotFoundError`. |
| `import_stubs` | The `.pyi` stub of the `vibe` module. | Type checking knows the `vibe` functions. |
| `scan` | `not_handled` | `PermissionError` |
| An arm that a newer protocol adds | `not_handled` | An error in the program. Log the arm name. |

The default answer is `not_handled`. The client must never answer a file or environment call with data.

### 4.5 Limits that the parent owns

Some limits are in the worker. Some limits are the parent's job. [source: `monty.proto`, `ResourceLimits` and `ChildEvent`]

| Limit | Who enforces it | Client work |
|---|---|---|
| `max_memory_bytes`, `max_recursion_depth`, `max_feed_duration_micros`, `max_turn_duration_micros` | Worker | Send the values in `Configure.limits`. |
| `max_suspensions` | **Parent** | Count suspensions in each feed. Send `AbortFeed` at the limit. |
| `max_total_sleep_micros` | **Parent** | Not needed while sleep is `zero`. |
| A worker that stops answering | **Parent** | Start a timer only while the parent waits for the child. Stop the timer while a host function runs. At the deadline, stop the process. |
| Context cancellation | **Parent** | On `ctx.Done()`, send `AbortFeed`. If the child does not answer in 1 s, stop the process. |
| After a memory or time limit | **Parent** | Discard the session. Do not feed it again. |

The timer rule is important for approval. A write can wait 5 minutes for the person ([code-mode.md](code-mode.md) §5.3). The worker is idle during that wait. The timer must not run then.

## 5. The worker binary

### 5.1 Two ways to get it

| Way | Work | Control |
|---|---|---|
| **A. Build from source** (recommended) | Install a pinned Rust toolchain on the four release runners. Run `cargo build --release --locked -p monty-runtime --no-default-features` at the pinned Monty tag. | We build exactly the source that we reviewed. We can drop the command-line front end and apply a patch if it is necessary. We control the macOS minimum version and the glibc baseline. |
| B. Take Pydantic's build | Download the `pydantic-monty-runtime` wheel or the `@pydantic/monty-*` npm package at a pinned version in the release workflow. Verify a pinned SHA-256. | Less work. We depend on Pydantic's build settings and release schedule. |

Use way B for the P0 spike only. Use way A for releases.

### 5.2 Facts for way A

- Monty 1.1.0 needs Rust 1.96 or later (`rust-version` in `Cargo.toml`). [source]
- The release profile uses `lto = "fat"`, `codegen-units = 1` and `strip = true`. [source]
- The `standalone` feature adds the interactive command line. A pool never uses it. `--no-default-features` removes it. [source: `crates/monty-runtime/Cargo.toml`]
- The type checker (`monty-type-checking`, based on `ty`) is not optional. It is a large part of the binary.
- Pydantic's 1.1.0 binaries: 31.7 MB (Linux x86_64), 27.5 MB (macOS arm64), 29.9 MB (Windows x86_64), uncompressed. The wheels are 12 to 14 MB. [measured]
- Our worker-only build (`--no-default-features`) on Linux x86_64, Rust 1.97: 27.5 MB uncompressed, 11.8 MB with gzip. A cold build took 17 min 42 s on 4 cores. Pydantic's build for the same target is 31.7 MB. [measured]
- The release workflow has a 60-minute job timeout. [vibe] Cache the Cargo build by Monty tag and toolchain, so that only a Monty upgrade pays the cold build time. The P0 spike records the size and time on all four runners.

### 5.3 Platform rules

The worker must obey the same rules as the `vibe` binary. [vibe]

| Target | Runner | Rule |
|---|---|---|
| Linux amd64 | `ubuntu-22.04` | glibc 2.35 baseline. Build on the same runner. |
| Linux arm64 | `ubuntu-22.04-arm` | As above. |
| macOS arm64 | `macos-15` | Minimum macOS 14.0 (`scripts/macos-target.sh`). Set `MACOSX_DEPLOYMENT_TARGET`. Add the binary to `scripts/check-macos-minos.sh`. |
| macOS amd64 | `macos-15-intel` | Minimum macOS 13.0. As above. |

### 5.4 Packaging

1. Name the binary `vibe-monty`. A plain `monty` name can conflict with a `monty` on the person's `PATH`.
2. Put it next to `vibe` in each release tarball. In the macOS app, put it in `Contents/MacOS/`.
3. Install it with `install.sh`. Remove it with the uninstall steps in `docs/install.md`.
4. Stamp its SHA-256 into `vibe` with `ldflags`. Verify the digest before the first worker starts.
5. Find it only next to the running `vibe` executable. Do not search `PATH`, and do not read an environment variable for the path.
6. Sign and notarize it with the app when the signed release comes (epic #77).

### 5.5 Licence notices

`scripts/gen-notices.mjs` covers Go modules and npm packages only. [vibe] The worker links many Rust crates. Add a Rust step:

1. Run `cargo about generate` (or `cargo-deny`) on the pinned Monty tag.
2. Append the result to `THIRD_PARTY_NOTICES.md`.
3. Make `task notices:check` fail when the Rust section is out of date.

## 6. Code generation

1. Copy `monty.proto` from the pinned Monty tag to `internal/montyclient/proto/monty/v1/monty.proto`.
2. Record the Monty tag and the SHA-256 of the file in `internal/montyclient/UPSTREAM`.
3. Generate Go code with `protoc-gen-go` through a new task, `task gen:monty`.
4. Check in the generated code.
5. In CI, run `task gen:monty` and fail when the result changes the tree.
6. In CI, fail when the file digest is not the digest in `UPSTREAM`.

## 7. Tests

| Test | What it proves | When it runs |
|---|---|---|
| Codec round trip | Each message that the client sends decodes to the same message. | Every PR |
| Arena fuzz | Random bytes from a "child" never panic, never allocate over the budget, and never make an index out of range. | Every PR (short), weekly (long) |
| Frame cap | A frame length over the cap stops the worker before allocation. | Every PR |
| Worker integration | A real `vibe-monty` runs feeds, host calls, `import vibe`, errors, limits, `AbortFeed` and a stopped process. | Every PR on Linux. macOS on push to `main`, as for the rest of CI. |
| No ambient access | Each `OsCall` arm in §4.4 gives the expected exception. The worker environment is empty (`os.environ` fails). | Every PR |
| Version mismatch | A worker that rejects the protocol version gives a clear Vibe error, not a hang. | Every PR |
| Approval wait | A host function that waits 6 minutes does not trigger the timer. | Every PR, with a fake clock |

CI needs the worker binary for the integration tests. Build it once for each runner and cache it by Monty tag and Rust toolchain version.

## 8. Upgrade procedure

Do these steps for each new Monty release.

1. Read the Monty release notes and the `monty-proto` changes.
2. Find the new `PROTOCOL_VERSION` and `MIN_SUPPORTED_PROTOCOL_VERSION` in `crates/monty-proto/src/lib.rs`.
3. Change the Monty tag in the release workflow and in `UPSTREAM`.
4. Copy the new `monty.proto`. Run `task gen:monty`.
5. Fix each compile error. Add handling for each new `OsCall` arm (default: `not_handled`).
6. If the protocol version changed, change `Configure.protocol_version` in the client.
7. Run all tests in §7 on Linux and macOS.
8. Run `cargo audit` on the new tag.
9. Run `task notices`.
10. Discard stored snapshots from the old version (code mode phase 3).

Monty 1.0 (September 2026) uses protocol 5. Version 1.1 (October 2026) is on `main`, which serves protocols 5 and 6. [source] Thus plan for one upgrade each month at the start. Most upgrades do not change the protocol version.

## 9. Ongoing work

| Task | Frequency | Owner |
|---|---|---|
| Monty upgrade (§8) | Approximately monthly | Code mode owner |
| Watch Monty security advisories (`github.com/pydantic/monty/security`) | Continuous (GitHub watch) | Code mode owner |
| `cargo audit` on the pinned tag | Each PR that changes the tag, and weekly | CI |
| Fuzz the arena decoder | Weekly | CI |
| Rust toolchain pin | With each Monty upgrade that raises `rust-version` | Code mode owner |

`govulncheck` does not cover the Rust worker. `cargo audit` is the equivalent check.

## 10. Effort

| Work | Estimate |
|---|---|
| Go client (§4), with tests (§7) | 2 to 3 weeks for one engineer |
| Worker build in the release workflow, packaging and notices (§5) | 1 week |
| Code generation and CI checks (§6) | 2 days |
| Each later Monty upgrade (§8) | 0.5 to 2 days |

## 11. Sources

- Monty source, `main` branch at the research date (Monty 1.1.0, protocol 6): https://github.com/pydantic/monty
  - `crates/monty-proto/README.md`
  - `crates/monty-proto/proto/monty/v1/monty.proto`
  - `crates/monty-proto/src/lib.rs` (`PROTOCOL_VERSION`, `MIN_SUPPORTED_PROTOCOL_VERSION`)
  - `crates/monty-runtime/Cargo.toml`, root `Cargo.toml`, `.github/workflows/ci.yml`
- `monty-go` v0.7.0 (commit `bd1b9d1`): https://github.com/CamdenClark/monty-go
- `pydantic-monty-runtime` 1.1.0 wheels on PyPI (binary sizes).
- Vibe: `.github/workflows/release.yml`, `scripts/macos-target.sh`, `scripts/gen-notices.mjs`, `go.mod`.
