> **SUPERSEDED (2026-09-22).** The Access-successor vision, PWA/DuckDB-Wasm stack and Tailscale mesh in this document are not the plan of record. The multi-user ambition remains deferred.
> See [`../direction.md`](../direction.md). Kept for history; do not plan new work from it.

# Vibe Studio
### System Design Document & Project Overview
**by sandwichlabs** · v0.1 · May 2026

---

> *"The goal was never the database. It was the moment a hotel receptionist clicked a button and the right guest appeared. The goal is to make that moment feel inevitable again."*

---

## 1. Vision & Founding Philosophy

**Vibe Studio** is a local-first, AI-augmented rapid application development platform for small and medium businesses. It is the spiritual successor to Microsoft Access 97 — not a nostalgia project, but a reckoning with a fundamental truth the software industry abandoned: **the most valuable software is bespoke, fast, and owned by the people who use it.**

Access 97 succeeded because it collapsed the distance between a business problem and a working solution. An office manager tracking training registrations could, in an afternoon, build a form, define a schema, and have a working multi-user application running on the office LAN. No cloud account. No vendor lock-in. No monthly seat fee. The data lived in the building.

The cloud era traded that sovereignty for convenience, and the tax has compounded. SMBs now pay recurring SaaS fees for tools that are 80% wrong for their workflow, their data lives on someone else's server, and the "citizen developer" path — the democratizing force that made Access so transformative — has been buried under Salesforce consultants and AWS certifications.

Vibe Studio reclaims the golden age paradigm with modern primitives:

| Access 97 Era | Vibe Studio Era |
|---|---|
| MDB file on a shared LAN drive | DuckDB-Wasm + OPFS, persistent in-browser |
| Switchboard Manager + Form Wizard | AI-generated UI via A2UI / Web Components |
| VBA macros behind command buttons | Local LLM agent (llama.cpp) as automation layer |
| NetBEUI / Windows file sharing | Tailscale WireGuard mesh — zero-config, encrypted |
| `.mdw` workgroup security | Tailscale identity-aware headers, per-node ACLs |
| Jet 3.5 client-side query engine | DuckDB-Wasm columnar OLAP, in-browser |
| Report Wizard → print to laser printer | Live dashboards, Parquet exports, PDF rendering |

---

## 2. Target Users

Vibe Studio serves three personas, deliberately mirroring the Access 97 trinity that made the platform so durable:

### 2.1 The Data Entry Worker
Front-line staff — receptionists, dispatch operators, warehouse clerks, retail floor employees. Their mental model is the **appliance**: they expect a focused, locked-down screen that does exactly one job well. Cognitive overhead is waste. Errors are catastrophic. Speed is the metric.

**What they need:** Curated, validated forms. Large touch targets. Immediate feedback. No access to schema. No awareness of SQL.

### 2.2 The Citizen Developer
The office manager, the operations lead, the department power user who has been force-feeding business logic into spreadsheet formulas for a decade. They understand their domain deeply. They can articulate the shape of their data. They just need scaffolding, not a computer science degree.

**What they need:** Conversational schema definition. Wizard-style form generation, AI-assisted. The ability to describe a workflow in plain English and receive a running application. Progressive disclosure — complexity available but not imposed.

### 2.3 The Studio Developer
The IT generalist, the freelance consultant, the technical co-founder of a 12-person company. They want to own the stack, audit the SQL, extend the agent, write custom commands. They will host their own Tailnet, manage ACLs, and deploy Vibe Studio nodes to client sites.

**What they need:** Full access to the query layer, schema editor, agent configuration, MCP server registry, node management, and export pipeline.

---

## 3. Core Architecture

Vibe Studio is a **Progressive Web App (PWA)** with no required cloud dependency. Every component runs either in the browser or as a local daemon on the user's machine, linked into a secure mesh via Tailscale.

