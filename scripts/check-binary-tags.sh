#!/usr/bin/env bash
# Fails when a built binary was compiled with the `ssofake` build tag (AWS sign-in against local fakes, Rodney QA
# only). Reads the build settings Go embeds (they survive -s -w). Fails closed: no arguments, a missing file, a file
# that is not a Go binary, or one with no build info all fail. Usage: scripts/check-binary-tags.sh BINARY...
set -euo pipefail
if [ "$#" -eq 0 ]; then
  echo "check-binary-tags: no binary given" >&2
  exit 1
fi
fail=0
for b in "$@"; do
  if [ ! -f "$b" ]; then
    echo "check-binary-tags: no such file: $b" >&2; fail=1; continue
  fi
  if ! info="$(go version -m "$b" 2>/dev/null)" || ! grep -qE '^[^[:space:]]+:[[:space:]]+go[0-9]' <<<"$info" || ! grep -qE '^[[:space:]]+path[[:space:]]' <<<"$info"; then
    echo "check-binary-tags: $b has no Go build info" >&2; fail=1; continue
  fi
  if grep -E '^[[:space:]]+build[[:space:]]+-tags=' <<<"$info" | grep -qw ssofake; then
    echo "check-binary-tags: $b was built with the ssofake tag" >&2; fail=1
  fi
done
[ "$fail" = 0 ] && echo "check-binary-tags: no binary carries the ssofake tag"
exit "$fail"
