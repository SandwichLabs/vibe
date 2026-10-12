#!/usr/bin/env bash
# Fails when an exported Studio App method (a Wails binding) has no non-test frontend caller
# (calls go through App.X from src/lib/bindings.ts), or when the hand-written AppApi interface in
# src/env.d.ts drifts from the Go methods (a method missing from it, or one it declares that Go lacks).
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
# AppApi (env.d.ts) must list exactly the Go methods: it is the frontend's only binding type source.
declared="$(awk '/^interface AppApi \{/{f=1;next} f&&/^\}/{f=0} f' "$src/env.d.ts" \
  | grep -E '^  [A-Z][A-Za-z0-9_]*\(' | sed -E 's/^  ([A-Za-z0-9_]+)\(.*/\1/' | sort -u)"
for m in $methods; do
  case "$allow" in *" $m "*) continue ;; esac
  if ! grep -qx "$m" <<<"$declared"; then
    echo "binding missing from AppApi in $src/env.d.ts: $m"
    missing=1
  fi
done
for m in $declared; do
  if ! grep -qx "$m" <<<"$methods"; then
    echo "AppApi in $src/env.d.ts declares a method Go does not have: $m"
    missing=1
  fi
done
exit $missing