```
┌─────────────────────────────────────────────────────────────┐
│                    VIBE STUDIO NODE                         │
│                                                             │
│  ┌─────────────┐    ┌──────────────┐    ┌───────────────┐  │
│  │  PWA Shell  │◄──►│ llama-server │◄──►│  MCP Servers  │  │
│  │  (Browser)  │    │ (llama.cpp)  │    │  (local daemons│  │
│  └──────┬──────┘    └──────────────┘    └───────────────┘  │
│         │                                                    │
│  ┌──────▼──────┐    ┌──────────────┐    ┌───────────────┐  │
│  │ DuckDB-Wasm │    │   j-bash     │    │  OPFS Storage │  │
│  │  (Web Worker)│   │  (TypeScript)│    │  (Persistent) │  │
│  └─────────────┘    └──────────────┘    └───────────────┘  │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              Tailscale Daemon (local)               │   │
│  │    serve / funnel / identity headers / ACLs         │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                          │  WireGuard / TLS
              ┌───────────▼────────────┐
              │     TAILNET MESH       │
              │  (SMB Office Network)  │
              │                        │
              │  Node A   Node B   ...  │
              │ (server) (workstation) │
              └────────────────────────┘
```

### 3.1 Data Layer — DuckDB-Wasm + OPFS

The Jet 3.5 engine's fatal flaw was the single shared `.mdb` file on a network drive: binary page writes over unreliable network connections produced irreversible corruption. Vibe Studio eliminates this architecture entirely.

**Each node owns its data locally.** DuckDB-Wasm runs inside a Web Worker, processing columnar data against the Origin Private File System (OPFS). This provides near-native I/O performance without blocking the UI thread, and persistence that survives browser restarts.

For multi-user scenarios (the canonical SMB use case), Vibe Studio uses a **hub-and-spoke replication model**:

- A designated **host node** (typically a dedicated mini-PC or always-on workstation) holds the authoritative dataset
- **Worker nodes** (employee workstations, laptops) hold local read caches and sync deltas via the Tailnet
- Write operations are queued locally and committed to the host node; conflict resolution follows a last-write-wins or configurable merge strategy per table

This is categorically more resilient than Jet's file-locking approach: network dropout does not corrupt data, merely defers sync.

**Storage hierarchy:**

| Tier | Technology | Persistence | Use Case |
|---|---|---|---|
| Hot (analytical) | DuckDB-Wasm in-memory | Session | Active queries, joins, aggregations |
| Warm (local) | OPFS (`opfs://`) | Permanent | Business tables, user data, app state |
| Cold (archive) | Parquet files (OPFS or S3) | Permanent | Historical records, reporting snapshots |
| Shared (network) | Host node DuckDB over Tailscale | Network-dependent | Multi-user authoritative source |

### 3.2 Inference Layer — llama.cpp

The AI agent is not a cloud API call. It is a locally running language model served by `llama-server` (llama.cpp), reachable on the Tailnet.

This design choice is philosophically non-negotiable for the SMB market: a bookkeeper's invoice data, a medical practice's scheduling records, a law firm's matter notes — these do not leave the building. Privacy is enforced as a **technical constraint of the system**, not a policy promise in a terms of service document.

**Hardware targets and quantization strategy:**

| Hardware | Backend | Recommended Model | VRAM Requirement |
|---|---|---|---|
| Apple M-series (M1/M2/M3) | Metal | Llama-3-8B Q4_K_XL | 8GB unified memory |
| NVIDIA RTX 3060+ | CUDA | Llama-3-8B UD-Q4 | 8GB VRAM |
| AMD / Intel integrated | Vulkan | Llama-3-8B Q4_0 | 8GB+ system RAM |
| CPU-only fallback | AVX2/AVX-512 | Phi-3 Mini Q4 | 4GB RAM |

The inference node is typically the host machine. Worker nodes connect to it over the Tailnet — a single GPU workstation serves the entire office without requiring every employee's laptop to run a local model.

**Agent reasoning loop:**

