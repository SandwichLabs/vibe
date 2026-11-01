-- slaw: {"title":"Top customers","description":"Customers by what they spent in one year","parameters":[{"name":"year","type":"integer","required":false,"default":"2013"},{"name":"min_spend","type":"number","required":false,"default":"10"}]}
SELECT c.customer_id, c.first_name, c.last_name, c.country, sum(l.unit_price * l.quantity) AS spent
FROM invoice_line AS l
JOIN invoice AS i ON i.invoice_id = l.invoice_id
JOIN customer AS c ON c.customer_id = i.customer_id
WHERE year(i.invoice_date) = $year
GROUP BY ALL
HAVING sum(l.unit_price * l.quantity) >= $min_spend
ORDER BY spent DESC, c.customer_id
