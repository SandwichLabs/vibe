#!/usr/bin/env bash
# Fails when a non-test Go file under studio/, cmd/ or internal/ is over 800 lines and is not
# on scripts/filesize-waivers.txt; when a waived file has grown past the line count recorded in
# its waiver; or when a waived file has shrunk under the limit (delete the waiver then).
# Generated files (a "// Code generated ... DO NOT EDIT." line near the top) are not counted.
# Waiver lines are "<path> <max-lines> <reason>"; blank lines and # comments are ignored.
# Usage: scripts/check-filesize.sh   (run from anywhere)
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
limit=800
waivers=scripts/filesize-waivers.txt
fail=0

# waiver_cap prints the recorded max-lines for path, or nothing when it is not waived.
waiver_cap() { awk -v p="$1" '$1 == p { print $2; exit }' "$waivers"; }

while IFS= read -r -d '' f; do
  lines=$(wc -l < "$f")
  [ "$lines" -gt "$limit" ] || continue
  if head -n 20 "$f" | grep -qE '^// Code generated .* DO NOT EDIT\.$'; then continue; fi
  cap="$(waiver_cap "$f")"
  if [ -z "$cap" ]; then
    echo "filesize: $f has $lines lines (limit $limit): split it, or add it to $waivers as '<path> <max-lines> <reason>'" >&2
    fail=1
  elif [ "$lines" -gt "$cap" ]; then
    echo "filesize: $f grew to $lines lines, past its waiver's $cap: split it rather than raise the waiver" >&2
    fail=1
  fi
done < <(find studio cmd internal -name '*.go' ! -name '*_test.go' -print0)

while read -r path cap _; do
  case "$path" in ''|'#'*) continue ;; esac
  if ! [[ "$cap" =~ ^[0-9]+$ ]]; then
    echo "filesize: waiver for $path needs '<path> <max-lines> <reason>' (got max-lines '$cap')" >&2
    fail=1
    continue
  fi
  if [ ! -f "$path" ]; then
    echo "filesize: waiver for $path, which no longer exists: remove it from $waivers" >&2
    fail=1
    continue
  fi
  if [ "$(wc -l < "$path")" -le "$limit" ]; then
    echo "filesize: $path is now within $limit lines: remove its waiver from $waivers" >&2
    fail=1
  fi
done < "$waivers"

[ "$fail" -eq 0 ] && echo "filesize: ok (limit $limit lines, $(grep -cvE '^[[:space:]]*(#|$)' "$waivers") waived)"
exit "$fail"
