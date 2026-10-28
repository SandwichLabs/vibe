# Data console R0 spike (#257): THROWAWAY, never merge

ghostty-web 0.4.0 vs @xterm/xterm 6.0.0 inside Studio's real webviews. Findings: `docs/plans/spike-console-r0-findings.md`.
Everything under `spike/console/`, `studio/spike_console.go` and `studio/frontend/src/spike/` exists only on
`spike/console-r0`. Edits to shipped files are marked `SPIKE #257 - never merge`
(`studio/main.go`, `src/App.vue`, `src/router/index.ts`, `package.json`, `.github/workflows/ci.yml`, `.gitignore`).

## Run it

```bash
cd studio/frontend && npm ci && npm run build            # 00-build.log
# 1. Chromium baseline (Rodney, headless Chrome; NOT a webview result)
spike/console/run-webkitgtk.sh chromium ''                # wails dev; prints the process group id
spike/console/rodney-matrix.sh                            # evidence/*-chromium-http-*
# 2. WebKitGTK. The host's own desktop is locked (compositors do not present windows, rAF never fires), so a
#    private headless mutter is started (own D-Bus bus, own Wayland socket, own Xwayland):
spike/console/headless-compositor.sh start
spike/console/run-webkitgtk.sh hw ''                      # native Wayland window in the private compositor
LBL=wl spike/console/webkit-matrix.sh webkitgtk-dev       # auto checks: *-webkitgtk-dev-wl-*
python3 spike/console/keys-run.py wl-ghostty 'widget=ghostty&wasm=url&sb=30000000'      # real keyboard + pointer
spike/console/run-webkitgtk.sh hx ''                      # same compositor, X11 (Xwayland) window; adds window PNGs
LBL=x11 spike/console/webkit-matrix.sh webkitgtk-dev
python3 spike/console/keys-run.py x11-ghostty 'widget=ghostty&wasm=url&sb=30000000' --shots
# 3. production origin: task build:studio:linux, then BIN=studio/build/bin/vibe-studio spike/console/run-webkitgtk.sh hw ''
LBL=prod spike/console/webkit-matrix.sh webkitgtk-prod
node spike/console/bundle-size.mjs studio/frontend/dist  # evidence/bundle-size.txt
spike/console/summarize.py [filter]                       # one line per check per results.json
```

Stop what you started by process group: `kill -- -<PGID>` (printed by run-webkitgtk.sh), then
`headless-compositor.sh stop`. Never `pkill -f`.

## Why not XTEST

Xwayland (>= 24) sends XTEST through a portal (`-enable-ei-portal`); in a private compositor that raises an
approval dialog nobody can click (it covered the middle of the screen and swallowed every pointer event).
`inject/main.go` uses the private mutter's own `org.gnome.Mutter.RemoteDesktop` on the private bus instead. It is
compositor-level input (real focus, real keymap, real GTK input path), and it works for native Wayland windows.

## Files

| File | Purpose |
|---|---|
| `src/spike/console/*` | the harness: adapters for both widgets, a fake REPL, theme from `--vibe-*`, the automatic checks, the route view |
| `studio/spike_console.go` | `/spike/evidence/<name>` POST sink (only when `VIBE_SPIKE_EVIDENCE_DIR` is set) and `route.txt` follower |
| `run-webkitgtk.sh`, `headless-compositor.sh` | start Studio / the private compositor |
| `rodney-matrix.sh`, `webkit-matrix.sh` | run the automatic checks per configuration |
| `keys-run.py`, `inject/` | real input for the shortcut, copy and paste checks |
| `xshot.py` | X11 window screenshot via XGetImage (ImageMagick `import` fails on this host) |
| `bundle-size.mjs`, `summarize.py` | reports |
| `mac/CHECKLIST.md` | what the owner runs on macOS |
| `evidence/` | gitignored full output of a run |
| `evidence-curated/` | the committed subset; the findings doc cites file names from here |
