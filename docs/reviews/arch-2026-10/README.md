# Architecture review, 2026-10 (#54)

- `RUBRIC.md`: the judge's rubric.
- `FINDINGS.md`: the survey.
- `JUDGE-1.md`: scores and the ruling on the eleven slices (21/36).
- `JUDGE-2.md`, `JUDGE-3.md`: mid-run passes (26/36, 32/36) and their remaining items.
- `JUDGE-4.md`: the clean ruling at 0e89fbd (34/36).
- `arch-2026-10-final.md`: final scores, waivers with reasons, and the guard rails that keep it clean.

The ast-grep rules live in `tools/ast-grep/` (`sgconfig.yml` and `rules/`). Run
`ast-grep scan -c tools/ast-grep/sgconfig.yml cmd internal studio` from the repo root (the rules' globs are relative to it).
