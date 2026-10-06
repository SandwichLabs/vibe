---
name: revenue-definitions
description: How we define revenue, net of refunds
table_name: orders
source: import
---

# Revenue

Revenue is `amount - refunds` where status <> 'void'.

- Booked vs recognised: see below
