#!/usr/bin/env bash
# Fails when the `ssofake` build tag (AWS sign-in pointed at local fakes, for Rodney QA only) can reach a CI,
# release or packaging build. Any mention of the word in a workflow, the Taskfile, wails.json or scripts/ must be one
# of the allowed lines: a comment or step name, the `wails dev` line and description of `task studio:dev:ssofake`, the
# CI `go vet -tags ssofake` line, or a call of this check or of check-binary-tags.sh. A tag passed through a variable
# (LINUX_TAGS, tags=...) therefore fails too. Built binaries are checked by scripts/check-binary-tags.sh.
# Usage: scripts/check-no-ssofake.sh   (run from anywhere)
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
fail=0
allowed='^[0-9]+:[[:space:]]*(#|- name:|run: scripts/(check-(no-ssofake|binary-tags)|test-check-binary-tags)\.sh|run: go vet -tags ssofake,webkit2_41,duckdb_arrow \./studio/$|out="\$\(deadcode -tags webkit2_41,duckdb_arrow \./cmd/assay \./studio \./tools/ssofake\)"$|desc: Studio dev with AWS sign-in|- wails dev -tags webkit2_41,duckdb_arrow,ssofake$|studio:dev:ssofake:$)'
files=(.github/workflows/*.yml Taskfile.yml studio/wails.json scripts/*.sh)
for f in "${files[@]}"; do
  [ -f "$f" ] || continue
  case "$f" in scripts/check-no-ssofake.sh|scripts/check-binary-tags.sh|scripts/test-check-binary-tags.sh) continue;; esac
  while IFS= read -r hit; do
    if ! grep -qE "$allowed" <<<"$hit"; then
      echo "ssofake: $f mentions the ssofake tag outside the allowed lines: $hit" >&2
      fail=1
    fi
  done < <(grep -nwi 'ssofake' "$f" || true)
done
[ "$fail" = 0 ] && echo "ssofake: no CI, release or packaging build carries the tag"
exit "$fail"
