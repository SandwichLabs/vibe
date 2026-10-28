#!/usr/bin/env bash
# SPIKE #257 - throwaway. Chromium baseline: run the auto sequence for each widget configuration via Rodney.
# Needs Studio running (wails dev, :34115). Evidence lands via the Go middleware.
set -u
base=${BASE:-http://localhost:34115}
for q in "widget=ghostty&wasm=inline" "widget=ghostty&wasm=url" "widget=ghostty&wasm=url&sb=30000000" "widget=xterm-webgl" "widget=xterm-webgl&pdb=1" "widget=xterm-dom"; do
  rodney open "$base/#/spike/console?$q&auto=1" >/dev/null 2>&1
  rodney reload >/dev/null 2>&1   # a hash-only change would keep the old page (and its state)
  rodney wait '[data-testid=spike-done]' >/dev/null 2>&1
  echo "$q -> $(rodney text '[data-testid=spike-status]')"
done
