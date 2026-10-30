-- vibe: {"title":"Sales lines","description":"One row per invoice line, with the customer's country, the track's genre and the invoice year: the rows every revenue metric is measured over"}
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
