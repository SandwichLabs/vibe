# HTML artifacts: Claude-style pages shared in a workspace

Status: **built, steps P1-P6 (2026-10-22); the network-free macOS host (#189) is decided in spike #248; spike P1 done on Linux (2026-10-18) and macOS (2026-10-21). The macOS page window and capabilities ran for the first time in the owner's macOS QA (2026-10-22, macOS 26.6.2, M5, at d4314d1) and held; its follow-up fixes are in *Owner QA on macOS* below. macOS 14 and 13 and a cross-machine bundle are not yet run.** See each step's *As built*. See *Spike P1* at the end. The static host is GO on both platforms, with conditions. The scripted host is GO on Linux behind a real network gate. On macOS it is GO behind four filtering layers, with **one accepted leak**: a script-clicked link to an IP address opens an empty TCP connection (owner decision 2026-10-21; the network-free replacement is tracked in #189). The conditions are written into H2-H4 below. This extends epic #162 (`docs/plans/data-artifacts.md`, called *the base plan* below). Level 1 shipped on 2026-10-17. The base plan's spike finished on macOS (#182): the iframe is dropped, the worker view (`view.js`) is the RESHAPE, and a1 (the meta `frame-src 'self'` on Studio's page) blocks every navigation vector on both platforms. This plan adds a third view kind beside A2UI and the worker view: an **HTML artifact**, a page of the author's own HTML (optionally with script) that works like a Claude artifact. It keeps the base plan's query boundary: a page asks for a named query with values and never sends SQL. Before any step is built, two things are needed: the owner's sign-off, and a spike of its own (step P1).

## Goal

A Claude artifact is a page someone wrote (or Claude wrote). It runs on its own origin, and it can declare a small set of **capabilities**, which the viewer grants when they first use them: a database, calling the model, connectors, saving a file, and a few more (grounding). In Slaw the same idea means the following:

1. **A page, shared by git.** The page lives at `artifacts/<name>/` beside `artifact.yaml`: an HTML template and optional local CSS, JS, fonts and images. A teammate pulls it and opens it. It runs live on *their* connections with *their* credentials. The folder holds no data and no answers (the base plan's rule).
2. **Data binding in Go, by name.** The page's HTML is a Go `html/template`. Its only data source is `{{ query "by_region" }}`: the manifest's named queries, run through `Handler.RunArtifactQuery` with values bound by `artifacts.Bind`. The template cannot name a connection, write SQL or read a file. Script, when a page has script, uses the same call: `slaw.run("by_region", {from: "2026-01-01"})`.
3. **Two hosts, chosen by whether the page has script.**
   - A **static page** (no script) renders *inside* Studio, in a script-less sandboxed frame. Links re-render it in Go with new parameter values. This is classic server-rendered HTML, built on the base plan's `/slaw-view/` middleware and its meta CSP on Studio's page.
   - A **scripted page** opens in its own **page window**. That window is a native web view Slaw owns, running in a separate helper process with no Wails bridge, no network it can use and no credentials (on macOS, with one accepted network leak and a shared keychain identity, H4). Capabilities are available only there.
4. **Capabilities, declared and granted.** A page declares what it needs in the manifest: `state` (a per-artifact document store), `download` (save a file the page made, through a save dialog) and `ask` (a plain completion from the person's configured model). The trust prompt lists the declared capabilities. A page that doesn't declare a capability doesn't get it, and changing the declaration asks again.
5. **Trust like `.env`, extended to a file set.** Nothing in `html/` renders until the person allows it. The allow is keyed by the artifact's path plus a hash over the manifest and every page file. The agent can write a page but can never allow one.

The A2UI view (level 1) stays as it is. The worker view (`view.js`, the base plan's RESHAPE) gets the manifest value `view: script`, which is new here; the base plan's D2 lists only `a2ui | html`. Once the scripted host passes P1, a scripted page with d3 from `slaw-lib/` does everything `view: script` does, and the static page draws without script. So `view: script` is built only as the **fallback** if the scripted host is dropped, and it can't be dropped too: it is the only custom drawing that renders inside Studio *with* script. In order of increasing power and cost, the view kinds are:

| `view:` | Author writes | Where it renders | Script | Gate |
|---|---|---|---|---|
| `a2ui` (shipped) | a closed JSON schema | in Studio | none (Studio's own components) | validation |
| `script` (base plan, step 9) | `view.js`, returning an SVG or A2UI | in Studio (inert `<img>` or validated A2UI) | in a terminable Worker | allow + hash |
| `html`, static | an `html/template` page | in Studio, in a script-less sandboxed frame | none | allow + hash |
| `html`, scripted | template + JS | in its own page window (helper process) | in a web view Slaw owns | allow + hash, capabilities listed |

## On Go templating as the binding layer

Templating is the right way to bind data. The reasons are below, but one limit comes first. **A template does not make the author's HTML safe.** `html/template` escapes *data* by context. The template text itself is the author's markup, and it can contain any `<script>`, `<iframe>` or `<a href>`. So the template decides how data gets in. Whether the page's own markup can run is the host's job, and its CSP decides.

What templating buys:

- **The query boundary becomes structural.** The template's `FuncMap` is the whole API: `query`, `param`, `link` and formatting helpers (H2). No function takes SQL, a connection or a path.
- **The first render needs no script.** A table, a pivot laid out by hand, an SVG chart built by `range` over rows: all of these work with JavaScript off, which is what lets a static page render inside Studio.
- **Row data is escaped by context.** A value such as `<img onerror=…>` in a database row is the most likely injection in a shared workspace, because whoever can write to the shared Postgres can write that value. It comes out as text in HTML, as a JSON string inside `<script>`, and as a refused URL inside `href`.
- **Errors at save.** `template.Parse` with the closed `FuncMap` catches unknown functions and syntax errors when `write_artifact` runs, so the model gets a usable error.

Alternatives considered: a logic-less template (Mustache) has no context-aware escaping in Go and no conditionals for empty results. Client-side binding only (page JS calls `slaw.run`) forces script on every page and loses the in-Studio host. Go's `html/template` is in the standard library, already understood, and contextual.

## Grounding

### Claude artifacts (what we are borrowing)

These come from the artifact runtime contract (0.2.85) and from Anthropic's help centre ([What are artifacts](https://support.claude.com/en/articles/9487310-what-are-artifacts-and-how-do-i-use-them)).

- **A page on its own origin.** Each artifact is an HTML document served from its own host, so browser storage is per artifact and never reaches other viewers. Script loads only from a short list of CDNs, at pinned versions.
- **Capabilities are declared, then granted.** The page declares `capabilities: {name: config}`. At run time it calls `await claude.use(name)`, which resolves the capability's frozen namespace, or `null` when it is not granted or not available. *Design for absence*: the page must render without the capability. Consent is lazy: a prompt arrives on the first call, never from `use()`, and a viewer's "no" is final for that page load.
- **The capabilities**, and what each means for Slaw:
  - **`db`**: documents in collections, with per-path access rules and a private per-person area. Slaw's `state` (H5).
  - **`sample`**: ask Claude, billed to the viewer, memoryless. Slaw's `ask` (H5).
  - **`downloads`**: offer a generated file, behind a confirmation. Slaw's `download` (H5).
  - **`mcp`**: the viewer's own connectors, with the viewer's credentials. In Slaw this role belongs to the query boundary itself: the viewer's connections, reached only through the manifest's named queries.
  - **`artifact`**: the page republishes itself. Not in Slaw: a page never writes its own files.
  - **`assets`**, **`room`**, **`comments`**, **`user`**, **`files`**: uploads, presence, comments, identity and project files. None of these fit (single user, files in git), so they are left out.
- **Each viewer uses their own access.** Connectors run as the viewer, and usage counts against the viewer's plan. Slaw already works this way: a pulled artifact runs on the puller's connections.
- **Storage is 20 MB per artifact, text only, with personal and shared scopes kept apart.** The help centre warns viewers to check whether an artifact uses shared storage before entering anything sensitive.
- **Publishing refuses** pages that impersonate someone, or that collect credentials or payment details under false pretences. In Slaw the equivalent is that a page window can never look like Studio's sign-in (H7).

What we **cannot** borrow is the isolation. A claude.ai artifact runs in a browser with site isolation: a cross-origin frame gets its own process. Slaw runs inside Wails, and the base plan's spike showed what that means for an iframe: on Linux it reaches the full binding IPC (#174), and a `while(true)` in it freezes Studio. The base plan's *Spike* section has the details. So the scripted page cannot be a frame in Studio's window, and the hosts below follow from that.

### Slaw (what is there)

- `artifacts.Bind`, `Handler.RunArtifactQuery` and `checkArtifactStatement` are the one run path. They give a 500-row cap, a 30 s timeout, read-only in every tier and the fixed denial text (base plan, *As built*).
- Base plan D9 already specifies a Wails `AssetServer.Middleware` under `/slaw-view/<token>/…` with response CSP headers, the path rules that avoid Wails' runtime injection (`assetserver.go:139-183`), and the meta CSP `frame-src 'self'` on Studio's page (candidate a1, verified on Linux).
- `internal/trustfile` is base plan step 8 and **not yet built**: the allow-by-path-and-hash store, extracted from `internal/dotenv`, with `views.json` beside `dotenv.json`. This plan's H6 is its first consumer.
- `internal/llm` runs the in-app assistant on the configured provider (`docs/reference/llm-config.md`).
- Wails on macOS checks `isMainFrame` for binding messages (`WailsContext.m:539`, `darwin/frontend.go:174`); on Linux it does not (#174). Its macOS user script is added with `forMainFrameOnly:false` (`WailsContext.m:296`), and the `external` handler applies to every frame. Wails never enables WebKitGTK's web-process sandbox (no `webkit_web_context_set_sandbox_enabled` under `internal/frontend/desktop/linux/`).
- Wails v2 has one window and owns its web view. Studio links WebKitGTK 4.1 on Linux and WebKit on macOS, so a second native web view needs no new system library.
- `cmd/studio_gui.go` launches the separate `slaw-studio` binary. A helper mode of that binary is not a CLI command, so "CLI mirrors UI" does not apply to it.

## Decisions

### H1. File layout and manifest

```
artifacts/<name>/
├── artifact.yaml
├── view.a2ui.json        (view: a2ui)
├── view.js               (view: script)
└── html/                 (view: html)
    ├── index.html.tmpl   the entry template; other *.tmpl files are partials
    ├── app.js            optional; any number of .js/.mjs files makes the page "scripted"
    └── assets/…          style.css and .svg .png .jpg .webp .woff2 .json (static reference data, e.g. a topojson)
```

```yaml
view: html
html:
  entry: index.html.tmpl          # default; must be a .tmpl directly under html/
  window: {width: 1200, height: 800}   # scripted pages only; clamped
  capabilities:                   # scripted pages only; a closed set
    state: {scope: local}
    download: {}
    ask: {}
```

- **Strict decode as today.** `html` is allowed only with `view: html`. Unknown capability names are refused. A capability on a static page is a validation error ("capabilities need script"), so the declaration never lies about the host.
- **Static or scripted is derived, not declared.** A page is scripted if `html/` holds any `.js` or `.mjs` file, or if a template contains a `<script>` element or an inline event handler. This uses the same tokenizer `html/template` uses (`golang.org/x/net/html`, already in `go.mod` as an indirect dependency, promoted to direct in step P2). The derived kind is part of the hash, so a page can't switch hosts without asking again.
- **Caps:** at most 200 files and 5 MB under `html/`; any one file at most 1 MB; templates at most 256 KB each. Allowed extensions are the closed list above. Only `.tmpl`, `.js` and `.mjs` files sit at the top of `html/`; everything else lives under `assets/`, so nothing unreachable is hashed. There are no symlinks and no dotfiles (`.DS_Store` and `Thumbs.db` are skipped: not hashed, not served), no folders deeper than `html/assets/<dir>/`, and no top-level `_slaw/` or `slaw-lib/` (reserved for the host). A folder that breaks a rule lists with its error and does not render (refuse, don't repair).
- **No CDN, no network.** A page vendors what it needs into `html/`, where it is hashed and shown at allow. Slaw also serves a small pinned set at `slaw-lib/` (d3 modules, vega + vega-lite, Observable Plot) from the embedded FS, licences in `THIRD_PARTY_NOTICES.md` (base plan open question 5, extended).

### H2. Templates: what a page can reach

- `html/template`, parsed with `Option("missingkey=error")`. The entry template and its partials come only from `html/*.tmpl`; the `ParseFS` root is the artifact folder's `html/`, opened through the store (no symlinks).
- **The `FuncMap` is the whole API**, and it is closed:
  - `query "name"` returns `{Columns, Rows, Truncated}` for that named query, with the current parameter values. Queries run lazily and once per render per *(query, values)*, through `RunArtifactQuery`. An error renders as a fixed text block, never the policy's reason.
  - `param "name"` returns the bound value of a parameter. `params` returns the manifest's declared parameters, with options, for building controls.
  - `link "name" value …` returns a URL to this page with those parameters overridden. It is the only way to make a self-link that keeps the token path.
  - Formatting: `number`, `percent`, `date`, `bytes`, `truncate`.
  - SVG helpers: `scaleLinear`, `scaleBand` and `pathLine` return plain numbers and strings, so a static page can draw a chart without script.
  - `json` returns the value for a `<script type="application/json">` data block (scripted pages), escaped by `html/template`'s JS context.
- **No function returns `template.HTML`, `template.JS`, `template.CSS`, `template.URL` or `template.HTMLAttr`.** Data is always escaped. A lint test fails if one is ever added. That test is the single most important line in this plan: one `safeHTML` helper turns any database row into script inside a page that holds capabilities.
- **Limits.** A render runs with the 30 s query timeout and a 10 MB output cap; the writer errors and the render stops. Template execution itself is bounded by the data (at most 20 queries × 500 rows), and `text/template` caps recursion depth.
- **Save-time vet.** `write_artifact` parses every template with the closed `FuncMap` (unknown function or bad syntax: refused), checks that every `query` name is a literal naming a manifest query, and checks every `param` and `link` name the same way. A `query` with a computed name is refused at save; at run it is still checked against the manifest. That makes "which queries a page uses" derivable, like the base plan's derived `Uses`.
- **URL attributes are an allow-list, checked at save (P1).** A template may contain `<link>` only with `rel=stylesheet` and a relative `assets/` href. It may contain no `target`, `ping` or `download` attribute, no `<meta http-equiv>` and no `<base>`. Every URL attribute (`href`, `src`, `srcset`, `poster`, `action`, `formaction`, `data`, `xlink:href` and SVG `href`, plus CSS `url()` in `style`) must be a `link` call, a `#fragment` or a literal relative `assets/` path. Absolute URLs, and any other templated value in a URL attribute, are refused at save. P1 found that `<link rel=preconnect>` and a `target=_blank` click open a TCP connection under every CSP layer, a1 included, on both platforms. The DNS lookup for a host name was measured in the Linux helper and the macOS static host. On macOS `<link rel=dns-prefetch>` also resolves the name, and with an `https://` name, preconnect and `target=_blank` also send it in the TLS ClientHello (SNI). Without script, a host name built from a template value would leak data over DNS.

### H3. The static host: inside Studio

- Served by the base plan's `/slaw-view/` middleware (D9) at `/slaw-view/<token>/page?<params>`, with static files at `/slaw-view/<token>/assets/<path>`. **The token is per (Studio process, artifact)**, not per process as in D9. One page's token opens no other artifact, and the route takes the artifact from the token, never from the path. The path never ends in `/` or `index.html`, so Wails injects no runtime. Response CSP, with `<a>` standing for `<studio-origin>/slaw-view/<token>/assets/`:
  `sandbox; default-src 'none'; script-src 'none'; style-src <a> 'unsafe-inline'; img-src <a> data:; font-src <a>; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'`
  Sub-resources can load only from `assets/`, which never runs a query. An `<img src=…/page?…>` or a CSS `url()` pointing at the render route is refused by the CSP. P1 confirmed it: no request arrived in any run. As a second layer, the route rate-limits renders to 4 in flight and 60 a minute per token, so a page can't fire renders in a loop. On WebKitGTK 2.54 and WKWebView (macOS 26) the `/slaw-view/` handler receives no `Sec-Fetch-*` headers, so the route can't tell a navigation from a sub-resource and relies on the limit.
  The response also carries `X-Content-Type-Options: nosniff` and `Cache-Control: no-store`. The iframe gets `sandbox=""`, with no `allow-scripts`, `allow-same-origin`, `allow-forms`, `allow-popups` or `allow-top-navigation`.
- **No script runs in the frame, so (b) and (d) do not apply, but (a) still does.** Without script there is no IPC to post (d), no loop to freeze on (b), and no `location =`. Navigation that needs no script remains:
  - a person's click on an outside `<a href>`, which the sandbox does not stop;
  - a `<meta refresh>`. The HTML spec blocks it in a frame sandboxed without `allow-scripts` (the *sandboxed automatic features* flag); the base plan's spike frame had `allow-scripts`, so it doesn't tell us WebKit's behaviour here.

  Both are stopped by the meta CSP `frame-src 'self'` on Studio's page (base plan candidate a1), which blocked all 9 vectors on Linux and on macOS (#182). **So the static host depends on a1**, which ships with it. P1 (Linux) confirmed the following:
  - a1 is required for an outside-link click; without it, the click reached the listener.
  - `<meta refresh>` is blocked by the script-less sandbox alone.
  - The narrowed form `frame-src <origin>/slaw-view/` works: Studio boots, the frame and its links work, and a link to `/` or `/index.html` loads nothing.

  The macOS run confirmed all three, with the same dev origin. It also showed that Wails' all-frames user script does not run in a frame without `allow-scripts`.

  a1 ships narrowed. It is emitted into `index.html` at build time for each target origin (`wails://wails.localhost:34115` in dev, `wails://wails` in a build), and P3 re-verifies it in the production build on both platforms. A meta CSP inserted by script was not tested and is not used.
- **Interactivity without script.** `<a href="{{ link "region" .region }}">` navigates the frame to the same route with new values. There are no forms: the frame has no `allow-forms`, so a form can't submit, and that keeps the sandbox minimal. Go binds them (`artifacts.Bind` refuses bad values) and re-renders. Values are read only from the query string of the `/slaw-view/` route, never from Studio's own state. Parameter controls can also stay as Studio's level-1 components around the frame (`useArtifactParams`); a change reloads the frame with the new query string.
- **Serve what was hashed.** As base plan D9: the middleware reads the file set once, hashes it with the manifest, compares it with the allowed hash and renders those bytes. A watcher change tears the frame down.
- **Sizing.** The frame has a fixed height that the person can resize. With no script inside, the frame can't report its own height (`ResizeObserver` needs script).

### H4. The scripted host: a page window Slaw owns

- **A helper process.** `slaw-studio --view-host` is the same binary in a mode that never starts Wails. Studio spawns it with:
  - a scrubbed environment: only what the toolkit needs. P1 measured it on Linux as `XDG_RUNTIME_DIR`, plus `WAYLAND_DISPLAY` when it isn't `wayland-0`; `GDK_BACKEND`, `HOME` and `DBUS_SESSION_BUS_ADDRESS` were not needed. None of `AWS_*`, `*_TOKEN`, `*_PASSWORD` or the workspace `.env` overlay is passed, because the overlay is never in the environment and the helper never sees the parent's;
  - one dedicated pipe pair (`ExtraFiles`) for a JSON-lines protocol, not stdin/stdout, which GTK may write to; stderr goes to Studio's debug log; no other inherited file descriptors. **The helper marks the pipe `CLOEXEC` at once**: P1 found that without it, the fd leaks into `WebKitNetworkProcess`, `xdg-dbus-proxy` and the web process;
  - on Linux, `SysProcAttr.Pdeathsig = SIGKILL`, so the helper dies with Studio. On macOS the helper exits when its protocol pipe closes;
  - **a tree kill.** The helper runs in its own process group (or PID namespace), and Studio kills the whole group. P1 found that SIGKILL to the helper alone left a spinning `WebKitWebProcess` and its bwrap parents alive at 72% CPU. On macOS a busy WebContent process died with the SIGKILLed helper (the WebKit XPC services are children of launchd, so a process-group kill wouldn't reach them; P4 re-checks that none survive);
  - **no network**: structural on Linux; on macOS, filtering layers with one accepted leak.
    - **Linux.** The helper runs under bwrap, or an equivalent clone, with `--unshare-net`, the real uid mapped to itself, and an **allow-listed filesystem view**:
      - a generated `/etc/nsswitch.conf` of `hosts: files`, and an empty `resolv.conf`;
      - `--tmpfs /run`, with only `$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY` bound back (plus the session bus, if P4 measures it as needed);
      - no system bus, and no avahi, resolved, nscd or sss sockets.

      P1 found that a network namespace alone leaves name resolution open: systemd-resolved's varlink socket answered from inside both a user+net namespace and `bwrap --unshare-net`. WebKit sent it a lookup for a link the navigation policy had refused, so the lookup happens regardless of the policy decision. Hiding only resolved's socket is not enough either: avahi (which multicasts `.local` names on the LAN) and `org.freedesktop.resolve1` on the system bus stay reachable.

      P4 verifies end to end, with WebKit under this exact launcher: no `connect()` to any unix socket outside the allow-list (strace, or fake sockets at every known resolver path). **If a user+net namespace can't be created** (for example under Ubuntu's `apparmor_restrict_unprivileged_userns`), the page window is refused. Unprivileged bwrap needs user namespaces too, so it is no fallback.
    - **macOS: four filtering layers, one accepted leak.** WebKit's Networking process is started by launchd, not by the helper, so no namespace applies. The helper sets up, before the first load:
      - the strict CSP (below);
      - the navigation policy (below);
      - a `WKContentRuleList` that blocks every URL except `slaw-page:`, compiled at start, with the first load waiting for the compile callback. P1 found that it stops every load, plus `dns-prefetch` and `preconnect`, even under a permissive CSP. No setting or header turns DNS prefetch off (`_setDNSPrefetchingEnabled:NO` and `X-DNS-Prefetch-Control: off` did nothing);
      - `WKWebsiteDataStore.proxyConfigurations` (macOS 14+) pointed at a dead loopback port. Host names in loads and link-click preconnects go to the proxy and are not resolved locally; `dns-prefetch` goes around the proxy, and only the rule list stops it.

      Settings, not a layer: WebRTC and media devices off with the private preferences `peerConnectionEnabled` and `mediaDevicesEnabled` (P1 measured `RTCPeerConnection` as undefined with them).

      **The accepted leak:** a link to an IP address that page script clicks (`a.click()`), with or without `target=_blank`, opens a TCP connection to the address and port the page chose. WebKit preconnects on the click before the rules or the policy apply, and IP addresses bypass the proxy (verified for loopback and the LAN in P1; the owner QA on 2026-10-22 also reached a non-private TEST-NET address). Over plain HTTP one empty connection per click and no request; `https://` to an IP address sends a TLS ClientHello (no SNI) and four connections per click. The page can't read a reply, but the choice of address, port and timing carries a few bytes per click, the click needs no gesture and can repeat without limit, and the page can reach hosts on the person's LAN and on loopback (where Studio's own listeners are). `sandbox-exec (deny network*)` closed it in P1, but it is deprecated and won't ship. An App Sandbox helper would need a separate signed executable, which waits on a developer account. The owner accepted the leak on 2026-10-21. #189 tracks a network-free model (WASM/WASI) to replace it. Until then:
      - the trust prompt for a scripted page on macOS says that the page can use a link to open a network connection to an address it chooses, though it can't read a reply;
      - `docs/reference/artifacts.md` states the limitation;
      - macOS 13 and older, where `proxyConfigurations` doesn't exist, refuse the page window.

  The helper is never given the workspace path, a connection, a credential or a store. It receives the page's bytes over the pipe and serves them from memory.
- **The native web view is ours, not Wails'.** On Linux it is WebKitGTK through cgo; on macOS, WKWebView through Objective-C. We own the native code (a few hundred lines per platform). `webview/webview` exposes no navigation policy, custom scheme or ephemeral data store, and it registers its own `__webview__` message handler. The helper calls `webkit_web_context_set_sandbox_enabled(TRUE)` before creating the web view, which Wails never does (grounding). **This is required.** In P1 the web process was unconfined without it. With it, the web process ran under bwrap in its own user, pid and net namespaces, with seccomp, NoNewPrivs and no capabilities. `WebKitNetworkProcess` is not sandboxed either way, so the helper's namespace is what gates the network. The helper must not run with `HOME=/root`, which breaks WebKit's bwrap. Owning the view gives what Wails withheld (base plan grounding):
  - **A navigation policy.** The web view refuses every navigation except the initial page and same-page `link` URLs: GTK's `decide-policy` and `WKNavigationDelegate`. It also refuses new windows, downloads the page starts itself, and every permission request (camera, microphone, geolocation, notifications, clipboard read).
  - **A custom URL scheme.** `slaw-page://<name>/` is served from memory with the response CSP (P1: a CSP response header is honoured on a WebKitGTK custom scheme; the response's headers must include `Content-Type`, or a script is silently skipped under `nosniff`):
    `default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; worker-src blob:; frame-src 'none'; form-action 'none'; base-uri 'none'`
    `connect-src 'self'` lets script `fetch("assets/x.json")`. That is harmless: the scheme handler serves only bytes held in memory, and nothing it serves runs a query. Queries go only through `slaw.run`. The host's own files are on the page's origin too, so `'self'` covers them: `slaw-page://<name>/_slaw/theme.css` and `slaw-page://<name>/slaw-lib/…`. Studio sends the library files the page's templates and scripts reference over the pipe when the window opens, in chunks, outside the 2 MB per-message cap.
    No `'unsafe-eval'` and no inline script: data reaches script through the `json` block. Libraries that need `eval` (some vega builds) use their CSP-safe build.
  - **One message handler, main frame only.** It lives in the page's main world, beside the injected `slaw` API: a main-world object can't post to a handler in another content world, and the page is untrusted either way, so an isolated world would buy nothing. Frames are refused by `frame-src 'none'`. P1 found that, with a permissive CSP, both a `srcdoc` and a `src` frame can post to the handler. A `srcdoc` frame is stopped by the inherited `script-src`, a `src` frame by `frame-src 'none'`, and both by `decide-policy` (which refuses `about:srcdoc`). The handler also checks `frameInfo.isMainFrame` on macOS; on Linux, where the frame isn't available, there is nothing else to check, because the helper's view holds only the page.
  - **Ephemeral data.** A non-persistent website data store, so no `localStorage`, IndexedDB or cookies survive the window. A page keeps state only through `state`.
  - **Off where the platform allows:** WebRTC and media streams (`enable-webrtc`, `enable-media-stream` on WebKitGTK; macOS has only the private preferences), DNS prefetch (on macOS only the content rule list stops it), plugins, the developer inspector (unless a Studio setting turns it on for authors), and spatial navigation. CSP `connect-src` covers neither WebRTC nor DNS prefetch on every WebKit. **These settings are not a layer.** In WebKitGTK 2.54, `enable-dns-prefetching` and `enable-hyperlink-auditing` are deprecated no-ops (`<a ping>` still fired), and only media stream, localStorage and IndexedDB being off measurably did anything. The network gate above is the gate. At start the helper logs `typeof RTCPeerConnection`: WebRTC isn't built into the Fedora build P1 used, so STUN is unverified on builds that ship it.
- **The page-side API**, injected by the helper as a user script in the main world before the page runs. It mirrors `claude.use`:
  ```js
  const rows = await slaw.run("by_region", { from: "2026-01-01" }) // {columns, rows, truncated}
  slaw.params()                    // declared parameters, defaults, static options
  const state = await slaw.use("state")   // namespace or null; same for "download", "ask"
  ```
  `slaw.run` is always present. `slaw.use(name)` resolves `null` for anything not declared, and the page must render without it. Theme roles arrive as CSS custom properties (`--slaw-*`) in a host stylesheet at `slaw-page://<name>/_slaw/theme.css`, built from the validated theme.
- **The bridge in Studio.** Messages go from the page to the helper, over the pipe, to Studio, through `studio/pagehost.go`, a non-binding helper. Studio pins the artifact name when it opens the window: a message never names its artifact. Each message is shape-checked and size-capped (64 KB in, 2 MB out). Limits are 4 calls in flight and 60 a minute per window. Errors are fixed texts, never a path or the policy's reason (base plan *As built*: "a level-2 bridge would have to use fixed text").
- **Watchdog.** The page host posts a heartbeat. On a missed beat, a crash, or a watcher change to the artifact, Studio kills the helper process and shows "This page stopped responding" or "This page changed" in Studio, with Reopen. The tree kill above takes the web process with it.
- **The window can't impersonate Studio.** Its native title is always `<artifact title> — Slaw page`; `document.title` is ignored. There is no menu bar beyond Close. It is never full-screen without a person's gesture. Studio never asks for a credential inside a page window, and the user guide says so in the same paragraph as the trust prompt.
- **The macOS process model.** The helper runs from inside the app bundle with the accessory activation policy, so it shows no second Dock icon. P1 (macOS) verified no Dock icon and the window coming to the front; Cmd-W was not tested and moves to P4.
- **The macOS keychain.** Keychain ACLs are per application. A helper that is the same executable as Studio shares Studio's keychain identity, so an escape from the web process into the helper could read Slaw's items without a prompt. (Linux Secret Service has no per-binary ACL at all, so there it is the OS user either way.) The mitigation is a separate helper executable in `Contents/MacOS/` with its own identifier. That waits on the developer account too (#189), so for now the helper is the same executable, and this exposure is part of the accepted macOS limitation.

### H5. Capabilities

Every capability is declared in the manifest, listed in the trust prompt, and resolved through `slaw.use`. Each one maps to a Studio method behind the pipe, never to an agent tool: a page is not a way into the one tool surface.

- **`state`**: a small document store for the page's own state (an annotations list, saved filter sets, a checklist, which cohort is pinned).
  - API: `get(key)`, `set(key, value)`, `delete(key)`, `list(prefix)`; values are JSON; `onChange(fn)` for other windows of the same artifact.
  - **v1 scope: `local` only.** The store is `.slaw-local/artifacts/<name>/state.json` (git-ignored, machine-local), compare-and-write through `filestore`, at most 1 MB in total and 64 KB per value, with writes debounced. It is keyed by the artifact's name, not its hash, so state survives an edit and a re-allow.
  - **`shared` scope is deferred, by decision.** A committed `artifacts/<name>/state/` would let a page copy query results from a person's connection into a file that git then pushes. That breaks *data stays where it lives* and is an exfiltration path through git, with the person's own `git push` as the carrier. The shape that fits *data stays where it lives* is a `state` backed by a **writable connection**: a DuckDB file under `data/`, or a named connection the person made writable, with the base plan's write gates (tier, writable connection, approval). Then shared state is data in a database, not answers in git (*Later*).
  - Like Claude's `db`, the store's contents are **untrusted input** to the page. Another version of the page wrote them.
- **`download`**: `save({filename, data, type})`, limited to CSV, JSON, SVG, PNG, PDF and plain text, at most 20 MB. It opens Studio's native save dialog (Wails `runtime.SaveFileDialog`) with the name filled in. The person can cancel, and a save is never silent. It is allowed only from a person's gesture: the injected API reads `navigator.userActivation.isActive` inside the web view, before page script can wrap it. That check only stops a page from saving on load. The page could forge a flag it sends itself, and it can fake a gesture too, so **the save dialog is the real gate**.
- **`ask`**: `ask(prompt, {system?})` returns `{text}`.
  - It is a single completion on the person's configured provider (`internal/llm`), with **no tools**: never the agent's registry, never task mode.
  - It returns `null` when no provider is configured (*AI as collaborator, not dependency*).
  - Limits: 10 calls a minute, a prompt cap of 32 KB, an output cap of 4,000 tokens.
  - The trust prompt names the provider: "This page can send text, which may include query results, to <provider>". For a remote provider this is the one capability that moves data off the machine, so it also gets its own toggle in Settings > Workspace > Pages, defaulting to on at allow time and revocable separately. It defaults to on because the agent already sends query results to the same provider, and the prompt names it. A reviewer who disagrees should argue from that.
  - Calls appear in the agent activity log with the page's name. They are not hidden.
- **Not offered:** connectors or MCP (the query boundary is the connector), writes of any kind, network, files outside `state`, the agent's tools, the person's identity, presence and comments.

### H6. Trust over a file set

- The base plan's `ViewHash` becomes `PageHash`. It is a sha256 over a **canonical manifest of the file set**. Entry 0 is `artifact.yaml`, length-prefixed, so a pull that changes a query's SQL or connection after allow asks again. Then come the files in `html/`, sorted by path, each as the length-prefixed path, the length-prefixed content and the file's role. The list also includes the derived static/scripted kind and the declared capabilities. A renamed, added, removed or moved file changes the hash, and so does a byte moved across a file boundary.
- **The allow prompt** shows:
  - the file list with sizes, and each file in a read-only CodeMirror;
  - the queries the page uses (H2's derived list), with their connections;
  - for a scripted page, the declared capabilities, each with a plain line of what it does;
  - for a scripted page on macOS, the network disclosure from H4 (a page can open a connection to an address it chooses).

  The person allows or denies. The decision is stored in `views.json` (base plan step 8), never in the folder or a bundle.
- **The agent can write a page but can't allow one.** There is no tool and no MCP method for it. That includes agent edits: every edit asks again (base plan open question 6, which still stands).
- **Re-ask on change.** A watcher change to any file in the artifact tears down the static frame or kills the page window, then re-checks the hash.

### H7. Agent and bundle

- **`write_artifact`** gains `html_files: {path: text}`. Text files only: binary assets arrive by git or a bundle, not from the model. It is checked against H1's rules and vetted per H2 at save. `get_artifact` returns the file list and each text file's source (capped as long SQL is). The agent never sees a render: there is no tool that runs a page, as no tool runs an artifact today.
- The built-in `data-artifacts` skill gets an *HTML pages* section: the `FuncMap`, a static example (a table and an SVG bar chart drawn with `scaleBand`), a scripted example (`slaw.run` with vega-lite from `slaw-lib/`), and the rule that data reaches script only through `{{ json }}`.
- **Bundles.** Kind `artifact` gains the `html/` entries under H1's rules. They are checked before any write, and the bundle caps of 10 MB per entry and 50 MB in total still apply. A decision never travels with a bundle.
- **CLI.** `slaw artifact list` shows `html (static)` or `html (scripted)`. `slaw artifact view status|allow|deny|revoke <name>` (base plan D8) covers pages, and `status` lists the capabilities. There is no command to open or run a page: it opens from Studio.

## Steps

The base plan's per-step gates and review rules still apply: Sonnet implements, Opus reviews adversarially, and nothing is committed until APPROVE. Base plan step 8 (trust) comes first, generalised to file sets in H6, and is shared with the worker view. The static host doesn't depend on the macOS worker result.

#### Step P1. Spike: both hosts in the real webview, on Linux and macOS (not committed)

- **Static host:** a `sandbox=""` frame with `script-src 'none'` in the Wails window.
  - Rerun every D10 vector and confirm none runs: inline `<script>`, `onerror`, `javascript:` URLs, `<meta refresh>`, SVG `<script>`, `<object>`/`<embed>`.
  - Confirm a person's click on an outside link is blocked by the meta `frame-src` (a1 blocked all 9 scripted vectors on both platforms, #182; this is the script-less case). Try narrowing it to `frame-src <studio-origin>/slaw-view/` and confirm Studio still works.
  - Links re-render through the middleware, and a `<form>` does not submit.
  - The `<studio-origin>/slaw-view/<token>/assets/` source matches for styles, images and fonts, although the `sandbox` directive gives the document an opaque origin.
  - An `<img>` and a CSS `url()` pointing at `/page` are refused by the CSP, and by the route if the CSP lets one through.
  - On macOS, Wails' `forMainFrameOnly:false` user script does not run in a frame without `allow-scripts`.
  - The frame navigating itself to `/` and `/index.html` by a link (not probed by #182): Studio's page loads without script, and nothing reaches Go.
  - Confirm no Wails runtime is injected and no message reaches Go.
  - Ground truth is the loopback listener and the Go log, as in step 7.
- **Scripted host:** the helper process with its own web view on both platforms.
  - `fetch`, XHR, WebSocket, `EventSource`, `sendBeacon`, `<img src=http…>`, CSS `url()` and `@import`, `<link rel=dns-prefetch|preconnect|prefetch>`, `<a ping>`, WebRTC with a STUN server, `window.open`, `location =` and a form submit. Each must produce **no** listener hit, and no DNS query either (watch with `tcpdump port 53`).
  - The message handler refuses a frame (a `srcdoc` frame, which `frame-src 'none'` should already stop).
  - **The network gate.** On Linux the helper's network namespace has only `lo`, which the helper can't use to reach Studio's listener. On macOS the `sandbox-exec` profile is applied. In both cases there are zero DNS queries and zero STUN packets, and a `fetch` to the loopback listener gets no hit. (Historical: macOS as built uses the content rule list and the proxy instead, with one accepted leak; see H4.)
  - The web process sandbox is on (`/proc/<webprocess>/status` and the bwrap parent on Linux; `sandbox-exec`'s confinement on macOS; historical, see H4).
  - `while(true){}`: the heartbeat is missed, the helper is killed, its web process exits and Studio never stutters.
  - The helper's environment and file descriptors, read from `/proc/<pid>/environ` and `fd` on Linux and with `ps eww` / `lsof` on macOS.
  - The macOS Dock and focus behaviour.
  - A real d3 and a vega-lite chart render under the CSP.
- **Output:** a *Spike* section appended here, with each check marked Verified or Not verified, and a decision per host. The static host can ship even if the scripted host fails. If the scripted host fails on either platform, it is dropped, and pages stay static plus `view.js`.

#### Step P2. `internal/artifacts`: the `html/` file set, the template vet, `PageHash` (Go only)

- File-set loading through the store: H1's rules, the derived kind, caps, and `Rev` over all files (extending the two-file revision). The template parse uses the closed `FuncMap` (defined in a leaf, `internal/artifacts/pagefuncs`, so the vet and the renderer share it) and derives the queries a page uses. Watcher kinds extend to `artifacts/<name>/html/**`.
- **Tests:**
  - Every H1 refusal, plus symlinks and dotfiles.
  - Derived kind: a `<script>` inside a template, an `onclick=`, and a `.js` file each make the page scripted.
  - `PageHash` changes when a file is added, removed or renamed, and when a byte moves across a file boundary.
  - **The FuncMap lint:** reflect over every function and fail if any return type is one of `html/template`'s trusted string types.
  - **The H2 URL allow-list:** refused at save: `<link rel=preconnect>`, `rel=dns-prefetch` and any `rel` other than `stylesheet`; a stylesheet href that isn't a literal relative `assets/` path; `target`, `ping` and `download`; `<meta http-equiv>`; `<base>`; absolute URLs; and a templated value that isn't a `link` call in any URL attribute (the full list in H2).
- **Review first:** template paths escaping `html/` (`{{ template "../x" }}` is a name, not a path, so check `ParseFS`'s root); a computed `query` name; the derived kind being fooled (a `<script>` split across partials, an SVG asset with a script, which is served as an `<img>` only).

#### Step P3. Rendering and the static host

- `artifacts.RenderPage(ctx, runner, name, values, w)` with lazy, memoised queries, the output cap and fixed error text. `studio/viewserver.go`, the base plan's step 9 middleware, gets the `/page` route and H3's CSP. `components/StaticPage.vue` hosts the frame, with the level-1 controls around it. Resizable height.
- **Tests:**
  - Go: every response under the route has the CSP; one artifact's token can't render or serve another's; `/page` refuses a sub-resource request and rate-limits; there is no injection on the route's paths; a hostile row value comes out as text in HTML, as a string in `{{ json }}`, and as `#ZgotmplZ` in `href`; a parameter in the query string goes through `Bind`; the output cap holds.
  - Rodney: layout only. Rodney's origin (`http://localhost:34115`) differs from the window's, so the narrowed meta blocks the frame there; QA through the dev window's origin or a QA-only meta.
  - a1 narrowed and emitted into `index.html` at build time per origin (H3), then re-verified in a production build on both platforms (the outside-link click and the `/` navigation from P1), in the real Wails window.
- **Review first:** any path where data becomes markup; the token-to-artifact mapping; the `link` helper (no `javascript:`; only this route); the frame CSP missing on an error response.

**As built (P3, 2026-10-21).**

- **The renderer** is `artifacts.RenderHTMLPage(ctx, run, artifact, values, w)`, not `RenderPage` (that name is the script view's A2UI page validator). It takes the artifact as loaded (the files that were hashed) rather than a name, so what renders is what was hashed; queries go through a `PageRunner` (Studio's is `RunArtifactQuery`, with the artifact pinned). It writes nothing on failure and the host sends a fixed error block (`PageErrorHTML`).
- **P2's reviewer note, the unbounded loop, is closed by a work budget, not a goroutine.** Nothing can interrupt `{{ range 3000000000 }}{{ end }}`, and an abandoned goroutine would keep spinning, so the parsed trees are rewritten before execution (`render_guard.go`): every `range` gets `| slawRange__` (charges the length or integer it iterates) and every `{{template}}` call is preceded by a `{{ $slawStep__ := slawStep__ }}` declaration (one step), against one budget of 2,000,000 per render. The guard names are added to the function map after the author's text is parsed, so a template cannot call them. A declaration prints nothing and `html/template`'s escaper skips it. **Recursion between templates** is refused by the P2 vet at save; the renderer refuses it again as defence in depth, so the call depth is at most the number of templates and the budget bounds the width. The render also has a 30 s deadline and a 10 MB output cap. **Strings are metered**: inside a render `printf`, `print`, `println`, `js`, `html` and `urlquery` are shadowed (`pagefuncs.FuncsMetered`) by versions that estimate their result and refuse before allocating, and every string our own functions return is charged, against one 64 MB budget (over it: `RenderTooLarge`). Round 2 of the review built a gigabyte string in variables with a ×10 `printf` chain that the output cap and the tick never saw. The context (the deadline, or a cancel from a revoke, a new token or a workspace switch) is checked before every action, `if`, `with` and `range` in every list through a `slawTick__` declaration (the budget itself is charged only by ranges and template calls), so inside a loop body the worst overrun is one action. A first version checked it once per range and let a loop of heavy actions run for minutes past the deadline.
- **The rendered output is tokenized again** with the vet's rules (`render_check.go`): defence in depth, as the P2 reviewer suggested.
- **Query-string values** are parsed by `ParsePageQuery` (declared names, once each, typed by parameter type); `;` in a query string is a refusal, not a dropped pair. Bind checks them again on every query.
- **The route** is in `studio/viewserver_page.go`. A page token's re-check (reads and vets every file) is reused for 500 ms per token, because a page makes a request per asset; a revoke or deny drops the artifact's tokens at once, and a render is discarded if the re-check fails after it. **A page's queries run the manifest that was hashed** (`Handler.RunManifestQuery` over the grant's held manifest, with the same vet at every run, Bind and read-only gate), not `artifact.yaml` as it is on disk when a lazy query runs. The route also refuses a request whose `Sec-Fetch-Dest`/`Mode` header (when the webview sends one) says it is not a frame navigation.
- **a1** is emitted by a Vite plugin from `scripts/frame-csp.mjs`: `wails://wails` (Linux and macOS) or `http://wails.localhost` (Windows) in a build, `wails://wails.localhost:34115` plus the QA origin `http://localhost:34115` when serving (never in a build). The frontend refuses to open a page when the meta is missing or does not name the window's origin.
- **Trust** reuses `views.json` and `ArtifactViewStatus` for pages (the status carries the file list, the derived kind and the queries). The file list is shown read-only (`PageFiles.vue`); the full prompt (queries and capabilities listed, the macOS disclosure) and the Settings list stay for P6. `slaw artifact view` prints a page's files.
- **Linux, real window.** Dev window and a production binary, a loopback listener, real XTEST clicks: no listener hit for an outside link, `javascript:`, forms, meta refresh, `<img>`/CSS `url()` pointing at `/page`; a click to `/` or `/index.html` loads nothing (one `GET /`, at boot); no IPC from a frame; the real route loads its page, stylesheet and image under the production CSP. With the meta removed the outside click and `/`, `/index.html` all fire. macOS production remains to be run (the checklist is `P3-MAC-QA.md` in the job directory).

#### Step P4. The page window

- `cmd/slaw-studio` helper mode; `internal/pagehost` holds the pipe protocol, a leaf shared by the helper and Studio. Native code in `studio/pagewindow_{linux,darwin}.go` (cgo, build-tagged like Wails' own). `studio/pagehost.go` covers spawning with a scrubbed environment, the bridge, limits and the watchdog. In Studio: "Open page" and the "stopped responding" / "changed" states.
- **Tests:**
  - Go, protocol: message shapes refused, caps, rate limits, artifact pinning (a message that names another artifact is refused by shape, since no field exists for it).
  - Go, environment: the scrubbed env, asserted on a fake spawn.
  - Live in the Wails build on both platforms: the P1 hostile page rerun as an allowed page. On macOS also: the 14-group matrix under the four layers, where only the IP-literal link click may connect; a public IP address through the proxy (a TEST-NET address on a `lo0` alias); the rule list in place before the first load; `https://` to an IP literal (TLS ClientHello); no WebContent surviving a kill; Cmd-W, focus and the window above Studio; a re-run on the oldest supported macOS (14); and refusal on macOS 13. On Linux also: no `connect()` to a unix socket outside the allow-list (H4), and a re-run on Ubuntu 24.04 and the oldest supported WebKitGTK.
- **Review first:**
  - an environment or file descriptor reaching the helper;
  - a navigation the policy lets through (redirects, `about:blank`, `blob:` documents);
  - the network namespace (Linux) or the rule list and proxy (macOS) missing on any spawn path (a fallback that skips them must fail closed);
  - the helper outliving Studio;
  - `document.title` reaching the native title;
  - `slaw.run` able to reach a query the page's derived list doesn't hold. That is allowed, since it is still a manifest query, but it must be logged.

**As built (P4, 2026-10-21).**

- **Helper mode** is `slaw-studio --view-host`, handled at the top of `studio/main.go` (the plan's `cmd/slaw-studio` does not exist: Studio's `main` is that file). `internal/pagehost` is the leaf: the message shapes (`Decode` checks the type, exactly the fields of its shape and none other, every value's range; 64 KB from the helper, 2 MB to it, 1 MB chunks for files and documents), the fixed error codes and texts, the injected `slaw` script, the limiter. No message has a field that names an artifact: `open.name` is the helper's own URL host, sent by Studio.
- **Pipes.** Two `os.Pipe`s, not a socketpair: descriptor 3 is the helper's read end and 4 its write end, both `CLOEXEC` at once in the helper (any other descriptor above 4 is marked too). Studio closes its copies of the child's ends, so each side sees EOF when the other goes. The helper's stderr goes to Studio's debug log.
- **Linux gate** (`studio/pagehost_gate_linux.go`), bubblewrap with `--unshare-all --die-with-parent --new-session --cap-drop ALL --clearenv`, an allow-listed view (`/usr` read-only, usr-merge links, a few optional `/etc` entries, generated `nsswitch.conf`/`resolv.conf`/`hosts`/`passwd`/`group`, `/proc`, `/dev` and `/dev/dri`, `/sys` read-only, tmpfs `/tmp` and `/run` with only the Wayland socket bound back, the Studio binary read-only as `/slaw-view-host`), and the environment set inside by bubblewrap (`HOME=/tmp/slaw-home`, `PATH`, `XDG_RUNTIME_DIR`, `NO_AT_BRIDGE`, `GSETTINGS_BACKEND=memory`, `GIO_USE_VFS=local`, `GDK_BACKEND=wayland`). bubblewrap's own environment is `XDG_RUNTIME_DIR` (and `WAYLAND_DISPLAY` when it is not `wayland-0`). Every `Open page` probes the gate first (the same view running `/usr/bin/true`); a system that cannot make it, or has no Wayland socket, gets a fixed refusal and **no helper starts**. The helper itself refuses (exit 3) unless `HOME` is the gate's and its network namespace holds loopback only. **Deviations from the plan:** (1) **X11-only sessions are refused**, not served through `DISPLAY`: the X server's abstract socket does not cross a network namespace, and a path socket would need the cookie file handed to the helper. Under Wayland (XWayland included) there is no change. (2) **The helper must be the version Studio is.** Studio records the device and inode of its own executable at startup and starts a helper only while the file at that path is still that file; an update that renames a new file over the old one (same path, new inode) or removes it gets a fixed "restart Slaw" refusal and no helper (`exeRecord`, tested). (An earlier cut relied on `os.Executable`, which trims " (deleted)" and follows the new file.) (3) **The generated `/etc` files are not files on the host**: each is the read end of a pipe handed to bubblewrap with `--ro-bind-data`, so nothing is left in `/tmp` after a quit or a kill. (4) **Every descriptor above 4 is marked close-on-exec in the helper** (walked from `/dev/fd`, however high), so nothing the parent forgot to mark reaches WebKit's processes; the live test plants one at 100.
- **Tree kill.** `Setpgid` plus `kill(-pgid, SIGKILL)`, and the helper is pid 1's child in bubblewrap's pid namespace, so a kill of the outer bubblewrap takes everything under it. `Pdeathsig` on Linux is a second chain (it fires when the *thread* that started the helper exits, which Go does not guarantee never happens, so the pipe's EOF is the one that always holds).
- **Native views.** `studio/pagewindow_linux.c` (WebKitGTK 4.1, build tag `webkit2_41`) and `studio/pagewindow_darwin.m` (WKWebView) implement the four calls of `pagewindow.h`; `pagewindow_cgo.go` is the one Go side for both. The scheme handler is **asynchronous** (a request waits for Studio when a navigation needs a new render), so every response is a `slaw_host_respond` call from any goroutine. WebKitGTK calls are version-guarded (`WEBKIT_CHECK_VERSION`) so the oldest 4.1 builds still compile. `localStorage`/IndexedDB are off as settings, so they are `undefined` in the page (P1 saw the same).
- **What Studio sends.** The entry document is rendered before the helper starts (a render error refuses the open with the renderer's fixed text), then `open`, every non-template file, `_slaw/theme.css`, the vendored libraries the document or a script names (`slaw-lib/<file>.js`), the document, `go`. A `?query` link is a `doc` request: Studio renders again under the same 4-in-flight / 60-a-minute limiter as `slaw.run`, and answers with the page or the fixed error page. **The P2 vet and the P3 render check did not know a script's `src`**: a scripted page could not render, and `slaw-lib/` could not be named. Both now allow `<script src>` for a top-level `.js`/`.mjs` of the page and for `slaw-lib/<name>.js` (`scriptSrcOK`, one rule for both). `_slaw/theme.css` is linked first in the document's `<head>` by the host (a template can't name it); it is built in Go from `--slaw-*` roles the frontend reads off the window (a validated name and a plain value; nothing that loads or closes a declaration).
- **Watchdog.** The page posts a heartbeat every 500 ms; 4 s without one (25 s to the first) ends the window. A message flood (more than 400 a second) or any protocol violation also ends it. Window ends are the `pagewindow:state` event (`open`, `stopped`, `changed`, `closed`); a change to the artifact (watcher), a revoke or deny, a workspace close and Studio quitting all end the window at once.
- **`slaw.run` outside the template list** (a manifest query the templates do not use) is allowed and logged at info level. `slaw.use(name)` resolves `null` for every name (P5 fills `open.caps` and `call`; every `call` is "unavailable" until then).
- **Linux, real window and real gate.** `studio/pagehost_live_test.go` (skipped unless `SLAW_PAGEHOST_LIVE=1`; the test binary is the helper) and the real app under `task studio:dev`: the P1 hostile page as an allowed scripted page, with a loopback listener on the host: 13 groups, **zero** accepts, requests or packets; the same page with the network namespace left out reaches the listener (the instrument works); the helper's environment is exactly the list above and no secret of the parent's; fds 3 and 4 are held by the helper only; every process of the tree is in a network namespace holding only `lo`; the web process runs under WebKit's own sandbox (`NoNewPrivs 1`, `Seccomp 2`, `CapEff 0`); no socket path outside the allow-list can be connected (probe from inside the view; `connect()` calls of the helper and the network process traced with gdb); `while(true){}` is killed at about 6 s with the whole tree, no survivor. macOS is written but not built or run (`P4-MAC-QA.md` in the job directory lists what a Mac must check). Not verified: Ubuntu 24.04 (AppArmor's user-namespace restriction, which must produce the fixed refusal), the oldest supported WebKitGTK, STUN (`RTCPeerConnection` is not built into this WebKitGTK).

#### Step P5. Capabilities: `state`, `download`, then `ask`

- **`state`**, local scope (H5), and **`download`**, with the gesture check. **`ask`** comes last, behind its separate toggle and in the activity log.
- **Tests:**
  - `state`: the caps; a value over 64 KB refused; compare-and-write under two windows; the file lands in `.slaw-local`, never in the folder.
  - `download`: refused without `userActivation`, and a cancel resolves with no write.
  - `ask`: `null` without a provider; no tools are ever sent (assert the request a fake provider receives); the rate limit.
- **Review first:** any capability reachable without its declaration; `ask` reaching the tool registry; `state` used as a channel between artifacts (keyed by name, so it is per artifact; check that the name comes from Studio, not the page).

**As built (P5, 2026-10-21).**

- **Wire.** Two helper-to-Studio types and one back. `put` is a chunk of a call argument that does not fit a 64 KB line (a 64 KB state value, a 32 KB prompt, a 20 MB file as base64): the injected API sends `{t:"put", up, seq, s, last}` chunks one at a time, each acknowledged with an ordinary reply before the next, then the call names the upload with `up` instead of `arg`. Studio assembles in order, at most 2 uploads and 28 MB each, and takes it at the call. Chunks have their own flood bound (3,000 a second) beside the 400 for other lines. `push` (Studio to helper) delivers a capability event (`state.onChange`) by `window.__slawEvent`. New error codes `gesture` and `model` with fixed texts.
- **Granted set.** `open.caps` is what the manifest declared, minus `ask` when its toggle is off; the helper's `slaw.use` resolves `null` for the rest, and Studio refuses a call to a capability the window was not granted, so a page that bypasses the API gets "That is not available."
- **`state`** is `internal/pagestate` (a new leaf): one store per artifact name and workspace, debounced 300 ms, flushed at window and workspace end, merged on conflict. The name comes from the closure `pageServices` builds at open. `onChange` fans out to every window of the same artifact. There is only one window per artifact today (a reopen replaces it), so the fan-out is exercised by a second writer (another store, in the tests and in the live run), not by a second window.
- **`download` gesture: deviation from the plan.** The plan says the injected API reads `navigator.userActivation.isActive`. Measured in the real helper (WebKitGTK 2.5x), `isActive` is **true inside any reply Studio delivers** (the helper's `evaluate_javascript`) and for a while after, so a page could `await slaw.run(...)` and then save with no person involved. A check in the page's API is also no gate at all, since a page can post to `window.webkit.messageHandlers.slaw` directly (found in review). The gate is native: the helper records the person's physical input on the web view in C, outside the web process (`pagewindow_linux.c`: button-press, touch-begin and key-press that is not Escape or an auto-repeat; `pagewindow_darwin.m`: an `NSEvent` local monitor for mouse-down and key-down, same exclusions), and `pageHelper.message` forwards a `download` call to Studio only if `slaw_host_take_input()` finds one within 5 s and consumes it (one input, one save). Without one the helper answers `gesture` itself. Studio adds a per-artifact dialog throttle (none for 10 s after a cancel, 3 a minute). The injected API's own trusted-event tracker stays as a courtesy. A real click is needed to show it live (see below). The macOS monitor is unbuilt and needs a Mac run.
- **`download` in Studio** is `pagecaps_download.go`: closed types with signature checks on png and pdf, the clean file name, one dialog at a time, and the decision asked again after the dialog returns (not the half-second cache) before the write. The dialog is Wails' `runtime.SaveFileDialog`, parented to Studio's window (the page window is another process).
- **`ask`** is `llm.Runner.Complete`: `model.Generate` with a system and a user message and no `Tools`; a test asserts the request a fake provider receives has none and the output cap. `llm.Configured` decides "no provider" (a cloud provider with no key, a local one with no endpoint). The toggle is `pageask.json` beside `views.json` (`workspace.PageAskEnabled` / `SetPageAsk`): on at allow time unless the person turned it off before (a deny that survives edits), off after an edit until the next allow. Settings > Workspace > Custom views and pages has a checkbox per page that declares `ask` (P6 named the provider in the prompt and renamed the section Pages). There was no agent activity log in the code, so there is now a small one: `agent activity` at info level, an `agent:activity` event and `ListAgentActivity` (a 100-entry ring; P6 shows it as *Recent model use by pages* in Settings).
- **Tests.** `internal/pagestate` (caps, 64 KB refused, two stores merged, debounce, subscribers, a corrupt file left alone, bad names), `internal/pagehost` (shapes, and the injected script played under node against a fake webview: grants, gesture, chunking with surrogate pairs, events, a frozen API), `internal/llm` (no tools, the output cap, no key in an error), `internal/workspace` (the toggle), `internal/bundle` (state never in a bundle), `studio/pagecaps_test.go` (undeclared capabilities unreachable, state round trip and file location, uploads in order and capped, download cancel and refusals and revoke-during-dialog, ask null and toggle and rate, no detail in a failure, state not a channel between artifacts), and `TestLive_CapabilitiesInTheRealWindow` (real helper: state across a reopen, a push, the gesture refusal, ask, a refused 70 KB value).
- **Live, real Wails window on Linux (GNOME on Wayland).** The page's `state` persisted across Close and Open (`count` 1 then 2), `ask` reached a fake OpenAI-compatible server through Studio with the request `{messages: [system, user], max_tokens: 4000}` and no `tools`, a 70 KB state value was refused, and the activity line was logged. **The native save dialog was not seen**: this session has no monitor, no screenshot permission (the shell denies it) and no input injection (mutter refuses a remote-desktop session, a virtual keyboard reached no window), so the dialog path is covered by the Go tests with a fake dialog and needs a person at a desktop.

#### Step P6. Trust prompt, agent tool, skill, bundle, CLI, docs

- The H6 prompt (including the macOS network disclosure) and Settings > Workspace > Pages; H7's `write_artifact` and `get_artifact` changes, the skill section, bundle entries and `slaw artifact view`. Docs:
  - `docs/reference/artifacts.md`: an *HTML pages* section with the `FuncMap`, the hosts, the capabilities, the CSPs, the macOS network limitation and the shared keychain identity (H4, #189);
  - `agent-policy.md`: pages are content and never run for the agent;
  - `workspaces.md`: layout and bundle entries;
  - user docs: the trust prompt and the warning about a page asking for a credential;
  - the `CLAUDE.md` tree and *Key concepts*;
  - `direction.md` §9.
- **Rodney:** an agent-written static page shows as source; allow; it renders; edit it on disk and it asks again.
- **Live in the Wails window:** a scripted page with `state`, `download` and `ask`.

**As built (P6, 2026-10-22).**

- **The allow prompt** is `PagePrompt.vue` around the existing `PageFiles`: the host line, the queries with their connections (`ArtifactViewStatus.query_uses`: **every manifest query for a scripted page**, since `slaw.run` reaches any of them, with the templates' marked; the templates' own for a static page). The prompt text travels inside `ArtifactViewState` (one read with the hash) and Allow is off without it (review fix). `NetworkDisclosure` names the LAN and this computer, each declared capability with a plain line, the macOS disclosure (Go decides by `runtime.GOOS`, so the frontend needs no platform test) and the credential line. The sentences are in `internal/artifacts/pageprompt.go` and are shared with `slaw artifact view status`; the one dynamic part is the provider named for `ask` (`config.LLMConfig.AskTarget`: label and model, a local server by scheme and host only; "whatever model provider is set up later" when `llm.Configured` is false). Studio gets them in `ArtifactViewState` (its `disclosure`), built from the same read as the hash. `ask` stays on at allow time, as H5 decided; the prompt names the provider and Settings turns it off.
- **Settings > Workspace > Pages** (the old *Custom views and pages* section): each decision shows its kind and declared capabilities, Revoke, the per-page ask toggle, and *Recent model use by pages* (the 100-entry activity ring: page, time, bytes sent and received, outcome; never text). The orchestrator removed `ListAgentActivity` from P5 because nothing called it; this list is its caller.
- **The agent tool** is `internal/agents/handler_artifact_pages.go`. `html_files` is `{path: text}`; a value `{"keep": true}` keeps a stored file that `get_artifact` returned without text (one file over 30,000 bytes, or text beyond 120,000 bytes in all, comes back `text: null, text_omitted: true`, never a prefix). **Deviation:** the plan says nothing about binary assets already in a page; since the agent cannot send them and a page write replaces the file set, the tool carries stored binary assets (and non-UTF-8 files) over unchanged when the write has the `rev`, and cannot remove them. A binary path in `html_files` is refused. The write is validated (`VetPage`) before the SQL policy so the model gets a usable error, vetted again by the store, and is a compare-and-write on the page `Rev`. The skill's HTML examples are tested: the test writes them through the tool and renders them.
- **Bundles.** `classify` accepts `artifacts/<id>/html/<path>` when `artifacts.CheckPageFilePath` accepts the path; a page is `artifact.yaml` plus its files with no view file, and `PageFromText` (decode, vet, seal) runs on the whole set while the bundle opens. P2's "export skips pages" is gone. The 10 MB per entry and 50 MB total caps and the 2,000-entry cap are unchanged (a page has at most 201 entries). Overwrite replaces the whole page.
- **CLI.** `slaw artifact view status` prints the kind, the queries with connections, the capabilities with the same plain lines, the files and the text; `list` already printed `html (static)` / `html (scripted)`.
- **Not done / for the owner:** the macOS items (done, see *Owner QA on macOS (2026-10-22)* below), a real-desktop run of the save dialog (the P5 note still stands unless a person ran it), and the Ubuntu 24.04 and oldest-WebKitGTK checks from P4.

**Owner QA on macOS (2026-10-22).** The owner ran the whole checklist on macOS 26.6.2 (build 25G83), an Apple M5, at `d4314d1`. Evidence is on branch `spike/html-pages-p1`, in `spike/pagehost/mac/qa-2026-10-22/`.

- **Held.** The page window built and ran on a Mac for the first time with no code change. The four layers held, each with a control that proved its instrument: zero listener hits and zero DNS lookups in the 14-group matrix except the IP-literal link clicks, and 47 of 47 helpers logged their rule list before the first navigation. The WebContent process dies with the helper (a busy one was gone within 5 s of the watchdog kill). The native gesture gate was verified, including Escape, key repeat, a raw `postMessage` and the throttle. The open panel is refused. `ask` sends no tools. A bundle carries no state and no decisions, and binary assets are byte-identical after import. The static host's meta CSP and isolation held in the production window.
- **The leak, measured, and covered by #189.** A script-clicked IP-literal link, **with or without `target=_blank`** (the vet refuses `target` in a template, but script can add it), opens a connection to the chosen address. Plain HTTP: one empty connection per click. `https://` to an IP: a TLS ClientHello (ALPN h2 and http/1.1, no SNI since the target is an IP) and four connections per click. The reach includes loopback, the LAN and a non-private address (a TEST-NET address on a `lo0` alias was reached). Host-name links stay at zero. The owner decided the `target=_blank` case is the same accepted leak, not a regression.
- **The dead proxy port does not refuse at once on 26.6.2.** A connect to the bound, non-listening port hangs about 7.8 s and then fails with ETIMEDOUT (the unbound-port control is refused in 0 s). There was no failover: two 125 s holds showed no lookup and no connection, so `nw_proxy_config_set_failover_allowed(false)` holds. A host-name load therefore hangs until it times out. The reference doc and the helper's comment now say so.
- **Fixed after the QA** (branch `fix/html-pages-mac-qa`):
  - `slaw-rules-<pid>` folders piled up (71 in one session) because every Studio-side close SIGKILLs the helper's process group before its `atexit` runs. Studio now removes the folder after it reaps a helper (the path is Studio's temp directory plus the pid it started, passed to the helper in `SLAW_RULES_PARENT`, the helper's one environment variable, set only when Studio has an absolute `$TMPDIR`; never helper-sent text; with none, the helper uses its per-user temporary folder and nothing is removed or swept by path), and a starting helper sweeps `slaw-rules-<digits>` folders in that directory whose pid is gone (a real directory it owns, never a link). A close the person asks for (Close window, the helper's own Close or Quit) sends the helper `quit` and kills the tree only after a one-second grace; a revoke, change or close-all during that second kills at once, and the closing window's state writes and calls are refused. Every other end kills at once.
  - A file dropped on the page window gave the page the file's name, size, type and modified time. The view now refuses drops on both platforms (macOS: no registered drag types and the dragging methods return none/NO; Linux: `gtk_drag_dest_unset` plus `drag-motion` and `drag-drop` handlers that refuse).
  - Studio's parameter controls and name fields now set `autocapitalize="off" autocorrect="off" spellcheck="false"` (macOS had turned `main` into `Main` and `winopen` into `win open`).
  - Cmd-Q in the page window is Quit with the same action as Close (the helper ends, Studio is untouched); it did nothing before.
  - `slaw export bundle` counts artifacts by item, not by file ("3 data artifacts" for 2).
  - An open artifact view clears "Changed since you allowed it" when a file is restored (the new read is `allowed`, so there is nothing to approve and it is adopted at once).
  - The helper's working directory is `/`, not Studio's.
  - A helper that exits with code 5 (the rule list will not compile) or 6 (no proxy socket) shows "This page window could not be set up on this Mac." A missed heartbeat or a crash keeps "This page stopped responding". The pipe closing just before the process is reaped is why the manager waits up to 500 ms for the exit code.
- **Not run:** macOS 14 (the oldest supported) and macOS 13 (the refusal), and a bundle exported on another machine (the export was made on the same Mac). Exit code 5 itself was not observed in the run (the behaviour was). The fixes above need a re-check on a Mac: `MAC-RECHECK.md` in the job directory lists it.

## Security: what the adversarial reviewer should hit

- **Data becoming markup.** This is the attack a shared workspace makes likely: a row in a shared database, or a value in `state`, that carries `<script>`, `javascript:` or `</script>`. Every route in from data must go through `html/template` escaping or `textContent`. No trusted-type helper may exist (H2 lint).
- **The template as code.** The template is the author's code. Its markup is untrusted (the host's CSP is the gate), and its `FuncMap` is the only API.
- **The query boundary.** Neither the template nor the script reaches SQL, a connection name or another artifact. Studio pins the artifact by name. Every value goes through `Bind`; every run goes through `RunArtifactQuery`'s gates. Row caps and rate limits hold.
- **Exits.**
  - Things that must be **impossible**: network on Linux, because the helper has none to use (namespace, then CSP); navigation away; a new window; the clipboard read; the Wails bridge; a static page reaching another artifact's render.
  - Accepted on macOS (H4, #189): every load, lookup and request is blocked, but a script-clicked IP-literal link can open a TCP connection to an address the page chose.
  - Things that may happen only **after consent or a gesture**: `download`, through the save dialog; `ask`, to the named provider; `state`, local disk only.
  - Things that **remain**, because the person does them: copying text, printing, drag-out, screenshots.
- **The helper process.** It has no credentials, environment, workspace path or file descriptors beyond its pipes. It is killed on a missed heartbeat or a change, and it dies with Studio. A WebKit compromise inside the helper still runs as the person's OS user. WebKit's own web-process sandbox is the defence in depth: bubblewrap on WebKitGTK, which the helper turns on because Wails doesn't, and the platform sandbox on macOS. P1 confirms it is on. On macOS the helper shares Studio's keychain identity until it becomes a separate executable (H4, #189).
- **Impersonation.** A page window that draws a fake "Sign in to prod_pg" form. The native title is fixed, Studio never asks for a credential in a page, and `ask` and `download` make a typed password leave only with consent. Say so in the trust prompt.
- **Trust.** `PageHash` covers `artifact.yaml`, every page file, the kind and the capabilities. The served bytes are the hashed bytes. The agent can't allow a page. A clone at another path asks again, and a bundle never carries a decision.

## Later

- **Shared `state`, backed by a writable connection** (a DuckDB file under `data/`, or a named connection), with the write gates. Never committed files (H5).
- **Embedding a scripted page inside Studio's window.** This would need two things: Wails fixing #174 (main-frame-only IPC on Linux; macOS already drops sub-frame messages, #182), and WebKit site isolation for frames, so a loop can't freeze Studio (the iframe failed (b) on both platforms). Or a move to Wails v3 multi-window. Revisit when either lands.
- **Binary assets from the agent** (base64, with type sniffing and caps).
- **A page as a notebook cell**, embedded by name with fixed parameters (the static host only).
- **Exporting a static page's render to HTML or PDF**, at the person's current parameters. It would be a snapshot made on request, never stored in the folder.
- **Per-page row caps above 500**, with a hard ceiling (base plan *Later*).

## Open questions

1. **Is a separate window acceptable UX for scripted pages?** An embedded view isn't possible without frame isolation and, on Linux, #174 (*Later*). The static host covers in-Studio pages.
2. **Native code. Answered:** we own it (H4). `webview/webview` has no navigation policy, custom scheme or ephemeral store, and it registers its own handler. The macOS network gate is **answered** too: no `sandbox-exec`; content rules plus a dead proxy, with the IP-literal click leak accepted until #189.
3. **The `slaw-lib/` set and versions**, shared with the worker view (base plan open question 5).
4. **The CSP narrowing to `frame-src …/slaw-view/`** may not be honoured for paths on a custom scheme. If it isn't, `frame-src 'self'` stays, and the static frame can still navigate to Studio's own page. That is harmless without `allow-scripts`. **Answered for the dev origin:** P1 showed the narrowing works on both platforms; P3 re-checks it in a production build.
5. **`ask` defaults.** On at allow time (H5 gives the reason). The owner may prefer off for remote providers.

## Spike P1 (Linux run 2026-10-18; macOS run 2026-10-20/21)

**Setup.** The run used Fedora with WebKitGTK **2.54.1**, the real Wails window for the static host, and a Go + cgo WebKitGTK helper spawned by a stand-in parent for the scripted host. The branch is `spike/html-pages-p1` (throwaway; never merged).

**Method.** Ground truth was a loopback listener that logged every accept, request line and UDP packet, plus the Go log. Every vector set was first run against a control (no CSP, no sandbox) to prove it fires. The scripted host's layers were measured one at a time: CSP and settings only; network namespace only, with a permissive CSP; then both together.

An Opus verifier re-checked the Linux logs and rated the results **trustworthy with corrections**; an Opus review of this plan update checked the macOS claims against the mac logs. Those corrections are applied here and in H2-H4.

### Static host (H3)

| Check | Linux (WebKitGTK 2.54.1) |
|---|---|
| Inline script, `onerror`, SVG script, `<object>`/`<embed>`, iframes, `poster`, audio, `<base>` | **Blocked.** The control run made 20 listener hits; the H3 CSP + `sandbox=""` + a1 made none. `sandbox=""` alone stops all script, Wails IPC and `BrowserOpenURL` included, but non-script loads still go out, so the CSP is needed. |
| `javascript:` URL click | **Blocked** under a1, with a real click. The control reached the listener. |
| `<meta refresh>` (to outside, `javascript:`, self, `/`) | **Blocked** by the script-less sandbox, even without a1. |
| A person's click on an outside link | **Needs a1.** Without a1 it reached the listener; with a1 there was no hit. |
| a1 narrowed to `frame-src <origin>/slaw-view/` | **Works**, as a static meta with the dev origin. Studio boots, the frame and its links work, and a link to `/` or `/index.html` loads nothing. |
| Links re-render the page | **Verified.** |
| Form submit (GET and POST) | **Blocked.** |
| `assets/` source under the opaque origin | **Matches.** CSS, PNG and TTF loaded, and `assets/ok.js` was not requested. |
| `<img>` and CSS `url()` pointing at `/page` | **Blocked by the CSP.** The route can't check `Sec-Fetch-*`, because the handler receives none, so it relies on the rate limit. |
| The frame navigating to `/` or `/index.html` | Under plain a1: one static GET, no sub-requests, no script, nothing reaches Go. Under narrowed a1: nothing. |
| Wails runtime injection | **None.** |
| `<link rel=preconnect>` and a `target=_blank` click | **Open a TCP connection under every layer.** Hence H2's URL allow-list. |

**macOS (26.6.2, Apple silicon, WKWebView):** the same results row for row, with the same dev origin. The differences:

- `<link rel=dns-prefetch>` also resolves the name. With an `https://` name, preconnect and a `target=_blank` click also send a TLS ClientHello carrying the name.
- WKWebView sends no `Sec-Fetch-Dest` either.
- Wails' all-frames user script does not run in a frame without `allow-scripts`.
- Studio's App Transport Security (`NSAllowsLocalNetworking` only) refuses plain-HTTP names before any lookup. It helps, but it isn't a control to rely on.
- Content rule lists can't be added, because Wails v2 doesn't expose its WKWebView configuration. So the H2 allow-list is the only defence against hint leaks here.

**Decision: GO on both platforms, with conditions.** a1 ships narrowed and is emitted at build time; the route relies on the rate limit; the H2 URL allow-list applies. P3 re-verifies in a production build on both platforms.

### Scripted host (H4)

| Check | Linux |
|---|---|
| CSP response header on a custom scheme | **Honoured.** The headers must include `Content-Type`. |
| CSP only (no namespace) | Blocks `fetch`, XHR, WebSocket, EventSource, beacon, `<img>`, CSS `url()` and `@import`, `<a ping>` and form submit. It **does not** stop navigation (`location =`, `window.open`, an anchor click, a script-added `<meta refresh>`). |
| `decide-policy` | **Closes** all five navigation vectors. |
| Settings (DNS prefetch, hyperlink auditing) | **Inert** (deprecated no-ops). They are not a layer. |
| Network namespace only (permissive CSP) | The host listener saw **nothing**. The same namespace with a reachable listener showed every vector fires, so the silence came from the namespace. |
| All layers together | **No hits.** |
| DNS | **Escapes** through systemd-resolved's varlink socket, under both a user+net namespace and `bwrap --unshare-net`, regardless of the policy decision. A tmpfs over `/run/systemd/resolve` closes that socket, but `/run/avahi-daemon/socket` and the system bus (`org.freedesktop.resolve1`) still connect. Hence H4's allow-listed filesystem view, which **P4 must verify end to end**. |
| Protocol fd | It **leaks** into `WebKitNetworkProcess`, `xdg-dbus-proxy` and the web process unless it is `CLOEXEC`. With `CLOEXEC`, only the helper holds it. |
| `while(true){}` | The watchdog fired at 2.0 s, but SIGKILL to the helper **orphaned** a spinning web process and its bwrap parents. A process-group kill or `CLONE_NEWPID` left no orphans. |
| Web process sandbox | **Off unless `set_sandbox_enabled(TRUE)`.** With it on, the web process runs under bwrap in its own user, pid and net namespaces, with seccomp 2, NoNewPrivs and no capabilities. `WebKitNetworkProcess` is never sandboxed. `HOME=/root` breaks WebKit's bwrap. |
| Frames posting to the handler | **Possible** with a permissive CSP (both `srcdoc` and `src`). `frame-src 'none'` and `decide-policy` each stop it. |
| Environment | The minimum is `XDG_RUNTIME_DIR` (plus `WAYLAND_DISPLAY` if it isn't `wayland-0`). Wayland and the session-bus path sockets work inside the namespace. |
| Native title vs `document.title` | **The title is fixed.** |
| Ephemeral store | Nothing survived between runs: no localStorage, cookie or IndexedDB. |
| d3 and vega-lite under the strict CSP | **Render**, with and without the namespace. |
| WebRTC and STUN | **Not verified.** `RTCPeerConnection` is not built into this WebKitGTK. |

**macOS (26.6.2, Apple silicon), full results in `spike/pagehost/mac/P1-MAC-RESULTS.md` on the spike branch.** No single layer is enough:

| Layer | Closes | Leaves open |
|---|---|---|
| Strict CSP + navigation policy | subresources, forms, navigations | `dns-prefetch`, `preconnect`, DNS + TCP on refused navigations |
| `WKContentRuleList` (block all but `slaw-page:`) | every load, plus `dns-prefetch` and `preconnect` | DNS + TCP on a script-clicked outside link |
| `proxyConfigurations` (dead loopback port) | host-name resolution for loads and link clicks (not `dns-prefetch`) | IP addresses (loopback, LAN and a non-private TEST-NET address reached in the owner QA) |
| `sandbox-exec (deny network*)` | all TCP/UDP from the WebKit services | DNS |

Under rules + CSP + policy only the `main` and link-click groups were run: zero except the click. The other groups are inferred from the `pol` and `allprx` runs. Adding the proxy closed the click by name, leaving only the IP-literal click (`RP.alink`: one accept, no request over plain HTTP). P4 runs the full 14-group matrix on exactly this set. Adding `sandbox-exec` gave zero in all 14 groups. Other checks:
- The handler refused both frames.
- The freeze was caught at 2.09 s, and the WebContent process died with the helper.
- The helper's environment was empty, and the XPC services hold none of the parent's sockets.
- Storage was ephemeral, and the native title stayed fixed.
- d3 and vega-lite rendered under the rules.
- Dock: no icon, and the helper window came to the front. Cmd-W was not tested.

**Decision: GO on Linux, only behind the H4 network gate.** The gate is a user+net namespace plus an allow-listed filesystem view, together with `CLOEXEC`, a tree kill, `set_sandbox_enabled(TRUE)`, the uid mapped to itself, and `frame-src 'none'` plus `decide-policy`. **GO on macOS with the accepted leak** (owner, 2026-10-21): the CSP, the policy, the content rule list and the dead proxy, without `sandbox-exec`, because it is deprecated. The IP-literal click leak is documented (H4) and tracked in #189.

**Scope.** The Linux results come from Fedora with WebKitGTK 2.54.1, the macOS results from macOS 26.6.2 on Apple silicon only; P4 re-runs on macOS 14. Before P3 and P4 ship, they are re-run on Ubuntu 24.04 (AppArmor's user namespace restriction) and on the oldest WebKitGTK Slaw supports. Both hosts fail closed.

