# Chinook glossary

## Revenue

What customers paid for tracks, summed from invoice lines (`unit_price * quantity`), in dollars. Not the sum of `invoice.total`: see the `settled_by` note on the revenue metric.

## Active customer

A customer with at least one invoice line in the period or group being looked at. Not "has an account": all 59 customers have bought something at some point.

## Invoice

One purchase by one customer, made of one or more invoice lines.

## Invoice line

One track on one invoice. The store sells each track once per line (quantity is 1 everywhere, and an assertion keeps it so).

## Average invoice value

Revenue divided by the number of distinct invoices in the group. Read it beside `invoice_count`: the same average can come from one large invoice or many small ones.

## Support rep

The sales support agent (an employee) assigned to a customer.
