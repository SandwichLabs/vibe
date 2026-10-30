# Gapminder glossary

## Life expectancy

Years a newborn would live if today's death rates stayed the same. In this data it is a figure for a whole country in a whole year. The continent figure is the population-weighted mean of its countries, not a plain mean of country figures.

## Population-weighted

Each country counts in proportion to its population. `sum(life_exp * pop) / sum(pop)`. A weighted mean is a ratio of two sums: it is computed in one expression and is never itself summed or averaged again.

## GDP per capita

Gross domestic product divided by population, in inflation-adjusted US dollars. To get a total, multiply by population again.

## Country year

One country in one survey year. The data has every fifth year from 1952 to 2007, so twelve rows for each of 142 countries.

## Continent

One of Africa, Americas, Asia, Europe, Oceania. Gapminder's own grouping, fixed per country in this excerpt.
