#!/usr/bin/env bash
# Tests scripts/check-binary-tags.sh: it must fail with no arguments, on a file that is not a Go binary and on a
# binary built with the ssofake tag, and pass on a clean one.
set -uo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
t="$(mktemp -d)"
trap 'rm -rf "$t"' EXIT
check="$root/scripts/check-binary-tags.sh"
cat > "$t/main.go" <<'GO'
package main

func main() {}
GO
(cd "$t" && printf 'module x\n\ngo 1.21\n' > go.mod && go build -ldflags "-s -w" -o clean . && go build -ldflags "-s -w" -tags ssofake -o tagged .)
echo "not a binary" > "$t/text"
bad=0
"$check" >/dev/null 2>&1 && { echo "FAIL: passed with no arguments"; bad=1; }
"$check" "$t/text" >/dev/null 2>&1 && { echo "FAIL: passed on a non-Go file"; bad=1; }
"$check" "$t/missing" >/dev/null 2>&1 && { echo "FAIL: passed on a missing file"; bad=1; }
"$check" "$t/tagged" >/dev/null 2>&1 && { echo "FAIL: passed on a tagged binary"; bad=1; }
"$check" "$t/clean" >/dev/null 2>&1 || { echo "FAIL: failed on a clean binary"; bad=1; }
[ "$bad" = 0 ] && echo "check-binary-tags: self-test ok"
exit "$bad"
