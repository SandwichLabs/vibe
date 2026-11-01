# Answers you can check: the 12-month plan

Status: **accepted direction, not built (2026-10-26).** Umbrella epic #250; epics E1-E9 are #241-#249 (label `answers-you-can-check`). The draft was written as a no-limits moonshot, then reviewed by an independent judge against this codebase. This plan is the result after that review, and its *Review changes* section records what the review changed. Designs: the [Assay Design System](https://claude.ai/artifact/LrBzD4cQ6BYYoseLnuFP9a) and the [Answers You Can Check canvas](https://claude.ai/artifact/3rVxGVVeKW6CR14JCPCGVT) (proposed screens; each epic links its screens and open design questions); see [`../design/README.md`](../design/README.md).

## Thesis

Assay already has the hard parts:

- data stays where it lives;
- a workspace is a folder you can diff;
- notebooks and artifacts store the question, never the answer;
- every read goes through one policy gate;
- the agent never sees a credential.

Producing an answer from data is now cheap. Trusting it is the hard part.

The goal for the next year is **every number Assay shows comes with its evidence.** You can see the query, the connection, and the snapshot it read. An independent second derivation can check it when you ask. Its definition is a reviewed file in git. A headless run can re-check it, and it can be re-run at a past snapshot where the source keeps one. The agent stops being an oracle and becomes an analyst whose work you can audit.

The plan has one spine and two side tracks:

```
E1 receipts ──► E3 verifier ──► E4 semantic layer ──► E5 assay check ──► E7 watchers
    │              ▲                    │                                    
    ├──► E6 as-of  │                    └──► E9 aggregate-only tier          
    │              │                                                          
E2 AssayBench ──────┘   (measure before claiming)                              
E8 compute decision spike (parallel from day one: #189, #149, Python)         
```

E1 to E5 on their own are a coherent release. If the year goes badly, shipping those is still worth it.

## Principles this plan refines (written into CLAUDE.md before E1 lands)

- **Receipts are machine-local.** A receipt records *that* a question was asked and what it touched:
  - query hash and bound-parameter hash;
  - connection name, type and read-only flag;
  - snapshot facts;
  - row count;
  - result hash, keyed with a per-workspace local key so a small result can't be brute-forced;
  - duration and policy verdict.

  A row count is a weak answer, so receipts live in `.assay-local/receipts/`, never in the folder, a bundle or git. Numbers leave the machine only through an export the person chooses or an `assay check` report they publish. "Outputs are never persisted" still holds for the folder.
- **Receipt ids are content addresses, not signatures.** Assay has no identity a signature could prove anything to.
- **Receipts belong to tool results, not to Studio's renderer.** `run_query`, `run_saved_query` and `preview_query` return a `receipt_id`, so Claude Code over MCP or an ACP agent can cite one too. Chips in Studio's chat are a convenience on top. The agent can read only receipts for runs it made.
- **No AI needed, except in E3.** E1, E2's deterministic subset, E4, E5, E6, E7 and E9 all work with no provider configured, and each epic tests that.
- **Live stays the default.** "As of" is an explicit control that isn't persisted. A notebook can store a pinned snapshot id the person chose, never an output.

## The epics

### E1 (#241). Run receipts and the evidence drawer

Every value rendered from a query opens to its evidence: the bound SQL and params, the connection and its status, snapshot facts, the policy verdict and the receipt id.

- **Hook.** One hook at the read chokepoint: `Handler.RunReadOnlySQLIn` / `handleRunQuery` (`internal/agents/handler_query.go`) and the person's `RunQuery` (`studio/app_queries.go`).
- **Store.** A new leaf, `internal/receipts`: model, strict decode, a capped and pruned store.
- **Snapshot facts, collected per connection type:**

  | Source | What is recorded | Replayable? |
  |---|---|---|
  | DuckLake | `ducklake_current_snapshot()` | Yes |
  | Iceberg / Delta, where attached | table version | Yes |
  | Postgres | `pg_current_wal_lsn()`, through Assay's own session | No: reference only |
  | DuckDB file | size and mtime | No: reference only |
  | S3 | ETag, from one `HEAD` through the private lister `internal/storageglob` already uses | No: reference only |

  Only lake snapshots are **replay handles**. Every other source is labeled "live only" in the receipt itself.
