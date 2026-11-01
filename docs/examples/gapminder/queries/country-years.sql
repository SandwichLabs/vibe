-- assay: {"title":"Country years","description":"One row per country and year: the rows every gapminder metric is measured over"}
SELECT id, country, continent, year, life_exp, pop, gdp_percap
FROM gapminder
