# WKWebView human checks: xterm-dom

Driven by Claude Code with real HID-level input (CGEvent posted at the HID tap, so dead keys and the IME go through the macOS input system), AX window control and screencapture; not a person at the keyboard. Trackpad scrolling is synthetic (continuous, phased scroll events), not a physical swipe. Keyboard layout: US (same Option dead keys as ABC). IME: Japanese Romaji, enabled for the run and disabled afterwards. Local build of 5eeb889 (`wails build -tags duckdb_arrow -devtools`). Per-step JSON: `human-ax-<widget>-check<N>.json`; screenshots: `human-ax-*.png` (a curated subset). Format: # | action | what happened | verdict.

- 4 | Option+e e, Option+u u | rx: \u{e9} then \u{fc}, once each | pass
- 5 | Japanese Romaji, type "kanji", Return | nothing in rx while composing; composition drawn inline at the caret with the candidate panel just below; commit sends 感じ once | pass
- 6a | help, table, Ctrl+C, Option+b, Option+f | Ctrl+C -> \x03 and "^C (cancel)"; Option+b/f send ∫ / ƒ (macOS characters, not meta) | pass (macOS characters)
- 6b | Cmd+A, Cmd+C in terminal, paste into seed box | whole buffer copied; seed box holds it | pass
- 6c | Cmd+V plain, then "paste on" + Cmd+V | plain: echoed once; with paste on: \x1b[200~seed paste check\x1b[201~ | pass
- 6d | Up Down Left Right Home End Ctrl+A Ctrl+L Ctrl+R | 9/9 reach rx; page did not scroll, reload or open search | pass
- 7 | table, drag 2 lines, Cmd+C, double-click a word | clipboard equals the dragged rows; double-click selects "customer". Note: a drag ending on the centre of the last cell stops before that cell (cell-boundary selection) | pass
- 10 | lines 10000, scroll during and after drain (trackpad-style + wheel) | each 25-event burst took ~0.5 s (no stall), viewport follows, last line present | pass (synthetic)
- 3 | theme schoolhouse-light -> chalkboard with output on screen | terminal background and the existing output recolour live (human-ax-xterm-dom-3-*.png) | pass
- 9 | resize small/wide, zoom, full screen | `size` stays 160x38: the harness fits once at open (no ResizeObserver), so this checks the harness. Small window clips the fixed grid at the bottom; no stale pixels | n/a (harness)
- 1 | Retina screenshot (2x) of table + unicode | crisp; box-drawing lines meet | pass
- 15 | right-click, Inspect Element | no context menu (Wails default-context-menu flag, see ghostty); Cmd+Shift+F12 opens the Web Inspector | fail as written; inspector reachable via Cmd+Shift+F12
