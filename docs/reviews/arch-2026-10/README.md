# Architecture review, 2026-10 (#54)

- `RUBRIC.md`: the judge's rubric.
- `FINDINGS.md`: the survey.
- `JUDGE-1.md`: scores and the ruling on the eleven slices.

The ast-grep rules live at the repo root (`sgconfig.yml` and `.ast-grep/rules/`) so their `files:` and
`ignores:` globs resolve. Run `ast-grep scan cmd internal studio` from the root.
