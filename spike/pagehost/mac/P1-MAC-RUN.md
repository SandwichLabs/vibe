# P1 on macOS: manual steps for the owner

Same checks as the Linux run (`SPIKE-P1-RESULTS.md`, in the scratchpad), on a Mac. **None of the macOS code here (`pagehost_mac.m`, `parent_mac.py`, `listener_mac.py`) has been compiled or run**; expect a compile error or two. Everything stays on loopback; never look up a real host. A vector is "blocked" only if **no request reached the listener and nothing appeared in a Go/log line**; a console error never counts. Record every result as Verified / Not verified with the log lines, plus macOS version and chip (arm64/Intel).

## 0. Setup

Prerequisites: macOS 14+ (the proxy test needs it; 13 works for the rest), Xcode command line tools (`xcode-select --install`: `clang`; `sandbox-exec` ships with the OS), Go 1.27, Node, Wails CLI **v2.16.0**, Python 3 (the system 3.9 is fine), optional `brew install cliclick`.

```
git fetch origin && git checkout spike/html-pages-p1
cd studio/frontend && npm ci && cd ../..
cd spike/pagehost/mac && clang -fobjc-arc -Wall -o pagehost_mac pagehost_mac.m -framework Cocoa -framework WebKit
```
All commands below run from `spike/pagehost/mac/` unless noted. `studio/frontend/index.html` on the branch already carries the a1 meta; `index.none.html`, `index.a1.html`, `index.narrow.html` here are the variants to copy over it.

**Local DNS test zone (needs sudo; loopback only).** `*.localhost` resolves without DNS, so it proves nothing. Instead:
```
sudo mkdir -p /etc/resolver
printf 'nameserver 127.0.0.1\nport 5354\n' | sudo tee /etc/resolver/spike.test
scutil --dns | grep -A4 spike.test        # should list the scoped resolver
```
Every `*.spike.test` lookup then goes to a UDP logger on 127.0.0.1:5354 (built into `parent_mac.py` and `listener_mac.py`, which log `dns query name=… qtype=…` and answer 127.0.0.1). **Remove afterwards:** `sudo rm /etc/resolver/spike.test`. Unscoped names never go there, so nothing real is ever queried. Sanity check: `dscacheutil -q host -a name probe.spike.test` must log one query.

---

## Part B. Scripted host (do this first: it decides whether it exists on macOS)

`parent_mac.py` is the Studio stand-in: loopback listener (TCP+UDP), DNS logger on 5354, a socketpair passed as **fd 3** (fixed: `pass_fds=(cefd, 3)`, because `preexec_fn` runs before `close_fds`), **empty environment**, optional `sandbox-exec -p '(version 1)(allow default)(deny network*)'`, watchdog, `ps eww`/`lsof` snapshots, fake rows for `vibe.run`. Logs: `logs/<name>.{listener,parent,helper,proc}.log`. Flags: `--csp strict|permissive|none`, `--via header|meta`, `--group <g>`, `--ttl <s>`, `--no-policy`, `--sandbox-net`, `--proxy`, `--watchdog`. Check first that the helper really got the pipe: the parent log must show `PAGE {"v":"env",…}` lines (no lines = fd 3 problem).

### B1. The decisive test: does `sandbox-exec` on the helper gate the network?

On macOS WKWebView networks in `com.apple.WebKit.Networking`, an XPC service that **launchd** starts for the app, not a child of the helper, so the profile almost certainly does **not** constrain it. Run these two side by side (permissive CSP and no policy so every vector can fire; the control proves the vectors fire):
```
./parent_mac.py --name B1.ctl  --csp permissive --no-policy --group main --ttl 9
./parent_mac.py --name B1.sbx  --csp permissive --no-policy --sandbox-net --group main --ttl 9
```
Read `logs/B1.sbx.listener.log`: any `HIT http … /v/…` (fetch, xhr, img, …), any `dns query` or `udp` line means the profile is **not** a gate. `logs/B1.sbx.proc.log` shows the `com.apple.WebKit.*` processes and their parent (expect launchd, not the helper). If `B1.sbx` is silent, repeat all of B2 with `--sandbox-net`; if it leaks, go to B1b. Also record whether `B1.ctl` hit at all; if not, the instrument is broken, stop.

