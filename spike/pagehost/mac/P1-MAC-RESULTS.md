# P1 on macOS: results

## Summary for review

- **Static host (Wails window, a1 + narrowed meta): GO.** The template vet must ban `<link rel=preconnect>`, `<link rel=dns-prefetch>` and `target=`. On macOS, with an `https://` name, each of them sends the name out in DNS, and preconnect and `target=_blank` also send it in TLS SNI. Everything else is blocked. Content rule lists aren't available here, because Wails v2 doesn't expose its WKWebView configuration.
- **Scripted host (separate WKWebView helper): no single layer is enough; five layers together gave zero leaks in all 14 groups.**

  | Layer | Closes | Leaves open |
  |---|---|---|
  | Strict CSP + navigation policy | subresources, forms, navigations | dns-prefetch, preconnect, DNS + TCP on refused navigations |
  | `WKContentRuleList` (block all except `vibe-page:`) | **every load, plus dns-prefetch and preconnect** | DNS/TCP on a script-clicked outside link |
  | Data-store proxy (dead loopback port) | name resolution | IP literals (loopback and LAN verified; public not tested) |
  | Process sandbox denying network | all TCP/UDP from WebKit | DNS |

- **Decision (owner, 2026-10-21): don't ship `sandbox-exec`**, because it is deprecated. Something else has to close the last gap, a script-clicked link to an IP literal, which rules + proxy leave as an empty TCP connection to an address and port the page chose. Options, none tested yet:
  1. **App Sandbox on a separate helper executable** without `com.apple.security.network.client`. This is the supported mechanism, and a separate executable also gets its own keychain identity. Needs testing: does it gate WebKit's Networking process the way `sandbox-exec` did, and does an ad-hoc signature carry the entitlement on the unsigned v0.1.0 pipeline?
  2. **A host-injected document-start capture listener** that refuses clicks on outside links. Enforced in the page, so possibly bypassable.
  3. **Proxy only.** Requires public IP literals to be proxied (not tested), and a page could still reach the LAN.
- **Other macOS facts**:
  - A busy WebContent process dies with the helper (on Linux it survived).
  - The XPC services don't inherit the parent's socket.
  - WebRTC is removable (`RTCPeerConnection` undefined).
  - WKWebView never sends `Sec-Fetch-Dest`.
  - Studio's ATS (`NSAllowsLocalNetworking` only) refuses plain-HTTP names, so name tests must use HTTPS.

Run 2026-10-20 on **macOS 26.6.2 (25G83), Apple Silicon (arm64)**, Go 1.27.2, Wails v2.16.0, system Python 3. Branch `spike/html-pages-p1` at 087cc5f. Logs: `logs-mac.tgz` beside this file, which unpacks to `logs/` (scripted host, `<name>.{listener,parent,helper,proc}.log`) and `logs/A/` (static host, `<run>.{spike,listener,dev}.log`). Everything stayed on loopback, except the LAN-IP proxy check, which used this Mac's own address. `/etc/resolver/spike.test` was installed for the run and removed afterwards.

"Verified" below means the listener and the Go/helper log agree. "No hit" means no TCP accept, no request line, no UDP and no `dns query` in the listener log.

## Deviations from P1-MAC-RUN.md

- **Build:** the link needs `-framework Network` (`nw_endpoint_create_host`, `nw_proxy_config_create_http_connect`). Otherwise `pagehost_mac.m` compiled clean with `-Wall`, and `parent_mac.py` worked as written: fd 3 arrived and the `PAGE {"v":"env",…}` lines appeared.
- **Missing runner variants:** `spikeRunner.ts` never ran the macOS name variants (`lnk-dns-prefetch-name`, `lnk-preconnect-name`, `click-outside-name`, `click-blank-name`) because they weren't in its variant list. I added them, plus HTTPS versions (`*-sname`, `inert-sname`). The patch is applied on this branch (`studio/frontend/src/spikeRunner.ts`, `studio/spike_static.go`).
- **ATS:** under App Transport Security, `http://*.spike.test` gets no DNS lookup and no connection in Studio, even in the control. The dev `Info.plist` sets only `NSAllowsLocalNetworking`; IP literals and `localhost` are exempt. **Name tests in Part A therefore have to use `https://`**, which ATS allows. The HTTPS variants are the ones that count.
- **DNS cache:** names repeated across runs are sometimes answered from the resolver cache, so a run can show no DNS lookup even though one would happen. Every DNS-absence claim below was rerun with a unique suffix per run (`U.*`, `D.*`, `D2.*`). `_dns.resolver.arpa` lookups are the system's resolver discovery and are ignored.
- **Clicks:** a small CGEvent helper (no cliclick). It only worked after Ghostty was given the Accessibility permission. Before that, the clicks were silently dropped (the first `ctl` attempt).

