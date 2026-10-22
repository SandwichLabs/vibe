# HTML pages (#162, P2–P6): macOS owner QA results

- **Date:** 2026-10-22, 08:18–10:20 CDT
- **Machine:** macOS 26.6.2 (build 25G83), Apple M5 (arm64)
- **Toolchain:** Go 1.27.3 darwin/arm64, Wails CLI v2.16.0, Xcode Command Line Tools 27.0.0.0.1788430756 (Apple clang 21.0.0; no full Xcode installed), Node **v24.16.0** (the brief asked for 22)
- **Commit tested:** `d4314d1` (main, P6 #197), version stamp `v0.1.4-27-gd4314d1`
- **Checklist:** `HTML-PAGES-MAC-QA.md` from `html-pages-mac-qa.zip` (sha256 `cf549858545ce7bb5b4189ee93a55fb58abb82112d37571e13c9d3f258fadb31`)

Log names below are files in `logs.tgz`.

## Summary for review

| Part | Verdict |
|---|---|
| B. Static page host | **Pass.** Every box is Verified, and the control proves the instrument. |
| C. Page window and network gate | **Pass with findings.** One stop condition was hit (`open` group, see below). The dead proxy port does not refuse at once on 26.6.2. Rule-list folders are never cleaned up after a Studio-side close. §7 (macOS 14 and 13) can't be run on this Mac. |
| D. Capabilities | **Pass, with one finding:** a dropped file's name, size, type and mtime reach the page. Its contents can't be read. |
| E. Prompt, agent, bundles, CLI | **Pass.** The bundle round trip was done on one Mac only, so the cross-machine half is Not verified. Minor: the export count is off by one. |

### Stop conditions hit

1. **Listener hit in a protected run: group `open`** (C2). The log has one `tcp-accept`, then `tcp-noreq`, with no request line (`C2-m1-open.listener.log`). Isolated with my own page `openiso` (`C2-iso*-*.listener.log`, page source in `qa-pages/openiso/`):
   - `window.open(http://IP)` and `window.open(self)` both return null and produce nothing.
   - The **script-clicked `<a target=_blank href=http://127.0.0.1:39547/…>`** reproduces it alone, twice.

   This is the same IP-literal preconnect as the accepted `alink` leak (#189); the only difference is the `target=_blank` attribute. The vet refuses `target` in templates, but script can add it at run time. Recorded as a stop because the checklist allows hits only in `alink`/`alinkhttps`. Whether the #189 wording covers it is a decision for the Linux session.

No other stop condition was hit:
- No request line outside `alink`/`alinkhttps`, and no `GET /` after boot.
- No DNS lookup of a unique name in any protected run.
- No WebContent process survived.
- Every helper (47 of 47) logged its rule list before its first navigation.
- No save dialog appeared without a real click, and no open panel appeared.

### Boxes Not verified, and why

- **C §7, macOS 14 rerun:** no macOS 14 machine. P1 and this run are both 26.6.2 only.
- **C §7, macOS 13 refusal:** no macOS 13 machine. `PageWindowSupport()` returns `""` on 26.6.2, as expected.
- **C §3, helper exit code 5 with a broken rule list:** the behaviour is Verified (no web view loaded, no window kept, Studio says stopped), but nothing reports the exit code, so code 5 itself was not observed.
- **C §4b, "no rule-list folder left after a normal close":** fails. See finding 2.
- **C §6, "Cmd-Q in the page window ends only the helper":** Cmd-Q does nothing at all. Studio is unaffected, which is the part that matters.
- **E §3, "across machines":** the export was made on this Mac, not on Linux. The import side is Verified.

### Findings (no product decisions made)

1. **The dead proxy port does not refuse at once on macOS 26.6.2** (`C4b-proxyport-unixsocket.txt`).
   - A connect to the helper's bound, non-listening socket (`lsof`: `127.0.0.1:53546 (CLOSED)`, not LISTEN) **hangs about 7.8 s and fails with ETIMEDOUT** (errno 60).
   - `nc -z -G 2` times out after 2.0 s.
   - The control connect to an unbound port is refused in 0.000 s.

   `pagewindow_darwin.m` (the `deadProxyPort` comment) and `docs/reference/artifacts.md:393` ("every connect to the proxy is refused at once") assume an immediate reset. The code's own comment says a hanging connect is the path P1 never measured, the one that "could fail over to a direct connection". **This run measured it and there was no fallback:** `alinkname` and `main` were each held open 125 s with zero listener lines and zero lookups (`C4b-hold120-*`, `C-dns-tcpdump.reduced.txt`). `nw_proxy_config_set_failover_allowed(false)` holds. Host-name loads now hang until timeout instead of failing at once.
2. **`vibe-rules-<pid>` folders are never removed after a Studio-side close.** 71 piled up in `$TMPDIR` during this run (`Z-cleanup.txt`).
   - Every close Studio starts goes through `pageWindow.finish` → `kill()` → SIGKILL of the helper's process group (`pagehost_launch_unix.go:35-39`), so the helper's `atexit(removeRulesDir)` (`pagewindow_darwin.m:328`) never runs.
   - That covers: the Close window button, ⌘W (the helper sends `EV_CLOSED` → `TypeClosed` → `finish` wins the race against `[NSApp terminate:]`), Revoke, a file edit, a workspace switch, quitting Studio, the watchdog, and replacing a window.
   - Only `kill -9` of Studio, where the helper exits on the closed pipe by itself, removed its folder (`C4-kill9-studio.txt`).
3. **A file dropped from Finder exposes metadata to the page** (`D2-03`, `D2-04`). The page gets a `drop` with `files=1`, and `name=qa-drag.txt size=25 type=text/plain lastModified=…` matches the source. **Reading the contents fails** (`NotReadableError`), and the page does not navigate.
4. **Studio's text parameter fields are auto-capitalised and autocorrected by macOS** (`C2-m1-*.meta.txt` first runs). Typing `alink` became `Alink` when focus left the field; `winopen` became `win open`. The notebook-name dialog did the same ("Qa"). On a Mac this changes the parameter values people send. The capitalisation was stopped with an app-domain default for the run, which has since been removed. The spelling correction kept happening even with `NSAutomaticSpellingCorrectionEnabled=0`, so I renamed my test groups instead.
5. **`alinkhttps` sends a TLS ClientHello, and one click gives 4 connections** (loopback and TEST-NET). Each starts `16 03 01 05 e5 01 …`, has ALPN `h2`/`http/1.1`, and has no SNI because the target is an IP. The listener answers with plain HTTP, which may be what triggers the retries. `alink` over plain HTTP gives exactly one accept and `tcp-noreq`.
6. **The leak reaches a non-private address.** With 192.0.2.1 on a lo0 alias, `alink` gave 1 accept and `alinkhttps` gave 4 ClientHellos. `main` gave 0 hits. So the IP-literal leak is not limited to loopback and the LAN.
7. **Minor:**
   - `vibe export bundle --artifacts binpage,caps` printed "3 data artifacts" for 2 (single exports print 1).
   - After a page file is edited and then restored, Studio's open view keeps saying "Changed since you allowed it" until you reopen the artifact. The CLI says `allowed` at once.
   - The helper's cwd is Studio's cwd (here the workspace folder, because Studio was started from it).
   - The broken-rule-list and kill cases show the same "This page stopped responding" text as a real hang.
   - The page window's title bar has a zoom button, not a full-screen button.

### How the run was driven

- I had no Screen Recording permission, and granting it would have meant relaunching the terminal. So every UI observation is an Accessibility-tree dump (`*.ax.txt`).
- Every click and key is a real CGEvent posted at the HID tap, from `bin/axq` (source `bin/axq.swift`; Accessibility granted, `trusted=true` in every line). The B2 harness clicks used `harness/click.swift` as written.
- The C matrix ran in the **production build's** window, not `task studio:dev`: GROUP/TARGET were typed into Studio's controls and Open page clicked. Only the two binding-only checks used `task studio:dev`, through a headless Chrome on the dev server (CDP, `bin/cdp-eval.mjs`), because the Chrome extension wasn't connected.
- The probe's `vibe.run` values aren't logged at info level, so for parts of C2, D and E a temporary stderr line (`patches/qa-temp-pagerun-log.patch`, reverted) printed them as `QA-PAGERUN …` in the Go logs.
- `ask` used `harness/fakellm.py` on 127.0.0.1:18080 through a throwaway `VIBE_CONFIG_FILE` (`qa-vibe.yaml`). The real `~/.vibe.yaml` was not touched (it does not exist on this Mac).
- The resize box (B1) was checked by the owner with the trackpad, because synthetic drags didn't move the frame.

---

## Part A. Lessons applied

- `-framework Network` linked: `otool -L` shows Network.framework (`B0-meta-and-binary.log`).
- No bash-4 features: every script I wrote runs under `/bin/bash` 3.2.
- `/usr/bin/log` was used.
- Accessibility was granted to the terminal.
- Names were unique per run; `alinkname` uses `Date.now()`, and the DNS control used `qa-dnscontrol-<epoch>.spike.test`.

## Part B. The static page host

### 0. Build and meta (`C0-build-studio.log`, `B0-meta-and-binary.log`)

- `task build:studio` built on the first try. The only linker message was `ld: warning: ignoring duplicate libraries: '-lobjc'`.
- The meta is exactly `content="frame-src wails://wails/vibe-view/"` (one CSP meta).
- The bundle is `studio/build/bin/Vibe Studio.app` (the checklist says `vibe-studio.app`, which doesn't exist after the task's rename): Mach-O 64-bit arm64, ad-hoc signed, links Cocoa, WebKit and Network.
- The build changes tracked files (`package.json.md5` and a mode change on three `wailsjs/runtime` files). They were restored after every build.

### 1. The page in the production window (`B1-*.ax.txt`)

- **Verified:** opens as **HTML page / Not allowed yet** with the file list and the source, and nothing is drawn (`B1-01`).
- **Verified:** after Allow the frame shows the page (`B1-02`). The stylesheet applies (16 px body margin and the table's `min-width:320px`), and the SVG logo loads as a 40×40 `AXImage`. The real-route `GET …/assets/style.css` and `…/assets/logo.svg` are in `B2-iso-protected.go.log`.
- **Verified:** the frame holds only the page; no Studio navigation is inside its web area.
- **Verified:**
  - Top N = 5 re-renders as "Top 5 by revenue" with 5 rows, hostile values shown as text (`B1-03`).
  - A real click on the page's EU link re-renders as "Region: EU" (`B1-04`). Studio's REGION control still shows `all`.
- **Verified (owner, trackpad):** the frame resizes by dragging its corner (`resize: vertical`). Synthetic CGEvent drags did not move it (`B1-05`).
- **Verified:** editing `html/assets/style.css` removes the frame within 0.5 s and shows **Changed since you allowed it** with a new hash (`B1-06`).
- **Verified:** Studio works normally under the meta (`B1-07`): a notebook created and `select 42` run, the agent panel (⌘J), the workspace switcher, and the Launcher.

### 2. Isolation with real clicks (harness; `B2-iso-protected.*`, `B2-iso-control-nometa.*`, `patches/qa-p3-harness-hooks.patch`)

Mode `real only=inert,ipc,refresh-outside,refresh-js,refresh-self,refresh-root,click-outside,click-js,click-form-get,click-form-post,click-rerender,click-root,click-index`. One `click.swift` click per `READY_CLICK` (7 clicks, `B2-iso-protected.click.log`).

- **Verified:** the listener log is **empty** for every variant (`B2-iso-protected.listener.log`, 0 lines).
- **Verified:** `click-rerender` re-requests `page?o=…&region=east&v=click-rerender`.
- **Verified:** `click-root` and `click-index` fire a second `FRAME-LOAD`. The only `REQ GET /` is at boot (09:03:48); there is no `GET /index.html`.
- **Verified:** no `IPC` line came from a frame, and there is no `BOOT-IN-FRAME`.
- **Verified:** `REAL state=allowed kind=html page=static` gives `FRAME-LOAD real` with `GET …/assets/style.css` and `GET …/assets/logo.svg`.
- **Verified (control):** the meta was stripped from `dist/index.html` and rebuilt with `wails build -s`. Then the listener got `GET /v/click-outside`, and the Go log has `REQ GET /` (09:05:56) and `REQ GET /index.html` (09:06:02) after boot.

### 3. What the vet covers (`B3-*`)

- **Verified:**
  - `rel=preconnect` and `rel=dns-prefetch` (on a real asset href): `<link> is only allowed with rel="stylesheet" (other values start network connections)`.
  - `target=_blank`: `the target attribute is not allowed (it opens a connection or a window)`.
  - All three list as **Needs fixing**, and opening one shows **This artifact can't be shown yet** with 0 web areas.
- `TestVetPage_URLAttributeAllowList` passes (`B3-vet-gotest.log`).

## Part C. The page window and the network gate

### 0. Build

- **Verified:** the Objective-C compiles with no error or warning (see B0).
- `file` gives Mach-O 64-bit arm64; the helper is the same binary run as `--view-host`.
- `sw_vers` 26.6.2, `uname -m` arm64.

### 1. Probe page and allow screen (`C1-E1-allow-screen-scripted.ax.txt`)

- **Verified:** the scripted page's allow screen says it has script and runs in its own window, and has the line "On macOS a scripted page can open a network connection to an address it chooses … though it cannot read a reply." It also lists `probe … (script only)` and the credential line.

### 2. The 14-group matrix (production window; target `127.0.0.1:39547`; `C2-m1-<group>.{listener.log,page.txt,meta.txt}`; DNS: `C-dns-tcpdump.reduced.txt` plus the listener's own DNS on 5354 via `/etc/resolver/spike.test`)

| group | listener | page side |
|---|---|---|
| main | 0 | every vector rejected or `csp=`-blocked; `rtc` and `md` undefined; getUserMedia rejected; `notification=denied`; clipboard `NotAllowedError`; no `requestFullscreen`; `alert=undefined confirm=false prompt=null`; geolocation denied |
| open | **1 accept + noreq (STOP)** | `window.open=null`, `window.open-self=null` |
| form | 0 | `csp=form-action` for POST and GET, `form=still here` |
| loc | 0 | `loc=still here` |
| alink | 1 accept + `tcp-noreq` | `alink=still here` |
| alinkname | 0 | `alinkname=still here`; no lookup of `alink-name-…`/`alink-sname-…` |
| alinkhttps | 4 accepts, each a TLS ClientHello `16 03 01 …` | `alinkhttps=still here` |
| metarefresh | 0 | `metarefresh=still here` |
| selfnav | 0 | every nav `still here js=undefined` (other?x=1, about:blank, javascript:, blob:, data:, file:) and `samepage=arrived` (`C2-studio-pagerunlog.go.log`) |
| frame | 0 | `csp=frame-src …/assets/frame.html`; **no `posted-from-srcdoc`** in the page-run log |
| title | 0 | `title=Vibe Studio - Sign in`; the native title stays `Probe — Vibe page` |
| storage | 0 | `{"prevLocalStorage":null,"prevCookie":"","idb":"opened"}` |
| charts | 0 | `d3=path length 33`, `vega-lite=svg bytes 7906`, no FAIL |
| freeze | 0 | killed: "This page stopped responding. Its window was closed." |

- **Not verified, stop:** zero everywhere except `alink`/`alinkhttps`. `open` hit, as described above.
- **Verified:** `alink` gives one `tcp-accept`, then `tcp-noreq`, with no request line and exactly one accept per click (also in the control).
- **Recorded:** `alinkhttps` sends a TLS ClientHello (`16 03 01`). There are 4 connections for one click.
- **Verified:** `alinkname` has zero TCP and zero DNS (listener 0 lines; tcpdump has no 5354 packet and no probe name between 09:31 and 09:37).
- **Verified:** `main`, `selfnav`, `frame`, `title`, `storage` and `charts` are as expected (table above).
- **DNS instrument proven:** `dscacheutil` on `qa-dnscontrol-1792680290.spike.test` was logged by the listener's DNS (A, AAAA) and by tcpdump (12 udp/5354 packets at 09:44) (`C2-dns-control.txt`).
- **Control verified** (`patches/qa-control-no-rules-no-proxy.patch`, reverted; `C2-control-*`):
  - `main` gave a DNS lookup of `pf-dns.spike.test` (A and AAAA) and a preconnect accept, 4 lines. P1's control saw 16; this one keeps the CSP.
  - `alinkname` gave a DNS lookup of `alink-sname-1792680416701.spike.test` and 4 TLS connections.
  - `alink` was the same as in the protected run.

### 3. The rule list before the first load

- **Verified:** `/usr/bin/log show` since 08:25 has 47 helper processes, and **47 of 47** logged `rule list added, first load next` before `first navigation started` (`C3-nslog-order.txt`).
- **Verified (behaviour):** with the closing `]` removed from `kBlockRules` (`patches/qa-broken-rule-list.patch`, reverted):
  - No helper remains, and there is not a single `vibe page:` line (no first navigation).
  - Studio shows "This page stopped responding. Its window was closed." (`C3-broken-rule-list.txt`).
  - Exit code 5 was not observable.
- **Verified:** `PageWindowSupport()` returns `""` (`C3-C5-bindings.txt`).

### 4. Process model (`C4-*`)

- **Verified:**
  - The helper's ppid is Studio. Each helper brings its own Networking, GPU and WebContent services (ppid 1).
  - The freeze group's WebContent (58726) was at **99.3% CPU** at t+2 s. It was **gone with the helper by t+5 s**, and Studio said stopped (`C4-freeze-timeline.txt`).
- **Verified:** all of the helper's WebKit services are gone after each of these:
  - Close window and ClosePageWindow (`C4-close-window-button.txt`, `C3-C5-bindings.txt`)
  - ⌘W (`C6-cmdW.txt`)
  - Studio ⌘Q with a window open (`C4-quit-studio-with-window.txt`)
  - `kill -9` of Studio, where the helper exits on the pipe (`C4-kill9-studio.txt`)
  - `kill -9` of the helper (`C4-kill9-helper.txt`)
- **Verified:** `lsappinfo` reports `ApplicationType="UIElement"`, and `NSRunningApplication.activationPolicy=1` (accessory), so there is no second Dock icon (`C2-title-window.ax.txt`).
- **Verified:** the helper's environment is empty (`ps eww`). Its fds are 0 and 1 `/dev/null`, 2, 3 and 4 pipes, a kqueue, the proxy socket, and fd 7, a unix socket whose peer about 50 system processes also connect to (Studio fd 7 included). None of the helper's WebKit services holds a socket to it (`C4-helper-env-fds.txt`). The helper's cwd is `/private/tmp/p4-ws`, inherited from Studio.

### 4b. App Nap, occlusion, cleanup

- **Verified:** the window was minimised (`AXMinimized=1`, not on screen) with Studio in front for over 60 s. There was no watchdog kill, and Studio still said "open in its own window" (`C4b-minimise-60s.txt`).
- **Not verified (fails, finding 2):** rule-list folders are left after a normal close (Close window, ⌘W) and after Studio ⌘Q. They are removed after `kill -9` of Studio. One remains after `kill -9` of the helper, as expected.
- **Partly verified (finding 1):**
  - The port is bound and not `(LISTEN)`, as expected.
  - `nc -z` **is not refused at once**: connects time out after about 7.8 s.
  - With `alinkname` and `main` each held open 125 s: listener 0, tcpdump no lookups, so **no fallback** (`C4b-hold120-*`).

### 5. Opening, replacing, limits

- **Verified:** Open page shows "The page is open in its own window." with a Close window button (`C5-allowed-panel.ax.txt`).
- **Verified:** while open, the panel offers only Close window. A second `OpenArtifactPageWindow('probe', …)` through the binding **replaces** the window: new helper 74241, old 74216 gone, now showing group `storage` (`C3-C5-bindings.txt`).
- **Verified:** with 4 windows open (probe, probe2, probe3, probe4), the 5th (openiso) is refused with "too many page windows are open" (`C5-four-window-limit.txt`).
- **Verified:** a file edit closes the window within 1 s and the page asks again (`C5-edit-closes-window.txt`).
- **Verified:** Revoke closes the window within 1 s (`C5-revoke-closes-window.txt`). The first click only brought Studio forward.
- **Verified:** a workspace switch closes the window (`C5-workspace-switch-closes-window.txt`).
- **Verified:**
  - 300 `vibe.run` calls in 40 ms: 4 ok and 296 "This page is asking for data too quickly.", and the window stays.
  - 1000 calls: the window is killed ("stopped responding") (`C5-flood.txt`, page `qa-pages/flood`).

### 6. Window behaviour

- **Verified:** the page window opens in front with focus (`lsappinfo front` = helper; focused window `Probe — Vibe page`).
- **Verified:** ⌘W closes it, and Studio's panel goes back to Open page (`C6-cmdW.txt`). The helper's menu bar has one app menu with **Close** and **Close All** (`C6-front-contextmenu.txt`).
- **Partly verified:** ⌘Q in the page window **does nothing**. Neither Studio nor the helper quits (`C6-cmdQ.txt`). Cmd-Tab was not watched directly; the helper is `UIElement` / activation policy accessory, which macOS leaves out of Cmd-Tab.
- **Verified:**
  - The title bar reads `Probe — Vibe page`, and the window resized when a page changed size (700×500 and 960×720 manifests).
  - The traffic lights are close, minimise and **zoom** (`AXZoomButton`, no full-screen button).
  - A real right-click opens no context menu.
- **Verified:** the panel says "Vibe never asks you for a password or a key in a page window".

### 7. Oldest supported macOS

- **Not verified:** no macOS 14 or 13 machine.

### 8. TEST-NET 192.0.2.1 (`C8-testnet-listener.log`, `C8-testnet-group-times.txt`)

The alias was added by the owner's sudo script.
- **Verified:** `main` gives zero hits on 192.0.2.1 (09:48:52–09:49:16).
- **Recorded:**
  - `alink` → **1 accept, no bytes** (09:49:23).
  - `alinkhttps` → **4 accepts, each a TLS ClientHello** `16030105e5010005e10303…` (09:49:47).

  The leak reaches a non-private address and goes around the proxy.

## Part D. Capabilities

Fixture `caps` from the zip, plus my own `gesture` page (`qa-pages/gesture`). The gesture page uses raw `postMessage` downloads, which skip the API's own gesture check, so it tests the helper's native NSEvent gate on its own.

### D1. The physical-input gate

- **Verified:** with no input, the API save is rejected ("Saving a file needs a click or a key press first."), and Studio shows no sheet (`D1-01-no-input.txt`). That holds even though `navigator.userActivation.isActive` reads true inside the reply. A raw post with no input gets no sheet either (`D1-06`).
- **Verified:**
  - A real click on Save gives the **native sheet in Studio's window**, "save a file from the page caps", with the name filled as `clicked.csv` (the page asked for `../x/clicked`) (`D1-02`).
  - Cancel resolves `{"saved":false}` and writes nothing (`D1-03`).
  - A click 1 s after a cancel gets no sheet: "This page is asking for data too quickly." (`D1-04`).
  - After 10 s the sheet opens again, and Save writes exactly `63 0a 31 0a` (`D1-05`, the file is `D1-05-saved-clicked.csv`).

  The save landed in the sheet's last-used folder (`~/projects/sandwich-labs/vibe-testing`) and was moved into the results right away. My typed file name went to the page window, not the sheet.
- **Verified:** one click and two `dl.save` calls: the first opens the sheet, and the second is rejected for gesture (`D1-08`). The control, a raw post right after a real click, opens the sheet, so the raw message is well formed (`D1-07`).
- **Verified:**
  - A single `k` press opens the sheet (`D1-11`).
  - Option-Tab focuses a button, and Space 7 s later opens the sheet (`D1-12`).
  - Escape alone: the raw post gets no sheet, and the API save is rejected (`D1-09`).
  - Holding `r` 8 s (140 autorepeat events reached the page as `repeat`): at +6.5 s the raw post gets no sheet, and the API save is rejected (`D1-10`).
- **Verified:** a raw `download` post to `window.webkit.messageHandlers.vibe` without the API gets no dialog (`D1-06`).
- **Verified:** with Studio in front and keys typed into it during the page's 1.5 s raw-post window, no sheet appears (`D1-13`). Studio's input never reaches the helper.

### D2. Open panel

- **Verified:** a real click on an `<input type=file>` `.click()` gives **no open panel** in either process, and the input gets `cancel` at once (`D2-01`).
- **Partly verified (finding 3):** a Finder drag onto the page window delivers `drop` with `files=1` and the file's name, size, type and lastModified. Reading the contents fails (`NotReadableError`), and the page does not navigate (`D2-02` … `D2-04`). The first attempt had no `drop` because of a bug in my page (`{once:true}`).

### D3. state, ask

- **Verified:**
  - `count` 1 → 2 → 3 → 4 across close and reopen.
  - The only state file is `/tmp/p4-ws/.vibe-local/artifacts/caps/state.json` = `{"version":1,"values":{"count":2}}`, and `.gitignore` has `.vibe-local/`.
  - The 70 KB value is rejected with "That result is too large."
  - A reopen replaces the window (C5).

  (`D3-01-state.txt`)
- **Verified:** `ask` reaches the model once per open. The fake model's request has **no `tools`**, `max_tokens: 4000`, the fixed preamble plus the page's system text (`D3-ask-request-shape.txt`).
  - With no usable provider it resolves `null`, and the activity log says `outcome:"no model"`, no request (`D3-04`).
  - Switching the toggle off in Settings > Workspace > Pages closed the open window at once, and the reopened page got `vibe.use('ask')` = null with no request (`D3-03`).
- **Verified:** an `agent activity` line per ask (source, artifact, kind, outcome, byte counts, no text). Settings > Workspace > Pages shows them under **Recent model use by pages** (`D3-02`, `E4-settings-pages.ax.txt`).

## Part E. Trust prompt, agent tool, bundles, CLI

- **Verified:** the prompt on macOS:
  - The `caps` allow screen lists all 4 queries with their connection, marked "(script only)".
  - It describes `ask`/`download`/`state` in plain words; `ask` names "Local server at http://127.0.0.1:18080 (qa-fake)".
  - It has the macOS network line and the credential line (`E1-allow-screen-caps.ax.txt`).
  - The static `revenue` page shows the credential line and **no** network line (`B1-01`).
  - `vibe artifact view status` prints the same lines (`E1-cli-status-caps.log`, `E1-cli-status-probe.log`).
- **Verified:** an agent-written page over MCP (`bin/mcp_write.py`, `E2-*`):
  - Headless, it is refused because "Ask before changes" is on.
  - Through Studio's MCP server (pass-through) the approval card reads "Save data artifact agentpage with an HTML page (it stays inert until you allow it)" with the SQL. After "Do it", the result says nothing renders until allowed.
  - Studio lists it as **Not allowed yet** with 4 files, 0 web areas and no helper.
  - After Allow, the window shows "answer 42" and its script runs.
  - Editing `a.js` ends the window within 1 s, and the page goes back to **Changed since you allowed it**.
- **Partly verified:** bundle round trip on this Mac (`E3-*`).
  - `vibe export bundle --artifacts binpage,caps` holds only the 6 page files, with no `.vibe-local/`, no state and no decision.
  - Launching Studio with the `.vibe` gives the import screen ("Contains no data and no credentials", 2 to import). It created `~/vibe-mac-qa/imported`.
  - All 6 files are identical by SHA-256, including the binary `dot.png`.
  - Both pages arrive **not decided** (binpage was allowed at the source).

  The cross-machine half is not done, and the export count printed 3 for 2.
- **Verified:** Settings > Workspace > Pages lists each page with its kind, its capabilities, Allowed, the ask toggle and Revoke, plus the recent model use (`E4-settings-pages.ax.txt`).
- **Verified:** `docs/reference/artifacts.md:393` covers the macOS leak, and `:395` covers the shared keychain identity (#189) (`E5-docs-grep.txt`). Line 393's "refused at once" conflicts with finding 1.

## Cleanup

- **Temporary patches:** all reverted and kept in `patches/`. They are the P3 harness hooks, the page-run log, the no-rules-no-proxy control and the broken rule list. The no-meta control was a `dist/` edit, rebuilt over. `git status` is clean at d4314d1, and the app on disk was rebuilt clean (`Z-final-clean-build.log`; no `QA-PAGERUN` in the binary).
- **App defaults:** the 5 app-domain defaults I added on `dev.sandwichlabs.vibe-studio` were deleted. The `NSAutomaticQuoteSubstitutionEnabled` that was already there was kept (`00-app-defaults-autocap.txt`, `Z-cleanup.txt`).
- **Leftover files:** 71 leftover `vibe-rules-*` folders were deleted. The saved test file was moved out of `vibe-testing`.
- **QA processes:** all stopped, including the listeners, the fake model, headless Chrome and the dev server.
- **sudo changes:** the owner ran the teardown, and I checked afterwards that `/etc/resolver/spike.test`, the lo0 alias 192.0.2.1 and tcpdump are all gone. `/etc/resolver` and lo0 match the pre-run state (`00-sudo-prestate.txt`, `00-sudo-teardown.txt`). The full tcpdump capture held this Mac's ordinary DNS traffic and was deleted. Only `C-dns-tcpdump.reduced.txt` is kept. Note that tcpdump shows no names for udp/5354, so the listener's own `dns query` lines are the name-level witness on that port.
- **Left on disk:**
  - Test workspaces: `/tmp/p4-ws`, `~/vibe-mac-qa/{ws-b,imported,bundle,dragsrc,qa-vibe*.yaml}`.
  - Studio's recent list now includes p4-ws, ws-b and imported, and the `caps` ask toggle and page decisions live in the user config dir.
