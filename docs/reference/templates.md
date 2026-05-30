# Starter Templates

A **template** seeds a fresh `.vibe` with a complete little relational application:
DuckDB user tables (with inline foreign keys so referential integrity is enforced),
sample data, Access-style metadata (`_vibe_relationships`, `_vibe_field_meta`,
`_vibe_forms`, `_vibe_nav`), and a Switchboard nav entry. They are the modern
stand-ins for the Access "Database Wizard" starter databases.

Pass a template name to the container's `WithTemplate` option when creating a file.
The registered starter templates are:

## contacts
A contact book: a `contacts` table plus a one-to-many `contact_notes` child. Ships
a **master-detail** form (contact + notes subform) on the switchboard.

## inventory
Stock tracking: `suppliers` and `products` with a supplier→product relationship.
Ships a **continuous** Products form and a **columnar** Suppliers form.

## order-entry
The canonical order app: `customers` → `orders` → `order_items` (two-level
relationship). Ships a **master-detail** Order Entry form with a line-items
subform.

## training-registrations
Course sign-ups: `courses` with a one-to-many `registrations` child. Ships a
master-detail Course Registrations form.

---

Each template is registered in `internal/container/templates.go`; the
docs-consistency test in `internal/docsref/` fails if a registered template name
is missing from this list (and `container.ListTemplates()` is the source of truth).
