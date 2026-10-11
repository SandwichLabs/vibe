#!/usr/bin/env bash
# Fails when an exported Studio App method (a Wails binding) has no non-test frontend caller.
# Usage: scripts/check-bindings.sh   (run from anywhere; needs ripgrep-free grep only)
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
src="studio/frontend/src"
# Frontend text that counts as a caller: everything except tests, specs and env.d.ts.
callers="$(mktemp)"
trap 'rm -f "$callers"' EXIT
find "$src" -type f \( -name '*.ts' -o -name '*.vue' -o -name '*.js' \) \
  ! -name '*.spec.*' ! -name '*.test.*' ! -name 'env.d.ts' ! -path '*/__tests__/*' ! -path '*/wailsjs/*' \
  -print0 | xargs -0 cat | sed -E 's/<!--.*-->//g; s#[[:space:]]//.*$##' | grep -vE '^[[:space:]]*(//|\*|/\*|<!--)' > "$callers"
# Exported methods on *App in non-test studio/*.go files.
methods="$(grep -hE '^func \(a \*App\) [A-Z][A-Za-z0-9_]*\(' studio/*.go --exclude='*_test.go' \
  | sed -E 's/^func \(a \*App\) ([A-Za-z0-9_]+)\(.*/\1/' | sort -u)"
# Methods the Wails runtime calls itself (lifecycle hooks), never the frontend.
allow=" Startup Shutdown BeforeClose DomReady "
missing=0
for m in $methods; do
  case "$allow" in *" $m "*) continue ;; esac
  if ! grep -qE "\\b(App|b)\\??\\.$m\\b" "$callers"; then
    echo "binding without a non-test frontend caller: $m"
    missing=1
  fi
done
exit $missing
