# Starter Templates

A **template** seeds a fresh **workspace** (Studio's New workspace dialog, or `vibe workspace init --template`) with a complete little relational dataset, written into the workspace's local `data/local.duckdb` connection and a `notebooks/*.vnb` file:
DuckDB user tables (with inline foreign keys so referential integrity is
enforced), sample data, and a starter **notebook** with a cell over each table
so the workspace is immediately explorable. (#68: forms, the switchboard,
relationship/field-meta metadata, and macros were dropped from every
template along with the Access application layer — a template is now just
tables + seed data + a notebook.)

Pick a template in the New workspace dialog ("Start from"), or pass its name to
`vibe workspace init --template`. The registered starter templates are:

## contacts
A contact book: a `contacts` table plus a one-to-many `contact_notes` child.
The starter notebook has a cell listing contacts and a cell of recent notes
joined back to their contact.

## inventory
Stock tracking: `suppliers` and `products` with a supplier→product
relationship. The starter notebook lists suppliers, lists products joined to
their supplier, and charts the lowest-stock products.

## order-entry
The canonical order app: `customers` → `orders` → `order_items` (two-level
relationship). The starter notebook has a cell each for customers, orders
(joined to their customer), and order line items (joined to their order).

## training-registrations
Course sign-ups: `courses` with a one-to-many `registrations` child. The
starter notebook lists courses, lists registrations joined to their course,
and charts registrations per course.

---

Each template is registered in `internal/templates/templates.go`; the
docs-consistency test in `internal/docsref/` fails if a registered template name
is missing from this list (and `templates.List()` is the source of truth).
