# Macros

A **macro** is an ordered list of actions triggered by a button, a form event, or
a switchboard entry — the Access macro homage, reimagined as the no-code
automation layer. Macros are persisted in `_vibe_macros`; their action kinds are
the `Action*` constants in `internal/automation/macro.go`.

Each macro action below is recognized by the macro engine. The docs-consistency
test in `internal/docsref/` fails if an action constant exists in code but is not
documented here.

## Navigation actions

### OpenForm
Opens a bound form by id (optionally with a filter or a target record).

### OpenReport
Opens a banded report by id, optionally filtered.

### OpenSwitchboard
Navigates to a switchboard page (the App-Mode nav shell).

### GoToRecord
Moves the current form to a record: first, previous, next, last, or new.

### FindRecord
Searches the current form's record source for a matching value.

## Data actions

### RunQuery
Runs a named query from the query registry.

### RunSQL
Executes an explicit SQL statement (Studio/full sandbox only).

### SetValue
Sets a field or parameter to a computed value.

### SetFilter
Applies a filter expression to the current form or report.

## Output and messaging actions

### MsgBox
Shows a message box to the user.

### Export
Exports the current object or result set (CSV/XLSX/PDF — see
[`import-office.md`](import-office.md)).

### EmailObject
Emails an exported object as an attachment (Office Link).

## Composition

### RunMacro
Runs another macro by name, allowing macros to be composed from smaller macros.