## Part B: scripted host

### B1: does `sandbox-exec (deny network*)` gate WKWebView? **Partly. It blocks TCP and UDP but not DNS.**

- **Verified:** the `com.apple.WebKit.{Networking,WebContent,GPU}` services run under launchd (ppid 1), not under the helper (`B1.sbx.proc.log`). The prediction was that the profile wouldn't apply to them. Instead, **every HTTP, WebSocket, beacon, preconnect and navigation was blocked**: `B1.sbx`, `B2.sbx.*` and `B2.allsbx.*` show 0 `HIT http` and 0 `tcp-noreq`, while `B1.ctl` shows 16 HTTP requests. The page reports `fetch rejected TypeError: Load failed`.
- **Verified:** **DNS still leaks.** `B1.sbx2` and `B2.sbx.main` show `dns query name=fetch-name|img-name|pc-name|pf-dns.spike.test`. `U.allsbx.alinkname` shows the name of a link click being resolved even with strict CSP and the navigation policy.
- The only sandbox report in the system log is for the helper itself: `Sandbox: pagehost_mac(23399) deny(1) network-outbound /private/var/run/mDNSResponder`. Lookups happen outside the helper, apparently in the Networking process. Nothing in the log shows how the TCP block works. One guess is that the Networking process checks the helper's sandbox through its audit token, but that is **not verified**.
- `B1.ctl` hit every vector, so the test setup works.

### B1b: `WKWebsiteDataStore.proxyConfigurations`. **Names are gated; IP literals and dns-prefetch are not.**

The proxy was set (`H proxyConfigurations set`). A second run pointed it at a live logging proxy (`followup/proxylog.py`) instead of the dead port 9:

- **Verified:** host names go to the proxy and are **not resolved locally**. The proxy received `CONNECT fetch-name.spike.test:…`, `img-name`, `pc-name` and `alink-name`; the DNS logger saw nothing for them (`PX.lo.main`, `PX.lo.alinkname`).
- **Verified: IP literals go around the proxy**, for loopback and for this Mac's LAN address (192.168.13.158). `B1b.on`, `LAN.on` and `PX.lan.main` have the full 14 HTTP hits, and the proxy received none. Whether a **public** IP literal also goes around it is **not verified**, because that test would leave loopback. If the exemption is "private/local only", a public IP would be gated. That is the next test (a `lo0` alias for a TEST-NET address would keep it local).
- **Verified: `<link rel=dns-prefetch>` resolves locally with the proxy on.** `pf-dns` appears in `PX.lo.main`, `PX.rep1`, `PX.rep2`, `U.allprx.main` and `U.allboth.main` (unique names).
  - The private `_setDNSPrefetchingEnabled:NO` call is accepted (`H spi _setDNSPrefetchingEnabled:NO ok`) but has no effect (`D2.*`).
  - `X-DNS-Prefetch-Control: off` on the page response has no effect either (`D.hdr`).
  - The KVC key `dnsPrefetchingEnabled` doesn't exist.
- **WebRTC:** `rtc` was `undefined` in every run, so the private settings `peerConnectionEnabled`/`mediaDevicesEnabled` (logged `ok`) remove `RTCPeerConnection`. No UDP reached the listener in any run. I didn't run the variant with that setting removed. mDNS (`.local`) is not tested.

