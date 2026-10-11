#!/usr/bin/env bash
# Fails on any function unreachable from the CLI and Studio entry points that is not
# in scripts/deadcode-allow.txt. Install: go install golang.org/x/tools/cmd/deadcode@latest
# To clear a finding: delete the function, or move a test-only helper to an export_test.go.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
gopath="$(go env GOPATH)"
export PATH="$PATH:$gopath/bin"
command -v deadcode >/dev/null || { echo "deadcode not installed: go install golang.org/x/tools/cmd/deadcode@latest" >&2; exit 2; }
allow="$(mktemp)"
trap 'rm -f "$allow"' EXIT
grep -vE '^[[:space:]]*(#|$)' scripts/deadcode-allow.txt > "$allow"
out="$(deadcode -tags webkit2_41,duckdb_arrow ./cmd/vibe ./studio)"
new="$(printf '%s\n' "$out" | grep -vEf "$allow" || true)"
if [ -n "$new" ]; then
  echo "new unreachable functions (delete them, or move test-only helpers to export_test.go):"
  printf '%s\n' "$new"
  exit 1
fi
echo "deadcode: clean ($(printf '%s\n' "$out" | grep -c . || true) allow-listed)"
