# Example: Palmer Penguins

The smallest semantic-layer example: one table, one entity, three metrics, four dimensions, assertions (one of them fails on purpose, on real data), a glossary and a saved query with a parameter. No joins; the [Chinook](../chinook/) example has those.

The reference for every file here is [`docs/reference/semantics.md`](../../reference/semantics.md).

## The data

Body measurements of 344 penguins of three species on three islands of the Palmer Archipelago, Antarctica, 2007 to 2009. Collected by Dr. Kristen Gorman at Palmer Station (LTER) and published in the R package [palmerpenguins](https://github.com/allisonhorst/palmerpenguins) by Allison Horst, Alison Hill and Kristen Gorman. The data is released under **CC0** (public domain).

`data/penguins.csv` is that package's `penguins.csv` with one edit: the literal `NA` of the original is an empty field. Without that, `vibe import` reads `NA` as text, and four numeric columns become `VARCHAR`. With empty fields, missing values are real NULLs and the numbers are typed (`DECIMAL`, `BIGINT`). The size is 15 KB.

## Setup

Run every command below from this folder (`cd docs/examples/penguins`).

The workspace was made with `vibe workspace init docs/examples/penguins`. The CSV is not queryable until it is a table, and an entity's `table` is a SQL name, not a file path, so the first step loads it into the workspace's local DuckDB (`data/local.duckdb`, git-ignored):

```console
$ vibe import data/penguins.csv --key auto
Imported 344 row(s) into local.main.penguins
```

The file has no id column. `--key auto` adds a generated `id` (a `BIGINT` primary key) so the entity has something that identifies a row:

```console
$ vibe query "DESCRIBE penguins"
column_name        column_type    null  key   default                           extra
-----------------  -------------  ----  ----  --------------------------------  -----
id                 BIGINT         NO    PRI   nextval('_vibe_seq_penguins_id')  NULL
species            VARCHAR        YES   NULL  NULL                              NULL
island             VARCHAR        YES   NULL  NULL                              NULL
bill_length_mm     DECIMAL(18,2)  YES   NULL  NULL                              NULL
bill_depth_mm      DECIMAL(18,2)  YES   NULL  NULL                              NULL
flipper_length_mm  BIGINT         YES   NULL  NULL                              NULL
body_mass_g        BIGINT         YES   NULL  NULL                              NULL
sex                VARCHAR        YES   NULL  NULL                              NULL
year               BIGINT         YES   NULL  NULL                              NULL
(9 rows)
```

Each import also writes a table-bound skill, `skills/workspace-local-main-<table>/SKILL.md`, with the table's row count, column types and sample values (generated, regenerated on every import). They are left out of this folder, so the import step creates them; delete them or commit them as you like.

## What is defined

```
queries/penguins.sql              the rows the metrics are measured over
queries/heavy-penguins.sql        a saved query with a parameter
semantics/entities/penguin.yaml
semantics/metrics/penguin_count.yaml
semantics/metrics/avg_body_mass_g.yaml
semantics/metrics/gentoo_share.yaml
semantics/glossary.md
notebooks/penguin-metrics.vnb
```

```console
$ vibe semantics list
KIND      NAME             SUMMARY                                                                                      ERROR
entity    penguin          one row per penguin                                                                          -
metric    avg_body_mass_g  avg(body_mass_g) by species, island, sex, year                                               -
metric    gentoo_share     1.0 * count(*) FILTER (WHERE species = 'Gentoo') / nullif(count(*), 0) by island, sex, year  -
metric    penguin_count    count(*) by species, island, sex, year                                                       -
glossary  glossary.md      5 terms                                                                                      -
```

`list` reads the files and nothing else: no database is opened. A file that fails validation would be listed here with its error in the last column. The `-` means none do.

```console
$ vibe semantics show metric gentoo_share
version: 1
name: gentoo_share
title: Gentoo share
description: The fraction of penguins that are Gentoo, a ratio of two counts over the same rows.
query: penguins
measure: 1.0 * count(*) FILTER (WHERE species = 'Gentoo') / nullif(count(*), 0)
dimensions: [island, sex, year]
format: percent
owner: example
settled_by:
  text: "The denominator is every penguin in the group, including those with no sex or mass recorded, so the share does not move when a measurement is missing."
assertions:
  - {kind: non_negative}
```

A metric is a **measure** over a **saved query**: `gentoo_share` is `1.0 * count(*) FILTER (...) / nullif(count(*), 0)` over the rows of `queries/penguins.sql`. It is a ratio of two counts over the same rows, so it is computed in one expression, never averaged across groups. The `settled_by` block records why the denominator is every bird, not just the ones with a recorded sex.

The entity says what a row is and which assertions must hold:

```console
$ vibe semantics show entity penguin
version: 1
name: penguin
description: One of the 344 penguins measured at Palmer Station, Antarctica, 2007-2009.
grain: one row per penguin
table: penguins
id_column: id
columns:
  - {name: sex, description: "male or female; empty when the field team could not tell"}
  - {name: body_mass_g, description: body mass in grams}
owner: example
assertions:
  - {kind: unique, column: id}
  - {kind: not_null, column: species}
  - {kind: accepted_values, column: species, values: [Adelie, Chinstrap, Gentoo]}
  - {kind: not_null, column: sex}
```

## Check

`vibe semantics check` turns each declared assertion into one read-only query that counts the violating rows:

```console
$ vibe semantics check
STATUS  SUBJECT                 CHECK                    DETAIL
pass    entity penguin          unique id                
pass    entity penguin          not_null species         
pass    entity penguin          accepted_values species  
fail    entity penguin          not_null sex             11 violating rows
pass    metric avg_body_mass_g  non_negative             
pass    metric gentoo_share     non_negative             
pass    metric penguin_count    non_negative             
pass    metric penguin_count    not_null                 

7 passed, 1 failed, 0 could not run
Error: 1 of 8 checks failed
(exit status 1)
```

One check fails on purpose. Eleven penguins have no recorded sex, so `not_null` on `sex` finds 11 rows. This is a true statement about the data, not a bug: it is what a check is for. The command exits with status 1 when any check fails (2 when a check could not run at all), so a script or CI job can tell "fix the data" from "fix the setup". To run only some:

```console
$ vibe semantics check metric
STATUS  SUBJECT                 CHECK         DETAIL
pass    metric avg_body_mass_g  non_negative  
pass    metric gentoo_share     non_negative  
pass    metric penguin_count    non_negative  
pass    metric penguin_count    not_null      

4 passed, 0 failed, 0 could not run
```

`vibe semantics check entity penguin --json` prints one object per check instead.

## Ask the metrics

`metric('name')` is a call Vibe expands before the SQL runs. You can use it in any SQL you type, in the CLI, the Query Explorer, a notebook cell, or an agent's `run_query`. It expands to a `SELECT` over the metric's saved query, grouped by the dimensions you name in `by`:

```console
$ vibe query "SELECT * FROM metric('penguin_count')"
penguin_count
-------------
344
(1 row)
expanded 1 metric: penguin_count
```

```console
$ vibe query "SELECT * FROM metric('penguin_count', by => 'species') ORDER BY 2 DESC"
species    penguin_count
---------  -------------
Adelie     152
Gentoo     124
Chinstrap  68
(3 rows)
expanded 1 metric: penguin_count
```

```console
$ vibe query "SELECT * FROM metric('avg_body_mass_g', by => ['species', 'sex']) ORDER BY 1, 2"
species    sex     avg_body_mass_g
---------  ------  ------------------
Adelie     female  3368.8356164383563
Adelie     male    4043.4931506849316
Adelie     NULL    3540
Chinstrap  female  3527.205882352941
Chinstrap  male    3938.970588235294
Gentoo     female  4679.741379310345
Gentoo     male    5484.836065573771
Gentoo     NULL    4587.5
(8 rows)
expanded 1 metric: avg_body_mass_g
```

The `NULL` sex rows are the eleven birds above. The averages leave out the two birds with no mass.

```console
$ vibe query "SELECT * FROM metric('gentoo_share', by => 'island') ORDER BY 2 DESC, 1"
island     gentoo_share
---------  ------------------
Biscoe     0.7380952380952381
Dream      0
Torgersen  0
(3 rows)
expanded 1 metric: gentoo_share
```

A dimension that is not declared is refused, and the refusal names the ones that are:

```console
$ vibe query "SELECT * FROM metric('penguin_count', by => 'colour')"
Error: metric('penguin_count'): 'colour' is not a dimension of penguin_count (dimensions: species, island, sex, year)
(exit status 1)
```

`vibe query` says `expanded 1 metric: ...` on stderr, so you can see that a metric ran. There is no join and no filter argument: filter on a dimension with an outer `WHERE`; any other restriction belongs in the saved query.

## A saved query with a parameter

`queries/heavy-penguins.sql` declares `$min_mass` in its header, an integer defaulting to 5000:

```console
$ head -n 2 queries/heavy-penguins.sql
-- vibe: {"title":"Heavy penguins","description":"Penguins at or above a body mass, heaviest first","parameters":[{"name":"min_mass","type":"integer","required":false,"default":"5000"}]}
SELECT species, island, sex, year, body_mass_g
```

`--saved` runs it by id, and `-p name=value` sets a parameter, typed by its declaration. The value is bound, never written into the SQL:

```console
$ vibe query --saved heavy-penguins --limit 3
species  island  sex   year  body_mass_g
-------  ------  ----  ----  -----------
Gentoo   Biscoe  male  2007  6300
Gentoo   Biscoe  male  2007  6050
Gentoo   Biscoe  male  2008  6000
(3 rows)
showing the first 3 of 67 rows (--limit 0 for all)
```

```console
$ vibe query --saved heavy-penguins -p min_mass=5800
species  island  sex   year  body_mass_g
-------  ------  ----  ----  -----------
Gentoo   Biscoe  male  2007  6300
Gentoo   Biscoe  male  2007  6050
Gentoo   Biscoe  male  2008  6000
Gentoo   Biscoe  male  2009  6000
Gentoo   Biscoe  male  2008  5950
Gentoo   Biscoe  male  2009  5950
Gentoo   Biscoe  male  2007  5850
Gentoo   Biscoe  male  2007  5850
Gentoo   Biscoe  male  2009  5850
Gentoo   Biscoe  male  2008  5800
Gentoo   Biscoe  male  2008  5800
(11 rows)
```

## The notebook

`notebooks/penguin-metrics.vnb` has four SQL cells, all calling `metric()`. Open it in Studio (Library, then Penguin metrics): every data cell re-runs on open, because a notebook stores the questions, not the answers.

## Open it in Studio

Close Studio before running `vibe import`, `vibe semantics check` or `vibe semantics profile` from a terminal: they cannot use a local DuckDB that Studio holds.

```
vibe studio --gui
```

Choose **Open folder** and pick `docs/examples/penguins` (run the import step first, or the metrics have no table to read). Then **Semantics** in the sidebar:

- **Metrics**: each card shows the measure, the dimensions and the saved query (a link that opens it in the Query Explorer). **Try it** ticks dimensions and runs the metric by them. **Run assertions** shows the same pass and fail as `vibe semantics check`.
- **Entities**: `penguin` with its grain, table, id column and assertions. Its **Run assertions** shows `not_null sex` failing with 11 violating rows.
- **Glossary**: the five terms, rendered.
- **Joins**: empty here.

## The agent and proposals

An agent can read all of this (`list_definitions`, `get_definition`, `metric()`), but it cannot write under `semantics/`. It can only **propose**. Ask the Studio chat, or an MCP client attached with `vibe mcp serve`, something like "propose a metric for the average flipper length by species". With **Ask before changes** on (the default), a headless `vibe mcp serve` checks the proposal but files nothing, since no one can approve it there. Use Studio's chat, keep Studio open with its MCP server on so you get an approval card, or turn that switch off. Then:

```
vibe semantics proposals
vibe semantics show-proposal <id>
vibe semantics accept <id>
```

Nothing changes until you accept. The [Chinook example](../chinook/README.md#proposing-a-metric) walks through that flow.