### B2: the layers (`B2.<cfg>.<group>`, 14 groups each)

| Config | HTTP to listener | tcp-noreq | DNS |
|---|---|---|---|
| ctl (permissive, no policy) | all of `main` (16) + every nav group | yes | all names |
| csp strict, header | 0 in `main`; **navigation groups hit**: window-open, location, locreplace, alink, alinkname, metarefresh | 2 (preconnect) | pc-name, pf-dns, alink-name |
| csp strict, meta | identical to header | 2 | same |
| + navigation policy (`pol`) | **0 everywhere** | preconnect; refused `alink` still connects | **pc-name, pf-dns, alink-name still resolve** (`U.pol.*`) |
| sandbox only (`sbx`) | 0 | 0 | all names |
| proxy only (`prx`) | as ctl, apart from the named targets | yes | pf-dns only |
| strict + policy + sandbox (`allsbx`) | 0 | 0 | pc-name, pf-dns, alink-name |
| strict + policy + proxy (`allprx`) | 0 | preconnect to IP | pf-dns |
| strict + policy + sandbox + proxy (`allboth`) | 0 | 0 | **pf-dns only** (dns-prefetch) |

Forms (`form`, `formget`) are blocked by CSP `form-action 'none'`. A refused navigation with no gate still opens a TCP connection and resolves the name, as on Linux.

### B3: other checks

| Check | Result |
|---|---|
| Handler refuses a frame | **Verified.** With permissive CSP and no policy, both frames posted and both were refused (`H message-from-subframe REFUSED` ×2, no `frame-post`). With strict CSP, `frame-src` blocks `frame.html` (`csp-violation frame-src`). |
| `while(true){}` | **Verified.** The watchdog missed the heartbeat after 2.09 s and SIGKILLed the helper (rc=-9). **No `com.apple.WebKit.WebContent` survived**; the WebKit services left running are 40–44 days old and belong to another app. This differs from Linux, where a busy web process outlived the helper. |
| Env and fds | **Verified.** `ps eww` shows an empty environment. The helper has fds 0–2, fd 3 (the socketpair) and fd 4, a unix socket AppKit opens itself. The GPU, Networking and WebContent services hold **no unix sockets** (`lsof`), so the parent's socket stays in the helper. |
| Web-process sandbox | WebContent is signed with `com.apple.developer.hardened-process` and `web-browser-engine.restrict.notifyd` (among others). WebKit ships `com.apple.WebProcess.sb` and `com.apple.WebKit.NetworkProcess.sb`. The log shows its own denials, for example `WebContent deny iokit-get-properties`. |
| Dock/focus | **Verified.** `lsappinfo` reports `ApplicationType=UIElement` (no Dock icon), and the helper is frontmost (`in front`). **Cmd-W not tested.** As an accessory app with no menu bar, Cmd-W probably does nothing. |
| `document.title` | **Verified.** `H title doc.title=Vibe Studio - Sign in window.title=Spike page host`; the native title never changes. |
| Ephemeral data | **Verified.** `prevLocalStorage: null, prevCookie: "", idb: "opened v1"` on every run. |
| d3 + vega-lite (strict CSP) | **Verified.** `d3 rendered path length 33` and `vega-lite rendered svg bytes 7906 rects 13`, with no `js-error` (Linux: 7902 bytes). |
| Keychain identity | The sandbox result means a shippable network gate would be App Sandbox on a **separate helper executable** with no `network.client`, not `sandbox-exec`, which is deprecated. A separate executable also gets its own keychain identity. |

## Part A: static host in the Wails window

The origin is `wails://wails.localhost:34115`, the same as Linux, so `index.narrow.html` needed no edit. Every run ended with `ALLDONE app-children=1`.

