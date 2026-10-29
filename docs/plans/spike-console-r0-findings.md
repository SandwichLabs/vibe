# Spike: the data console's terminal widget (R0, #257)

**Status: done 2026-10-28; the owner chose xterm.js (decision 2) the same day. Automated Linux runs done; macOS (WKWebView) automatic runs and the human checklist done 2026-10-28 with agent-driven HID input and synthetic scrolling (section 4.4); the Linux human checks (section 8.1) move into R2's acceptance (#259).** Spike code is on `spike/console-r0` (pushed, never merged, like `spike/html-pages-p1`); this file, the status line and decisions in [`data-console.md`](data-console.md) and a row in `docs/README.md` are what merge. Evidence paths below are file names in `spike/console/evidence-curated/` on that branch (the full gitignored set is reproducible from `spike/console/README.md`).

**Ask.** Run `ghostty-web` and `@xterm/xterm` through one harness in Studio's real webviews (WebKitGTK, WKWebView) and record: does it render and theme from the `--vibe-*` roles, do dead keys, IME, shortcuts, copy, bracketed paste and resize work, do 10,000 lines scroll smoothly, does the WASM load under Studio's CSP, what do the bundles weigh, and is `go-libghostty` used for Go golden-screen tests. Stop the epic if neither widget passes on both webviews.

## 1. Result

- **Decision (owner decision 2, decided 2026-10-28): `@xterm/xterm` 6.0.0 with `@xterm/addon-fit` 0.11.0 and `@xterm/addon-webgl` 0.19.0, DOM renderer as the fallback. The WKWebView run agrees; the Linux human checks (section 8.1) are R2 acceptance checks.** By the decision rule recorded in the plan, ghostty-web does not pass on WebKitGTK, and the decision rests on rows 3, 6 and 8, not on row 10: its everyday real paste path ignores bracketed-paste mode (row 8, FAIL), its theme cannot be changed in place (row 3, PARTIAL), and Alt is not Meta and Shift+Insert is not paste (row 6, PARTIAL). Row 10 adds a byte-unit `scrollback` whose default keeps 469 lines (a constant fixes that) and a marginal frame-pacing miss on native Wayland. xterm.js passed or was PARTIAL (row 6, needs two app key bindings) on every row that could be run here.
- **WKWebView agrees (section 4.4).** On macOS 26.6.2 (WebKit 21624) xterm.js, WebGL and DOM, passed every automatic row and every human check that could be run (dead keys, Japanese IME, Cmd+A/C/V, bracketed Cmd+V, live theme, 10k lines with no frame over 50 ms). ghostty-web repeated its Linux failures and added macOS ones: a real Cmd+V is not bracketed (row 8, FAIL), Option+B/F send nothing, Cmd+A types `a`, the IME preedit sits at the terminal's top-left instead of the caret, and the theme still needs a recreate. ghostty-web's frame pacing was fine on WKWebView (p95 17 to 18 ms), and the Wayland miss in row 10 did not reproduce there (a different machine and a smaller grid, 155x43); its cause is unknown.
- **The epic does not stop.** xterm.js passes rows 1, 9 and 10 on WebKitGTK and WKWebView (the stop rule needs both widgets to fail).
- **go-libghostty (owner decision 3): no.** It built here in 2 minutes with Zig 0.17.0 and CMake 4.3, and asserted a screen correctly, but it needs a Ghostty source build on every CI runner, has no API stability promise, and R1 does not need screen-state assertions.
- **The plan's "ghostty-web is a drop-in for xterm.js" does not hold** (section 5): five behaviours differ in ways R2 would have to work around.
- **WASM under Studio's CSP: yes.** Both ghostty load paths work on `wails://wails.localhost:34115` (dev) and `wails://wails` (production build) in WebKitGTK 2.54.1.
- **Not run, and never marked passed:** dead keys and IME composition on Linux; a physical keyboard, trackpad or mouse wheel anywhere (the macOS human checks used real HID-level key events but synthetic scroll events); the owner's own Linux session (it was locked); a window-resize refit (the harness fits once at open, section 4.4). They are listed in sections 4.4 and 8.

## 2. How it was run, and what could not be

| | |
|---|---|
| Host | Fedora 44, GNOME Shell 50.6 / mutter 50.6, WebKitGTK 4.1 **2.54.1**, GTK 3.24.52, Go 1.27.3, Node 22.19.0, Vite 8.3.4, Wails CLI v2.16.0, amdgpu (Mesa) |
| Widgets (exact) | `ghostty-web` 0.4.0, `@xterm/xterm` 6.0.0, `@xterm/addon-fit` 0.11.0, `@xterm/addon-webgl` 0.19.0 (`package.json`, `-E`) |
| Harness | route `#/spike/console` (rendered from `App.vue` before the workspace gate, so no DuckDB), a `TermAdapter` per widget, a fake REPL that logs every byte it receives, the theme built from `--vibe-*` through the browser (`getComputedStyle` on a probe element, resolved to RGB through a canvas), and an automatic check sequence per configuration. Evidence is POSTed to a Go middleware (`studio/spike_console.go`, only when `VIBE_SPIKE_EVIDENCE_DIR` is set) |
| Chromium baseline | Rodney headless Chrome against `wails dev` on :34115. Counts as a baseline only |
| **WebKitGTK** | the real Wails window (`wails dev -tags webkit2_41,duckdb_arrow`, and the `task build:studio:linux` binary for the production origin), native Wayland and X11 (Xwayland), in a **private headless mutter** (below) |
| Input | real keyboard and pointer events injected at compositor level, with real focus, keymap and GTK input path (`spike/console/keys-run.py`, `inject/`) |
| **WKWebView** | macOS 26.6.2 (25G83), WebKit 21624, Apple M5, built-in Retina 2880x1864 (`devicePixelRatio` 2). A local build of `5eeb889` with the CI command (`wails build -tags duckdb_arrow -devtools`, Go 1.27.3, Wails CLI v2.16.0), production origin `wails://wails`, window 1280x832 (page 1280x800). The six automatic configurations of `spike/console/mac/CHECKLIST.md` section 2, then section 3 by accessibility scripting on the owner's Mac with the owner hands off: keys are CGEvents posted at the HID tap (so dead keys and the IME go through the macOS input system; layout US, Japanese Romaji enabled for the IME check), clicks and drags are CGEvents, window control is AX, screenshots are `screencapture`. Trackpad and wheel scrolling are synthetic scroll events, not a physical swipe (`spike/console/mac/drive.swift`, `human-ax.py`) |

Three things about this host changed the method, and they are findings in their own right:

1. **The owner's desktop was locked for the whole spike** (`LockedHint=yes`, `org.gnome.ScreenSaver` active). A locked compositor presents no windows: WebKit reported `visibilityState: hidden` on Wayland and 0 `requestAnimationFrame` frames in 1 s (X11: window painted black, 0 frames). Every WebKitGTK result here therefore comes from a private headless mutter 50.6 (own D-Bus bus, own Wayland socket, own Xwayland, the real GPU through gbm), which touches nothing of the user's session. Window geometry is 1400x900 at scale 1 and 1920x1080 at scale 2 (`devicePixelRatio` 2).
2. **XTEST cannot be used.** Xwayland 24+ routes XTEST through a portal approval dialog; in the private compositor that dialog sat in the middle of the screen and swallowed every pointer event until its backend process was stopped. Input is injected through the private mutter's own `org.gnome.Mutter.RemoteDesktop` on the private bus instead (`spike/console/inject/main.go`). The first key after a click was dropped until a modifier tap was added (the same in both widgets). Note that this is not a physical keyboard.
3. **`wails dev` and `wails build` rewrite `go.mod`** (`v2.17.0` to `v2.16.0`, drops `mimetype`); see the Wails version facts below. The spike reverted those files.

