# Example: Gapminder

The weighted-average example. One fact table (a country in a year), one lookup table (country codes), three metrics by continent and year, and a join to the lookup whose profile shows "orphans" that are perfectly normal.

New to this? Start with the [penguins example](../penguins/README.md). The reference for every file here is [`docs/reference/semantics.md`](../../reference/semantics.md).

## The data

An excerpt of the [Gapminder](https://www.gapminder.org/data/) data: life expectancy, population and GDP per capita for 142 countries, every fifth year from 1952 to 2007 (1,704 rows), plus ISO 3166-1 country codes. The files here come from Jennifer Bryan's [`gapminder` R package](https://github.com/jennybc/gapminder) (`inst/extdata/gapminder.tsv` and `country-codes.tsv`).

**Licence and attribution.** The Gapminder data is licensed by Gapminder Foundation under **CC BY 4.0** ([creativecommons.org/licenses/by/4.0](https://creativecommons.org/licenses/by/4.0/)). Source: Gapminder, [www.gapminder.org/data](https://www.gapminder.org/data/), via the `gapminder` R package (which itself declares its own packaging as CC0). If you reuse these files, credit Gapminder. Changes made here: the tab-separated files were converted to CSV, and the columns `lifeExp` and `gdpPercap` were renamed `life_exp` and `gdp_percap` so they follow the lower-case name rule (a dimension or column name is `^[a-z_][a-z0-9_]*$`). The file is 82 KB, and the codes file 3 KB.

## Setup

Run every command below from this folder (`cd docs/examples/gapminder`).

An entity's `table` is a SQL name, not a file path, so the first step loads the CSVs into the workspace's local DuckDB (`data/local.duckdb`, git-ignored). The excerpt has no id column (a country and a year together identify a row, but an entity needs one `id_column`), so we ask for a generated one. The codes are keyed by their ISO alpha code:

```console
$ vibe import data/gapminder.csv --key auto
Imported 1704 row(s) into local.main.gapminder
```

```console
$ vibe import data/country_codes.csv --key iso_alpha
Imported 187 row(s) into local.main.country_codes
```

```console
$ vibe query "DESCRIBE gapminder"
column_name  column_type  null  key   default                            extra
-----------  -----------  ----  ----  ---------------------------------  -----
id           BIGINT       NO    PRI   nextval('_vibe_seq_gapminder_id')  NULL
country      VARCHAR      YES   NULL  NULL                               NULL
continent    VARCHAR      YES   NULL  NULL                               NULL
year         BIGINT       YES   NULL  NULL                               NULL
life_exp     DOUBLE       YES   NULL  NULL                               NULL
pop          BIGINT       YES   NULL  NULL                               NULL
gdp_percap   DOUBLE       YES   NULL  NULL                               NULL
(7 rows)
```

Each import also writes a table-bound skill, `skills/workspace-local-main-<table>/SKILL.md`, with the table's row count, column types and sample values (generated, regenerated on every import). They are left out of this folder, so the import step creates them; delete them or commit them as you like.

## What is defined

```
queries/country-years.sql             one row per country-year: the rows the metrics measure
queries/life-expectancy-ranking.sql   a saved query with two parameters
semantics/entities/                   country_year, country_code
semantics/metrics/                    population, life_expectancy, life_expectancy_by_country, gdp_total
semantics/joins/country_year_code.yaml
semantics/glossary.md
notebooks/world-metrics.vnb
```

```console
$ vibe semantics list
KIND      NAME                        SUMMARY                                                                ERROR
entity    country_code                one row per country                                                    -
entity    country_year                one row per country per year                                           -
metric    gdp_total                   sum(gdp_percap * pop) by continent, year, country                      -
metric    life_expectancy             sum(life_exp * pop) / nullif(sum(pop), 0) by continent, year, country  -
metric    life_expectancy_by_country  avg(life_exp) by continent, year                                       -
metric    population                  sum(pop) by continent, year, country                                   -
metric    population_by_country       sum(pop) by country, year                                              -
join      country_year_code           country_year to country_code (many_to_one)                             -
glossary  glossary.md                 5 terms                                                                -
```

## The weighted average

Life expectancy for a continent is not the mean of its countries' figures: a country of a billion people should count for more than an island of a hundred thousand. The metric is a **population-weighted** mean, a ratio of two sums:

```console
$ vibe semantics show metric life_expectancy
version: 1
name: life_expectancy
title: Life expectancy (population-weighted)
description: Life expectancy at birth in years, averaged over countries weighted by population, so a country counts in proportion to its people. By country and year it is that country's own figure.
query: country-years
measure: sum(life_exp * pop) / nullif(sum(pop), 0)
dimensions: [continent, year, country]
format: number
owner: example
settled_by:
  text: "One analyst took the plain mean of the country figures, another weighted by population. In the Americas in 2007 the weighted figure is 75.4 and the plain mean 73.6, because the United States (78.2 years) and Mexico (76.2) have far more people than the typical small country of the region. The weighted mean answers 'how long does the typical person on this continent expect to live?', so that is the definition. The plain mean is kept as life_expectancy_by_country for comparison."
assertions:
  - {kind: non_negative}
  - {kind: not_null}
```

It is one expression, `sum(life_exp * pop) / nullif(sum(pop), 0)`, so each group is a real weighted mean (`nullif` keeps an empty group from dividing by zero, giving NULL instead of an error). A weighted mean must be computed from the raw rows for every group you ask for. You cannot average the country figures, or the continent figures, afterwards: that gives the plain mean. The `settled_by` block records why the weighted one is the definition.

The two side by side, 2007 (the second metric is the plain mean `avg(life_exp)`, kept to show the difference):

```console
$ vibe query "SELECT w.continent, w.life_expectancy AS weighted, p.life_expectancy_by_country AS plain_mean FROM metric('life_expectancy', by => ['continent', 'year']) AS w JOIN metric('life_expectancy_by_country', by => ['continent', 'year']) AS p ON p.continent = w.continent AND p.year = w.year WHERE w.year = 2007 ORDER BY 1"
continent  weighted           plain_mean
---------  -----------------  -----------------
Africa     54.56441057558195  54.80603846153845
Americas   75.35668222743027  73.60812
Asia       69.44386304205017  70.72848484848484
Europe     77.89057081069896  77.64859999999999
Oceania    81.06215400970112  80.7195
(5 rows)
expanded 2 metrics: life_expectancy, life_expectancy_by_country
```

## Check

```console
$ vibe semantics check
STATUS  SUBJECT                            CHECK                      DETAIL
pass    entity country_code                compile                    compiles
pass    entity country_code                unique iso_alpha           
pass    entity country_code                unique country             
pass    entity country_year                compile                    compiles
pass    entity country_year                unique id                  
pass    entity country_year                not_null life_exp          
pass    entity country_year                not_null pop               
pass    entity country_year                accepted_values continent  
pass    entity country_year                row_count                  1704 rows
pass    entity country_year                range life_exp             
pass    metric gdp_total                   compile                    compiles
pass    metric gdp_total                   non_negative               
pass    metric gdp_total                   not_null                   
pass    metric life_expectancy             compile                    compiles
pass    metric life_expectancy             non_negative               
pass    metric life_expectancy             not_null                   
pass    metric life_expectancy_by_country  compile                    compiles
pass    metric life_expectancy_by_country  non_negative               
pass    metric population                  compile                    compiles
pass    metric population                  non_negative               
pass    metric population                  not_null                   
pass    metric population_by_country       compile                    compiles
pass    metric population_by_country       unique                     
pass    metric population_by_country       range                      

24 passed, 0 failed, 0 could not run
```

All pass. `country_year` asserts the continent is one of the five known values (`accepted_values`), and that no row lacks a life expectancy or a population (a row with a NULL `life_exp` leaves `sum(life_exp * pop)` but its `pop` stays in `sum(pop)`, which drags the weighted mean down, so `not_null` guards it). An entity's `unique` takes one column, not two, so there is no entity assertion that a country and year pair is unique (the generated `id` is unique by construction); the `population_by_country` metric covers that pair.

Three more kinds are here. `country_year` declares `row_count` (at least 1,704 rows; the DETAIL says how many it found) and a `range` on `life_exp` (between 20 and 90 years; a value outside it would be counted, so a unit slip such as a fraction of 1 or a percentage over 100 is caught). `population_by_country` is measured by country and year, and its saved query has exactly one row for each, so it declares `unique`: the check counts the country-and-year pairs the saved query has more than one row for, and `range` with only a `min` keeps the sums non-negative. `unique` on a metric is for a saved query that is meant to be at the metric's grain. The other metrics here are measured over the same rows with fewer dimensions, so the same assertion on `life_expectancy_by_country` (by continent and year) would fail: pointed at `[continent, year]`, `population_by_country` reports `fail  unique  60 violating rows`, one for each of the 5 continents and 12 years that hold many rows.

## Ask the metrics

```console
$ vibe query "SELECT * FROM metric('population', by => 'year') ORDER BY 1"
year  population
----  ----------
1952  2406957150
1957  2664404580
1962  2899782974
1967  3217478384
1972  3576977158
1977  3930045807
1982  4289436840
1987  4691477418
1992  5110710260
1997  5515204472
2002  5886977579
2007  6251013179
(12 rows)
expanded 1 metric: population
```

```console
$ vibe query "SELECT p.continent, p.population, l.life_expectancy FROM metric('population', by => ['continent', 'year']) AS p JOIN metric('life_expectancy', by => ['continent', 'year']) AS l ON l.continent = p.continent AND l.year = p.year WHERE p.year = 2007 ORDER BY 2 DESC"
continent  population  life_expectancy
---------  ----------  -----------------
Asia       3811953827  69.44386304205017
Africa     929539692   54.56441057558195
Americas   898871184   75.35668222743027
Europe     586098529   77.89057081069896
Oceania    24549947    81.06215400970112
(5 rows)
expanded 2 metrics: population, life_expectancy
```

A population is a snapshot, so ask for it by `year` or by `continent` and `year`. Leave `year` out and you get a sum of twelve snapshots, which is not a number of people; the metric's description says so, and nothing stops you. Total GDP is the same kind of figure (per-capita GDP times population, summed):

```console
$ vibe query "SELECT year, gdp_total / 1e12 AS gdp_trillion FROM metric('gdp_total', by => 'year') ORDER BY year"
year  gdp_trillion
----  ------------------
1952  7.03768910830791
1957  8.896268418182263
1962  11.004370714586017
1967  14.248094060021938
1972  18.42143381476223
1977  22.318196019084958
1982  25.381986325295735
1987  30.132797018057246
1992  34.50091055578727
1997  41.00607686847419
2002  47.2663334867083
2007  58.10933471390455
(12 rows)
expanded 1 metric: gdp_total
```

And a weighted metric over time:

```console
$ vibe query "SELECT year, life_expectancy FROM metric('life_expectancy', by => ['continent', 'year']) WHERE continent = 'Asia' ORDER BY year"
year  life_expectancy
----  -----------------
1952  42.9411433815645
1957  47.28834935225735
1962  46.57369394338028
1967  53.88260961430009
1972  57.52158513059763
1977  59.5564806946822
1982  61.57471594719365
1987  63.53710156384658
1992  65.1487439322734
1997  66.77092122763746
2002  68.13731854047926
2007  69.44386304205017
(12 rows)
expanded 1 metric: life_expectancy
```

## Profile the join

`country_year_code` joins each country-year to its country's ISO code, `many_to_one`:

```console
$ vibe semantics profile country_year_code
join country_year_code (declared many_to_one)
  left rows            1704
  right rows           187
  max right per left   1
  left orphans         0
  right orphans        45
  verdict              consistent
recorded in semantics/joins/country_year_code.yaml (measured:)
```

How to read it:

- `max right per left 1`, `left orphans 0`: every country-year finds exactly one code. This is the half of a `many_to_one` that has to hold, and it does.
- **`right orphans 45`**: 45 of the 187 codes belong to countries that never appear in the excerpt:

```console
$ vibe query "SELECT country FROM country_codes WHERE country NOT IN (SELECT country FROM gapminder) ORDER BY 1 LIMIT 6"
country
----------
Armenia
Aruba
Azerbaijan
Bahamas
Barbados
Belarus
(6 rows)
```

  The code list is wider than the data. Extra rows on the "one" side are exactly what a `many_to_one` expects, so the verdict is `consistent`, and the profile says "right orphans" to inform you, not to warn. An inner join from the data to the codes drops nothing; starting from the codes, 45 have no data rows.

The result is written to the `measured:` block of `semantics/joins/country_year_code.yaml`.

## A saved query with parameters

`queries/life-expectancy-ranking.sql` declares `$year` (default 2007) and `$continent` (default `Asia`):

```console
$ head -n 1 queries/life-expectancy-ranking.sql
-- vibe: {"title":"Life expectancy ranking","description":"Countries of one continent in one year, longest-lived first","parameters":[{"name":"year","type":"integer","required":false,"default":"2007"},{"name":"continent","type":"text","required":false,"default":"Asia"}]}
```

```console
$ vibe query --saved life-expectancy-ranking --limit 4
country           life_exp  pop        gdp_percap
----------------  --------  ---------  ----------
Japan             82.603    127467972  31656
Hong Kong, China  82.208    6980412    39725
Israel            80.745    6426679    25523
Singapore         79.972    4553009    47143
(4 rows)
showing the first 4 of 33 rows (--limit 0 for all)
```

```console
$ vibe query --saved life-expectancy-ranking -p year=1952 -p continent=Europe --limit 4
country      life_exp  pop       gdp_percap
-----------  --------  --------  ----------
Norway       72.67     3327728   10095
Iceland      72.49     147962    7268
Netherlands  72.13     10381988  8942
Sweden       71.86     7124673   8528
(4 rows)
showing the first 4 of 30 rows (--limit 0 for all)
```

## The notebook

`notebooks/world-metrics.vnb` has four SQL cells calling `metric()` (continents, Asia over time, weighted against plain mean, world GDP). Open it from Studio's Library.

## Open it in Studio

Close Studio before running `vibe import`, `vibe semantics check` or `vibe semantics profile` from a terminal: they cannot use a local DuckDB that Studio holds.

```
vibe studio --gui
```

Choose **Open folder** and pick `docs/examples/gapminder` (after the import step). **Semantics** in the sidebar:

- **Metrics**: `life_expectancy`'s card shows the weighted measure and the `settled_by` text. **Try it** with `continent` and `year` ticked runs it as a grid.
- **Entities** and their **Run assertions**.
- **Joins**: `country_year_code` shows declared `many_to_one` beside measured, with the 45 right orphans, and no warning.
- **Glossary**: the five terms.

## The agent

An agent can use these metrics (`metric()` in `run_query`, `list_definitions`, `get_definition`) but cannot change them. Ask it, for instance, "propose a metric for GDP per capita of a continent". The right answer is `sum(gdp_percap * pop) / nullif(sum(pop), 0)` (a population-weighted mean of per-capita GDP, which equals total GDP over total population), and **not** `avg(gdp_percap)`. Whatever it files, `vibe semantics proposals`, `show-proposal <id>` and `accept <id>` are yours to run; the [Chinook example](../chinook/README.md#proposing-a-metric) shows the commands.
