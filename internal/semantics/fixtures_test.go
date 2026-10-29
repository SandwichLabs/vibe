package semantics

const goodEntity = `version: 1
name: customer
description: People who bought something.
grain: one row per customer
table: prod_pg.public.customers
connection: prod_pg
id_column: customer_id
columns:
  - {name: email, pii: true, description: contact address}
  - {name: region}
owner: data team
assertions:
  - {kind: unique, column: customer_id}
  - {kind: not_null, column: created_at}
  - {kind: accepted_values, column: status, values: [active, churned]}
`

const goodMetric = `version: 1
name: net_revenue
title: Net revenue
description: Revenue after refunds.
query: net_revenue_base
measure: sum(amount)
dimensions: [region, month]
format: currency
owner: finance
settled_by:
  text: "A counted refunds, B excluded them. Refunds are excluded."
  ref: ""
assertions:
  - {kind: non_negative}
  - {kind: not_null}
`

const goodJoin = `version: 1
name: orders_customers
left: orders
right: customer
on:
  - {left: customer_id, right: customer_id}
cardinality: many_to_one
description: each order has one customer
measured:
  at: 2026-10-29T15:30:12Z
  left_rows: 12000
  right_rows: 800
  max_fanout: 1
  left_orphans: 12
  right_orphans: 3
  verdict: consistent
`

const goodGlossary = "# Glossary\n\nIntro.\n\n## Active customer\n\nBought in the last 90 days.\n\n```\n## not a term\n```\n\n## Refund ##\n\nMoney back.\n"
