# macOS checklist for the data console R0 spike (#257)

Throwaway. Everything here is for the owner on a Mac (Apple silicon). The result you hand back fills the
"WKWebView" column of `docs/plans/spike-console-r0-findings.md`. Nothing in this folder ships.

## 1. Get the build (about 5 minutes of waiting)

```bash
gh workflow run ci.yml --ref spike/console-r0
gh run list --workflow ci.yml --branch spike/console-r0 --limit 1      # note the run id
gh run watch <id>                                                       # wait for build-smoke (macos-arm64)
gh run download <id> -n vibe-studio-spike-macos-arm64 -D ~/vibe-spike
cd ~/vibe-spike && ditto -x -k vibe-studio-spike-mac.zip .
xattr -dr com.apple.quarantine vibe-studio.app                          # unsigned build (docs/packaging.md)
```

Fallback if CI is unavailable (needs Go 1.27, Node 22, Wails v2.16): `git checkout spike/console-r0 &&
(cd studio/frontend && npm ci) && (cd studio && wails dev -tags duckdb_arrow)`; use the same environment
variables below, and Rodney is not needed.

## 2. Automated run, one window per configuration (about 2 minutes each)

The route opens itself and runs the checks; the window shows `auto done` when finished. Quit with Cmd+Q
between runs. All files land in `~/vibe-spike-evidence` (the page POSTs to the app's own asset server).

```bash
mkdir -p ~/vibe-spike-evidence
run() { VIBE_SPIKE_EVIDENCE_DIR=$HOME/vibe-spike-evidence VIBE_SPIKE_ROUTE="#/spike/console?$1${AUTO-&auto=1}" ~/vibe-spike/vibe-studio.app/Contents/MacOS/vibe-studio; }
run 'widget=ghostty&wasm=url&sb=30000000'
run 'widget=ghostty&wasm=inline&sb=30000000'
run 'widget=ghostty&wasm=url'                 # default scrollback option: expect the 10k-line check to FAIL (known)
run 'widget=xterm-webgl'
run 'widget=xterm-webgl&pdb=1'                # only for pixel sampling in the theme check
run 'widget=xterm-dom'
```

If `~/vibe-spike-evidence` stays empty, the POST was refused: re-run from a terminal and keep its output;
the harness then writes every file to the app log as `[spike-evidence] <name> <body>`.

## 3. Human checks (about 10 minutes per widget)

Start a window per widget WITHOUT the automatic run: `AUTO= run 'widget=ghostty&wasm=url&sb=30000000'`, then the same for `widget=xterm-webgl` and `widget=xterm-dom` (the `run` function from section 2).
The `rx:` line at the bottom of the window shows the bytes the widget handed to Go, escaped. After EACH check
click the `snapshot` button (writes `snapN.json` with the screen text, the received bytes and the selection).
Write one line per check in `~/vibe-spike-evidence/human-wkwebview-<widget>.md`: action, what you saw, pass/fail.

| # | Do this | Pass means |
|---|---|---|
| 4 | Keyboard layout "ABC". Click the terminal. Press Option+e then e. Then Option+u then u. | `é` then `ü` appear once each in the terminal and `rx:` shows them as UTF-8 once |
| 5 | Switch input source to Japanese (Hiragana) or Pinyin. Type `nihao` (Pinyin) or `kanji` (Hiragana). Do not press Enter yet. Then commit the candidate. | the composition window sits over the terminal; NOTHING appears in `rx:` while composing; the committed text appears once |
| 6a | Type `help` Enter, `table` Enter. Press Ctrl+C. Press Option+b, then Option+f. | Ctrl+C gives `\x03` (shows `^C (cancel)`). Record what Option+b/f send: `\x1bb`/`\x1bf` (meta) or `∫`/`ƒ` (macOS characters). Either is acceptable; write down which |
| 6b | Press Cmd+A, Cmd+C, then click the `seed` text box, select all, Cmd+V. | the terminal text is copied (the box now holds it) |
| 6c | In the box, select the seed text, Cmd+C. Click the terminal. Cmd+V. Run `paste on` first, then paste again. | plain paste: text echoed; with `paste on`, `rx:` shows `\x1b[200~...\x1b[201~` |
| 6d | Press Up, Down, Left, Right, Home, End, Ctrl+A, Ctrl+L, Ctrl+R. | each reaches `rx:` as an escape sequence or control byte and the page does not scroll, reload or open search |
| 7 | Run `table`. Drag across 2 lines. Cmd+C. Paste into TextEdit. Double-click a word. | pasted text equals the dragged text; double-click selects one word |
| 10 | Run `lines 10000`. While it drains and for 3 s after, scroll with the trackpad (two-finger swipe) and with a mouse wheel if you have one. | scrolling follows your fingers, no multi-second freeze, no blank frames. Note any stutter |
| 3 | Change the `theme` dropdown (schoolhouse-light -> chalkboard) with output on screen. | colours change live. Note whether the old output recoloured (xterm) or stayed old / needed a reload (ghostty) |
| 9 | Drag the window edge to resize; maximise; then full screen (green button). | `size` matches the grid; no clipped last row; no stale pixels |
| 1 | Zoom into `snapshot`'s PNG or the window on the Retina display. | glyphs are crisp (not blurry upscaled), box-drawing lines meet without gaps |
| 15 | Right-click the page, Inspect Element. | the Web Inspector opens (the build uses `-devtools`) |

Fill the `Fail` lines with the exact text of any error shown in the window or the log.

Check 15 as written cannot pass: `-devtools` alone leaves Wails' default context menu disabled (it is on only in
debug builds or with `EnableDefaultContextMenu`). Cmd+Shift+F12 opens the Web Inspector in this build.

### 3a. Section 3 driven by accessibility scripting (how the 2026-10-28 run was done)

Needs Accessibility (and Screen Recording) for the terminal app, and hands off the keyboard and mouse for about
4 minutes per widget. Keys are CGEvents at the HID tap (so dead keys and the IME go through macOS), and the driver
refuses to type unless the spike window is frontmost. It enables Japanese Romaji for check 5 and switches back to
US; disable `com.apple.inputmethod.Kotoeri.RomajiTyping` and `com.apple.50onPaletteIM` afterwards (`./drive tis
disable <id>`). The clipboard text is restored on exit. Trackpad scrolling is synthetic, not a physical swipe.

```bash
cd spike/console/mac && swiftc -O -o drive drive.swift
APP=~/vibe-spike/vibe-studio.app/Contents/MacOS/vibe-studio
P=$(./launch.sh $APP 'widget=ghostty&wasm=url&sb=30000000'); ./human-ax.py $P ghostty ~/vibe-spike-evidence/human-ax; kill $P
# then widget=xterm-webgl and widget=xterm-dom; add only=4,5,... to rerun some checks
```

## 4. Hand it back

```bash
(cd ~ && zip -r vibe-spike-evidence.zip vibe-spike-evidence)
```

Attach `vibe-spike-evidence.zip` to #257 (or drop it in the session), together with `sw_vers` and `system_profiler SPDisplaysDataType | grep -i resolution`.
Delete `~/vibe-spike` and `~/vibe-spike-evidence` afterwards; the spike writes nothing else to your machine.