| Bullet | Result |
|---|---|
| ctl | Every vector fires: inline and external script, CSS, fonts, `img`, `object`/`embed`, `srcdoc`, `iframe`, `poster`/`audio`, `refresh-outside`, and `click-outside`/`js`/`form-get`/`form-post`/`ping`. |
| nox (CSP + `sandbox=""`, no a1) | `/v/click-outside` **leaks**, as expected. For `https://` names, `click-outside-sname` leaks DNS plus a TLS ClientHello. |
| a1: inert vectors | **No `/v/` hit** between `BEGIN` and `END inert`. The only listener activity is a `tcp-noreq` from the page's own `<link rel=preconnect>`. `inert-sname` (CSS and `img` by HTTPS name) gets no DNS lookup and no connection. |
| a1: outside link click | **Blocked**, for the IP literal and for an HTTPS name (`click-outside-sname`: no DNS lookup, no connection). |
| a1/narrow: `preconnect`, `dns-prefetch`, `target=_blank` | **They leak, and further than on Linux.** `lnk-dns-prefetch-sname` resolves the name. `lnk-preconnect-sname` and `click-blank-sname` resolve it **and send a TLS ClientHello** (host name in SNI) to the listener. With an IP literal they produce `tcp-noreq`. CSP, `sandbox` and a1 don't stop any of the three. |
| Narrowing | A click to `/` loads nothing (no `REQ GET /` after the click), and Studio still boots. |
| a1: frame to `/` and `/index.html` | Only the HTML is fetched (`REQ GET /`, `REQ GET /index.html`). No subresources, no `BOOT-IN-FRAME`, no IPC. |
| Re-render | `REQ GET …page?…region=east&v=click-rerender` |
| Forms | No `/v/form-get` or `/v/form-post` under a1. |
| Assets | `assets/ok.css`, `ok.png` and `ok.ttf` are served. **`assets/ok.js` is never requested.** No `page?v=img-target` request. (`ok.ttf` reads a Linux font path, so it's empty on macOS; that doesn't matter here.) |
| `Sec-Fetch-Dest` | **Never sent** by WKWebView (`dest=""` on all 81 `REQ` lines), the same as Linux. |
| Meta refresh | `refresh-self` and `refresh-root` aren't followed in the sandboxed frame. |
| sbonly (Wails `forMainFrameOnly:false` script in a frame without `allow-scripts`) | **Verified that it doesn't run.** No `frame-raw-ipc`, no `/v/ipc-script-ran`, no `BOOT-IN-FRAME`. |
| ipc ctl | The detector works: `/v/ipc-script-ran` hit, and the dev log shows `Blocked request from not main frame` ×2. No `frame-raw-ipc` reached Go. |
| fetch | `page` is 200 with the H3 CSP, `nosniff` and `no-store`. `wails-runtime-injected=false`. `page/` and `index.html` return **400** with `default-src 'none'`. `nope` is 404. |

## Follow-up (2026-10-21): `WKContentRuleList` against dns-prefetch

The helper is `followup/pagehost_rules.m` (env `SPIKE_RULES`) and the parent is `followup/parent_rules.py`. The rule list is compiled into a throwaway `WKContentRuleListStore storeWithURL:` and added to the `WKUserContentController` before the first load:

```json
[{"trigger":{"url-filter":".*"},"action":{"type":"block"}},
 {"trigger":{"url-filter":"^vibe-page:"},"action":{"type":"ignore-previous-rules"}}]
```

Every run used a unique DNS suffix (`R.*`, `RP.*`, `RS.*`, `RA.*`, `RALL.*`).