Limits of that stand-in. The private compositor is a fair stand-in for rendering, API behaviour and DOM-level input; it is **not** one for IME, dead keys or perceived smoothness:

- There is no ibus daemon and no GTK input-method module on the private bus, so the layer that matters for dead keys and IME is absent (the likely reason the keysym dead-key attempt produced plain `e`).
- No gnome-settings-daemon or xsettings, so font hinting, anti-aliasing and text scaling can differ from the owner's session; crispness claims are about canvas backing-store size, not about how it looks on the owner's screen.
- The monitor is a virtual 60 Hz one with no scanout, so frame times are the compositor's virtual clock (`raf.json`: 62 frames per second, p50 17 ms).
- `document.hasFocus()` was false in the automatic runs (`*-raf.json`); the injected-key runs clicked the terminal first.
- Window and grid sizes differ per column (section 3 row 10 and Appendix B), so timings compare within one environment only.
- Two early runs (`wails-wayland-123136.log`, `wails-x11-123652.log`) did open a Studio window on the owner's own locked session before the private compositor existed; nothing was captured or injected there.

Wails version facts: `origin/main`'s `go.mod` requires `wails/v2 v2.17.0` (dependabot, #191), while `ci.yml` (`WAILS_VERSION`), `release.yml` and CLAUDE.md pin the CLI at v2.16.0. The v2.16.0 CLI rewrote `go.mod` to v2.16.0 on `wails dev` and `wails build` here, which means CI build-smoke and release builds may link a different Wails than govulncheck checks. This is outside the spike; it should be reconciled separately.

Clipboard checks go through the harness's own seed textarea, so the real clipboard path is exercised. ImageMagick `import` did not work here (`missing an image filename`), so window screenshots use `xshot.py` (XGetImage under Xwayland); native-Wayland screenshots are not possible, so those cells cite in-page `canvas.toDataURL()` PNGs.

## 3. Results matrix

Columns: **C** Chromium baseline (Rodney, headless; never a webview result), **WL** WebKitGTK 2.54.1 on native Wayland, **X** WebKitGTK on X11 (Xwayland), **WK** WKWebView (macOS 26.6.2, production build, `devicePixelRatio` 2; env `wkwebview-prod`, human checks in `human-wkwebview-<widget>.md` and `human-ax-<widget>-check<N>.json`). All WebKitGTK cells are the private compositor described in section 2. Statuses: PASS, FAIL, PARTIAL (what), NOT RUN (why), **pending owner**. ghostty-web is the `wasm=url&sb=30000000` configuration unless a cell says otherwise (section 4 explains why that scrollback value); xterm.js is the WebGL renderer unless it says DOM. Every number is in the `results.json` named in the last column (`<config>-<env>-results.json`, envs `chromium-http`, `webkitgtk-dev-wl`, `webkitgtk-dev-x11`, `webkitgtk-dev-wl2x`, `webkitgtk-prod-wl2x`); full per-configuration tables are in the appendix.

