### **1\. Research & Analysis: How MotherDuck Unifies Notebooks, Flights, Guides, and MCP**

MotherDuck pairs an embedded-first database engine ([DuckDB](https://duckdb.org)) with cloud-scale storage and serverless compute ([MotherDuck Architecture and Capabilities](https://motherduck.com/docs/concepts/architecture-and-capabilities/)). Over the course of 2025–2026, MotherDuck evolved from a cloud data warehouse into an **agentic data platform** centered around four foundational subsystems orchestrated through the [Model Context Protocol (MCP)](https://motherduck.com/docs/sql-reference/mcp/):

&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+-----------------------------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|      External AI Agents / LLMs    |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  (Claude Desktop, Cursor, Copilot)|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+-----------------+-----------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;JSON-RPC over SSE / Stdio  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+-----------------+-----------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|    MotherDuck MCP Server Layer    |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+-----------------+-----------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+-------------------+---------------+-------------------+-------------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|                   |                                   |                   |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v                   v                                   v                   v  
\+----+---------+   \+-----+----------+                  \+-----+----------+  \+-----+----------+  
|    Guides    |   |   Notebooks    |                  |    Flights     |  |     Dives      |  
|  (Context &  |   | (Interactive   |                  | (Agent-Native  |  |  (Interactive  |  
|  Semantics)  |   |  Exploration)  |                  |  ETL / Python) |  | Visualizations)|  
\+----+---------+   \+-----+----------+                  \+-----+----------+  \+-----+----------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|                   |                                   |                   |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+-------------------+---------------+-------------------+-------------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+-----------------+-----------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|   DuckDB Engine / Storage Layer   |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|     (Ducklings & MD Storage)      |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+-----------------------------------+

#### **A. MotherDuck Notebooks: Exploratory & Reproducible SQL**

As detailed in the [MotherDuck Web UI Tour](https://motherduck.com/docs/getting-started/interfaces/motherduck-quick-tour/), the MotherDuck web interface is built around a reactive notebook-style workspace. Key characteristics include:

> * **Cell Execution & Query Plan Visualizations**: SQL cells execute against attached local DuckDB instances or remote cloud databases.  
> * **Result Ingestion & Caching**: Interactive tabular results allow sorting, client-side filtering, pivoting, and inline exports without re-executing heavy compute.  
> * **Reproducible Analytical Sequences**: Notebooks replace ad-hoc SQL scratches by chaining cells: loading data, transforming intermediate sets, validating schema constraints, and preparing datasets for downstream consumption.

#### **B. MotherDuck Flights: Agent-Native Python Pipelines**

Introduced as an autonomous execution plane ([Introducing Flights: Agent-Native Ingest](https://motherduck.com/blog/flights-agent-native-ingest/) and [Running Python with Flights](https://motherduck.com/docs/key-tasks/flights/)), Flights solve the limitation that SQL cannot easily make authenticated HTTP calls or run specialized data-wrangling libraries:

> * **The Anatomy of a Flight**: Each Flight comprises four elements:  
  1. **Code**: Python script (with access to subprocess and standard OS facilities).  
  2. **Requirements**: Package dependencies installed in an isolated runtime (supporting dlt, requests, pandas, polars, etc.).  
  3. **Config & Secrets**: Environment variables and credentials fetched securely via MotherDuck Secret Manager rather than plain-text scripts.  
  4. **Logs**: Standard output/error capture stored per run.  
> * **Execution & Lifecycle**: Flights run on demand or on a cron schedule ([Scheduling Flights and Managing Runs](https://motherduck.com/docs/key-tasks/flights/scheduling-and-runs/)). They can be created, monitored, and triggered via SQL table functions ([Flights SQL Functions](https://motherduck.com/docs/sql-reference/motherduck-sql-reference/flights/)) such as:  
  * MD\_CREATE\_FLIGHT(...)  
  * MD\_RUN\_FLIGHT(flight\_name \=\> '...')  
  * MD\_LIST\_FLIGHTS()  
  * MD\_GET\_FLIGHT\_LOGS(flight\_name \=\> '...', run\_id \=\> ...)  
> * **Dual Compute Plane**: MotherDuck utilizes "Ducklings" for sub-100ms SQL query execution, while isolated Linux execution containers run the Python runtime with a pre-authenticated md: DuckDB connection.

#### **C. MotherDuck Guides: The Agentic Semantic & Context Layer**

Schema definitions alone do not tell an LLM what business metrics mean or which columns represent canonical foreign keys. MotherDuck introduced **Guides** ([Using Guides to Improve AI Query Accuracy](https://motherduck.com/docs/key-tasks/guides/)):

> * **Context Stored Alongside Data**: Guides are Markdown documents stored directly inside MotherDuck containing business logic, metric definitions (e.g., how to compute Net Revenue Retention), joins to avoid, and personal conventions.  
> * **SQL Management**: Administered through SQL functions ([Guides SQL Functions](https://motherduck.com/docs/sql-reference/motherduck-sql-reference/guides/)):  
  * MD\_CREATE\_GUIDE(topic \=\> 'finance/billing', title \=\> 'MRR Rules', content \=\> '...')  
  * MD\_LIST\_GUIDES(topic \=\> '...')  
  * MD\_GET\_GUIDE(id \=\> '...')  
  * MD\_UPDATE\_GUIDE(...)  
> * **Agent Context Injection**: Instead of manually copy-pasting prompts into chat windows, any AI agent connecting via MCP automatically fetches relevant Guides before authoring queries or creating pipelines.

#### **D. MCP-Based Orchestration**

MotherDuck provides both a cloud-hosted MCP endpoint (https://api.motherduck.com/mcp) and a local open-source server ([Local MCP Server for DuckDB and MotherDuck](https://github.com/motherduckdb/mcp-server-motherduck)). The MCP server groups capabilities into functional tool suites ([MotherDuck MCP Server Documentation](https://motherduck.com/docs/sql-reference/mcp/)):

> * **Discovery & Schema**: list\_databases, list\_tables, list\_columns.  
> * **Execution**: query (read-only), query\_rw (DDL/DML).  
> * **Context Injection**: list\_guides, get\_guide, create\_guide.  
> * **Pipeline Orchestration**: list\_flights, get\_flight\_guide, create\_flight, run\_flight, get\_flight\_logs.  
> * **Visualization**: Tools to generate and configure "Dives" (visual charts and reports).

### **2\. The Local "Microsoft Access" Paradigm for Agentic Data**

In traditional data stacks, setting up an analytics pipeline requires multiple decoupled services: an orchestrator (Airflow/Dagster), a cloud warehouse (Snowflake/BigQuery), a dbt repo, a semantic layer tool, and a BI dashboard.

In contrast, **Microsoft Access** succeeded in the 1990s and 2000s because it was a **single-file, self-contained desktop system**:

> * A single .mdb or .accdb file stored **Tables**, **Queries**, **Forms**, **Reports**, and **VBA Code Modules**.  
> * It offered immediate local execution with no server provisioning or IAM configuration.  
> * It featured dual modes: **Design View** (editing structure and logic) and **Datasheet/Form View** (interacting with data).

#### **The Modern Reimagining: VibeStudio & .vibe**

We translate this paradigm into a local, agentic environment:

> * **The File Container (.vibe)**: A single-file database package containing user tables, SQL queries, interactive notebooks, Python ETL pipelines (Flights), and agent instructions/domain knowledge (Guides).  
> * **Local MCP Server**: An embedded daemon that exposes the internal objects of the .vibe file to local developer environments (Cursor, VS Code, Claude Desktop, Windsurf) or the built-in desktop agent.  
> * **The Access-Style UI ("VibeStudio")**: A desktop application featuring the classic object navigation sidebar, two-way design/datasheet switching, and an integrated Agentic Action Terminal.

### **3\. File Format Specification: The .vibe Container**

A .vibe file is an open, durable, single-file bundle packaged as an uncompressed or standard Zip64 archive with a dedicated 64-byte binary magic header, or alternatively mounted directly as an archive directory.

#### **A. Container Structure & File Layout**

my\_analytics.vibe (Zip64 Container)  
│  
├── manifest.json                  \# Workspace metadata, GUID, engine target, compatibility  
├── data/  
│   └── store.duckdb               \# Core DuckDB transactional & analytical database  
├── guides/                        \# Agent Context & Semantic Layer  
│   ├── index.json                 \# Fast index of topics, descriptions, and tag maps  
│   ├── finance\_mrr.md             \# Guide: MRR calculation logic and join conditions  
│   └── core\_entities.md           \# Guide: Canonical IDs, foreign keys, and edge cases  
├── flights/                       \# Local Python Automation Pipelines  
│   ├── sync\_stripe/  
│   │   ├── flight.toml            \# Metadata: schedule, timeout, entrypoint  
│   │   ├── requirements.txt       \# Flight-specific dependencies  
│   │   ├── config.json            \# Non-secret environment variables  
│   │   └── main.py                \# Python execution script  
│   └── refresh\_marts/  
│       ├── flight.toml  
│       └── main.py  
├── notebooks/                     \# Interactive Analytical Notebooks  
│   ├── customer\_churn.vbn         \# Cell definitions, cached result metadata, layout  
│   └── weekly\_kpis.vbn  
├── views\_dives/                   \# Access-style Forms, Charts, and Dives  
│   ├── revenue\_dashboard.json     \# Chart definitions, layout grid, query bindings  
│   └── customer\_entry\_form.json   \# Form inputs, schema constraints, validation rules  
└── logs/  
&nbsp;&nbsp;&nbsp;&nbsp;└── flights/  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;└── sync\_stripe/  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;├── run\_0001.log       \# Stdout / stderr traces from previous runs  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;└── run\_0002.log

#### **B. The Binary Header (.vibe)**

When serialized as a standalone file, bytes 0–63 contain the Vibe magic signature:

> * Bytes 0–7: Magic Signature ASCII: VIBEFILE  
> * Bytes 8–11: Format Version Uint32 Little-Endian (0x00000001)  
> * Bytes 12–27: File UUID (128-bit RFC 4122 binary representation)  
> * Bytes 28–31: Flags (Bit 0: Compressed payloads; Bit 1: Encryption active; Bit 2: Read-only lock)  
> * Bytes 32–47: Checksum (SHA-256 truncated to 16 bytes of the inner ZIP manifest)  
> * Bytes 48–63: Reserved padding (0x00...0x00)  
> * Bytes 64+: Standard Zip64 stream containing the directory tree described above.

#### **C. Component Schema Definitions**

##### **1\. manifest.json**

{  
&nbsp;&nbsp;"vibe\_version": "1.0.0",  
&nbsp;&nbsp;"min\_duckdb\_version": "1.1.0",  
&nbsp;&nbsp;"project\_id": "8f3b6c41-92b5-4b07-99e2-5e9e03d4cbb8",  
&nbsp;&nbsp;"project\_name": "SaaS Operations Warehouse",  
&nbsp;&nbsp;"created\_at": "2026-09-17T08:00:00Z",  
&nbsp;&nbsp;"updated\_at": "2026-09-17T08:21:00Z",  
&nbsp;&nbsp;"author": "Zac Orndorff",  
&nbsp;&nbsp;"python\_runtime": {  
&nbsp;&nbsp;&nbsp;&nbsp;"default\_version": "3.11",  
&nbsp;&nbsp;&nbsp;&nbsp;"package\_manager": "uv"  
&nbsp;&nbsp;}  
}

##### **2\. Guide Format (guides/\*.md)**

Guides are standard Markdown documents with YAML frontmatter:

\---  
id: "g-9b7e12c4"  
title: "SaaS Revenue & MRR Definitions"  
topic: "finance/metrics"  
author: "Zac Orndorff"  
version: 2  
updated\_at: "2026-09-15T14:30:00Z"  
target\_tables: \["raw\_stripe\_charges", "dim\_customers", "fct\_mrr\_movements"\]  
\---

\# SaaS Revenue & MRR Calculation Rules

\#\# Core Business Logic  
\* \*\*MRR Definition\*\*: Only charges with \`status \= 'succeeded'\` and \`refunded \= false\` are counted.  
\* \*\*Currency Conversion\*\*: Convert all transactions to USD using \`rate\` from \`dim\_exchange\_rates\` on \`charge\_date\`.  
\* \*\*Join Rules\*\*: Always join \`raw\_stripe\_charges.customer\_id\` to \`dim\_customers.stripe\_id\`. Never join on email.  
\* \*\*Caution\*\*: Historical records prior to 2024-01-01 include beta trial credits; filter with \`is\_beta\_account \= false\`.

##### **3\. Flight Configuration (flights/\<name\>/flight.toml)**

name \= "sync\_stripe"  
id \= "fl-4a21d89e"  
description \= "Pulls daily transaction records from Stripe API into raw\_stripe\_charges"  
entrypoint \= "main.py"  
schedule \= "0 2 \* \* \*"  \# Daily at 2:00 AM local time  
timeout\_seconds \= 300

\[dependencies\]  
packages \= \["requests\>=2.31.0", "duckdb\>=1.1.0"\]

\[environment\]  
STRIPE\_API\_VERSION \= "2024-06-20"  
BATCH\_SIZE \= "500"

\[secrets\]  
\# Names of environment variables pulled from the local OS keychain or .env  
required\_secrets \= \["STRIPE\_API\_KEY"\]

### **4\. System Architecture for the Local Application ("VibeStudio")**

\+-----------------------------------------------------------------------------------+  
|                            VibeStudio Desktop UI                                  |  
|  \+---------------------+  \+----------------------------------------------------+  |  
|  | Access-Style Nav    |  | Main Workspace Canvas                             |  |  
|  | \- 🗄️ Tables (DuckDB)|  | \- Datasheet View / Design View                     |  |  
|  | \- 📓 Notebooks      |  | \- SQL & Python Interactive Cells                   |  |  
|  | \- ✈️ Flights        |  | \- Flight Execution Log Visualizer                  |  |  
|  | \- 🧭 Guides         |  | \- Guide Markdown Editor & Linter                   |  |  
|  | \- 📊 Dives & Forms  |  | \- Visual Dashboard / Form View                     |  |  
|  \+---------------------+  \+----------------------------------------------------+  |  
|  \+-----------------------------------------------------------------------------+  |  
|  | Bottom Drawer: AI Copilot Agent & Live Tool Call Inspector                  |  |  
|  \+-----------------------------------------------------------------------------+  |  
\+------------------------------------------+----------------------------------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+----------------------+----------------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|                                             |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v                                             v  
&nbsp;&nbsp;&nbsp;&nbsp;\+---------------+---------------+             \+---------------+---------------+  
&nbsp;&nbsp;&nbsp;&nbsp;|   Embedded DuckDB Engine      |             |   Local Flights Runner        |  
&nbsp;&nbsp;&nbsp;&nbsp;|  \- In-process storage         |             |  \- uv-managed isolated envs   |  
&nbsp;&nbsp;&nbsp;&nbsp;|  \- Vector / FTS extension     |             |  \- Subprocess runner          |  
&nbsp;&nbsp;&nbsp;&nbsp;|  \- Direct Parquet / Arrow IO  |             |  \- Injected VIBE\_DB\_PATH      |  
&nbsp;&nbsp;&nbsp;&nbsp;\+---------------+---------------+             \+---------------+---------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|                                             |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+----------------------+----------------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+------------------+------------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|     Embedded Local MCP Server       |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|   (JSON-RPC over Stdio / SSE)       |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  \- Tools, Prompts, Resources        |  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  \- Guide Semantic Search & Injection|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\+------------------+------------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;^  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;External Agents (Cursor, Claude)

#### **Core Components**

> 1. **The Access-Style UI Shell**:  
   * **Left Object Browser**: Categorized by Tables, Queries/Notebooks, Flights, Guides, and Dives/Forms.  
   * **Dual-View Toggle**: Every object supports **Design View** (schema definitions, raw SQL, Flight Python code, Guide raw markdown) and **Datasheet/Live View** (active tabular data, rendered notebooks, flight execution history, formatted guides).  
   * **Transactional Save**: Modifying objects stages changes in an in-memory directory and commits them back to the .vibe bundle atomically.  
> 2. **Local DuckDB Engine**:  
   * Runs in-process. All table operations execute with zero networking overhead.  
   * Extensions enabled by default: fts (full-text search over Guides and Notebook comments) and parquet.  
> 3. **Local Flights Execution Plane (uv runner)**:  
   * Uses Astral's [uv](https://github.com/astral-sh/uv) to manage ephemeral, isolated virtual environments with sub-second dependency resolution.  
   * Injects environment variables into the process:  
     * VIBE\_DATABASE\_PATH: Absolute path to data/store.duckdb.  
     * VIBE\_FLIGHT\_NAME: Name of the active Flight.  
     * System secrets loaded from the local OS keyring.  
> 4. **Local MCP Server Engine**:  
   * Embedded server running either inside the desktop process or as a standalone CLI companion (vibe mcp serve my\_project.vibe).  
   * Provides full Agent Context Protocol support, allowing external agents to query the database, read guides, and orchestrate flights.

### **5\. Local MCP Tool Suite Specification**

The local .vibe MCP server exposes the following tools, mirroring and adapting MotherDuck's remote MCP toolset for local operations:

| Category | MCP Tool Name | Arguments | Output / Description |
| :---- | :---- | :---- | :---- |
| **Catalog & Schema** | list\_tables | schema\_name?: string | Returns list of user tables, views, row counts, and column schemas. |
| **Catalog & Schema** | describe\_table | table\_name: string | Returns detailed column types, nullability, and primary key metadata. |
| **Query Engine** | query | sql: string | Executes read-only SQL query against the local DuckDB instance; returns formatted rows or Arrow batches. |
| **Query Engine** | query\_rw | sql: string | Executes DDL/DML statements (CREATE TABLE, INSERT, UPDATE, ALTER). |
| **Guides (Context)** | list\_guides | topic?: string | Lists available Guides with IDs, titles, topics, and summarized descriptions. |
| **Guides (Context)** | get\_guide | guide\_id: string | Fetches full Markdown content and frontmatter for a specific Guide. |
| **Guides (Context)** | create\_or\_update\_guide | title: string, topic: string, content: string | Writes or updates a Markdown Guide inside the .vibe container. |
| **Flights (ETL)** | list\_flights | *none* | Lists all registered Flights, their schedules, entrypoints, and last run status. |
| **Flights (ETL)** | create\_flight | name: string, script: string, requirements?: string\[\], schedule?: string | Creates a new Flight with Python code and optional cron schedule. |
| **Flights (ETL)** | run\_flight | name: string, sync?: boolean | Triggers immediate execution of a Flight; streams or returns stdout/stderr. |
| **Flights (ETL)** | get\_flight\_logs | name: string, run\_id?: number | Fetches console logs and exit codes for a previous Flight run. |
| **Notebooks** | list\_notebooks | *none* | Returns all interactive notebooks stored in the .vibe file. |
| **Notebooks** | append\_cell | notebook\_id: string, cell\_type: "sql"|"python", content: string | Appends and optionally executes a cell in a notebook for user inspection. |

#### **MCP Custom SQL Functions inside DuckDB**

To allow local SQL statements to trigger pipelines just like MotherDuck's MD\_RUN\_FLIGHT, VibeStudio installs custom DuckDB table macros:

\-- Trigger a local flight directly from SQL  
SELECT \* FROM vibe\_run\_flight('sync\_stripe');

\-- Read a guide directly in SQL  
SELECT \* FROM vibe\_get\_guide('finance/metrics');

\-- List all flights and execution statuses  
SELECT \* FROM vibe\_list\_flights();

### **6\. Workshop Blueprint: "Build Your Own Agentic Data Platform"**

> * **Format**: 1-Day Hands-on Developer Workshop (6 Hours)  
> * **Goal**: Every participant builds a working clone of the .vibe agentic data engine from scratch, connects it to Claude Desktop / Cursor via MCP, and executes an autonomous data extraction, modeling, and dashboarding task.  
> * **Language & Tooling**: Python 3.11+ / Go, DuckDB, uv, FastMCP / MCP SDK, Textual / Webview for the Access-style UI.

&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;WORKSHOP FLOW  
\+--------------------------+  
|  Lab 1: .vibe Container  | \-\> Zip64 \+ Embedded DuckDB engine  
\+------------+-------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v  
\+--------------------------+  
|  Lab 2: Guides & Context | \-\> Markdown store \+ BM25 search  
\+------------+-------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v  
\+--------------------------+  
|  Lab 3: Local Flights    | \-\> uv-isolated Python runner \+ logs  
\+------------+-------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v  
\+--------------------------+  
|  Lab 4: Local MCP Server | \-\> Tool endpoints & schema discovery  
\+------------+-------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v  
\+--------------------------+  
|  Lab 5: Access-Style UI  | \-\> Nav Pane \+ Design & Datasheet Views  
\+------------+-------------+  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;|  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;v  
\+--------------------------+  
|  Lab 6: End-to-End Test  | \-\> Autonomous agent pipeline  
\+--------------------------+

#### **Lab 1: The .vibe Container & Storage Engine (45 mins)**

> * **Objective**: Implement the file packaging format and initialize an embedded DuckDB database within a .vibe file.  
> * **Key Tasks**:  
  1. Build the archive packer/unpacker module to handle the .vibe file format.  
  2. Implement an atomic flush mechanism so edits in memory or SQLite/DuckDB are safely written back to disk.  
  3. Write a CLI command vibe init \<project\_name\> that generates the base structure (manifest.json, data/store.duckdb, empty guides/, flights/, notebooks/).  
> * **Hands-on Code Blueprint (vibe\_container.py)**:

import zipfile  
import json  
import os  
import duckdb  
from pathlib import Path

MAGIC\_HEADER \= b"VIBEFILE\\x01\\x00\\x00\\x00"

class VibeContainer:  
&nbsp;&nbsp;&nbsp;&nbsp;def \_\_init\_\_(self, path: str):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.path \= Path(path)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.temp\_dir \= Path(f"/tmp/vibe\_{self.path.stem}")  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.temp\_dir.mkdir(parents=True, exist\_ok=True)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.db \= None

&nbsp;&nbsp;&nbsp;&nbsp;def open(self):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;if self.path.exists():  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;with open(self.path, "rb") as f:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;header \= f.read(12)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;if not header.startswith(b"VIBEFILE"):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;raise ValueError("Invalid .vibe file header")  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;with zipfile.ZipFile(self.path, "r") as zf:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;zf.extractall(self.temp\_dir)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;else:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.\_scaffold\_new()

&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;db\_path \= self.temp\_dir / "data" / "store.duckdb"  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;db\_path.parent.mkdir(parents=True, exist\_ok=True)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.db \= duckdb.connect(str(db\_path))  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;return self

&nbsp;&nbsp;&nbsp;&nbsp;def \_scaffold\_new(self):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;(self.temp\_dir / "guides").mkdir(exist\_ok=True)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;(self.temp\_dir / "flights").mkdir(exist\_ok=True)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;(self.temp\_dir / "notebooks").mkdir(exist\_ok=True)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;(self.temp\_dir / "data").mkdir(exist\_ok=True)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;manifest \= {  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"vibe\_version": "1.0.0",  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"project\_name": self.path.stem  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;}  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;with open(self.temp\_dir / "manifest.json", "w") as f:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;json.dump(manifest, f, indent=2)

&nbsp;&nbsp;&nbsp;&nbsp;def save(self):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;if self.db:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.db.close()  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;temp\_zip \= self.temp\_dir.with\_suffix(".tmp.zip")  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;with zipfile.ZipFile(temp\_zip, "w", zipfile.ZIP\_DEFLATED) as zf:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;for root, \_, files in os.walk(self.temp\_dir):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;for file in files:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;full\_path \= Path(root) / file  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;rel\_path \= full\_path.relative\_to(self.temp\_dir)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;zf.write(full\_path, rel\_path)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;with open(self.path, "wb") as out\_f, open(temp\_zip, "rb") as in\_f:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;out\_f.write(MAGIC\_HEADER)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;out\_f.write(b"\\x00" \* 52\)  \# 64-byte padded header  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;out\_f.write(in\_f.read())  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;temp\_zip.unlink()

#### **Lab 2: Building the Guides Semantic Context Engine (45 mins)**

> * **Objective**: Create a context engine that indexes domain knowledge and injects it into LLM queries.  
> * **Key Tasks**:  
  1. Build a parser for markdown documents with frontmatter in guides/\*.md.  
  2. Implement a DuckDB full-text index over the guides using the fts extension.  
  3. Create an agent pre-computation hook: given a user query or table name, return the most relevant Guides.  
> * **Hands-on Code Blueprint (guides\_engine.py)**:

import yaml  
from pathlib import Path

class GuidesManager:  
&nbsp;&nbsp;&nbsp;&nbsp;def \_\_init\_\_(self, vibe\_root: Path, db):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.guides\_dir \= vibe\_root / "guides"  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.db \= db  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.\_init\_fts()

&nbsp;&nbsp;&nbsp;&nbsp;def \_init\_fts(self):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.db.execute("CREATE TABLE IF NOT EXISTS \_vibe\_guides (id VARCHAR, topic VARCHAR, title VARCHAR, content VARCHAR);")  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.sync\_to\_db()

&nbsp;&nbsp;&nbsp;&nbsp;def sync\_to\_db(self):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.db.execute("DELETE FROM \_vibe\_guides;")  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;for gpath in self.guides\_dir.glob("\*.md"):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;with open(gpath, "r") as f:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;raw \= f.read()  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;if raw.startswith("---"):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;parts \= raw.split("---", 2\)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;meta \= yaml.safe\_load(parts\[1\])  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;body \= parts\[2\]  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.db.execute(  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"INSERT INTO \_vibe\_guides VALUES (?, ?, ?, ?)",  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\[meta.get("id", gpath.stem), meta.get("topic", "general"), meta.get("title", gpath.stem), body\]  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;)

&nbsp;&nbsp;&nbsp;&nbsp;def find\_relevant\_guides(self, search\_term: str):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;query \= """  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;SELECT title, topic, content&nbsp;  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;FROM \_vibe\_guides&nbsp;  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;WHERE lower(content) LIKE ? OR lower(topic) LIKE ?  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"""  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;term \= f"%{search\_term.lower()}%"  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;return self.db.execute(query, \[term, term\]).fetchall()

#### **Lab 3: Local Flights Runner with Sandboxed Environments (60 mins)**

> * **Objective**: Build an isolated runner for executing Python ingestion pipelines and capturing logs.  
> * **Key Tasks**:  
  1. Use uv to spawn isolated, cached virtual environments for each Flight.  
  2. Inject connection handles and environment secrets directly into the subprocess.  
  3. Stream and persist stdout/stderr to logs/flights/\<flight\_name\>/run\_\<id\>.log.  
> * **Hands-on Code Blueprint (flight\_runner.py)**:

import subprocess  
import toml  
from pathlib import Path  
from datetime import datetime

class FlightRunner:  
&nbsp;&nbsp;&nbsp;&nbsp;def \_\_init\_\_(self, vibe\_root: Path, db\_path: Path):  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.vibe\_root \= vibe\_root  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;self.db\_path \= db\_path

&nbsp;&nbsp;&nbsp;&nbsp;def execute\_flight(self, flight\_name: str) \-\> dict:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;flight\_dir \= self.vibe\_root / "flights" / flight\_name  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;config\_path \= flight\_dir / "flight.toml"  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;if not config\_path.exists():  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;raise FileNotFoundError(f"Flight {flight\_name} does not exist.")

&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;config \= toml.load(config\_path)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;entrypoint \= flight\_dir / config.get("entrypoint", "main.py")  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;reqs \= config.get("dependencies", {}).get("packages", \[\])

&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\# Create run log directory  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;log\_dir \= self.vibe\_root / "logs" / "flights" / flight\_name  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;log\_dir.mkdir(parents=True, exist\_ok=True)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;run\_id \= datetime.utcnow().strftime("%Y%m%d\_%H%M%S")  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;log\_file \= log\_dir / f"run\_{run\_id}.log"

&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;\# Execute isolated script with uv  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;cmd \= \["uv", "run", "--no-project"\]  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;for pkg in reqs:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;cmd.extend(\["--with", pkg\])  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;cmd.extend(\["python", str(entrypoint)\])

&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;env \= {  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"VIBE\_DATABASE\_PATH": str(self.db\_path),  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"VIBE\_FLIGHT\_NAME": flight\_name  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;}

&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;with open(log\_file, "w") as log\_out:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;process \= subprocess.run(  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;cmd,  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;cwd=str(flight\_dir),  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;env=env,  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;stdout=log\_out,  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;stderr=subprocess.STDOUT,  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;text=True  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;)

&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;return {  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"flight": flight\_name,  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"run\_id": run\_id,  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"success": process.returncode \== 0,  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"log\_path": str(log\_file)  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;}

#### **Lab 4: Building the Embedded Local MCP Server (60 mins)**

> * **Objective**: Expose the .vibe file's internal capabilities over standard MCP JSON-RPC so an external LLM (Claude, Cursor) can query and control the system.  
> * **Key Tasks**:  
  1. Build an MCP server using the Python MCP SDK or FastMCP.  
  2. Implement tools: list\_tables, query, get\_guide, and run\_flight.  
  3. Wire up an MCP Resource that allows an AI model to read any Guide directly as a standard resource URI (vibe://guides/{name}).  
> * **Hands-on Code Blueprint (mcp\_server.py)**:

from mcp.server.fastmcp import FastMCP  
from vibe\_container import VibeContainer  
from guides\_engine import GuidesManager  
from flight\_runner import FlightRunner

mcp \= FastMCP("VibeStudio Local MCP Server")

\# Context state initialized upon opening a .vibe container  
container \= None  
guides\_mgr \= None  
flight\_runner \= None

def init\_workspace(vibe\_file: str):  
&nbsp;&nbsp;&nbsp;&nbsp;global container, guides\_mgr, flight\_runner  
&nbsp;&nbsp;&nbsp;&nbsp;container \= VibeContainer(vibe\_file).open()  
&nbsp;&nbsp;&nbsp;&nbsp;guides\_mgr \= GuidesManager(container.temp\_dir, container.db)  
&nbsp;&nbsp;&nbsp;&nbsp;flight\_runner \= FlightRunner(container.temp\_dir, container.temp\_dir / "data" / "store.duckdb")

@mcp.tool()  
def query(sql: str) \-\> str:  
&nbsp;&nbsp;&nbsp;&nbsp;"""Execute a read-only SQL query against the local DuckDB database."""  
&nbsp;&nbsp;&nbsp;&nbsp;res \= container.db.execute(sql).fetchdf()  
&nbsp;&nbsp;&nbsp;&nbsp;return res.to\_markdown(index=False)

@mcp.tool()  
def list\_guides() \-\> list:  
&nbsp;&nbsp;&nbsp;&nbsp;"""List all domain guides available in this .vibe file."""  
&nbsp;&nbsp;&nbsp;&nbsp;res \= container.db.execute("SELECT id, topic, title FROM \_vibe\_guides").fetchall()  
&nbsp;&nbsp;&nbsp;&nbsp;return \[{"id": r\[0\], "topic": r\[1\], "title": r\[2\]} for r in res\]

@mcp.tool()  
def get\_guide(topic\_or\_id: str) \-\> str:  
&nbsp;&nbsp;&nbsp;&nbsp;"""Retrieve full text of a Guide containing business definitions and join rules."""  
&nbsp;&nbsp;&nbsp;&nbsp;res \= guides\_mgr.find\_relevant\_guides(topic\_or\_id)  
&nbsp;&nbsp;&nbsp;&nbsp;if not res:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;return f"No guide found matching: {topic\_or\_id}"  
&nbsp;&nbsp;&nbsp;&nbsp;return f"\# {res\[0\]\[0\]} (Topic: {res\[0\]\[1\]})\\n\\n{res\[0\]\[2\]}"

@mcp.tool()  
def run\_flight(flight\_name: str) \-\> dict:  
&nbsp;&nbsp;&nbsp;&nbsp;"""Trigger a local Python pipeline flight to ingest or process data."""  
&nbsp;&nbsp;&nbsp;&nbsp;return flight\_runner.execute\_flight(flight\_name)

if \_\_name\_\_ \== "\_\_main\_\_":  
&nbsp;&nbsp;&nbsp;&nbsp;import sys  
&nbsp;&nbsp;&nbsp;&nbsp;if len(sys.argv) \> 1:  
&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;init\_workspace(sys.argv\[1\])  
&nbsp;&nbsp;&nbsp;&nbsp;mcp.run()

#### **Lab 5: The "Microsoft Access" Style UI Shell (60 mins)**

> * **Objective**: Build a dual-view desktop interface that mimics Microsoft Access while exposing modern agentic tooling.  
> * **Design Requirements**:  
  1. **The Navigation Pane**: Collapsible accordion on the left showing Tables, Notebooks, Flights, Guides, and Dives.  
  2. **Datasheet View vs. Design View**:  
     * *Tables*: Datasheet View shows interactive editable grid; Design View shows column types, primary keys, and nullability.  
     * *Notebooks*: Live View renders interactive executed outputs; Design View exposes raw SQL/Markdown cells.  
     * *Flights*: Live View displays execution logs and run history; Design View is a Python code editor with package management.  
     * *Guides*: Live View shows rendered typography; Design View exposes raw Markdown and YAML frontmatter.  
  3. **Agent Action Terminal**: An embedded chat pane on the right that displays incoming MCP tool requests from the local agent in real-time.

\+------------------------------------------------------------------------------------------------------+  
|  File   Edit   View   Insert   Tools   Window   Help                                  VibeStudio v1.0 |  
\+------------------------------------------------------------------------------------------------------+  
| \[ Save .vibe \]  |  View: \[● Datasheet View | ○ Design View \]  |  Run Flight: \[ ▶ sync\_stripe \]       |  
\+----------------------+-------------------------------------------------------+-----------------------+  
| All Vibe Objects   « | Tab: \[ raw\_stripe\_charges \] \[ MRR Guide \]             | AI Agent Copilot      |  
\+----------------------+-------------------------------------------------------+-----------------------+  
| 🗄️ Tables             | id          | customer\_id | amount\_cents | status     | \> Agent reading Guide: |  
|   ├── raw\_stripe\_ch. | ch\_101      | cus\_992     | 4900         | succeeded  |   "finance/metrics"   |  
|   └── dim\_customers  | ch\_102      | cus\_481     | 12000        | succeeded  |                       |  
| 📓 Notebooks         | ch\_103      | cus\_112     | 4900         | failed     | \> Running SQL check:  |  
|   └── churn\_analysis |                                                       |   SELECT count(\*)     |  
| ✈️ Flights           \+-------------------------------------------------------+   FROM raw\_charges    |  
|   ├── sync\_stripe    | Query Status: 3 rows returned in 1.4ms (DuckDB v1.1)  |                       |  
|   └── refresh\_marts  \+-------------------------------------------------------+ \> Found 1 failed      |  
| 🧭 Guides            | Log Output: \[sync\_stripe \- Run \#4\]                    |   charge. Applying    |  
|   └── finance/metrics| \[INFO\] 2026-09-17 08:20:01 \- Fetching Stripe batch 1  |   filter rule from    |  
| 📊 Dives & Forms     | \[INFO\] 2026-09-17 08:20:02 \- Inserted 3 new records   |   Guide.              |  
|   └── revenue\_chart  | \[SUCCESS\] Flight finished with exit code 0\.           |                       |  
\+----------------------+-------------------------------------------------------+-----------------------+

#### **Lab 6: Capstone Challenge — Autonomous Data Pipeline & Analysis (50 mins)**

> * **Scenario**:  
  * The workshop attendee receives an unconfigured .vibe file with empty tables.  
  * The user provides an open API URL containing mock e-commerce transactions.  
> * **Agentic Execution Flow**:  
  1. The attendee opens Claude Desktop / Cursor pointed at the local Vibe MCP Server.  
  2. The user instructs the agent: *"Read the project guidelines, build an ingest Flight for our transactions, store them in DuckDB, and compute monthly churn according to our Guide."*  
  3. **The Agentic Loop**:  
     * **Step 1**: The Agent calls list\_guides and get\_guide('finance/metrics') to understand metric boundaries and field types.  
     * **Step 2**: The Agent calls create\_flight('ingest\_mock\_data', ...) writing Python code that requests the API and writes rows into data/store.duckdb.  
     * **Step 3**: The Agent calls run\_flight('ingest\_mock\_data') and reads get\_flight\_logs to ensure execution was clean.  
     * **Step 4**: The Agent executes SQL via query\_rw to create analytical views (dim\_customers, fct\_monthly\_churn).  
     * **Step 5**: The Agent writes a notebook (notebooks/monthly\_kpis.vbn) with summary charts and reports back to the user in the UI.

### **7\. Comparison Matrix: MotherDuck Cloud vs. Vibe Local**

| Architectural Dimension | MotherDuck (Cloud Platform) | VibeStudio (Local .vibe Platform) |
| :---- | :---- | :---- |
| **Primary Engine** | Distributed Cloud DuckDB ("Ducklings") | Embedded In-Process DuckDB C-Engine |
| **File / Packaging Format** | Cloud-native metadata & object storage | Single-file .vibe container (Zip64 / DuckDB) |
| **Python Execution (Flights)** | Isolated cloud Linux containers | Sandboxed local processes powered by uv |
| **Context System (Guides)** | Cloud SQL table functions (MD\_\*\_GUIDE) | Markdown repository in .vibe \+ DuckDB FTS |
| **Interactive Querying** | Browser-based Web IDE | Desktop Native "Access-Style" Shell (Design/Datasheet) |
| **MCP Protocol Support** | Remote SSE MCP (\[https://api.motherduck.com/mcp\](https://api.motherduck.com/mcp)) | Local Stdio / Localhost SSE MCP Server |
| **Latency & Offline Operation** | Network-bound; requires internet connection | Zero-latency local execution; 100% offline capable |
| **Secret Management** | MotherDuck Cloud Secret Manager | Local OS Keyring / Environment variables |

### **8\. References & Documentation Grounding**

> * **MotherDuck Platform & Cloud Compute**: [MotherDuck Architecture and Capabilities](https://motherduck.com/docs/concepts/architecture-and-capabilities/)  
> * **MotherDuck Web UI & Notebooks**: [MotherDuck Web UI Tour](https://motherduck.com/docs/getting-started/interfaces/motherduck-quick-tour/)  
> * **MotherDuck Flights Architecture**: [Flights Overview & Concepts](https://motherduck.com/docs/concepts/flights/) and [Running Python with Flights](https://motherduck.com/docs/key-tasks/flights/)  
> * **MotherDuck Flights SQL Functions**: [Flights SQL Reference](https://motherduck.com/docs/sql-reference/motherduck-sql-reference/flights/) and [Introducing Flights Blog](https://motherduck.com/blog/flights-agent-native-ingest/)  
> * **MotherDuck Guides & Semantic Context**: [Using Guides to Improve AI Query Accuracy](https://motherduck.com/docs/key-tasks/guides/) and [Guides SQL Functions Reference](https://motherduck.com/docs/sql-reference/motherduck-sql-reference/guides/)  
> * **MotherDuck MCP Server & Agent Workflows**: [MotherDuck MCP Server Documentation](https://motherduck.com/docs/sql-reference/mcp/), [Connect to the MotherDuck MCP Server](https://motherduck.com/docs/key-tasks/ai-and-motherduck/mcp-setup/), and [Using the MotherDuck MCP Server](https://motherduck.com/docs/key-tasks/ai-and-motherduck/mcp-workflows/)  
> * **Local DuckDB MCP Server**: [Local MCP Server for DuckDB and MotherDuck (GitHub)](https://github.com/motherduckdb/mcp-server-motherduck)  
> * **Agentic Analytics & Semantic Layer**: [What is MCP? A Data Person's Guide to Agentic Analytics](https://motherduck.com/blog/what-is-mcp-guide-agentic-analytics/) and [Your Data Model Is the Semantic Layer](https://motherduck.com/blog/bird-bench-and-data-models/)