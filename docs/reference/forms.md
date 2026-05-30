# Forms

A **form** is a bound, writable view over a DuckDB table. Forms are the primary
data-entry surface in App Mode and the Access-style "bound form" homage. Each form
is persisted as a versioned JSON spec in `_vibe_forms` and embedded into a page
with the `{form:}` directive.

## Form archetypes

Every form declares an **archetype** that controls its layout and editing model.
The four archetypes mirror the Access form views:

| Archetype | Layout | Use when |
|---|---|---|
| **columnar** | One record, fields stacked label-over-control | Single-record editing (a contact, a supplier). |
| **continuous** | Many records, one repeating row band per record | Browsing/editing a list of like records. |
| **datasheet** | Spreadsheet-style grid of rows and columns | Dense tabular entry, Excel-like editing. |
| **master-detail** | A parent record plus a bound child subform | Order + line items, contact + notes. |

## Bound controls

Each field in a form binds a table column to a control. Control types come from
`_vibe_field_meta.control_type` and the form spec's `fields[].control_type`:

| Control | Binds | Notes |
|---|---|---|
| `text` | TEXT / VARCHAR | Single-line text box. |
| `number` | INTEGER / DOUBLE | Numeric entry. |
| `date` | TEXT (ISO date) | Date picker. |
| `lookup` | foreign-key column | Combo box resolved through a relationship. |
| `toggle` | BOOLEAN | Checkbox / switch. |

A field carries a human **caption** (from `_vibe_field_meta.caption`) and a sort
order so the form renders fields in a deliberate sequence.

## Master-detail and subforms

A **master-detail** form pairs a parent record with a child **subform**. The
subform's spec records the child table, the child foreign-key column, and the
parent key column it joins on. In a page, the parent is rendered with `{form:}`
and the child band with `{subform:}`.

```
{form: id=order_entry_form}
```

The form's own spec embeds the subform, so a single `{form:}` directive renders
the full master-detail composition; `{subform:}` is available for composing a child
band independently when needed.

## Write-back

Forms write directly to DuckDB through the CRUD path. Relationships declared in
`_vibe_relationships` (with inline DuckDB foreign keys) enforce referential
integrity on insert/update/delete, so a master-detail form cannot orphan its
children.
