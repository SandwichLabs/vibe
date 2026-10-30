-- vibe: {"title":"Heavy penguins","description":"Penguins at or above a body mass, heaviest first","parameters":[{"name":"min_mass","type":"integer","required":false,"default":"5000"}]}
SELECT species, island, sex, year, body_mass_g
FROM penguins
WHERE body_mass_g >= $min_mass
ORDER BY body_mass_g DESC, id