| # | Criterion | ghostty-web 0.4.0 C / WL / X / WK | @xterm/xterm 6.0.0 C / WL / X / WK | Evidence |
|---|---|---|---|---|
| 1 | Renders (prompt, `table` box, `unicode` widths, 16 colours) | PASS / PASS / PASS / PASS (Retina: crisp, box rules meet, CJK and emoji placed correctly). Box rules are font glyphs: faint with faint crosses at scale 1, crisp at scale 2 | PASS / PASS / PASS / PASS (WebGL and DOM crisp on Retina, box rules meet; emoji overlap in WebGL as on Linux) (WebGL draws box characters itself). DOM renderer: PARTIAL (rules broken between rows). Emoji and Devanagari overlap in WebGL without the unicode11 addon | `*-render-canvas.png`, `*-render-dump.txt`, `keys-x11-*-table.png`, `zoom-box-*-x11.png`, `zoom-2x-*.png`, `human-ax-*-1-retina-crop.png` |
| 2 | Theme from `--vibe-*` (no hex in stylesheets) | PASS / PASS / PASS / PASS | PASS / PASS / PASS / PASS | `*-theme.json` (4 built-ins, 4 distinct backgrounds); grep: no hex in the spike's CSS (one `#000` canvas reset in `theme.ts`) |
| 3 | Theme switch live, no reload | **PARTIAL: recreate only** (in place failed in all three: `options.theme =` warns "not fully supported"; the cursor follows `renderer.setTheme`, but the background and ANSI palette, which live in the WASM terminal, do not) / same / same / PARTIAL recreate only (automatic check 3; by hand, the harness chrome recolours and the terminal keeps the old background and output) | PASS in place (DOM and WebGL, measured with pixels) / PASS / PASS / PASS (pixels in `pdb` and DOM; by hand the old output recolours too) | `*-results.json` check 3, `*-theme-{before,after,recreated}.png`, `human-ax-*-3-{before,after}.png` |
| 4 | Dead keys | NOT RUN / **R2 acceptance (Linux, #259)** / R2 acceptance (Linux, #259) / **PASS** (Option+E E, Option+U U give `é` and `ü` once each, US layout) | NOT RUN / R2 acceptance (Linux, #259) / R2 acceptance (Linux, #259) / **PASS** (WebGL and DOM, same bytes) | section 8. A keysym-injection attempt (`dead_acute`+`e`) delivered plain `e`: inconclusive, not counted. WK: `human-ax-*-check4.json` |
| 5 | IME composition | NOT RUN / **R2 acceptance (Linux, #259)**: no IME engine installed, `ibus` has only xkb engines / same / **PARTIAL**: Japanese Romaji, nothing reaches the REPL while composing and `感じ` arrives once on commit, but the preedit and the candidate window sit at the terminal's top-left (over row 0), not at the caret | NOT RUN / R2 acceptance (Linux, #259) / R2 acceptance (Linux, #259) / **PASS** (WebGL and DOM): preedit inline at the caret with the candidates below it, nothing while composing, committed once | section 8. WK: `human-ax-*-check5.json`, `human-ax-ghostty-5-composing-full.png`, `human-ax-xterm-webgl-5-composing.png` |
| 6 | Shortcuts (injected keys) | C: NOT RUN (no real keys in headless Chrome) / WL: PARTIAL / X: PARTIAL / WK: PARTIAL (WK detail at the end of this cell). Ctrl+C `^C`, arrows, Home/End, Del, PgUp/PgDn, Ctrl+A/E/L/R/D/Z, Tab, Esc OK. Gaps: **Alt+B / Alt+F arrive as plain `b`, `f`** (no Meta); **Shift+Insert sends `ESC[2~`, not paste** (Ctrl+Shift+C is moot: the selection is already auto-copied). WK: **PARTIAL, worse than Linux**: Ctrl+C, arrows, Home/End, Ctrl+A/L/R OK; **Option+B / Option+F send nothing**; **Cmd+A types `a`** instead of selecting all; Ctrl+Cmd+F (leave full screen) is captured and sent as `ESC[102;5u` | C: NOT RUN / WL: PARTIAL / X: PARTIAL / WK: PASS (WebGL and DOM): Ctrl+C, arrows, Home/End, Ctrl+A/L/R OK, Cmd+A selects all, Option+B / Option+F send `∫` / `ƒ` (macOS characters, not Meta: `macOptionIsMeta` is a product choice). Alt+B is `ESC b`; Shift+Insert and Ctrl+Shift+V paste. Gaps: **Ctrl+Shift+C copies nothing by default** and **Ctrl+V is `^V`** (terminal convention); an app key handler fixes the first (`copykey=1`, X11 runs `keys-x11c-*` only, no Wayland copykey run) | `keys-wl-*-summary.json`, `keys-x11-*-summary.json`, `keys-x11c-*-summary.json`; WK `human-ax-*-check6{a,b,d}.json` |
| 7 | Copy | C: PASS (getSelection only) / WL: PASS / X: PASS / WK: PASS (drag two rows, Cmd+C: the clipboard equals them, though ghostty had already copied on mouse-up; double-click selects one word). getSelection equals the dragged text; **the clipboard is overwritten on mouse-up with no opt-out** | C: PASS (getSelection only) / WL: PASS (binding not run on Wayland) / X: PASS with the binding / WK: PASS (Cmd+C copies the selection with no binding; Cmd+A then Cmd+C copies the buffer). The clipboard stays untouched until Ctrl+Shift+C | check 7 in `*-results.json`; `keys-*-summary.json` A, B, C; WK `human-ax-*-check7.json` |
| 8 | Bracketed paste | C: PASS via the `paste()` API / WL: **FAIL** on the everyday path (click, then Ctrl+V or Ctrl+Shift+V arrives raw with mode 2004 on); middle-click wraps / X: FAIL (same) / WK: **FAIL**: with `paste on`, a real Cmd+V arrives raw (`seed paste check`, no `ESC[200~`); the automatic check 8 passes only through `paste()` | C: PASS / WL: PASS / X: PASS / WK: PASS (WebGL and DOM: Cmd+V wraps as `ESC[200~seed paste check ESC[201~`). Wrapped on every real path; `\n` becomes `\r` | check 8, `keys-*-summary.json` `ctrl+v on`, `ctrl+shift+v on`, `shift+Insert on`, `middle-click`; WK `human-ax-*-check6c.json` |
| 9 | Resize / fit (`onResize` equals `fit()`, grid fills, `size` matches) | PASS / PASS / PASS / PASS (automatic). Window resize by hand: NOT RUN in a meaningful form, the harness calls `fit()` once at open and has no ResizeObserver, so `size` stayed 155x43 at every window size; no stale pixels | PASS / PASS / PASS / PASS (automatic); by hand same harness limit (`size` stayed 166x38 WebGL, 160x38 DOM) | `*-resize.json`, `*-resize-*.png`; WK `human-ax-*-check9.json`, `human-ax-*-9-small.png` |
| 10 | 10k lines (drain, frame times over drain plus a 2 s scroll, scrollback kept) | C: PASS (p95 26 ms, grid 155x46) / WL: **FAIL (marginal)**: p95 34, 35, 34 ms at 170x50 in 3 runs, 6 or 7 of 115 frames over 33 ms (a 60 Hz virtual monitor, so each is one vsync late), none over 50 ms, max 48 ms; scroll-only p95 33, 32, 34 ms passed in 2 of 3 runs / X: PASS (28 ms, 155x46) / WK: PASS (WK detail at the end of this cell). Scale 2 passed (18 ms) on a smaller 115x25 grid. **FAIL at the default scrollback** (469 to 548 lines kept; a constant fixes it). WK: PASS (p95 17 ms url, 18 ms inline, 155x43, 0 of 122 frames over 33 ms, max 25 ms, drain 24 to 25 ms); FAIL at the default scrollback (515 lines); by hand, synthetic trackpad and wheel scrolling during and after the drain never stalled | C: PASS (25 ms in software-GL Chromium) / WL: PASS (18, 18, 18 ms) / X: PASS (17 ms) / WK: PASS (p95 17 ms WebGL 166x38 and DOM 160x38; 1 of 125 frames over 33 ms, the drain frame, max 41 to 49 ms; drain 54 to 62 ms; by hand no stall). DOM renderer also passes (17 to 18 ms) | `*-lines.json`, `*-wlr2-*`, `*-wlr3-*` (repeats); WK `*-wkwebview-prod-results.json`, `human-ax-*-check10.json` |
| 11 | WASM loads under Studio's CSP | C: PASS / WL: PASS / X: PASS / WK: PASS (both paths on `wails://wails`: `url` 200, `application/wasm`, 423,045 bytes; `inline` data URL fetched; initMs 15 to 20). Both `wasm=inline` (the 564 KB base64 data URL fetched from `wails://`) and `wasm=url` (`/assets/ghostty-vt-*.wasm`, `content-type: application/wasm`, 200, 423,045 bytes) load on the dev origin `wails://wails.localhost:34115` and the production origin `wails://wails`; initMs 14 to 46 ms; no CSP change (Studio's policy is `frame-src` only) | n/a (no WASM). WebGL context acquired in every run, WKWebView included | `*-init.json`, `ghostty-url-sb30000000-webkitgtk-prod-wl2x-init.json`, `ghostty-url-sb30000000-wkwebview-prod-init.json` |
| 12 | Bundle size | section 6 | section 6 | `bundle-size.txt` |
| 13 | API coverage for R1/R2 | section 5 | section 5 | |
| 14 | Errors (zero `console.error`, `onerror`, unhandled rejection over a full run) | PASS in every run, WKWebView included | PASS in all, WKWebView included | `*-errors.txt` empty |

## 4. What the runs showed

### 4.1 ghostty-web

- **Rendering is good.** CJK, emoji, combining marks and Devanagari are placed correctly in the canvas (`ghostty-url-sb30000000-webkitgtk-dev-wl-render-canvas.png`); xterm.js WebGL overlaps emoji and Devanagari without `addon-unicode11`. Box drawing uses font glyphs, so at scale 1 the rules are lighter than xterm's WebGL custom glyphs and the junctions show a faint cross; at scale 2 they are crisp. A duckbox table is readable either way.
- **Theme is fixed at construction.** The WASM terminal copies the palette when it is built. Assigning `options.theme` after `open()` only logs a console warning. The renderer's `setTheme` moves the cursor colour but not the background or the ANSI palette, which live in the WASM terminal (check 3, `*-theme-after.png`). The only working route is dispose and recreate, which empties the screen and scrollback, so R2 would need a raw byte log per console tab to replay.
- **Real paste ignores bracketed-paste mode on the everyday path.** There are two paste listeners. The textarea's calls `Terminal.paste()`, which wraps in `ESC[200~ ... ESC[201~` when the application asked for it (source ~2368). The container's `InputHandler.handlePaste` (~963) calls `onData(text)` raw. After a normal left click, `SelectionManager` moves focus from the textarea to `canvas.parentElement`, the container (~1913), so click then Ctrl+V or Ctrl+Shift+V lands on the raw handler (`keys-*-ghostty-summary.json`: `line-one\nline-two`, mode 2004 on). Middle-click and the context menu focus the textarea and are wrapped (`middle-click` shows `ESC[200~middle-paste ESC[201~`). Newlines stay `\n`, so a paste is indistinguishable from typing; what a newline then does depends on R2's line editor (in the harness it stayed in the line buffer, `keys-x11-ghostty-ctrl-v-on.png`). R2 would need a capturing `paste` listener that calls `term.paste()`.
- **Keys.** Alt+B and Alt+F arrive as plain `b` and `f`; there is no `macOptionIsMeta`. Shift+Insert is encoded as the Insert key (`ESC[2~`) instead of pasting. Selecting text copies it to the clipboard on mouse-up and there is no option to stop that. `attachCustomKeyEventHandler` uses the opposite return convention to xterm.js (true means handled), which makes a shared binding table error-prone.
- **`scrollback` is bytes, not lines.** The default 10,000 kept 469 lines of 80-column output (548 at scale 2); 30,000,000 kept all 10,001 lines. The 10k-line check fails at the default and passes at 30 MB, so "10k lines" cannot be taken from the option's name. A measured constant fixes it, so this alone is not a reason against ghostty-web.
- **`write(data, cb)` is not flow control.** The write is synchronous into the WASM terminal (10,000 lines in 18 to 40 ms) and the callback is `requestAnimationFrame(cb)`. In a window the compositor does not present (the locked-desktop attempt) the callback never fired and a "wait for flush" loop hung. `write('')` throws `RangeError: offset is out of bounds`.
- **Frame pacing on native Wayland is a marginal miss.** The virtual monitor is 60 Hz, so a two-vsync frame measures 33 to 34 ms and the test (combined p95 at most 33.4 ms) sits on that boundary. Over the 10k drain plus a 2 s scroll, 5 to 6 % of frames (6, 6, 7 of 115) were one vsync late, none over 50 ms, max 48 ms; scroll-only p95 was 33, 32 and 34 ms (2 of 3 runs passed). The combined figure fails because it adds the single 36 to 40 ms synchronous drain frame. Other environments passed, but on different grids (Wayland 170x50 in a 1400x863 window; X11 and Chromium 155x46 at 1280x800; scale 2 115x25 at 960x503 logical), so they are not same-configuration comparisons and no cause is claimed. The robust claim is that ghostty-web's frame time was about 1.5 times xterm.js's at a similar cell count (Wayland 34 versus 18 ms at roughly 8.2k to 8.5k cells; X11 28 versus 17 ms). xterm.js (WebGL or DOM) held 17 to 18 ms in every WebKitGTK run. This is not what decides the widget.
- **WASM loading.** Both paths work in WebKitGTK. `wasm=inline` uses `fetch()` on a 564 KB `data:` URL from the `wails://` origin; `wasm=url` fetches `/assets/ghostty-vt-*.wasm`. The npm ESM bundle **contains the base64 copy whatever the path** (section 6), so choosing `url` does not shrink the JS.

### 4.2 xterm.js

- Everything runnable passed. `options.theme` recolours live in the DOM and WebGL renderers; every real paste path is wrapped and converted correctly; Alt is Meta; `fit` and `onResize` agree.
- It needs three small decisions from R2: **Ctrl+Shift+C is not bound** (a key handler returning false after `navigator.clipboard.writeText` fixes it; verified with `copykey=1`), **Ctrl+V is `^V`** (terminal convention; paste is Ctrl+Shift+V, Shift+Insert and middle-click, and on macOS Cmd+V is the browser paste), and **`modes.bracketedPasteMode` updates only after the parser has run**, so read it in the `write` callback (the harness's `paste on` printed `false` for that reason).
- WebGL was acquired in every WebKitGTK run. Its canvas is cleared after compositing, so a pixel check needs `preserveDrawingBuffer` (the `pdb=1` runs; the timing runs do not use it). The DOM renderer meets the 10k numbers too, so it is a safe fallback, with rule gaps between rows and the default font glyphs for boxes.

### 4.3 Both

- Both widgets handled the harness's `table`, `colors`, `unicode`, `wide` and `size` commands, a two-size container resize, bracketed paste through the API, selection through the API, and the injected keys without any `console.error`.
- Scrolling: both scroll with smooth (pixel) wheel input. With discrete-only wheel steps xterm.js did not scroll and ghostty-web did; the injection sends no pixel value, so this is **not** counted against xterm.js and the mouse wheel is a human check.
- Timings at scale 1 versus 2 depend on window size as well as on the widget, so compare rows within one environment only.

### 4.4 WKWebView (macOS)

How it was run is in section 2 (WKWebView row); per-widget reports are `human-wkwebview-{ghostty,xterm-webgl,xterm-dom}.md`, per-step results `human-ax-<widget>-check<N>.json`.

- **xterm.js passed everything that could be run, in both renderers.** Dead keys, the Japanese IME (preedit inline at the caret), Ctrl+C, arrows, Home/End, Ctrl+A/L/R, Cmd+A, Cmd+C (no binding needed on macOS: the browser copy reaches the selection), Cmd+V plain and bracketed, drag and double-click selection, live theme switch including the old output, 10k lines at p95 17 ms. Option+B / Option+F send `∫` / `ƒ`: macOS characters, not Meta, so R2 decides `macOptionIsMeta`.
- **ghostty-web repeats its Linux paste and theme failures and adds three keyboard ones.** A real Cmd+V is raw in bracketed mode (the same split listener as section 4.1). Option+B / Option+F send nothing at all (Linux sent plain `b`, `f`). Cmd+A is not select-all; it types `a` into the line. It captures Ctrl+Cmd+F, the macOS full-screen shortcut, and sends `ESC[102;5u` with Cmd dropped. The IME commits correctly, but the preedit and candidate window are placed at the terminal's top-left, over row 0, not at the caret (`human-ax-ghostty-5-composing-full.png`); its hidden textarea does not follow the cursor. Rendering on Retina is crisp, and frame pacing is fine (p95 17 to 18 ms, no frame over 33 ms), so the native-Wayland miss of row 10 does not reproduce here.
- **Harness limits found on macOS.** The spike calls `fit()` once at open and has no ResizeObserver, so the human resize check measures the harness, not the widget (the automatic check 9 still covers `fit` and `onResize`); R2 must refit on container resize. Wails disables the default context menu unless built with debug or `EnableDefaultContextMenu`, so right-click > Inspect Element does not exist in a `-devtools` build; Cmd+Shift+F12 opens the Web Inspector. The same setting means a right-click anywhere, the terminal included, shows no menu, which R2 may want for copy and paste.
- **What the agent-driven run cannot claim.** Keys were real HID events, but scrolling was synthetic (continuous, phased scroll events), so "no stall" is measured and "follows the fingers" is not. The layout was US, which has the same Option dead keys as ABC.
## 5. API gap table (what R1/R2 need)

"Verified" names the run or file that shows the behaviour; "source" means read in `node_modules/ghostty-web/dist/ghostty-web.js` or `index.d.ts` (0.4.0) and not exercised.

| R1/R2 need | ghostty-web 0.4.0 | @xterm/xterm 6.0.0 |
|---|---|---|
| Bytes to Go (`onData`) | present; every key and paste in the injected runs arrived exactly once (`keys-*-summary.json`) | present; same |
| Grid size to Go (`onResize`) | present; equals `fit()` in every run (`*-resize.json`) | present; same |
| `write(data, cb)` for backpressure | **synchronous** into the WASM terminal; the callback is `requestAnimationFrame(cb)` (source, `writeInternal`). Observed failure mode: in a window the compositor does not present (`visibilityState` hidden, 0 frames) a flush loop that waits for the callback hung (first WebKitGTK attempt on the locked desktop; `hidden-ghostty-progress.txt` for an iconified window). `write('')` throws `RangeError: offset is out of bounds` (Chromium run, `Terminal.write` via `Uint8Array.set`). No pending-bytes or watermark API | asynchronous, chunked parse; the callback fires after the data is parsed (`*-lines.json`: 10,000 lines drained in 50 to 167 ms) |
| Real paste in bracketed-paste mode | **FAIL on the everyday path**: the container's `handlePaste` sends the text raw, and after a left click focus is on the container, so click then Ctrl+V or Ctrl+Shift+V is raw even with mode 2004 on (`keys-*-ctrl-v-on.json`: `line-one\nline-two`). The textarea listener calls `Terminal.paste()`, which wraps; middle-click reaches it and is wrapped. Shift+Insert is **not** paste: it sends `ESC[2~`. Newlines stay `\n` | wraps on every path (Ctrl+Shift+V, Shift+Insert, middle-click, `paste()`), converts `\n` to `\r`. Ctrl+V is `^V` (terminal convention), not paste |
| Bracketed-paste query | `hasBracketedPaste()`, `getMode(2004)`; synchronous and correct after the write | `modes.bracketedPasteMode`; **updates after the parser runs**, so reading it right after `write()` returns the old value (`paste on` printed `false` in the xterm runs); read it in the write callback |
| Selection text and programmatic select | `getSelection`, `select(col,row,len)`, `selectAll`, `selectLines` | same |
| Cmd shortcuts (macOS) | Cmd+A types `a` (no select-all); Cmd+C copies a drag selection (already copied on mouse-up, so this does not show Cmd+C works); Cmd+V pastes raw; Ctrl+Cmd+F is captured and sent as `ESC[102;5u` | Cmd+A selects all, Cmd+C copies, Cmd+V pastes (bracketed when asked); none reach the REPL |
| Copy to the clipboard | **auto-copies on mouse-up** (`copyToClipboard` in the selection manager; the clipboard changed after a plain drag, `keys-*-summary.json` A). No option to turn it off in `ITerminalOptions`. Ctrl+Shift+C does nothing extra | no auto-copy; Ctrl+Shift+C does nothing by default (clipboard stayed `untouched`); a custom key handler fixes it (`copykey=1`: clipboard became the selection) |
| Key handler hook | `attachCustomKeyEventHandler`; **return true = handled, skip default** | `attachCustomKeyEventHandler`; **return false = skip default**. Opposite conventions behind the same name |
| Alt as Meta | **lost**: Alt+B and Alt+F arrive as plain `b` and `f` (`keys-*-emacs.json`). No `macOptionIsMeta`. On WKWebView Option+B and Option+F send **nothing** (`human-ax-ghostty-check6a.json`) | `ESC b` and `ESC f` on Linux. On WKWebView Option+B / Option+F send `∫` / `ƒ` by default (`human-ax-xterm-webgl-check6a.json`); `macOptionIsMeta` turns that into `ESC b` |
| Other keys | arrows, Home/End, Delete, PageUp/Down, Ctrl+A/E/L/R/D/Z, Tab, Esc match xterm; Ctrl+Shift+A sends `^A` (Shift dropped) | match; Ctrl+Shift+A sends nothing |
| Theme from `--vibe-*` | set once at construction; `options.theme =` after `open()` logs "theme changes after open() are not yet fully supported"; the cursor follows `renderer.setTheme` but the background and palette **do not recolour** (`*-theme.json`: in place failed on Chromium, WebKitGTK Wayland and X11); `renderer.setTheme` is not enough because the WASM terminal keeps its palette. Recreate works (and loses the screen and scrollback unless replayed) | `options.theme =` recolours live (DOM and WebGL) |
| Fit | `FitAddon` is in the package (`fit()`, `proposeDimensions()`, `observeResize()`), reserves 15 px for its scrollbar | `@xterm/addon-fit` 0.11.0 |
| Scrollback cap | `scrollback` is a **byte budget** handed to libghostty, not a line count: the default 10,000 kept 469 lines of 80-column text; 30,000,000 kept the 10,001 lines (`*-lines.json`, `lengthAfter`). Needs a measured constant in R2 | lines (20,000 kept 10,001) |
| Scroll position | `getViewportY()` is lines **from the bottom** (206.9 after wheel-up, 0 after wheel-down) and fractional (smooth scrolling) | `buffer.active.viewportY` is the top line index |
| Box drawing | font glyphs on a canvas; vertical rules are lighter and joints show a faint cross (`zoom-box-ghostty-x11.png`) | WebGL draws box characters itself (clean, `zoom-box-xterm-webgl-x11.png`); the DOM renderer uses font glyphs and leaves gaps between rows (`zoom-box-xterm-dom-x11.png`) |
| Wide, emoji and combining text | correct widths for CJK, emoji, `e` + combining, Devanagari in the canvas PNG | CJK correct; emoji and Devanagari overlap in WebGL without `@xterm/addon-unicode11` |
| Parser hooks (`registerOscHandler`, `onBinary`, decorations) | missing | present; not needed by the REPL design |
| `dispose()` and re-open | present; re-open reused the loaded WASM | present |
| Hidden or occluded window | render loop and write callbacks are `requestAnimationFrame`-bound (see `write`). In an iconified window the automatic run stalled at its first flush (`hidden-ghostty-progress.txt`) | not isolated: the same run progressed several steps while the window was hidden (`hidden-xterm-progress.txt`), but stalled once at a flush too. Treat the write callback as unproven while hidden and test it in R2 |
| Dead keys, IME composition | `compositionstart/update/end` handled; `keyCode 229` and `isComposing` ignored in `keydown` (source). WKWebView: dead keys correct; IME commits once with nothing sent while composing, but the preedit and candidates sit at the terminal's top-left, not the caret. Linux: **not run** | handled (source). WKWebView: dead keys correct; IME preedit inline at the caret, committed once. Linux: **not run** |

## 6. Bundle sizes (`npm run build`, `evidence/bundle-size.txt`)

| Piece | Raw | gzip -9 | Notes |
|---|---:|---:|---|
| Studio's existing `assets/*.js` (the spike chunks removed; the lazy imports make the split exact) | 1,263,473 | 416,941 | the baseline |
| **ghostty-web** JS chunk | **636,528** | **183,786** | includes a 564,089-character base64 copy of the WASM; loaded in both `inline` and `url` paths |
| ghostty `ghostty-vt.wasm` asset | 423,045 | 125,202 | only fetched when `wasm=url`; still embedded in the Go binary |
| ghostty total in `dist/` | 1,059,573 | 308,988 | what `go:embed` adds to the binary |
| ghostty if the base64 literal were stripped (what a URL-only path could ship) | 72,468 + 423,045 | 19,549 + 125,202 | needs a patched build or an alias; not available from the npm package as is |
| **xterm.js** (`xterm` 330,956 + `addon-fit` 1,205 + `addon-webgl` 113,360) | **445,521** | **113,493** | plus `xterm.css` (7,112 raw in the npm package; in the build it is folded into `ConsoleSpikeView-*.css`, 4,585 B with the spike's styles); loaded lazily |

The plan's "about 400 KB" is the raw `.wasm`. What the default `init()` ships is a 682 KB ESM file (637 KB minified here); the honest comparison is ghostty-web +636 KB raw, +184 KB gzip (+50 % of Studio's JS raw, +44 % gzip) against xterm.js +446 KB raw, +113 KB gzip (+35 % raw, +27 % gzip). Neither is loaded until the console opens, and neither blocks the decision.

## 7. go-libghostty (decision 3)

Time-boxed experiment in a scratch directory outside the worktree. `go get go.mitchellh.com/libghostty@latest` (v0.0.0-20261021212106-df1957cbff56) fails to link until `libghostty-vt` exists (`Package libghostty-vt-static was not found in the pkg-config search path`). `make build` in a copy of the module (CMake `FetchContent` clones `ghostty-org/ghostty` at a pinned commit and runs `zig build`) succeeded in **118 s** with Zig 0.17.0 and CMake 4.3.0 (611 MB build directory); `make test` took 3 s and passed; a 12-line test fed `┌──┐\r\n│\e[1;32mhi\e[0m│\r\n└──┘` into a `Terminal` and asserted the exact plain-text screen (re-run with the assertion: `golibghostty-screen-test-v.log`, `golibghostty-make-build2.log`). So it works and is fast.

**Decision: no (the plan's default stands).** CI would need a Zig 0.17 install and a Ghostty source checkout on the Linux test runner and on macOS (and the release runners if the tests ran there), a network fetch of a 600 MB build tree, and a pinned commit to track; the README says "I'm not promising any API stability yet". R1's renderers emit a closed set of SGR sequences and `engine.TerminalText` scrubs every value, so byte-level goldens are exact and cheap, and nothing in R1 needs screen-state assertions. Revisit only if R4 (the line editor) needs to assert on cursor movement and wrapping.
## 8. What the owner runs

### 8.1 Linux (WebKitGTK), about 25 minutes, at the real keyboard

The host's own desktop was locked for the whole spike, so no window could be shown on it; the WebKitGTK runs used a private headless mutter (section 2). The checks below need a person, a layout change or an input method engine, none of which this host lets an agent change. These checks are now R2's acceptance (#259), run on the real console on the owner's session; the harness commands below remain a reference.

```bash
git fetch origin spike/console-r0 && git checkout spike/console-r0
cd studio/frontend && npm ci && cd ..
# one window per widget, no automatic run (the window shows buttons, a seed box and an `rx:` line with the bytes Go would receive)
VIBE_SPIKE_EVIDENCE_DIR=$PWD/../spike/console/evidence VIBE_SPIKE_ROUTE='#/spike/console?widget=xterm-webgl&copykey=1' task studio:dev
# then widget=ghostty&wasm=url&sb=30000000&copykey=1 and widget=xterm-dom
```

For each widget, after each check press `snapshot` (writes `snapN.json` with the screen text, the received bytes and the selection) and write one line per check in `spike/console/evidence/human-linux-<widget>.md`: action, what you saw, pass or fail.

| # | Do this | Pass means |
|---|---|---|
| 4 | GNOME Settings > Keyboard > add "English (US, intl., with dead keys)" and switch to it. Click the terminal. Type `'` then `e`, `"` then `u`, `'` then Space. Switch back. | `é`, `ü` and `'` appear once each in the terminal and `rx:` shows each as one UTF-8 sequence, nothing else |
| 5 | Add an input method engine (for example `ibus-libpinyin` or `ibus-anthy`), switch to it, type `nihao` or `kanji` without pressing Enter, then commit a candidate. If you have no engine, write NOT RUN. | the preedit appears over the terminal, **nothing** reaches `rx:` while composing, the committed text arrives once |
| 6 | Use the keys the injected run used, on your own keyboard: Ctrl+C, Ctrl+Shift+C after a drag selection, Ctrl+Shift+V, Shift+Insert, middle-click, Alt+B, arrows, Home/End, Ctrl+L, Ctrl+R. Compare with section 3 row 6 and the `keys-*-summary.json` files | same bytes as the injected run |
| 10 | Run `lines 10000`; scroll with the trackpad (two fingers) while it drains and for 3 seconds after; then with a mouse wheel. | follows your fingers, no freeze over about 0.3 s, no blank frames. Note any stutter and whether it differs between widgets |
| 3 | Pick another theme in the harness dropdown with output on screen (`schoolhouse-light` to `chalkboard`). In the real app this is Settings > Appearance; the harness drives the same `applyThemeToDocument` path | colours change live; note what happens to the old output |
| 9 | Resize the window by dragging an edge, maximise, restore | no clipped last row and no stale pixels (the harness does not refit on window resize, so `size` stays fixed; that check waits for R2) |
| 1 | Scale to 200 % (GNOME Settings > Displays) and reopen the harness | glyphs crisp, box lines meet, `devicePixelRatio` is 2 in `raf.json` |
| 6b | Right-click inside the terminal | record what happens (the webview context menu appears or not) |

### 8.2 macOS (WKWebView): done 2026-10-28

Run on the owner's Mac (section 2, WKWebView row; results in section 4.4 and the matrix). What remains for a person, a physical trackpad swipe and mouse wheel during `lines 10000` (the run used synthetic scroll events) and the window-resize refit, is part of R2's acceptance (#259). The original instructions follow.

`spike/console/mac/CHECKLIST.md` on the spike branch has the exact commands. In short: `gh workflow run ci.yml --ref spike/console-r0`, download the `vibe-studio-spike-macos-arm64` artifact of the `build-smoke (macos-arm64)` job, `xattr -dr com.apple.quarantine vibe-studio.app`, run the six automatic configurations with `VIBE_SPIKE_EVIDENCE_DIR` and `VIBE_SPIKE_ROUTE` set, then do the human checks (Option+E dead key, Option+B, Cmd+C/V/A, trackpad scroll during `lines 10000`, Japanese or Pinyin IME, Retina crispness, theme switch, resize, right-click > Inspect Element) and zip `~/vibe-spike-evidence` back. Section 3a of that checklist drives the same checks by accessibility scripting (how this run was done).

## 9. Decisions and follow-ups

### Decision 2: the widget

**Decided by the owner on 2026-10-28, as R0 recommended: xterm.js 6.0.0 (+ fit 0.11.0 + webgl 0.19.0, DOM renderer as the fallback).** Applying the plan's decision rule on WebKitGTK: ghostty-web is PARTIAL on theme switch (row 3) and shortcuts (row 6) and FAIL on real paste in bracketed mode (row 8); the rule makes xterm.js the fallback on any ghostty FAIL in rows 1, 8, 9, 10 or 11, and row 8 alone triggers it. Row 10 is a marginal miss and does not carry the decision. xterm.js is PARTIAL on row 6 (two app key bindings) and otherwise passed what could be run. Rows 4 and 5 are open for both widgets on WebKitGTK; if both fail them there that is a WebKitGTK finding and the smaller API gap (xterm.js) wins anyway. On WKWebView the rule gives the same answer: ghostty-web fails row 8 and is PARTIAL on rows 3, 5 and 6, while xterm.js passes every row that could be run.

What would change it: R2's Linux acceptance checks showing xterm.js failing dead keys or the IME on WebKitGTK while ghostty-web passes them, or ghostty-web fixing the five behaviours in section 5 (paste wrapping, in-place theme, Alt/Meta, scrollback in lines, rAF-bound write callbacks). If the epic's stop rule is ever evaluated, it needs both widgets to fail rows 1, 9 or 10 on WebKitGTK or WKWebView; xterm.js passes them on WebKitGTK, so it is not triggered.

### Decision 3: go-libghostty for Go golden-screen tests (recommendation)

**No.** See section 7.

### For R1 and R2 (if xterm.js stands)

- Render with WebGL and fall back to the DOM renderer on `onContextLoss` or a failed `WebglAddon`; both met 10k lines at p95 17 to 18 ms. Load `@xterm/addon-unicode11` (emoji and Devanagari overlap without it) and keep `customGlyphs` on for the duckbox rules.
- Bind Ctrl+Shift+C (Linux) and Cmd+C (macOS) through `attachCustomKeyEventHandler` (return false) and write with `navigator.clipboard`; the spike's `copykey=1` shows the shape. Do not bind Ctrl+C: it must stay `^C` for cancel.
- Read `modes.bracketedPasteMode` inside the `write` callback. Use the `write` callback for backpressure and cap the in-flight bytes; do not assume it fires in a hidden window.
- Set the theme through `options.theme` from a MutationObserver on `<html>` (`style`, `data-theme`), as the harness does; there is no need to recreate.
- Add `@xterm/xterm`, its two addons and (if used) unicode11 to `THIRD_PARTY_NOTICES` (MIT). Pin exact versions.
- macOS: Option sends `∫` / `ƒ`, not Meta. Decide `macOptionIsMeta` (a setting, or on by default for a REPL), and leave Cmd+C/V/A to the browser: they already work.
- Refit on container resize (ResizeObserver plus `fit()`); the spike harness fits only once at open.
- Expose the Web Inspector in dev builds through Cmd+Shift+F12 and decide whether the console needs its own context menu (copy, paste): Wails suppresses the default one.
- Keep the harness's evidence habit for R2's Rodney pass: a snapshot button that writes the received bytes and the screen text.
- Fix the `wails` CLI / `go.mod` version mismatch (v2.16.0 versus v2.17.0) before anyone builds the console on a dev machine.

## 10. Discrepancies with the issue and the plan doc

1. **"About 400 KB" of WASM.** That is the raw `.wasm`; the npm package ships a 682 KB ESM with the WASM inlined as base64, and a URL load path does not remove it (section 6).
2. **"The API is the same, so switching costs an import."** Not true: section 5 lists differences in paste, theme, Meta, copy, scrollback units, key-handler return values and write callbacks.
3. **"Cmd/Ctrl shortcuts"** was undefined; the tested list is in `keys-run.py` (Ctrl+C, Ctrl+Shift+C, Ctrl+V, Ctrl+Shift+V, Shift+Insert, middle-click, arrows, Home/End, Del, PgUp/PgDn, Alt+B/F, Ctrl+A/E/L/R/D/Z, Tab, Esc, double-click, drag, wheel).
4. **"WASM loads under Studio's CSP"** is trivially true for a `frame-src`-only policy; the real risks, `fetch()` of a `data:` URL from `wails://` and the production origin and MIME type, both passed.
5. **"10k lines scroll smoothly"** now has numbers (drain at most 2000 ms, p95 frame at most 33 ms, at most 5 % of frames over 50 ms, 10,000 lines kept).
6. **"A throwaway Studio route"** plus a Chromium baseline; headless Chrome never counts as a webview result.
7. **The plan assumed XTEST under XWayland and a visible desktop.** Neither held (section 2).
8. **ghostty-web's `scrollback` unit** (bytes) is new information, not in its README.
9. **The macOS checklist's "right-click, Inspect Element"** cannot pass in a `-devtools` build: Wails keeps the default context menu off unless the build is debug or sets `EnableDefaultContextMenu`. Cmd+Shift+F12 opens the inspector.
10. **The macOS checklist's resize check** assumed the harness refits; it fits once at open, so that check measures the harness.

## 11. What an agent can and cannot verify here

Can (and did): rendering via `canvas.toDataURL()` and X11 window PNGs, theme resolution, bracketed-paste bytes through the API and through real paste events, resize and fit numbers, 10k-line timings with repeats, WASM loading on both Linux origins, bundle sizes, injected key, pointer and clipboard behaviour at compositor level, scale 1 and 2, a go-libghostty build.

Can on macOS (and did, on the owner's Mac by accessibility scripting): every automatic configuration, real HID key events through the macOS input system (dead keys, Japanese IME), clicks, drags, Cmd shortcuts, the clipboard, theme switch, Retina screenshots, the Web Inspector.

Cannot: IM, dead keys or perceived smoothness in the private compositor (section 2 lists why); a physical trackpad or mouse wheel on macOS (scrolling there was synthetic); dead keys without changing a keyboard layout (a person does it); IME composition (no engine installed); a physical keyboard, trackpad or mouse wheel (the discrete-wheel injection carries no pixel delta); the owner's own session (locked); native-Wayland window screenshots; whether a person finds the frame pacing smooth. Synthetic DOM events are labelled synthetic and are never counted as IME or dead-key results. Headless Chrome is a baseline, not a webview result.

## Appendix A. Full automatic results per configuration

One table per environment (`<config>-<env>-results.json`; the check numbers are the matrix rows). `ghostty url, default sb` and `ghostty inline, default sb` use ghostty-web's default `scrollback` option and are expected to fail row 10 (section 4.1). `xterm-webgl` has no pixel readback (INFO on row 3) because WebGL clears its canvas after compositing; the `pdb` run turns on `preserveDrawingBuffer` for that one measurement. `wl2x` is native Wayland at scale 2 (`devicePixelRatio` 2, window 960x503 logical) and `prod` is the `task build:studio:linux` binary on the `wails://wails` origin.

### Chromium baseline (Rodney)

| check | ghostty url, sb=30M | ghostty url, default sb | ghostty inline, default sb | xterm-webgl | xterm-webgl pdb | xterm-dom |
|---|---|---|---|---|---|---|
| Renders | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme roles | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme switch | PARTIAL recreate only | PARTIAL recreate only | PARTIAL recreate only | INFO | PASS in place | PASS in place |
| Copy (getSelection) | PASS | PASS | PASS | PASS | PASS | PASS |
| Bracketed paste | PASS | PASS | PASS | PASS | PASS | PASS |
| Resize/fit | PASS | PASS | PASS | PASS | PASS | PASS |
| 10k lines | PASS 26ms p95, 29ms drain, 10001 lines | FAIL 26.8ms p95, 25ms drain, 515 lines | FAIL 25.8ms p95, 27ms drain, 515 lines | PASS 24.9ms p95, 147ms drain, 10001 lines | PASS 23.3ms p95, 167ms drain, 10001 lines | PASS 17.5ms p95, 74ms drain, 10001 lines |
| WASM/init | PASS 18ms | PASS 21ms | PASS 32ms | PASS 38ms | PASS 26ms | PASS 21ms |
| Errors | PASS | PASS | PASS | PASS | PASS | PASS |

### WebKitGTK native Wayland, scale 1 (dev origin `wails://wails.localhost:34115`)

| check | ghostty url, sb=30M | ghostty url, default sb | ghostty inline, default sb | xterm-webgl | xterm-webgl pdb | xterm-dom |
|---|---|---|---|---|---|---|
| Renders | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme roles | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme switch | PARTIAL recreate only | PARTIAL recreate only | PARTIAL recreate only | INFO | PASS in place | PASS in place |
| Copy (getSelection) | PASS | PASS | PASS | PASS | PASS | PASS |
| Bracketed paste | PASS | PASS | PASS | PASS | PASS | PASS |
| Resize/fit | PASS | PASS | PASS | PASS | PASS | PASS |
| 10k lines | FAIL 34ms p95, 38ms drain, 10001 lines | FAIL 34ms p95, 32ms drain, 515 lines | FAIL 36ms p95, 37ms drain, 515 lines | PASS 18ms p95, 58ms drain, 10001 lines | PASS 18ms p95, 56ms drain, 10001 lines | PASS 18ms p95, 55ms drain, 10001 lines |
| WASM/init | PASS 15ms | PASS 17ms | PASS 18ms | PASS 26ms | PASS 34ms | PASS 17ms |
| Errors | PASS | PASS | PASS | PASS | PASS | PASS |

### WebKitGTK X11 (Xwayland), scale 1

| check | ghostty url, sb=30M | ghostty url, default sb | ghostty inline, default sb | xterm-webgl | xterm-webgl pdb | xterm-dom |
|---|---|---|---|---|---|---|
| Renders | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme roles | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme switch | PARTIAL recreate only | PARTIAL recreate only | PARTIAL recreate only | INFO | PASS in place | PASS in place |
| Copy (getSelection) | PASS | PASS | PASS | PASS | PASS | PASS |
| Bracketed paste | PASS | PASS | PASS | PASS | PASS | PASS |
| Resize/fit | PASS | PASS | PASS | PASS | PASS | PASS |
| 10k lines | PASS 28ms p95, 33ms drain, 10001 lines | FAIL 28ms p95, 32ms drain, 515 lines | FAIL 30ms p95, 30ms drain, 515 lines | PASS 17ms p95, 65ms drain, 10001 lines | PASS 17ms p95, 63ms drain, 10001 lines | PASS 17ms p95, 59ms drain, 10001 lines |
| WASM/init | PASS 17ms | PASS 14ms | PASS 43ms | PASS 40ms | PASS 30ms | PASS 17ms |
| Errors | PASS | PASS | PASS | PASS | PASS | PASS |

### WebKitGTK native Wayland, scale 2

| check | ghostty url, sb=30M | ghostty url, default sb | ghostty inline, default sb | xterm-webgl | xterm-webgl pdb | xterm-dom |
|---|---|---|---|---|---|---|
| Renders | PASS | n/a | n/a | n/a | PASS | PASS |
| Theme roles | PASS | n/a | n/a | n/a | PASS | PASS |
| Theme switch | PARTIAL recreate only | n/a | n/a | n/a | PASS in place | PASS in place |
| Copy (getSelection) | PASS | n/a | n/a | n/a | PASS | PASS |
| Bracketed paste | PASS | n/a | n/a | n/a | PASS | PASS |
| Resize/fit | PASS | n/a | n/a | n/a | PASS | PASS |
| 10k lines | PASS 18ms p95, 26ms drain, 10001 lines | n/a | n/a | n/a | PASS 18ms p95, 57ms drain, 10001 lines | PASS 18ms p95, 55ms drain, 10001 lines |
| WASM/init | PASS 46ms | n/a | n/a | n/a | PASS 63ms | PASS 16ms |
| Errors | PASS | n/a | n/a | n/a | PASS | PASS |

### WebKitGTK native Wayland, scale 2, production build (`wails://wails`)

| check | ghostty url, sb=30M | ghostty url, default sb | ghostty inline, default sb | xterm-webgl | xterm-webgl pdb | xterm-dom |
|---|---|---|---|---|---|---|
| Renders | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme roles | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme switch | PARTIAL recreate only | PARTIAL recreate only | PARTIAL recreate only | INFO | PASS in place | PASS in place |
| Copy (getSelection) | PASS | PASS | PASS | PASS | PASS | PASS |
| Bracketed paste | PASS | PASS | PASS | PASS | PASS | PASS |
| Resize/fit | PASS | PASS | PASS | PASS | PASS | PASS |
| 10k lines | PASS 18ms p95, 23ms drain, 10001 lines | FAIL 18ms p95, 19ms drain, 548 lines | FAIL 18ms p95, 18ms drain, 548 lines | PASS 18ms p95, 54ms drain, 10001 lines | PASS 18ms p95, 50ms drain, 10001 lines | PASS 18ms p95, 52ms drain, 10001 lines |
| WASM/init | PASS 27ms | PASS 14ms | PASS 26ms | PASS 46ms | PASS 28ms | PASS 21ms |
| Errors | PASS | PASS | PASS | PASS | PASS | PASS |

### WKWebView, macOS 26.6.2, production build (`wails://wails`, `devicePixelRatio` 2)

`ghostty inline, default sb` is from a separate run the owner started by hand with the same build; the agent's own inline run used `sb=30000000` (PASS 18ms p95, 25ms drain, 10001 lines; `ghostty-inline-sb30000000-wkwebview-prod-results.json`). Grids: ghostty 155x43, xterm-webgl 166x38, xterm-dom 160x38 in a 1280x800 page.

| check | ghostty url, sb=30M | ghostty url, default sb | ghostty inline, default sb | xterm-webgl | xterm-webgl pdb | xterm-dom |
|---|---|---|---|---|---|---|
| Renders | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme roles | PASS | PASS | PASS | PASS | PASS | PASS |
| Theme switch | PARTIAL recreate only | PARTIAL recreate only | PARTIAL recreate only | INFO | PASS in place | PASS in place |
| Copy (getSelection) | PASS | PASS | PASS | PASS | PASS | PASS |
| Bracketed paste | PASS | PASS | PASS | PASS | PASS | PASS |
| Resize/fit | PASS | PASS | PASS | PASS | PASS | PASS |
| 10k lines | PASS 17ms p95, 24ms drain, 10001 lines | FAIL 18ms p95, 24ms drain, 515 lines | FAIL 18ms p95, 24ms drain, 515 lines | PASS 17ms p95, 59ms drain, 10001 lines | PASS 17ms p95, 62ms drain, 10001 lines | PASS 17ms p95, 54ms drain, 10001 lines |
| WASM/init | PASS 20ms | PASS 15ms | PASS 16ms | PASS 175ms | PASS 44ms | PASS 16ms |
| Errors | PASS | PASS | PASS | PASS | PASS | PASS |

## Appendix B. Frame-time repeats for row 10 (scale 1; window and grid differ per environment)

Grids: Wayland 1400x863 window, ghostty 170x50 and xterm-webgl 195x42; X11 and Chromium 1280x800, ghostty 155x46.

| config | run | p95 frame (ms) | frames over 33 ms | max (ms) | drain (ms) |
|---|---|---:|---:|---:|---:|
| ghostty-url-sb30000000 | Wayland run 1 | 34 | 6 of 114 | 48 | 38 |
| ghostty-url-sb30000000 | Wayland run 2 | 35 | 6 of 115 | 47 | 36 |
| ghostty-url-sb30000000 | Wayland run 3 | 34 | 7 of 115 | 45 | 40 |
| ghostty-url-sb30000000 | X11 | 28 | 0 of 121 | 33 | 33 |
| ghostty-url-sb30000000 | Chromium | 26 | 0 of 118 | 30.9 | 29 |
| xterm-webgl | Wayland run 1 | 18 | 1 of 123 | 34 | 58 |
| xterm-webgl | Wayland run 2 | 18 | 0 of 123 | 33 | 57 |
| xterm-webgl | Wayland run 3 | 18 | 0 of 123 | 32 | 57 |
| xterm-webgl | X11 | 17 | 0 of 128 | 33 | 65 |
| xterm-webgl | Chromium | 24.9 | 1 of 110 | 47.1 | 147 |
| xterm-dom | Wayland run 1 | 18 | 0 of 123 | 29 | 55 |
| xterm-dom | Wayland run 2 | 18 | 0 of 122 | 31 | 59 |
| xterm-dom | Wayland run 3 | 18 | 0 of 122 | 32 | 59 |
| xterm-dom | X11 | 17 | 0 of 128 | 28 | 59 |
| xterm-dom | Chromium | 17.5 | 0 of 126 | 23.4 | 74 |

The ghostty-web Wayland misses are consistent (three runs, 5 to 6 % of frames over 33 ms); the same widget passes under Xwayland and in Chromium, and xterm.js is flat at 17 to 18 ms everywhere. Frame time is `requestAnimationFrame` deltas while 10,000 lines drain and a 2-second programmatic scroll runs.

## Appendix C. Evidence index

Names are `<config>-<env>-<what>`; configs `ghostty-url-sb30000000`, `ghostty-url`, `ghostty-inline`, `xterm-webgl`, `xterm-webgl-pdb`, `xterm-dom` (a `-sbN` suffix is the ghostty scrollback option); envs as in Appendix A (`-wlr2`, `-wlr3` repeats; `-x11k`, `-inj`, `-injc` are the injected-key runs).

| File | What it holds |
|---|---|
| `*-results.json` | every automatic check with status and raw numbers |
| `*-init.json`, `*-raf.json` | origin, user agent, `devicePixelRatio`, WASM load path, init time; 1 s frame count and visibility |
| `*-render-canvas.png`, `*-render-dump.txt` | what the widget drew (canvas PNG) and the screen text |
| `*-theme.json`, `*-theme-before/after/recreated.png` | resolved roles for the four built-in themes; the live switch |
| `*-resize.json`, `*-lines.json`, `*-lines-tail.txt` | fit numbers; timings and scrollback kept |
| `*-rx.json`, `*-feed-log.json`, `*-errors.txt` | every byte sequence the widget handed to the REPL; commands and results; captured errors (empty) |
| `keys-<prefix>-<widget>-summary.json`, `keys-<prefix>-<widget>-<step>.json/.png` | the injected-key runs; prefixes `x11-`, `wl-` (default), `x11c-` (with the Ctrl+Shift+C binding), `wlw-` (wheel only) |
| `zoom-box-*-x11.png`, `zoom-2x-*.png` | box-drawing crops at scale 1 and 2 |
| `bundle-size.txt`, `00-build.log`, `08-build-final.log`, `10-prod-build.log` | sizes and builds |
| `hidden-*-progress.txt`, `hidden-*-raf.json` | iconified-window runs |
| `wails-*.log`, `mutter.log` | app and compositor logs |
| `*-wkwebview-prod-*`, `wkwebview-system.txt` | the six macOS automatic configurations; `sw_vers` and the display resolution |
| `human-wkwebview-<widget>.md`, `human-ax-<widget>-check<N>.json`, `human-ax-*.png` | the macOS human checks: one line per check, the bytes each step delivered and the screen tail, and a curated set of window screenshots (theme before and after, IME composition, Retina crops, small window) |

The spike branch carries the reproduction scripts (`spike/console/README.md`) and a curated subset of the evidence under `spike/console/evidence-curated/` (all JSON and text results, the summaries, the bundle, build and go-libghostty logs, and about 15 key PNGs; no binaries and not the roughly 285 per-step screenshots). The full set was 975 files and 20 MB and is not kept; names in this document refer to the full set, and the curated subset has the result files the matrix cites and the PNGs named in sections 3 and 4.
