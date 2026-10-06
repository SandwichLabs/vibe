-- vibe: {"title":"Top customers","description":"Biggest spenders <last 90 days> & more","connection":"prod_pg","parameters":[{"name":"min","type":"number","required":true,"default":"100"}]}
SELECT customer, sum(amt) AS total
FROM orders
WHERE amt > $min
GROUP BY 1
ORDER BY 2 DESC
LIMIT 10
