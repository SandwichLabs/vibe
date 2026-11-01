-- assay: {"title":"Life expectancy ranking","description":"Countries of one continent in one year, longest-lived first","parameters":[{"name":"year","type":"integer","required":false,"default":"2007"},{"name":"continent","type":"text","required":false,"default":"Asia"}]}
SELECT country, life_exp, pop, round(gdp_percap) AS gdp_percap
FROM gapminder
WHERE year = $year AND continent = $continent
ORDER BY life_exp DESC, country