### B1b. Alternative gate: `WKWebsiteDataStore.proxyConfigurations` (public, macOS 14+)

The helper (`-proxy 1`) routes the data store through an HTTP CONNECT proxy at a dead loopback port (127.0.0.1:9), so requests fail at the proxy and **proxied host names are not resolved locally**. Test on and off, permissive CSP, no policy, so the only gate is the proxy:
```
./parent_mac.py --name B1b.off --csp permissive --no-policy --group main --ttl 9
./parent_mac.py --name B1b.on  --csp permissive --no-policy --proxy --group main --ttl 9
./parent_mac.py --name B1b.on.alinkname --csp permissive --no-policy --proxy --group alinkname --ttl 7   # click by name
```
Expected for a gate: `B1b.on` has no `HIT http`, no `dns query` (the page's `fetch-name`, `img-name`, `pc-name`, `alink-name` host names are under `spike.test`), while `B1b.off` shows both (this proves the DNS logger works). The parent log must contain `H proxyConfigurations set -> 127.0.0.1:9`; if it says unavailable, you are below macOS 14. **UDP bypasses proxies**, so WebRTC must be off for this to be a gate: the page logs `typeof RTCPeerConnection` (`rtc` in the `env` line) and, when present, creates a connection with `stun:127.0.0.1:<port>`; the listener's UDP side must stay silent (`udp from …` / `STUN`). The helper tries the private preference keys `peerConnectionEnabled`/`mediaDevicesEnabled` and logs `H spi … ok|unavailable`; run `B1b.on` once without the SPI line working (comment the two `@try` lines) to see whether it matters. Write down what `rtc` was in each run. Also check mDNS/Bonjour is not a bypass (`.local` names).

### B2. The three layers on their own (as on Linux)

| Layer | Flags | Expect |
|---|---|---|
| Control | `--csp permissive --no-policy` | all of `main` hits; nav groups hit |
| CSP only | `--csp strict --no-policy` (repeat with `--via meta`) | fetch/XHR/WS/ES/beacon/img/CSS/ping/form: no hit. **Navigation groups still hit.** note `tcp-noreq` (preconnect) |
| + navigation policy | `--csp strict` | nav groups: no hit; `tcp-noreq`/`dns query` may remain (on Linux a refused navigation still connected and resolved) |
| Gate only | `--csp permissive --no-policy` + `--sandbox-net` or `--proxy` (whichever B1/B1b showed works) | no hit, no DNS, no STUN |
| All | `--csp strict` + that gate | zero |

Loop over groups: `for g in main open form formget loc locreplace alink alinkname metarefresh selfnav frame title storage storage charts; do ./parent_mac.py --name $CFG.$g --group $g --ttl 8 <flags>; done`. Group `alinkname` (click an outside link by `*.spike.test` name) is the DNS-through-refused-navigation case.

### B3. Other checks

| Check | How |
|---|---|
| Handler refuses a frame | `--group frame --csp permissive --no-policy`: on Linux both frames posted. Expect `H message-from-subframe REFUSED` here and no `frame-post`; then `--csp strict` (blocked by `frame-src 'none'`). |
| `while(true){}` | `--group freeze --watchdog --ttl 20`: watchdog logs the missed heartbeat and SIGKILLs the helper; read `after kill, WebKit services still running:`. On Linux a busy web process survived the helper's death; report any surviving `com.apple.WebKit.WebContent`. |
| Env and fds | `logs/<name>.proc.log`: only what the parent passed, fds 0-2 and 3 for the helper. Check `lsof` of the XPC services does not contain the parent's socket. |
| Web-process sandbox | Apple's `com.apple.WebProcess.sb` applies to the XPC service; note `codesign -d --entitlements :-` of `com.apple.WebKit.WebContent.xpc`. |
| Dock/focus | no second Dock icon (accessory policy), window in front of Studio, Cmd-W closes only it. |
| `document.title` | `--group title`: `H title doc.title=… window.title=Spike page host` once a second; window title must not change. |
| Ephemeral data | `--group storage` twice: second run shows `prevLocalStorage: null`, `prevCookie: ""`, IndexedDB `opened v1`. |
| d3 + vega-lite | `--group charts --csp strict`: `d3 rendered path length 33`, `vega-lite rendered svg bytes 7902`, no `js-error`. |
| Keychain identity | If the sandbox result is weak, note whether a **separate helper executable** with its own identifier is needed (same binary = same keychain ACL). |

---

## Part A. Static host, in the Wails window

1. In a second terminal: `python3 listener_mac.py 39547 /tmp/A.listener.log` (TCP, UDP and DNS on 5354 for `*.spike.test`).
2. For each run: copy the index variant over `studio/frontend/index.html` (from the repo root), `rm -f /tmp/A.spike.log`, then in `studio/`:
   `SPIKE_MODE='<mode>' SPIKE_LOG=/tmp/A.spike.log wails dev -tags duckdb_arrow`
   (no `webkit2_41` on macOS). The runner starts 2.5 s after load and logs `BEGIN/READY_CLICK/END <variant>`, finishing with `ALLDONE`; quit Studio then.
   - **Origin:** the first line is `BOOT top origin=<origin>` (Linux dev: `wails://wails.localhost:34115`). Put the macOS value in `index.narrow.html` (`frame-src <origin>/vibe-view/`).
   - **Clicks:** `click-*` variants wait 3.5 s after `READY_CLICK`; click once anywhere in the window (the frame covers it). Automate with `cliclick c:<x>,<y>` on the window centre if you like.
3. Variants (`all-variants.txt` is the Linux list; `ipc` is separate). **Added for macOS:** `lnk-dns-prefetch-name` (Cocoa may honour `dns-prefetch`; watch the DNS logger), `lnk-preconnect-name`, `click-outside-name` (outside-link click under a1 with a `spike.test` name: record whether a **`dns query`** appears even though the navigation is blocked), `click-blank-name`.

   | Run | index | `SPIKE_MODE` | Purpose |
   |---|---|---|---|
   | ctl | none | `run-control-only=<all>` | no sandbox, no CSP: vectors must fire |
   | nox | none | `run-only=<all>` | H3 CSP + `sandbox=""`, no a1: expect `/v/click-outside` to leak |
   | a1 | a1 | `run-only=<all>,lnk-dns-prefetch-name,lnk-preconnect-name,click-outside-name,click-blank-name` | the real configuration |
   | nar | narrow (edited) | same list | narrowed meta; Studio must still boot (`app-children=1`); a click to `/` loads nothing |
   | sbonly | none | `run-sbonly-only=ipc,inert` | **macOS-only bullet**: Wails' `forMainFrameOnly:false` user script must not run in a frame without `allow-scripts`: no `IPC SpikeProbe frame-raw-ipc`, no `/v/ipc-script-ran`, no `BOOT-IN-FRAME` |
   | ipc ctl | none | `run-control-only=ipc` | detector works: `frame-raw-ipc` appears (or "Blocked request from not main frame" in the dev log, as #182) |
   | fetch | a1 | `run-only=none` | `FETCH` lines: CSP header, `wails-runtime-injected=false`, `page/` and `index.html` 400 |

4. Read per bullet: inert vectors (no `/v/` hit between `BEGIN`/`END inert`); outside-link click; narrowing; re-render (`REQ GET …region=east…`); forms (`/v/form-get`, `/v/form-post` absent); `assets/` served and `assets/ok.js` not requested; no `…page?v=img-target`; whether WKWebView sends `Sec-Fetch-Dest` (the `REQ` lines print `dest=`; on Linux it was empty); frame to `/` and `/index.html` (`REQ GET`, but no `IPC`/`BOOT-IN-FRAME`; add `SPIKE_LOGALL=1` once to see every request); and whether `lnk-preconnect`/`click-blank` make a `tcp-noreq` in `/tmp/A.listener.log` (they do on Linux and defeat CSP, sandbox and a1).

## Clean up

`sudo rm /etc/resolver/spike.test`; stop `listener_mac.py`; `git checkout studio/frontend/index.html` if you changed it.

## Hand back

Log excerpts per row, OS version and chip, the B1/B1b answers, and a recommendation per host. Linux recommends: static host GO (a1, narrowed, template vet bans `<link rel=preconnect>`/`target=`); scripted host GO only behind a real network gate. On macOS the gate is the open question.