```
User Input (natural language or form action)
    │
    ▼
[THOUGHT]  LLM evaluates intent → selects tool
    │
    ▼
[ACTION]   j-bash executes:
           • sql "SELECT ..." → DuckDB-Wasm
           • fs_read /home/agent/data/customers/
           • mcp_call → local MCP server (calendar, email, filesystem)
    │
    ▼
[OBSERVATION]  Results returned to agent context
    │
    ├── Incomplete? → iterate
    │
    └── Complete? → generate response + optional UI update (A2UI)
```

### 3.3 Networking Layer — Tailscale

Access 97's LAN architecture worked brilliantly until the office acquired a second location, someone needed remote access, or IT replaced the hub with a misconfigured switch. Tailscale eliminates the entire class of physical network dependencies.

**Every Vibe Studio node joins a Tailnet.** The Tailscale daemon:

- Establishes encrypted WireGuard tunnels between all nodes without requiring port forwarding, VPN hardware, or IT infrastructure changes
- Provisions valid TLS certificates (via Let's Encrypt) for each node's `*.ts.net` URL, satisfying browser Secure Context requirements for OPFS and Web Workers
- Resolves the CORS nightmare of multi-service local development: `tailscale serve --set-path` maps the PWA shell, LLM API, and MCP servers to a single unified origin
- Injects identity-aware headers, enabling the application to authenticate users based on their Tailscale node identity — no login server required

```bash
# Unified origin configuration (run once on host node)
tailscale serve --bg --set-path /        http://127.0.0.1:3000   # PWA Shell
tailscale serve --bg --set-path /api/llm http://127.0.0.1:8080   # llama-server
tailscale serve --bg --set-path /api/db  http://127.0.0.1:4321   # DuckDB sync API

# Result: all services served from https://office-server.tailnet-name.ts.net
# Single origin. No CORS. Valid HTTPS. Zero configuration on worker nodes.
```

**Multi-location SMB scenario:** A retail chain with three locations uses Tailscale subnet routers at each site. The Vibe Studio host node at HQ serves all locations. Each store's POS terminal joins the Tailnet as a worker node. Inventory updates sync in real time over encrypted tunnels with no VPN appliance, no static IP, and no IT contractor visit.

### 3.4 UI Layer — A2UI + Web Components

The Switchboard Manager and Form Wizard were Access 97's most democratizing features: they let a non-programmer describe a workflow and receive a working user interface. Vibe Studio replaces them with an AI-driven UI generation system rooted in the A2UI protocol.

**The Citizen Developer experience:**

> *"I need a form for tracking equipment loans — who took what, when, and whether it's been returned."*

The agent receives this prompt, infers a schema (`equipment`, `employees`, `loans`), generates the table definitions, creates a columnar entry form, a continuous subform for loan history, and a dashboard with outstanding loans highlighted in amber. The entire scaffold is delivered as an A2UI surface update — a flat JSON adjacency list of Web Component definitions — rendered progressively as it streams in.

**Security model for generated UI:** All agent-generated components are encapsulated in Shadow DOM with closed shadow roots. Agent-produced markup cannot access or modify the host application shell. If the agent generates faulty or unexpected UI, the damage is contained within the shadow boundary.

**The dual-mode experience (Access 97 homage):**

| Mode | Who Uses It | What They See |
|---|---|---|
| **Studio Mode** | Citizen Developer, Studio Developer | Schema editor, agent chat, form builder, query explorer, node manager |
| **App Mode** | Data Entry Worker | Locked-down generated interface — forms, dashboards, navigation only. No access to schema or agent. |

---

## 4. Application Building Workflow

### 4.1 Schema Definition (Citizen Developer path)

The agent replaces the Table Wizard. The user describes their domain in plain language:

```
User: "I run a small HVAC company. I need to track customers, 
       service calls, technicians, and parts used."
```

The agent generates a normalized schema proposal — tables, primary keys, foreign keys, indexes — and presents it as an interactive diagram the user can modify before committing. No SQL required. SQL available if desired.

The Leszynski naming convention tradition is preserved: tables use `tbl_` prefixes internally; the agent surfaces human-readable names in all UI contexts.

### 4.2 Form Generation

Forms are generated following the canonical Access 97 layout archetypes, now AI-selected based on context:

| Archetype | When Agent Selects It | Vibe Studio Implementation |
|---|---|---|
| **Columnar (Single Record)** | High-field-count entry, complex records | Full-width A2UI form, one record at a time, tabbed if >12 fields |
| **Continuous (Tabular)** | List browsing, low-field records | Virtualized scrolling list, inline edit |
| **Datasheet** | Bulk entry, data triage | DuckDB-backed editable grid, sort/filter native |
| **Master-Detail (Subform)** | One-to-many relationships | Columnar header + continuous subform, FK-linked auto-filtering |

### 4.3 The Switchboard Successor — App Shell Navigation

Every deployed application gets an **App Shell** — the spiritual successor to the Switchboard Manager. The agent generates a navigation structure respecting the Access 97 principle of maximum eight primary actions per screen, with hierarchical drill-down for complex applications.

The App Shell is locked in App Mode: data entry workers see only their permitted actions. The agent enforces this via Tailscale identity headers — a technician on node `hvac-tech-1` sees the field service form; the owner on node `hvac-owner` sees billing and reports.

### 4.4 Reporting

Reports are live DuckDB queries rendered as responsive HTML — not static print-preview snapshots. Users can filter, drill down, and export to CSV or Parquet. The agent can generate new report queries from plain-language requests:

> *"Show me service calls where we used a part that's now out of stock, grouped by technician, for the last 90 days."*

The agent writes the SQL, DuckDB executes it locally, the result renders immediately. No query builder to learn. No waiting for a cloud data warehouse.

---

## 5. Multi-User Architecture & Concurrency

Access 97's most notorious failure mode was concurrent write corruption: two users editing the same record over a saturated LAN write producing a half-written binary page that destroyed the `.mdb` file irrecoverably.

Vibe Studio's architecture makes this class of failure impossible:

**Record-level optimistic concurrency:**
- Each record carries a `_version` integer and `_updated_at` timestamp
- Worker nodes submit writes as deltas with the expected version
- The host node applies the write if the version matches; returns the current record if it does not
- The UI surfaces conflicts to the user with a clear merge dialog — the Access 97 silent corruption replaced by an explicit human decision

**Write path:**
```
Worker Node                    Host Node
     │                              │
     │  PATCH /records/123          │
     │  { data, expected_version:4 }│
     ├─────────────────────────────►│
     │                              │── version check
     │                              │── if match: write, version=5
     │  200 OK { version: 5 }       │── if conflict: 409 + current record
     │◄─────────────────────────────│
```

**Offline-first:** Worker nodes queue writes locally when the Tailnet is unreachable (user is on a plane, site network is down). On reconnect, the queue replays. The host node processes the queue and returns any conflicts for resolution.

---

## 6. Security Model

| Threat | Access 97 Vulnerability | Vibe Studio Mitigation |
|---|---|---|
| Unauthorized data access | `.mdw` workgroup file trivially bypassed by copying `.mdb` to different machine | Data lives in OPFS, sandboxed to browser origin; off-node access requires Tailscale identity |
| Weak credential storage | Passwords in `.mdw` breakable with binary tools | No password storage in data files; auth delegated entirely to Tailscale node identity |
| Network interception | Plaintext NetBEUI / SMB traffic on LAN | All traffic encrypted via WireGuard; TLS at Tailscale serve layer |
| Schema tampering by end users | Data Entry Worker could open Database Window and delete tables | App Mode hides schema entirely; generated UI runs in Shadow DOM; agent has no schema-write tools in App Mode |
| Agent-generated malicious UI | N/A (no AI layer) | A2UI transmits declarative JSON blueprints, not executable code; Shadow DOM encapsulation contains damage |

---

## 7. Technology Stack Summary

| Layer | Component | Technology | Role |
|---|---|---|---|
| **UI Shell** | PWA App Shell | Vue 3 + Web Components | Navigation, App Mode / Studio Mode |
| **UI Generation** | A2UI Renderer | Web Components + Shadow DOM | Agent-generated interface artifacts |
| **Data** | DuckDB-Wasm | WebAssembly / OPFS | Columnar analytical engine, persistent local storage |
| **Inference** | llama-server | llama.cpp (C++ / GGUF) | Local LLM reasoning, tool orchestration |
| **Tooling** | j-bash | TypeScript (Vercel) | Virtualized shell; SQL, FS, and custom commands |
| **Networking** | Tailscale | WireGuard / TLS | Secure mesh, unified origin, identity auth |
| **Sync Protocol** | Custom delta sync | HTTP/JSON over Tailnet | Optimistic concurrency, offline queue |
| **MCP Servers** | Pluggable | Standard MCP | Calendar, email, filesystem, custom integrations |

---

## 8. Differentiation from Modern Alternatives

| Product | What It Gets Right | What It Gets Wrong |
|---|---|---|
| **Microsoft Access (modern)** | Familiar paradigm, existing user base | Windows-only, no LLM, cloud-push, dying |
| **Airtable / Notion DB** | Low friction, beautiful UI | Cloud-only, data sovereignty surrendered, per-seat pricing punishes growth |
| **Retool / AppSmith** | Developer-grade flexibility | Requires cloud or self-hosted infra expertise; not SMB-accessible |
| **Salesforce / HubSpot** | Deep vertical functionality | Enormously expensive, inflexible, vendor lock-in extreme |
| **FileMaker** | Proven SMB bespoke app platform | Claris/Apple pricing, limited AI integration, aging UX |

**Vibe Studio's moat:** The only platform that combines local-first data sovereignty, peer-to-peer mesh networking, AI-driven app generation, and an SMB-native UX paradigm. It is simultaneously easier to start than Retool and more private than Airtable.

---

## 9. Phased Roadmap

### Phase 1 — Foundation (Months 1–4)
- [ ] DuckDB-Wasm integration with OPFS persistence
- [ ] Tailscale node setup wizard (zero-configuration)
- [ ] Basic PWA shell with Studio / App mode toggle
- [ ] llama-server integration (local model, configurable)
- [ ] Schema definition via agent conversation
- [ ] Columnar and Datasheet form generation (A2UI)
- [ ] Simple App Shell navigation generator

### Phase 2 — Multi-User (Months 5–8)
- [ ] Host/worker node architecture with delta sync
- [ ] Optimistic concurrency with conflict resolution UI
- [ ] Offline write queue with replay-on-reconnect
- [ ] Tailscale identity-based permission system
- [ ] Multi-site subnet router support

### Phase 3 — Power Features (Months 9–12)
- [ ] Report builder with live DuckDB queries
- [ ] MCP server registry (calendar, email, filesystem, custom)
- [ ] Multi-agent workflows (planner + executor + critic)
- [ ] Parquet export and cold storage tier
- [ ] App distribution: export locked `.vibe` bundles for deployment to client sites

### Phase 4 — Ecosystem (Year 2)
- [ ] Vibe Marketplace: community-built app templates (like Access database sharing forums, but this decade)
- [ ] sandwichlabs-hosted Tailscale coordination relay (optional, for clients who want managed networking)
- [ ] Mobile-optimized App Mode (PWA on iOS/Android)
- [ ] External data connectors (QuickBooks, Google Sheets, REST APIs via MCP)

---

## 10. The Name

**Vibe** because the feel of the software matters. Access 97 had a vibe — the raised 3D command buttons, the tabbed Database Window, the satisfying click of a wizard completing. It communicated competence and warmth simultaneously. We are not building a utility. We are building a creative environment.

**Studio** because this is where things get made. Not a database tool. Not a form builder. A place where a motivated person with domain knowledge sits down and builds something real for their business, in an afternoon, and it works, and it's theirs.

**sandwichlabs** because the best software is made by people who are slightly too obsessed with something no one else thinks is important.

---

*Vibe Studio — sandwichlabs — v0.1 System Design*
*"The LAN is back. It never really left."*
