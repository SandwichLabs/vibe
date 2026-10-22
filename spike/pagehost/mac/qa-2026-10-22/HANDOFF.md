# Handoff: Mac QA of HTML pages (2026-10-22, macOS 26.6.2, M5, d4314d1)

Full detail and log names are in RESULTS.md. Nothing was committed or pushed. The patches in `patches/` are temporary QA instruments, not fixes; the build needed no fix.

## Fold back (facts for the plan, the reference docs and tests)

1. **The page window built and ran on macOS for the first time with no code change.** `pagewindow_darwin.m` compiled cleanly, and Network.framework is linked.
2. **The four layers held**, with a control proving each claim:
   - Zero listener hits and zero DNS everywhere except the IP-literal link clicks.
   - The rule list was in place before the first load in 47 of 47 helpers.
   - The watchdog kill takes a busy WebContent with it.
   - The UIElement helper has an empty environment.
3. **`docs/reference/artifacts.md:393` is wrong for 26.6.2.** "every connect to the proxy is refused at once" doesn't hold: a connect to the bound, non-listening port hangs about 7.8 s, then fails with ETIMEDOUT. **No failover was observed** in 125 s holds of `alinkname` and `main`. The `deadProxyPort` comment in `pagewindow_darwin.m` makes the same assumption.
4. **The leak's reach:** the IP-literal link leak also reaches a non-private (TEST-NET) address. HTTPS to an IP sends a full TLS ClientHello (no SNI), with 4 connections per click. Plain HTTP gives exactly one empty connection. Worth putting in the #189 text.
5. **What's checked now:**
   - Static host: meta exact, isolation clean, control positive. The B2 harness hooks are kept as `patches/qa-p3-harness-hooks.patch`.
   - D1 native gate, all as designed: real click, key press, Space, Escape, autorepeat, raw post, typing in Studio, the throttle, one input per save.
   - The open panel is refused.
   - `ask` sends no tools and `max_tokens` 4000.
   - Bundles carry no state and no decisions, and binary assets are byte-identical.
6. Small checklist fixes:
   - The app path is `Vibe Studio.app`.
   - macOS WebKit focuses buttons only with Option-Tab.
   - The probe's `vibe.run` values aren't logged at info level, so a stderr line (the page-run log patch) was needed.
   - Studio fields get auto-capitalised when typed into: set parameters through the bindings or turn the substitutions off.

## Needs a decision

1. **Stop hit in group `open`:** a script-clicked `<a target=_blank href="http://IP:port">` opens one empty TCP connection, the same as `alink`. Is it covered by #189's accepted leak (then update the wording to "a link, with or without target"), or is it a regression to fix?
2. **`vibe-rules-<pid>` folders pile up** (71 in one session). Every Studio-side close SIGKILLs the helper before `atexit` runs, and ⌘W loses the race too (`EV_CLOSED` → `finish` → kill). Options: let the helper exit cleanly on close before Studio kills it, have Studio remove `vibe-rules-<pid>` after reaping, or sweep stale folders at helper start.
3. **Dropped files:** a Finder drop gives the page the file's name, size, type and mtime (contents unreadable). Disable drops in the web view?
4. **macOS text substitutions on Studio's parameter and name fields** (`main` becomes `Main`, `winopen` becomes `win open`). Add `autocapitalize="off" autocorrect="off" spellcheck="false"`?
5. **⌘Q in the page window does nothing** (the checklist expected it to end the helper). Fine, or add Quit to the helper's menu?
6. Minor:
   - `vibe export bundle` prints "3 data artifacts" for 2.
   - Studio's open view stays "Changed" after a revert until you reopen the artifact.
   - The broken-rule-list and kill cases show "stopped responding".
   - The helper inherits Studio's cwd (a workspace folder when started from a terminal).
7. **Not run:** macOS 14 and 13 (§7), and the cross-machine bundle export. These need another Mac or a Linux export.

## State of the Mac afterwards

- **sudo changes:** all undone and confirmed (`logs/00-sudo-teardown.txt`): no `/etc/resolver/spike.test`, no lo0 alias, no tcpdump.
- **Repo:** `git status` is clean at d4314d1, and the app on disk is a clean build.
- **Left on disk:** the test workspaces `/tmp/p4-ws` and `~/vibe-mac-qa/`, plus Studio's recent list and the page decisions.

## What's in this folder

- `RESULTS.md`: verdicts, every box, and log names.
- `logs.tgz`: every log, named after its checklist step.
- `patches/`: the temporary QA instruments, all reverted.
- `qa-pages/`: my isolation, gesture and flood pages.
- `bin/`: the scripts that drove the run (`axq.swift` for Accessibility clicks and reads, the matrix runner, the MCP client, the sudo setup and teardown).
