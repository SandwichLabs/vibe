# Vibe v1.0 Reference

This is the shipped reference documentation for the Vibe application surface —
the modern, local-first successor to Microsoft Access 97. It is split by surface
so each area is easy to find:

| Reference | Covers |
|---|---|
| [`pages.md`](pages.md) | Page **directives** (`{table:}`, `{chart:}`, `{form:}`, …) |
| [`forms.md`](forms.md) | **Form** archetypes and bound controls |
| [`reports.md`](reports.md) | Banded **report** sections |
| [`macros.md`](macros.md) | **Macro** actions |
| [`import-office.md`](import-office.md) | **Import** Spreadsheet Wizard and **Office** Links |
| [`templates.md`](templates.md) | Bundled starter **templates** |

A test in `internal/docsref/` keeps these docs honest: every directive registered
in `internal/pages/parser.go`, every macro action in `internal/automation/macro.go`,
and every starter template in `internal/container` must appear here, and no doc may
reference a directive that the parser no longer recognizes.
