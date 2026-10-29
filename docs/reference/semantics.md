# Semantic layer: `semantics/`

The workspace can say what its data means, as reviewed files that sit in git
beside the queries and notebooks that use them. There are four kinds:

- **Entities**: a thing the data is about (a customer, an order): its grain, its table, its id column, which columns are personal data.
- **Metrics**: a named measure over a **saved query** (`net_revenue` = `sum(amount)` over the query `net_revenue_base`).
- **Joins**: how two entities relate, declared and, later, measured.
- **The glossary**: plain markdown, one `## term` section per term.

Everything is files you can write by hand. Nothing here needs AI, a connection or a running query: `vibe semantics list` reads the files and nothing else.

```
semantics/
├── entities/customer.yaml
├── metrics/net_revenue.yaml
├── joins/orders_customers.yaml
└── glossary.md
```

The folders are created by whatever writes the first file; `vibe workspace init` does not make empty ones (git does not keep an empty folder, and a missing `semantics/` is simply no definitions).

## Rules every yaml file shares

Each file is read as strictly as an [`artifact.yaml`](artifacts.md): by one shared decoder (`internal/strictyaml`).

- The file is at most **64 KiB** and at most **5,000** YAML nodes. The size is checked before anything is parsed.
- No control characters, no anchors or aliases, no explicit tags, no merge keys, one document, no unknown keys, no duplicate keys. Nothing is interpolated: `${ENV}` is just text.
- `version` is required and is `1`.
- `name` is required, must equal the file name without `.yaml`, and follows the **name rule**: 1 to 63 characters, `^[a-z_][a-z0-9_]{0,62}$` (lower-case letters, digits and `_`, not starting with a digit). The rule is the connection-name rule, because a name is emitted into SQL as a bare identifier or alias, and a name that never needs quoting can never carry anything else. A Windows device name (`con`, `nul`, `com1`...) is refused for the name of an entity, metric or join, since it would not be a usable file everywhere a workspace is cloned.
- Column, dimension and join-column names follow the same lower-case rule.
- **Reserved SQL words are refused** as a name, column, dimension or any part of a `table` (DuckDB's reserved keywords: `select`, `order`, `group`, `null`, `true`, `table` and so on, compared without regard to case for table parts). A name is emitted bare as an identifier or alias, and `GROUP BY null` would silently group by a constant. A test keeps the list equal to DuckDB's own `duckdb_keywords()`.
- At most **500** files per kind: the first 500 (by name) are listed, followed by one entry named `_truncated` whose error says how many were left out.
- A file that fails any of this is **not hidden**. It is listed with its error (`vibe semantics list`, `Error` in the store), it cannot be used, and it is never overwritten by Vibe unless you choose to overwrite it. That includes a name that is not valid (`Customer.yaml`) and a symbolic link (listed as "is a symbolic link; it is not read").
- A write is refused (as a conflict) when a file with the same name in different capitalisation exists, since on a case-insensitive filesystem it would replace that file.
- Free text fields (`description`, `owner`, `grain`, ...) have a length cap, and a control or bidirectional-override character is refused wherever it comes from, including a YAML escape such as `"\u0007"`.

Every file also has a **revision**, a hash of its bytes. A write is compare-and-write: it names the revision it read, and if the file changed on disk since, the write is refused and nothing is touched.

## Entities: `semantics/entities/<name>.yaml`

```yaml
version: 1
name: customer
description: People who bought something.   # optional, up to 2000 characters
grain: one row per customer                 # required, up to 200 characters
table: prod_pg.public.customers             # required
connection: prod_pg                         # optional
id_column: customer_id                      # required
columns:                                    # optional, up to 100
  - {name: email, pii: true, description: contact address}
owner: data team                            # optional, up to 200 characters
assertions:                                 # optional, up to 50
  - {kind: unique, column: customer_id}
  - {kind: not_null, column: created_at}
  - {kind: accepted_values, column: status, values: [active, churned]}
```

| Field | Meaning |
|---|---|
| `table` | One to three dotted identifiers (`table`, `schema.table` or `catalog.schema.table`), each `^[A-Za-z_][A-Za-z0-9_]{0,62}$`. The first part of a three-part name is a connection's catalog alias. |
| `connection` | Optional connection name (the name rule) where an unqualified `table` resolves. |
| `id_column` | The column that identifies one row of this entity. |
| `columns` | Notes on columns: `name` (unique), `pii: true` to flag personal data, `description`. Optional; a column that is not listed is simply unannotated. |
| `assertions` | Checks you declare. Each names a `column`. |

## Metrics: `semantics/metrics/<name>.yaml`

```yaml
version: 1
name: net_revenue
title: Net revenue              # optional, up to 200
description: Revenue after refunds.
query: net_revenue_base         # required: a saved query id (queries/<id>.sql)
measure: sum(amount)            # required
dimensions: [region, month]     # optional, up to 20
format: currency                # optional: number | currency | percent
owner: finance
settled_by:                     # optional
  text: "A counted refunds, B excluded them. Refunds are excluded."
  ref: ""
assertions:
  - {kind: non_negative}
  - {kind: not_null}
```

| Field | Meaning |
|---|---|
| `query` | The id of a [saved query](workspaces.md) (`queries/<id>.sql`): a lower-case letter or digit first, then letters, digits, `-` and `_`. A metric is a measure over that query's rows, so the SQL, its parameters and its connection live in one place, the saved query. The file is not required to exist when the metric is read, only when it is used. |
| `measure` | One SQL aggregate expression over the saved query's columns, which is placed after `SELECT` and before `AS <name>`. The rule: at most 4 KiB, on one line; no `;`, `--` or `/*`; balanced parentheses; only plain `'strings'` with every quote closed (no `E'..'`, no `$$..$$`); **no comma outside parentheses** (so no second column); none of the words `SELECT`, `FROM`, `WITH`, `UNION`, `INTERSECT`, `EXCEPT`, `AS`, `GROUP`, `HAVING`, `ORDER`, `LIMIT`, `OFFSET`, `QUALIFY`, `WINDOW` anywhere, and `WHERE` only directly inside `FILTER (...)`; no call to `getenv`, `query`, `query_table` or a file reader (`read_csv`, `read_parquet`, `read_json`, `read_text`, `glob`, ...). So `sum(amount)`, `sum(amount) FILTER (WHERE status = 'paid')`, `count(DISTINCT customer_id)`, `sum(a) / nullif(sum(b), 0)`, `avg(x)::DECIMAL(10,2)` and `sum(CASE WHEN refunded THEN 0 ELSE amount END)` are fine; `CAST(x AS ...)` (use `::`), `extract(year FROM d)` and `string_agg(x, ',' ORDER BY y)` are not. Commas inside a call's parentheses are fine. That is all that is checked here; whether the SQL is allowed is decided where it runs, by the same SQL policy as any other statement. |
| `dimensions` | Columns of the saved query that a caller may group by. Lower-case column names, no duplicates. |
| `format` | How a value is shown: `number`, `currency` or `percent`. Display only. |
| `settled_by` | The disagreement that settled this definition. `text` is required (up to 2000 characters); `ref` is free text (up to 200) reserved for a verifier card or receipt id. It is stored and shown, never followed. |

## Joins: `semantics/joins/<name>.yaml`

```yaml
version: 1
name: orders_customers
left: orders                     # entity names
right: customer
on:                             # 1 to 8 column pairs
  - {left: customer_id, right: customer_id}
cardinality: many_to_one        # one_to_one | many_to_one | one_to_many | many_to_many
description: each order has one customer
measured:                       # optional: written by a profiling run you start
  at: 2026-10-29T15:30:12Z      # RFC 3339
  left_rows: 12000              # counts: whole, non-negative, plain decimal
  right_rows: 800
  max_fanout: 1                 # most right rows for one left key
  left_orphans: 12              # left rows with no right match
  right_orphans: 3
  verdict: consistent           # consistent | fan_out | orphans
```

| `cardinality` | Meaning |
|---|---|
| `one_to_one` | each left row matches at most one right row, and the reverse |
| `many_to_one` | many left rows share one right row (an order and its customer) |
| `one_to_many` | one left row has many right rows |
| `many_to_many` | neither side is unique |

| `measured.verdict` | Meaning |
|---|---|
| `consistent` | the measurement agrees with the declared cardinality |
| `fan_out` | a key has more matches than the declared cardinality allows |
| `orphans` | rows on one side have no match on the other |

The `measured` counts are read strictly: a non-negative whole number written in decimal. `1.5`, `0x10`, `1e3`, `1_000`, `12.0`, `+5` and a quoted `"12"` are refused.

`cardinality` is what you **declare**. `measured` is what a profiling run found; the run is something the person starts, it is not part of reading these files, and a file with no `measured` block is complete.

## Assertions

An assertion is a check you declare; a runner (coming later) plans and runs it. Its `kind` is closed:

| Kind | On | Takes |
|---|---|---|
| `unique` | entity | `column` |
| `not_null` | entity or metric | `column` on an entity, nothing on a metric |
| `accepted_values` | entity | `column` and `values` (1 to 200 values, each up to 200 characters) |
| `non_negative` | metric | nothing |

## The glossary: `semantics/glossary.md`

Plain markdown, up to 256 KiB, no control characters. Vibe reads it only for its terms: the level-2 headings (`## Active customer`) outside code fences, in file order. The text is otherwise yours.

## From the command line

Both commands only read files; no connection is opened and no query runs.

```
vibe semantics list [--json]            kind, name, one-line summary, and any error
vibe semantics show <kind> <name>       the file exactly as stored (kind: entity | metric | join)
vibe semantics show glossary            the glossary
```

`list` includes unreadable files with their error. `show` prints a file even when it fails validation (so you can read and fix it), then says why on stderr and exits non-zero. Control and bidirectional characters in a name, summary, error or file are shown as `<U+XXXX>`.

## Sharing

The files are plain and diff-friendly, so git carries them. A [`.vibe` bundle](workspaces.md#sharing-without-git-vibe-bundles) carries them too: `semantics/entities/<name>.yaml`, `semantics/metrics/<name>.yaml`, `semantics/joins/<name>.yaml` and `semantics/glossary.md` are bundle entries, each decoded with the rules above before anything is written. Import is byte for byte, and a name that already exists is renamed (`net_revenue_2`, the name inside the file updated), replaced (against the revision just read) or skipped, like a query. When the rename is of something the same bundle refers to, the references follow: a metric whose saved query (`query:`) came in renamed from `q` to `q-2` is pointed at `q-2`, and a join whose entity came in renamed is pointed at the new entity, so an imported definition never silently uses the workspace's own namesake; a rewritten file is re-encoded (comments are not kept) and the result's warnings say so. The glossary is one file and cannot be kept beside another: a clash defaults to skip (the dialog shows "Can't keep both"), and you can still replace it. `vibe export bundle --semantics metrics/net_revenue` picks definitions by id (`entities/<name>`, `metrics/<name>`, `joins/<name>`, `glossary`). Anything under `.vibe-local/` is never in a bundle.

## Coming next

A metric will be usable in SQL by name, so that people and agents reuse one definition instead of re-deriving it. That is not part of what is described above: today these files are read, listed and shared, and nothing runs them.