- **Verified: the rule list stops `dns-prefetch`.** It also stops `preconnect` and every other load, even with permissive CSP and no navigation policy. In `R.ctl`/`R.ctl2` the listener saw 0 accepts and 0 DNS lookups, against 16 HTTP requests and all four names in `R.base`, the same build without rules. Under strict CSP + policy (`R.pol`), and with the proxy and sandbox added (`R.allboth`, `R.allboth2`), the result was also zero.
- **The page still works under the rules.** `vibe.run` returns rows, `fetch('/assets/x.json')` is 200, `/libs/*` load, d3 renders `path length 33`, and vega-lite renders `svg bytes 7906 rects 13` (`R.charts`). Every outside `fetch`/XHR/WS/ES fails, and `sendBeacon` returns `false`.
- **One leak is left: clicking a link to an outside URL.** `a.click()` here is called from script, so a page can trigger it without any user action. WebKit preconnects on the click before the rules or the navigation policy apply:
  - **Rules alone, or rules + policy:** an outside *name* is resolved and a TCP connection opens with no request (`R.ctl.alinkname`, `R.pol.alinkname`, `R.pol.alinkname2`: `dns alink-name ×3`, `tcp-noreq`).
  - **Rules + proxy:** names are closed (`RP.alinkname`, `RP.alinkname2`: nothing). An *IP-literal* link still opens an empty TCP connection to the IP and port the page chose (`RP.alink`, `RP.alink2`: accept=1, noreq=1).
  - **Rules + sandbox:** IP connections are closed (`RS.alink`: nothing), but the name is still resolved (`RS.alinkname`: `dns alink-name ×3`).
  - **Rules + proxy + sandbox: zero.**
- **Verified: rules + proxy + `sandbox-exec` + strict CSP + navigation policy gives zero in all 14 groups** (`RALL.*`): no TCP accept, no HTTP, no UDP, no DNS. frame, title, storage and charts behave as before.

The three network layers are complementary:

| Layer | Closes |
|---|---|
| Rule list | every load, plus dns-prefetch and preconnect |
| Proxy | name resolution on link-click preconnect |
| Sandbox | TCP to IP literals on link-click preconnect |

As an alternative to the sandbox for the IP-literal click case, a host-injected document-start capture listener that refuses clicks on outside links could work, but I haven't tested it. The sandbox is the one that is verified here.

## Recommendation (updated after the follow-up)

**Scripted host on macOS: GO behind all of these, together:**
1. strict CSP;
2. the navigation policy;
3. a `WKContentRuleList` that blocks everything except `vibe-page:`;
4. `proxyConfigurations` pointed at a dead loopback proxy;
5. a process sandbox that denies network.

With all of them, nothing reached the listener in any group. Before shipping, re-verify three things:
- The same matrix under **App Sandbox** on a separate helper executable (no `com.apple.security.network.client`), because `sandbox-exec` is deprecated. Why the sandbox blocks the WebKit services' connections at all is not verified, so App Sandbox has to be checked on its own terms.
- A public IP literal through the proxy, using a TEST-NET `lo0` alias.
- That a rule list compiled at startup is in place before the first page load. The spike waits for the compile callback before loading, and the product has to do the same.

The original recommendation follows.

## Recommendation (original)

- **Static host: GO**, the same as Linux: a1 with the narrowed meta. The template vet that bans `<link rel=preconnect>` and `target=` is **required**, not optional: on macOS, with an HTTPS name, each sends the name out in DNS and in TLS SNI. **Add `rel=dns-prefetch` to the ban.** On macOS, it is safest to ban every `<link rel>` except `stylesheet` and `icon` with asset URLs. Note that ATS (`NSAllowsLocalNetworking` only) already blocks plain-HTTP names in Studio. That helps, but it isn't a control to rely on.
- **Scripted host on macOS: NO-GO as a "no network" promise** with the gates tested:
  - `sandbox-exec` blocks every connection but not DNS.
  - The proxy stops name lookups but not IP literals (private ranges verified; public not tested).
  - Strict CSP, the policy, the sandbox and the proxy together leave **DNS via `<link rel=dns-prefetch>`**, a label-encoded exfiltration channel that no setting or header turned off.

  It becomes a **conditional GO** if that residual is accepted or closed. Next tests, in order:
  1. A `WKContentRuleList` that blocks everything except `vibe-page:`. Does it stop `dns-prefetch`?
  2. A host-injected document-start script that removes or neuters `rel=dns-prefetch` (`setAttribute`, `innerHTML`, parser-inserted). This can probably be worked around; content rules are the better bet.
  3. A public IP literal through the proxy, using a TEST-NET `lo0` alias.
  4. The same matrix under App Sandbox on a separate helper executable, instead of `sandbox-exec`.