- **Where the drawer appears.** Studio's drawer is on:
  - notebook cells;
  - A2UI `stat`/`table`/charts;
  - artifact components and pages;
  - chat, where tool calls expand, results can be kept, and numbers that match a receipt's result get a ✓ chip. Other numbers get a dotted underline: "not from a query". Nothing is struck through. The grounding check (`internal/llm/grounding.go`) stays the enforcement; chips only decorate.
- **Inline live values** in markdown cells (`{{saved_query_or_metric}}`). This bumps notebooks to schemaVersion 3 with a forward-compat rule, because `Coerce` refuses newer versions today.
- **"Why this number."** For a single-level `GROUP BY` without window functions or `DISTINCT`: an AST rewrite (`json_serialize_sql`, already used by the policy) that ungroups and filters to the clicked group. Other shapes are refused with a clear message.
- **Profile this run:** opt-in, the person's own query path only. It needs `EXPLAIN ANALYZE`, which the agent can never run.
- **Evidence-carrying exports** (absorbs #1 and #6's export menu). Exports embed receipt ids and snapshot facts, and show "verified ✓" only when E3 ran.
- **CLI:** `assay receipts show <id>` mirrors the drawer.

**Exit:**
- Every value rendered from a query in Studio opens to a receipt.
- A test proves receipts never reach the folder or a bundle.
- A redaction test finds no DSN, credential or path outside the workspace in a receipt.
- The grounding check behaves exactly as before.

**Depends on:** #193 (byte cap on artifact results) and #5 (result cap and cancel). **Absorbs:** #7, #30, #1, #6. **Related:** #18.

### E2 (#242). AssayBench

A benchmark that measures agentic data work against Assay. It exists before the verifier, so the verifier's claims can be measured.

- **Fixture workspaces:**
  - a synthetic SaaS business in DuckLake on local files;
  - a messy Postgres with orphans and timezone traps, using the `pg_harness_integration_test.go` pattern;
  - a federated S3 + Postgres workspace on Floci (#142);
  - a trap workspace whose correct answer is a clarifying question.
- **Two runners:** one for the in-app loop, one for an MCP client against `assay mcp serve`.
- **Scores:**
  - accuracy;
  - "asked instead of guessed";
  - refusals;
  - tool calls and approvals;
  - tokens and wall time;
  - **both runs agree *and* are both wrong**: correlated error, which the verifier must beat.
- **What blocks a PR:** only the deterministic subset (policy fixtures, receipt completeness, comparator tests).
- **Model evals** run nightly on `main`: n≥5 per task, a trend chart and a threshold alert.
- **Fixtures** are public with a private held-out set, so results can't be inflated by training on them. Fixtures are never pasted into prompts or skills.

**Exit:**
- Nightly runs are green on the local model and one frontier model.
- Reintroducing a fixed bug from #233 is caught.
- Variance is documented.

**Depends on:** #221. **Absorbs:** #142.

### E3 (#243). The verifier

"Check this answer" starts a second `internal/llm` run in a fresh context.

- **What the second run gets:** the question, the schema and the skills. It gets none of the first run's SQL or prose, and uses the same `internal/agents` registry (one tool surface).
- **Comparator** in Go: numeric tolerance, set and row comparison, column shape.
- **Disagreement card.** It shows both SQLs and the difference, phrased as a question: "A counted refunds, B excluded them. Which do you mean?" Its "Save as definition" button hands the answer to E4.
- **When the verifier runs:**
  1. A free-form question gets two derivations from scratch with no shared SQL. This catches ambiguous definitions, which is the valuable case.
  2. An answer backed by a metric gets no verifier. The metric is the check, and the receipt shows it.
- **Same data for both runs.** Both runs read the same lake snapshot, or a bounded time window on live sources so data that moved doesn't cause false disagreements. A card for a live source says the data moved when it did.
- **Budget:** a cap on tool steps, tokens, wall time and remote queries. It runs asynchronously behind a "checking…" badge and never blocks the reply.
- **Defaults.** v1 is the same model in a fresh context. A second provider slot is optional, and when set the card names which model said what (`docs/privacy.md` says so). The automatic setting is off by default; "Check this answer" is always there.
- **Probe skills**, shipped as built-in `SKILL.md` files: join fan-out, dropped NULLs, timezone boundaries, survivorship bias, and a bootstrap-CI skill for numbers that really are samples.

**Exit:**
- On AssayBench, confident-and-wrong answers fall below the stated threshold with the verifier on vs off, with the cost and latency difference published.
- The card is QA'd with Rodney.
- `docs/privacy.md` is updated.

**Depends on:** E1, E2.

### E4 (#244). Semantic layer as files

```
semantics/
  entities/customer.yaml       grain, id column, tables, PII flags
  metrics/net_revenue.yaml     references a parameterised saved query (#236), owner, the disagreement that settled it
  joins/orders_customers.yaml  cardinality measured by a profiling run, fan-out probe result
  glossary.md
```

- **Storage rules:** strict decode, byte caps, refuse rather than repair, one rev per file.
- **Bundles** carry the files.
- **`metric('net_revenue', by => 'region')`** is an Assay-SQL extension, like `$name`.
  - It is expanded by a pre-pass on the policy's own lexer (`sql_lex.go`), at the same point as the storageglob pre-expansion, **before** the policy runs. The allow-list and receipts therefore see plain SQL.
  - The same pass runs in `assay query` and the LSP.
  - It never becomes a DuckDB `MACRO`.
- **Proposals, never direct writes.** `propose_definition` / `list_definitions` / `get_definition` are content tools. A proposal goes to `.assay-local/proposals/`. The person accepts it in Studio's Semantics view or with `assay semantics accept`, the same pattern as allowing a view in `views.json`. No tool writes into `semantics/`.
- **Assertions** (`>= 0`, uniqueness) run through a runner that E5 and E7 call.
- A hostile metric in a cloned repo is refused at run time, like a hostile artifact query.

**Exit:**
- A metric written by hand, with no AI, runs in a notebook, an artifact and `assay query`.
- A test shows the policy sees the expanded SQL.
- The round trip from disagreement to proposal to acceptance to metric is QA'd.

**Depends on:** E1, E3 (as input). **Absorbs:** #235.

### E5 (#245). `assay check` and dependents

- **The run.** `assay check` opens the workspace headless through `hub.Open` with `env:` credentials. It runs, under the read-only policy:
  - every notebook data cell;
  - every artifact query;
  - every semantic assertion.
- **The report** is JSON plus markdown, with receipts and a text/number diff against a baseline report. There are no rendered images (§7: no server-side A2UI renderer).
- **Mirrored in Studio** by "Check workspace".
- **Dependents index.** A map from table and column to the cells, artifacts and metrics that use them, built by walking the AST. It powers "what depends on this" in the drawer and the report (this replaces the draft's `assay blame`).
- **GitHub Action example** that posts the markdown report as a comment. Studio never syncs PR comments (§7: no git client).
- **Partial failure.** One connection down degrades that connection's results, not the whole run.

**Exit:**
- A deliberately changed metric shows which artifacts moved.
- A run with MotherDuck or Postgres unreachable still reports everything else.
- A test finds no credential in any report.

**Depends on:** E1, E4, #126, #5.

### E6 (#246). As-of and snapshot diff (lake formats)

- **An as-of control** on notebooks and artifacts:
  - DuckLake: for the person and the agent;
  - Iceberg/Delta: for the person; for the agent only after #20 gives it a confined path.
- **Policy:** learns `AT (VERSION|TIMESTAMP => …)` on lake catalogs in `sql_policy_select.go`.
- **Artifact manifests** gain an optional `snapshot` parameter type.
- **"Re-run as I saw it"** from a receipt that holds a replay handle. Anything else shows "live only" and never pretends.
- **Snapshot-to-snapshot diff** for the person. `ducklake_table_changes` stays refused to the agent.

**Exit:**
- An artifact re-run at a past DuckLake snapshot matches the receipt's result hash.
- A Postgres-only notebook shows "live only".

**Depends on:** E1. **Related:** #20.

### E7 (#247). Standing questions

- **A watcher file** (`watchers/*.yaml`) holds:
  - a question (a saved query or metric, plus params);
  - a schedule, or "on snapshot change";
  - a threshold;
  - a delivery kind.
- **Phase 1** runs inside Studio while it is open.
- **Phase 2** is `assay watch serve`, a service mode that owns the hub, like `assay mcp serve`. It reuses `internal/hub`'s owner/proxy model and hands the hub back to Studio cleanly.
- **Change detection.** Watchers poll and short-circuit on the snapshot id (lakes) or the keyed result hash (everything else). There is no logical replication and no incremental view maintenance.
- **Delivery:**
  - a desktop notification;
  - a digest notebook pinned to receipt ids;
  - webhook or email only from per-person config, never a committed file.
- **Trust.** A workspace's watchers run only after the person enables them (the trustfile pattern), always at the read-only tier, never holding an approver. Keychain access from the background is documented per OS.

**Exit:**
- A threshold crossing on a local lake notifies and writes a digest cell with a receipt.
- A cloned workspace's watchers do nothing until enabled.

**Depends on:** E5, E4, E1.

### E8 (#248). Capability-secure compute: one decision spike

This merges #189 and #149 into one spike that measures three things:

1. **A network-free macOS page host:** QuickJS-in-wazero as a DOM-less script host, vs a signed App-Sandbox helper (needs #4's developer account).
2. **The agent's `run_code` engine:** Starlark vs Monty vs QuickJS-wasm, on token savings and first-try success with the local model (the code-mode §13 ask).
3. **Python:** Pyodide inside the page window (the webview is the host Emscripten wants), vs CPython-WASI in wazero (54 MB stdlib, measured in `spike-code-mode-findings.md` §4.5).

Pyodide and webR don't run under WASI, and swapping the JavaScript hosts for wasm would lose the DOM chart libraries, so the draft's "one wasm runtime for everything" is not the plan.

Python cells come only on an owner decision recorded in `direction.md` §9. Until then §7's "no Python execution plane" stands.

**Exit:**
- A findings doc with measurements.
- #189 either fixed or re-scoped with a date.
- `run_code` shipped, or declined with the numbers.

**Depends on:** nothing; it runs in parallel from the start. **Absorbs:** #189, #149.

### E9 (#249). Aggregate-only tier

A fourth tier, below `read-only`, in `internal/sandbox` (Rank/Tighter extends). A committed `assay.yaml` may set it as the ceiling.

Under this tier:
- `run_query` must be an aggregate over entities declared in E4.
- Assay injects `HAVING count(DISTINCT <id>) >= k`, with k set per workspace.
- PII-flagged columns can't be projected.
- Tools that return raw rows (`preview_query`, `describe_table` samples, non-aggregate saved queries) return shapes, not rows.

The person's own surfaces are unchanged. The threat it addresses is rows leaving the machine to a model provider.

Rewriting arbitrary SQL to enforce minimum group sizes is unsound. This tier is sound only because it restricts the *shape* of the SQL, using the existing AST walk. There is no differential privacy in v1. The trust prompt says "aggregates of at least k entities", never "anonymous".

**Exit:**
- Policy fixtures, fuzzed over the bench fixtures, show zero raw rows reaching the agent.
- "Only tighten" holds.

**Depends on:** E4.

## Prerequisites to clear first

These are small and independent, and each is open on its own:

| Issue | Why it comes first |
|---|---|
| #193 | Byte cap on artifact results: receipts and `assay check` must not materialise a huge cell. |
| #5 | Result cap and cancel: `assay check` and the verifier need cancel. |
| #221 | MCP surface edge cases: AssayBench runs over MCP. |
| #126 | MotherDuck offline: CI and watchers must open with a connection down. |
| #235 | Bind saved-query params in Studio: E4 builds on it. |
| #227, #230 | Flaky tests: fix them before E2 adds more CI. |

## Shape of the year

One developer with agents, not six squads:

| Quarter | Work |
|---|---|
| Q1 | E1, E2, E8 spike |
| Q2 | E3, E4 |
| Q3 | E5, E6 |
| Q4 | E7, E9, hardening; v1.0 "answers you can check" |

## Decisions taken (the owner can overturn any of them)

| # | Question | Decision |
|---|---|---|
| 1 | Where do receipts live? | Machine-local, with explicit export. The result hash is keyed. |
| 2 | How independent is the v1 verifier? | Same model in a fresh context. A second provider slot is optional. |
| 3 | Verifier default? | "Check this answer" always; automatic checking off by default. |
| 4 | How do metrics appear in SQL? | `metric()` as an Assay-SQL pre-pass extension, like `$name`, run in every surface including the LSP. |
| 5 | Is there a daemon? | Watchers run in Studio first; the daemon is phase 2 of E7. |
| 6 | Python? | Stays out until E8 reports. |
| 7 | What threat is the aggregate-only tier for? | The model provider (rows leaving the machine), not teammates. |
| 8 | The canvas UI? | Cut from the plan. At most a design exploration with no quarter. |
| 9 | AssayBench fixtures? | Public, plus a private held-out set. |

## Review changes (what the independent review changed)

**Cut:**
- Canvas-instead-of-tabs.
- Local-first multiplayer (§7 non-goal).
- A workspace-tuned LoRA (model ops, not data trust).
- WASM UDFs in DuckDB.
- Automatic bootstrap CIs (now a probe skill).
- Differential-privacy noise.
- Rendered dashboard images in PRs and Studio-synced PR comments (§7).

**Corrected:**
- DuckDB has no lineage API. Lineage is static column provenance walked over the AST, plus a "why" rewrite for simple shapes.
- Row counts per plan stage need `EXPLAIN ANALYZE`, so they moved to an opt-in profile.
- Postgres, MotherDuck and S3 have reference facts, not replay handles.
- There is no incremental view maintenance or logical replication; watchers poll and short-circuit.
- Pyodide/webR don't run in wasmtime.
- k-anonymity by rewriting arbitrary SQL is unsound; the tier restricts SQL shape instead.
- Verifier independence is now defined by tier.
- Receipts are content-addressed, not signed.
- Model evals don't gate PRs.

**Moved:**
- AssayBench now comes before the verifier.
- "If only one" is now receipts (AI-free, benefits every user), with the verifier as the first consumer.

## Missing pieces each epic must cover

- **Failure UX:**
  - data that moved between verifier runs;
  - a snapshot that has expired;
  - the keychain unreachable from the background;
  - one connection down;
  - the receipt store full.
- **Migration:** notebook schemaVersion 3; the artifact `snapshot` param; receipt pruning.
- **Performance:**
  - keyed hashing at `MaxQueryRows`;
  - the "why" query and the verifier double the load on remote sources;
  - `assay check` parallelises per connection within the pool's limits.
- **Privacy:** `docs/privacy.md` gains a line per epic that sends anything new anywhere.
- **Threat model for receipts:** brute force over small domains (hence the key), and receipts as an oracle (the agent reads only its own).
