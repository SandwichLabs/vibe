# Example: Chinook music store

The relational example: six entities, five joins with declared cardinalities (all five profiled, one of which disagrees with its declaration), four metrics cut by country, genre and year, a metric whose definition was settled by a disagreement, assertions, a glossary, a notebook, and the proposal flow.

New to this? Start with the [penguins example](../penguins/README.md), which has one table and no joins. The reference for every file here is [`docs/reference/semantics.md`](../../reference/semantics.md).

## The data

[Chinook](https://github.com/lerocha/chinook-database) is a sample database of a digital music store: customers, the invoices they were sent, the lines on each invoice, and the tracks and genres those lines sold. By Luis Rocha, **MIT licence** (Copyright (c) 2008-2024 Luis Rocha; the licence text is in `data/LICENSE`). This folder holds a derived copy: the v1.4.5 SQLite release, six tables exported to CSV with snake_case column names, only the columns used here, prices rounded to cents and invoice dates as dates. It is 187 KB. It holds 59 customers, 412 invoices, 2,240 invoice lines, 3,503 tracks, 25 genres and 8 employees, for 2009 to 2013. The names and email addresses are the sample's own.

## Setup

Run every command below from this folder (`cd docs/examples/chinook`).

An entity's `table` is a SQL name, not a file path, so the first step loads the CSVs into the workspace's local DuckDB (`data/local.duckdb`, git-ignored). Each table has its own id column, which we tell `slaw import` to use as the key:

```console
$ for t in customer employee genre track invoice invoice_line; do slaw import data/$t.csv --key ${t}_id; done
Imported 59 row(s) into local.main.customer
Imported 8 row(s) into local.main.employee
Imported 25 row(s) into local.main.genre
Imported 3503 row(s) into local.main.track
Imported 412 row(s) into local.main.invoice
Imported 2240 row(s) into local.main.invoice_line
```

Each import also writes a table-bound skill, `skills/workspace-local-main-<table>/SKILL.md`, with the table's row count, column types and sample values (generated, regenerated on every import). They are left out of this folder, so the import step creates them; delete them or commit them as you like.

## What is defined

```
queries/sales-lines.sql           one row per invoice line + country, genre, year: the rows the metrics measure
queries/top-customers.sql         a saved query with two parameters
semantics/entities/               customer, employee, genre, invoice, invoice_line, track
semantics/metrics/                revenue, invoice_count, avg_invoice_value, active_customers
semantics/joins/                  customer_invoice, invoice_invoice_line, invoice_line_track,
                                  track_genre, customer_support_rep
semantics/glossary.md
notebooks/store-metrics.slnb
```

```console
$ slaw semantics list
KIND      NAME                  SUMMARY                                                                              ERROR
entity    customer              one row per customer                                                                 -
entity    employee              one row per employee                                                                 -
entity    genre                 one row per genre                                                                    -
entity    invoice               one row per invoice                                                                  -
entity    invoice_line          one row per track per invoice                                                        -
entity    track                 one row per track                                                                    -
metric    active_customers      count(DISTINCT customer_id) by country, genre, year                                  -
metric    avg_invoice_value     sum(unit_price * quantity) / nullif(count(DISTINCT invoice_id), 0) by country, year  -
metric    invoice_count         count(DISTINCT invoice_id) by country, genre, year                                   -
metric    revenue               sum(unit_price * quantity) by country, genre, year                                   -
join      customer_invoice      customer to invoice (one_to_many)                                                    -
join      customer_support_rep  customer to employee (one_to_one)                                                    -
join      invoice_invoice_line  invoice to invoice_line (one_to_many)                                                -
join      invoice_line_track    invoice_line to track (many_to_one)                                                  -
join      track_genre           track to genre (many_to_one)                                                         -
glossary  glossary.md           6 terms                                                                              -
```

Nothing in the last column, so every file is valid. `list` reads only the files; it opens no database.

An entity says what a row is, where it lives, which column identifies it and which columns are personal data:

```console
$ slaw semantics show entity customer
version: 1
name: customer
description: A person or company that has bought from the store.
grain: one row per customer
table: customer
id_column: customer_id
columns:
  - {name: first_name, pii: true}
  - {name: last_name, pii: true}
  - {name: email, pii: true, description: contact address}
  - {name: support_rep_id, description: the employee who looks after this customer}
owner: sales
assertions:
  - {kind: unique, column: customer_id}
  - {kind: not_null, column: country}
```

`pii: true` marks the columns an agent should treat with care. The metrics are defined over the saved query `sales-lines`, which does the joins once, in one reviewable place:

```console
$ cat queries/sales-lines.sql
-- slaw: {"title":"Sales lines","description":"One row per invoice line, with the customer's country, the track's genre and the invoice year: the rows every revenue metric is measured over"}
SELECT
  l.invoice_line_id,
  l.invoice_id,
  i.customer_id,
  c.country,
  g.name AS genre,
  year(i.invoice_date) AS year,
  l.unit_price,
  l.quantity
FROM invoice_line AS l
JOIN invoice AS i ON i.invoice_id = l.invoice_id
JOIN customer AS c ON c.customer_id = i.customer_id
JOIN track AS t ON t.track_id = l.track_id
JOIN genre AS g ON g.genre_id = t.genre_id
```

### A metric settled by a disagreement

Finance said revenue is the sum of `invoice.total`. Marketing said it is the sum of the invoice lines. Over all time the two agree, but they differ as soon as you want revenue by genre (only lines have a genre) or join the invoice to its lines. The `settled_by` block records which one won and why:

```console
$ slaw semantics show metric revenue
version: 1
name: revenue
title: Revenue
description: What customers paid, in dollars, summed from invoice lines (unit price times quantity).
query: sales-lines
measure: sum(unit_price * quantity)
dimensions: [country, genre, year]
format: currency
owner: finance
settled_by:
  text: "Finance summed invoice.total, marketing summed invoice lines. Both reach 2328.60 over all time, but only lines can be cut by genre, and joining invoice to its lines repeats the total once per line (up to 14 times: see the invoice_invoice_line join), which inflates it (summing invoice.total over that join gives 20848.62, nearly nine times the real 2328.60). Revenue is the sum of the lines."
assertions:
  - {kind: non_negative}
  - {kind: not_null}
```

You can see the inflation for yourself. Joining invoices to their lines repeats each invoice's total once per line:

```console
$ slaw query "SELECT sum(i.total) AS summed_invoice_totals FROM invoice i JOIN invoice_line l USING (invoice_id)"
summed_invoice_totals
---------------------
20848.62
(1 row)
```

```console
$ slaw query "SELECT * FROM metric('revenue')"
revenue
-------
2328.60
(1 row)
expanded 1 metric: revenue
```

20,848.62 against 2,328.60. The `invoice_invoice_line` join below measures how bad the repeat can get.

## Check

```console
$ slaw semantics check
STATUS  SUBJECT                   CHECK                     DETAIL
pass    entity customer           compile                   compiles
pass    entity customer           unique customer_id        
pass    entity customer           not_null country          
pass    entity employee           compile                   compiles
pass    entity employee           unique employee_id        
pass    entity genre              compile                   compiles
pass    entity genre              unique genre_id           
pass    entity invoice            compile                   compiles
pass    entity invoice            unique invoice_id         
pass    entity invoice            not_null customer_id      
pass    entity invoice            row_count                 412 rows
pass    entity invoice            range total               
fail    entity invoice            freshness invoice_date    stale: latest 2013-12-22
pass    entity invoice_line       compile                   compiles
pass    entity invoice_line       unique invoice_line_id    
pass    entity invoice_line       not_null track_id         
pass    entity invoice_line       accepted_values quantity  
pass    entity track              compile                   compiles
pass    entity track              unique track_id           
pass    entity track              not_null genre_id         
pass    metric active_customers   compile                   compiles
pass    metric active_customers   non_negative              
pass    metric avg_invoice_value  compile                   compiles
pass    metric avg_invoice_value  non_negative              
pass    metric invoice_count      compile                   compiles
pass    metric invoice_count      non_negative              
pass    metric revenue            compile                   compiles
pass    metric revenue            non_negative              
pass    metric revenue            not_null                  

28 passed, 1 failed, 0 could not run
Error: 1 of 29 checks failed
(exit status 1)
```

One check fails on purpose, exit status 1: the `invoice` entity declares `freshness` on `invoice_date` with `max_age: 30d`, and the store's last invoice is from 2013, so the table is stale and the check says which date it saw (`stale: latest 2013-12-22`). The same entity declares `row_count` (at least 400 invoices; the DETAIL says how many it found) and a `range` on `total` (between 0 and 30), which pass. Note that `invoice_line.quantity` is asserted to be `1` everywhere: `accepted_values` compares as text, so `values: ["1"]`. If a future import brought in a line with quantity 2, this would fail and the glossary's "one track per line" would be wrong.

## Profile the joins

A join file carries what you **declare** (`cardinality`) and, once you run a profile, what was **measured**. Profiling does a few full scans of both tables, read-only, and writes only the `measured:` block of the join file (the rest of the file is kept as written):

```console
$ slaw semantics profile customer_invoice
join customer_invoice (declared one_to_many)
  left rows            59
  right rows           412
  max right per left   7
  left orphans         0
  right orphans        0
  verdict              consistent
recorded in semantics/joins/customer_invoice.yaml (measured:)
```

```console
$ slaw semantics profile invoice_invoice_line
join invoice_invoice_line (declared one_to_many)
  left rows            412
  right rows           2240
  max right per left   14
  left orphans         0
  right orphans        0
  verdict              consistent
recorded in semantics/joins/invoice_invoice_line.yaml (measured:)
```

```console
$ slaw semantics profile invoice_line_track
join invoice_line_track (declared many_to_one)
  left rows            2240
  right rows           3503
  max right per left   1
  left orphans         0
  right orphans        1519
  verdict              consistent
recorded in semantics/joins/invoice_line_track.yaml (measured:)
```

```console
$ slaw semantics profile track_genre
join track_genre (declared many_to_one)
  left rows            3503
  right rows           25
  max right per left   1
  left orphans         0
  right orphans        0
  verdict              consistent
recorded in semantics/joins/track_genre.yaml (measured:)
```

What these say:

- `customer_invoice`: one customer, up to 7 invoices; no orphans. `one_to_many` holds.
- `invoice_invoice_line`: **`max right per left` is 14.** One invoice has 14 lines. This is the number behind the revenue disagreement above: join the two and any invoice-level amount is repeated up to 14 times. The verdict is `consistent` because the declaration (`one_to_many`) already says so; the profile makes the size of the risk visible.
- `invoice_line_track`: **1,519 right orphans.** That is 1,519 of the 3,503 tracks that were never sold. For a `many_to_one` this is normal and not a problem (every sold track exists, so there are no left orphans, which would be the real problem), but it is the reason an inner join from `track` to sales silently drops 43% of the catalogue. Anything that asks "which tracks did not sell?" must start from `track`, not from `invoice_line`.
- `track_genre`: every track has exactly one of the 25 genres, and every genre is used.

The fifth join was declared wrong on purpose. Someone modelling quickly might write that a customer has one support rep, so `one_to_one`:

```console
$ slaw semantics profile customer_support_rep
join customer_support_rep (declared one_to_one)
  left rows            59
  right rows           8
  max right per left   1
  left orphans         0
  right orphans        5
  verdict              fan_out
warning: declared one_to_one, but up to 21 left rows share one key: joining repeats right rows, so sums over the right side are inflated
warning: 5 right rows have no match on the left (an inner join drops them)
recorded in semantics/joins/customer_support_rep.yaml (measured:)
```

The profile did its job, so the exit status is 0, but it prints a warning and records `verdict: fan_out`: 21 customers share one rep, so joining repeats the rep and any sum over the right side (say, a rep's salary) is inflated. Five of the eight employees have no customers at all (they are not sales agents). Change the declaration to `many_to_one` (`git checkout -- semantics/joins/customer_support_rep.yaml` puts the demo back) and profile again, and the verdict agrees with the data (the five right orphans stay: extra rows on the "one" side are normal):

```console
$ slaw semantics profile customer_support_rep
join customer_support_rep (declared many_to_one)
  left rows            59
  right rows           8
  max right per left   1
  left orphans         0
  right orphans        5
  verdict              consistent
recorded in semantics/joins/customer_support_rep.yaml (measured:)
```

The committed file keeps the wrong declaration and its `fan_out` block, so you see the warning when you profile it.

## Ask the metrics

Every metric is a measure over `sales-lines`, grouped by whichever of its dimensions you name (`country`, `genre` and `year`; `avg_invoice_value` has no `genre`, since by genre it would not be an invoice's value):

```console
$ slaw query "SELECT * FROM metric('revenue', by => 'year') ORDER BY 1"
year  revenue
----  -------
2009  449.46
2010  481.45
2011  469.58
2012  477.53
2013  450.58
(5 rows)
expanded 1 metric: revenue
```

```console
$ slaw query "SELECT * FROM metric('revenue', by => 'genre') ORDER BY 2 DESC LIMIT 5"
genre               revenue
------------------  -------
Rock                826.65
Latin               382.14
Metal               261.36
Alternative & Punk  241.56
TV Shows            93.53
(5 rows)
expanded 1 metric: revenue
```

```console
$ slaw query "SELECT * FROM metric('avg_invoice_value', by => 'country') ORDER BY 2 DESC, 1 LIMIT 5"
country         avg_invoice_value
--------------  -----------------
Chile           6.659999999999999
Hungary         6.517142857142857
Ireland         6.517142857142857
Czech Republic  6.445714285714286
Austria         6.088571428571428
(5 rows)
expanded 1 metric: avg_invoice_value
```

```console
$ slaw query "SELECT * FROM metric('active_customers', by => 'year') ORDER BY 1"
year  active_customers
----  ----------------
2009  46
2010  46
2011  47
2012  47
2013  46
(5 rows)
expanded 1 metric: active_customers
```

Several metrics in one statement, joined on the dimension they share:

```console
$ slaw query "SELECT year, revenue, invoice_count, avg_invoice_value FROM metric('revenue', by => 'year') JOIN metric('invoice_count', by => 'year') USING (year) JOIN metric('avg_invoice_value', by => 'year') USING (year) ORDER BY 1"
year  revenue  invoice_count  avg_invoice_value
----  -------  -------------  ------------------
2009  449.46   83             5.415180722891566
2010  481.45   83             5.800602409638554
2011  469.58   83             5.6575903614457825
2012  477.53   83             5.753373493975904
2013  450.58   80             5.63225
(5 rows)
expanded 3 metrics: revenue, invoice_count, avg_invoice_value
```

Metrics compose in a `SELECT` list too:

```console
$ slaw query "SELECT metric('revenue') AS revenue, metric('revenue') / metric('active_customers') AS revenue_per_customer"
revenue  revenue_per_customer
-------  --------------------
2328.60  39.46779661016949
(1 row)
expanded 3 metrics: revenue, revenue, active_customers
```

`avg_invoice_value` is `sum(unit_price * quantity) / nullif(count(DISTINCT invoice_id), 0)`, one expression, so each group is a real ratio and not an average of averages. Two things to know about additivity: `invoice_count` and `active_customers` count distinct values, so by genre they overlap (an invoice with a rock track and a jazz track is in both), and they do not sum to the total. The metric descriptions say so.

## A saved query with parameters

`queries/top-customers.sql` declares `$year` (default 2013) and `$min_spend` (default 10):

```console
$ head -n 1 queries/top-customers.sql
-- slaw: {"title":"Top customers","description":"Customers by what they spent in one year","parameters":[{"name":"year","type":"integer","required":false,"default":"2013"},{"name":"min_spend","type":"number","required":false,"default":"10"}]}
```

```console
$ slaw query --saved top-customers --limit 4
customer_id  first_name  last_name  country         spent
-----------  ----------  ---------  --------------  -----
6            Helena      Holý       Czech Republic  27.84
35           Madalena    Sampaio    Portugal        24.75
56           Diego       Gutiérrez  Argentina       24.75
18           Michelle    Brooks     USA             22.77
(4 rows)
showing the first 4 of 18 rows (--limit 0 for all)
```

```console
$ slaw query --saved top-customers -p year=2011 -p min_spend=15
customer_id  first_name  last_name   country  spent
-----------  ----------  ----------  -------  -----
46           Hugh        O'Reilly    Ireland  32.75
8            Daan        Peeters     Belgium  24.75
29           Robert      Brown       Canada   24.75
50           Enrique     Muñoz       Spain    22.77
25           Victor      Stevens     USA      20.84
4            Bjørn       Hansen      Norway   17.84
42           Wyatt       Girard      France   17.84
44           Terhi       Hämäläinen  Finland  15.88
17           Jack        Smith       USA      15.84
21           Kathy       Chase       USA      15.84
38           Niklas      Schröder    Germany  15.84
59           Puja        Srivastava  India    15.84
(12 rows)
```

## The notebook

`notebooks/store-metrics.slnb` has five SQL cells that call `metric()`. Open it from Studio's Library. A notebook stores the questions, not the answers, so every cell runs again when you open it.

## Open it in Studio

Close Studio before running `slaw import`, `slaw semantics check` or `slaw semantics profile` from a terminal: they cannot use a local DuckDB that Studio holds.

```
slaw studio --gui
```

Choose **Open folder** and pick `docs/examples/chinook` (after the import step). Then **Semantics** in the sidebar:

- **Metrics**: `revenue`'s card shows its measure, its dimensions, the saved query `sales-lines` as a link, and the `settled_by` text. **Try it** with `genre` ticked runs the metric by genre in a grid. **Run assertions** gives pass or fail per check.
- **Joins**: each card shows the declared cardinality beside the measured one, with the counts and the date. `customer_support_rep` shows a warning that the data contradicts its declaration. **Profile** measures a join again and rewrites its `measured:` block.
- **Entities**: grain, table, id column, the `pii` columns and **Run assertions**.
- **Glossary**: the six terms, rendered.
- **Proposals** (with a badge when any are open): see next.

## Proposing a metric

An agent can read the layer (`list_definitions`, `get_definition`, `metric()`) but it **cannot write under `semantics/`**. It can only propose, and you decide.

1. In Studio's chat (tier read-write or full), or from an MCP client attached with `slaw mcp serve`, ask: "How many tracks do we sell? Propose a metric." The agent calls `propose_definition`, which checks the definition with the same validator the files go through (name, measure, saved query exists), and files it in `.slaw-local/proposals/` on this machine. It is git-ignored and never shared. With **Ask before changes** on (the default), a headless `slaw mcp serve` checks the proposal but files nothing, since no one can approve it there. Use Studio's chat, keep Studio open with its MCP server on so you get an approval card, or turn that switch off.
2. You see it in Studio (Semantics, Proposals, with a line diff) or on the command line.

The session below was not produced by a model: the proposal file was written by hand, in the exact format `propose_definition` writes, so the commands have something to show. The commands and their output are real.

To follow along, create the proposal first:

```console
$ mkdir -p .slaw-local/proposals && cat > .slaw-local/proposals/20261030T001500Z-metric-tracks_sold-7f3a.yaml <<'EOF'
version: 1
id: 20261030T001500Z-metric-tracks_sold-7f3a
kind: metric
name: tracks_sold
status: open
proposed_at: "2026-10-30T00:15:00Z"
proposed_by: agent
rationale: Revenue says how much was paid, but not how many tracks that was. Every invoice line sells quantity tracks, so the number sold is sum(quantity) over the same sales-lines rows as revenue, with the same dimensions.
evidence:
  - saved query sales-lines
  - invoice_line.quantity is 1 on all 2240 rows
replaces_rev: ""
definition:
  version: 1
  name: tracks_sold
  title: Tracks sold
  description: How many tracks were bought. Each invoice line is quantity tracks.
  query: sales-lines
  measure: sum(quantity)
  dimensions: [country, genre, year]
  format: number
  owner: sales
  assertions:
    - {kind: non_negative}
decided_at: ""
EOF
```

```console
$ slaw semantics proposals
ID                                        KIND    NAME         STATUS  BY     WHY
20261030T001500Z-metric-tracks_sold-7f3a  metric  tracks_sold  open    agent  Revenue says how much was paid, but not how many tracks tha…
```
```console
$ slaw semantics show-proposal 20261030T001500Z-metric-tracks_sold-7f3a
Proposal  20261030T001500Z-metric-tracks_sold-7f3a
Proposes  metric tracks_sold  (open, 2026-10-30T00:15:00Z, by agent)
Writes    semantics/metrics/tracks_sold.yaml
Rev       e31ef0f34c990a23
Why       Revenue says how much was paid, but not how many tracks that was. Every invoice line sells quantity tracks, so the number sold is sum(quantity) over the same sales-lines rows as revenue, with the same dimensions.
Evidence  saved query sales-lines
Evidence  invoice_line.quantity is 1 on all 2240 rows

semantics/metrics/tracks_sold.yaml is new.

+ version: 1
+ name: tracks_sold
+ title: Tracks sold
+ description: How many tracks were bought. Each invoice line is quantity tracks.
+ query: sales-lines
+ measure: sum(quantity)
+ dimensions:
+   - country
+   - genre
+   - year
+ format: number
+ owner: sales
+ assertions:
+   - kind: non_negative
```
```console
$ slaw semantics accept 20261030T001500Z-metric-tracks_sold-7f3a
Accepted 20261030T001500Z-metric-tracks_sold-7f3a: wrote semantics/metrics/tracks_sold.yaml
Proposal  20261030T001500Z-metric-tracks_sold-7f3a
Proposes  metric tracks_sold  (open, 2026-10-30T00:15:00Z, by agent)
Writes    semantics/metrics/tracks_sold.yaml
Rev       e31ef0f34c990a23
Why       Revenue says how much was paid, but not how many tracks that was. Every invoice line sells quantity tracks, so the number sold is sum(quantity) over the same sales-lines rows as revenue, with the same dimensions.
Evidence  saved query sales-lines
Evidence  invoice_line.quantity is 1 on all 2240 rows

semantics/metrics/tracks_sold.yaml is new.

+ version: 1
+ name: tracks_sold
+ title: Tracks sold
+ description: How many tracks were bought. Each invoice line is quantity tracks.
+ query: sales-lines
+ measure: sum(quantity)
+ dimensions:
+   - country
+   - genre
+   - year
+ format: number
+ owner: sales
+ assertions:
+   - kind: non_negative
```
```console
$ slaw query "SELECT * FROM metric('tracks_sold', by => 'year') ORDER BY 1"
year  tracks_sold
----  -----------
2009  454
2010  455
2011  442
2012  447
2013  442
(5 rows)
expanded 1 metric: tracks_sold
```

`show-proposal` prints who proposed it, why, the evidence, and the diff against the file it would write. `accept` writes exactly what was shown and pins it to the version you looked at: if the proposal or the target file changed after you looked, it refuses. `slaw semantics reject <id>` declines it and touches nothing. After accepting, commit `semantics/metrics/tracks_sold.yaml` like any other file. (This workspace does not include that file: the repository keeps the layer you see above.)
