# Workspaces and connections: Studio design spec

**Issue:** #106 (design pass) · **Epic:** #105 · **Feeds:** #111 (Slice 1 Studio shell), #112, #116, #117 (Slice 2 connections UI)
**Plan:** [`docs/plans/workspaces-and-connections.md`](../../plans/workspaces-and-connections.md) §4.1, §4.2, §4.5, §4.8 and the spike amendments in [`spike-duckdb-hub-findings.md`](../../plans/spike-duckdb-hub-findings.md)
**Mockups:** open [`index.html`](index.html) (light and dark toggle on every page). Screenshots are in [`shots/`](shots/).

The mockups are static HTML built from the real Studio stylesheets (`schoolhouse/*.css`, `theme/bridge.css`) and the `--vibe-*` roles from `theme/builtin.ts` (light = SchoolHouse, dark = SchoolHouse Dark). They are documents, not shipped code. To regenerate after editing the sources: `node docs/design/workspaces-connections/build.mjs`.

| # | Page | Shot |
|---|---|---|
| 01 | [Launcher](01-launcher.html) | `shots/01-launcher-{light,dark}-1280.png` |
| 02 | [New workspace](02-new-workspace.html) | `shots/02-…` |
| 03 | [Import .vibe](03-import-vibe.html) | `shots/03-…` |
| 04 | [Shell + switcher](04-shell-switcher.html) | `shots/04-…` (+ `-900` narrow) |
| 05 | [Connections rail](05-connections-rail.html) | `shots/05-…` (+ `-900`) |
| 06 | [Connection editor](06-connection-editor.html) | `shots/06-…` |
| 07 | [States](07-states.html) | `shots/07-…` (+ `-900`) |
| 08 | [Query editor + SQL cells](08-query-editor-cells.html) | `shots/08-…` |
| 09 | [Settings split](09-settings.html) | `shots/09-…` |
| 10 | [Agent approval card](10-agent-approval.html) | `shots/10-…` |

## 0. Principles and shared rules

**Tone.** Plain words, written for the three persona-panel users (Rosa the clerk, Marcus the citizen developer, Priya the consultant). No "container", "DSN", "attach", "catalog alias", "DuckDB blob" in primary copy. Technical detail sits behind "Show details". Error text says what happened and what to do, never "corrupt database" (SYNTHESIS T9). Every place a secret could be mentioned says where it is kept ("your keychain") and that Vibe can't show it again.

**Look.** Everything is existing SchoolHouse: 2px borders, hard offset shadows, Fraunces headings, Inter body, JetBrains Mono for paths, names and caps labels, Lucide icons through `Icon.vue`. No hex: every colour is a `--vibe-*` role or a bridged `--sh-*` token, translucent tints use `color-mix()`. New classes are listed in [`mock-additions.css`](mock-additions.css) under "[new]" and are written so they can be lifted into `schoolhouse/` unchanged.

**Status is never colour alone.** Each connection state has its own icon shape, a text name and a tooltip (see 05 C). Scope is always a text badge, in plain words: **Team**, **This computer**, **All my workspaces**. (`shared`, `local` and `user` stay the file-level terms; they are never shown in the UI, and the word "local" in the UI only ever means the workspace's own `local` database.)

**Focus.** Use the existing single `:focus-visible` ring from `theme/bridge.css`. Every dialog uses `composables/useModal.ts` (focus moves in, Escape closes, focus returns to the opener). Menus and the tree add roving focus (below). Nothing sets `outline: none` without the ring.

**Autosave replaces Save.** Notebooks, queries and skills are files and save as you go, and connection data lives in the connection, so the Save / Revert / dirty pill from the container era (#81, #98) is gone from the shell. The sidebar footer shows "Saved as you go", the folder, and the git branch. (review #111: the changed-file count is dropped. Studio never runs `git` on open, because a shared folder's `.git/config` — `core.fsmonitor`, filters — can execute commands. The branch is read from `.git/HEAD` in pure Go.)

**Narrow windows (≤ 1000px).** The nav collapses to a 56px icon rail; the Connections tree becomes a drawer opened from the plug icon (05 A at 900px, `shots/05-…-900.png`); the top bar drops the branch chip and the agent pill. This answers the "two fixed rails eat 44% at 1100px" complaint (T9).

## 1. Launcher (replaces `UnlockView`)

**Purpose.** The first screen when no workspace is open. Get back into recent work, or start, in one click. Absorbs #56 and the Unlock items of #89 / T9.

**Components.**
- Brand row (existing wordmark mark).
- Recent list: `<ul role="list">`, each item a row `button` plus a "Remove from list" icon button. Row shows folder icon tile, name, path (mono, truncated middle-first when long), git branch chip or "no git", last opened ("2 hours ago", relative).
- Three action cards: Open folder…, New workspace…, Import a .vibe file… (existing `.card` + `.btn` look, new `.act-card`).
- Trust line (shield icon).
- First-run hero (existing `.empty-hero`) plus three mini cards.

**States.**
- Recents (A), first run (B), missing folder row (A), plain-folder confirmation (C).
- Missing folder: dashed icon tile, "Folder not found" badge, **Locate…** (re-points the entry) and Remove. Never auto-removed. The row is not openable.
- Opening takes time: the row shows "Opening…" and the rest of the list is inert.
- Open fails (not a workspace, unreadable `vibe.yaml`): inline `.banner attn` under the list with the plain reason and "Show details".

**Final copy.**
- Lead: "Open a folder to work on your data. Notebooks, saved queries and skills live in that folder as plain files."
- Missing: "Old Ledger's folder isn't there anymore. It may be on a drive that isn't plugged in. Locate it, or remove it from this list. Removing never deletes files."
- Trust line: "Your data stays where it already lives. Vibe keeps passwords in this computer's keychain, never in the workspace folder, so a workspace is safe to put in git or share with a teammate."
- First run heading: "Pick a folder to start". Body: "A workspace is just a folder. Your notebooks, saved queries and skills are kept in it as ordinary files, so you can back it up or share it with git. Your data stays wherever it is now."
- Plain folder dialog: title "Make this folder a workspace?" Body: "This folder doesn't have a `vibe.yaml` yet. Vibe will add one, plus `notebooks/`, `queries/`, `skills/` and `data/`. Your other files are left alone and nothing is moved." Buttons: Cancel · **Make it a workspace**.

**Keyboard and focus.** Initial focus on the most recent row (or New workspace when empty). Up/Down move between rows, Enter opens, Delete or Backspace removes from the list (with a toast "Removed. Undo"), Tab reaches the action cards. Ctrl/Cmd+O Open folder, Ctrl/Cmd+N New workspace.

**A11y.** Row buttons have `aria-label` "Open Acme Analytics, last opened 2 hours ago, branch main". The missing row uses `aria-describedby` pointing at the explanation. The remove button's label names the workspace. Dim backdrop of dialog C is `inert`.

**Bindings and events** (#111). `ListRecentWorkspaces() []RecentWorkspace{path,id,name,lastOpened,gitBranch,missing}` (checks the folder exists, never dials anything), `OpenWorkspace(path)`, `RemoveRecentWorkspace(path)` **(new)**, `RelocateRecentWorkspace(oldPath,newPath)` **(new)**, `ChooseFolder(title) string` and `ChooseFile(title, filters) string` **(new, native dialogs)**, `InitWorkspaceInFolder(path)` **(new, plain-folder case)**. Events: `workspace:opened`, `workspace:closed`.

**Reuse / new.** Replaces `views/UnlockView.vue` with new `views/LauncherView.vue`. Reuses `.card`, `.btn`, `.banner`, `.empty-hero`, `ConfirmDialog` pattern, `useModal`. New: `RecentWorkspaceRow`, `ActionCard` (CSS only).

## 2. New workspace dialog

**Purpose.** Create the folder, `vibe.yaml`, `data/local.duckdb` and optional git repo. Opened from the launcher and from the switcher menu.

**Fields.**
- **Name** (required), help "Shown in the window title and the workspace menu."
- **Where to put it** (text + Choose…), default is the last used parent folder, help "Vibe creates a new folder here."
- **Will create** preview tile: `~/work/acme-analytics` (slug of the name).
- **Start a git repository** checkbox, on when `git` is found. Help "So you can track changes and share notebooks with your team. Vibe never commits or pushes for you." When git is missing the box is disabled and the help reads "git wasn't found on this computer."
- **Start from** radio cards: Blank (default) · Contacts · Inventory · Order entry · Training registrations (from `container.TemplateDescription`, one plain sentence each).
- Info note listing exactly what is created.

**States.** Default (A). Folder exists (B): "There's already a folder called acme-analytics in ~/work. Pick another name or location. Vibe won't put a workspace inside a folder that already has files in it, unless you use Open folder instead." The Where field gets `aria-invalid` and `aria-describedby`, Create is disabled. Creating: button "Creating…", fields disabled. Other errors (no permission): same attn note with details link.

**Keyboard and focus.** Focus lands in Name. Enter in a text field submits when valid. Escape cancels. Template cards are a radio group (arrow keys). On a validation error focus goes to the first invalid field.

**Bindings.** `CreateWorkspace({name,parentDir,git,template}) → WorkspaceInfo`, `ListStarterTemplates()` **(new, wraps the template registry)**, `CheckGitAvailable() bool` **(new)**, `ChooseFolder`. Events as above.

**Reuse / new.** New `NewWorkspaceDialog.vue`; reuses `.modal-overlay`, `.field-input`, `.card.is-selected`, `useModal`.

## 3. Import .vibe flow

**Purpose.** Bring an older locked `.vibe` (formatVersion 1, SQLCipher) or a shared bundle (formatVersion 2) into a **new** workspace (plan §4.6, §4.8). Importing a bundle into the open workspace with rename / overwrite / skip is a separate #118 flow and reuses this dialog's preview step.

**Steps.**
1. **Unlock** (legacy only; unencrypted bundles skip to step 2, encrypted bundles show this step too): file tile, passphrase field, warning.
2. **Preview:** workspace name and location, counts (Notebooks, Saved queries, Skills, Tables), a tree of what will be created including `data/<stem>.duckdb` and the connection `<stem>` set as default. Nothing is written yet.
3. **Result:** success banner, what was written, anything to double-check (pages converted by `notebooks.MigratePages`), Open workspace.

**Final copy.**
- Step 1 intro: "Older Vibe files keep their data inside one locked file. Vibe will copy it into a normal workspace folder. The original isn't changed."
- Warning (always visible, not hidden behind a link): "**Vibe can't recover a forgotten passphrase.** There is no reset and no back door: the passphrase is the only key. If it's lost, nobody can open this file, including us."
- Wrong passphrase: "That passphrase didn't work. Check Caps Lock and try again." Details (collapsed): the raw error.
- Preview heading: "Here's what Vibe will create". Sub: "Nothing is written until you press Import."
- Encryption note (shown before the delete-the-original advice): "**In the new workspace your data is a normal file, not locked with a passphrase.** Use your computer's disk encryption if this data is sensitive."
- Preview note: "Your original `harbor-lane.vibe` stays exactly as it is. Delete it once you've checked the new workspace."
- Result: "Imported into a new workspace" / "Harbor Lane Hardware is ready". Lines: "6 notebooks, 4 saved queries and 2 skills were written as files." · "Your data was copied to `data/harbor-lane.duckdb` and added as the connection `harbor_lane`, the default. A new empty `local` database is there too." · "2 older pages were converted to notebooks. Charts and tables carry over, but have a quick look at **Monthly summary** and **Reorder list**." Footer: "The old file's passphrase is no longer needed." Buttons: Show in folder · **Open workspace**.

**Keyboard and focus.** Focus moves to the passphrase field on step 1 and to the Import button on step 2. A passphrase field is `autocomplete="off"`, never echoed, never logged. Back returns to the previous step with entered values kept (except the passphrase, which is cleared when leaving step 1 backwards).

**A11y.** The stepper has `aria-label="Step 2 of 3"`. Step content change is announced with `role="status"`. The wrong-passphrase message is `role="alert"` and linked with `aria-describedby`.

**Bindings and events** (#110, #111). Split so the preview is real and the passphrase crosses the bridge once: `PreviewVibeImport(req PreviewImportRequest{path, passphrase}) → ImportPreview{handle, kind:"legacy"|"bundle", encrypted, notebooks, queries, skills, tables, bytes, warnings}` **(new)**; Go keeps the opened file under `handle` (short timeout) and Vue never holds the passphrase; then `RunVibeImport(req RunImportRequest{handle, name, parentDir}) → WorkspaceInfo` and `CancelVibeImport(handle)`. This replaces `ImportLegacyVibe(path, passphrase, dest)` in #111's list. An imported workspace also gets the empty `local` connection, and the default connection is the imported `<stem>`. Errors are typed (`ErrWrongPassphrase`, `ErrNotAVibeFile`) so the UI never says "corrupt". Event `workspace:opened` on success.

**Reuse / new.** New `ImportVibeDialog.vue` (with a small stepper); reuses `.url-tile`, `.stat`, `.banner`, `.note`. The existing `ImportView.vue` (CSV/xlsx import) is unrelated and stays.

## 4. App shell with workspace switcher

**Purpose.** Switch workspaces at any time without leaving the app. Includes the restart prompt for a different MotherDuck login (D10).

**Layout changes to `AppShell.vue`.**
- Top bar: the "Vibe Studio" wordmark becomes just the mark; next to it the **switcher button** (folder icon, workspace name, git branch chip, up-down chevron). The "Local · Encrypted" pill is replaced by an agent-trust pill ("Agent asks before changes" / "Read-only" when opened read-only). Agent button and theme toggle stay.
- Sidebar: nav on top (Home, Notebooks, Queries, Skills, Agent, Import, Settings), the Connections rail underneath (screen 05), the folder pill at the bottom. **Tables** is removed from the nav because the rail replaces it; **Dashboard** is renamed **Home**.
- Remove Save, Revert, Close file, the dirty pill and the "Open file" label.

**Switcher menu** (`role="menu"`). Sections: Current workspace (check mark) · Recent (name, path · branch, relative time; missing entries disabled with Locate…) · Open folder… (Ctrl/Cmd+O) · New workspace… (Ctrl/Cmd+N) · Import a .vibe file… · Manage workspaces… (opens the launcher). Footer note: "Switching saves your open notebooks first. Nothing you were working on is lost." A recent entry whose MotherDuck login differs from the open one carries a "Restarts" badge.

**Restart prompt (B).** Title "Restart to open Northwind Client?" Body: "Northwind Client signs in to MotherDuck with a different login than Acme Analytics. Vibe can only use one MotherDuck login at a time, so it needs to restart to switch." Note: "Your notebooks are saved first, and Vibe will reopen on Northwind Client by itself." Buttons: Stay in Acme Analytics (default and safe) · **Restart and switch**.

**Flow.** `usePendingSaves.flushAll()` → (any failure: confirm "A notebook couldn't be saved: … Switch anyway?") → `CloseWorkspace` → `OpenWorkspace(next)`. Views re-mount keyed on the workspace session id.

**Keyboard and focus.** Ctrl/Cmd+Shift+O opens the menu (proposed; Obsidian-like). Opening focuses the current item, Up/Down move, Enter activates, Escape closes and returns focus to the button, Home/End jump. The button has `aria-haspopup="menu"`, `aria-expanded`, and an accessible name "Workspace: Acme Analytics. Switch workspace".

**Bindings and events.** `GetWorkspaceInfo()`, `ListRecentWorkspaces()`, `CloseWorkspace()`, `OpenWorkspace(path)`, `SwitchWorkspace(path) → SwitchResult{ok bool, relaunchRequired bool, reason string}` (reason is `"motherduck-account"` for D10) **(new; wraps close and open and does the D10 check without dialling)**, `RelaunchInto(path)` **(new)**. Events: `workspace:opened`, `workspace:closed`, `workspace:git-changed {branch, changed}` **(new; drives the footer pill)**.

**Reuse / new.** Modify `AppShell.vue`; new `WorkspaceSwitcher.vue`, `RelaunchDialog.vue`, and a small `usePopoverMenu` composable (roving focus, Escape, click-away) shared with the rail context menu and the connection selector. Retire `useFileDirty` surfaces for connection data.

## 5. Connections rail

**Purpose.** DataGrip/DBeaver-style explorer: see every connection, expand to schemas, tables, views and columns, start a query, and fix problems in place.

**Structure.** `role="tree"` under a "Connections" header with **+** (New connection) and refresh-all buttons, then a filter field (filters tables and columns of expanded connections), then the tree. Levels: connection → schema → table or view → column. A connection row shows: chevron, type tile (`DK PG MD SQ S3`), name, optional padlock (read-only), optional layered icon (shadows another scope), **scope badge** (Team / This computer / All my workspaces), **status icon**. Table rows show an approximate row count; views say "view"; column rows show type, `PK` (marigold) or `FK` (teal).

**Status icons** (05 C). Shape, colour and text for each; the row's detail line appears only when action is needed.

| State | Icon | Detail line (final copy) | Click or Enter |
|---|---|---|---|
| not connected | hollow circle (muted ink, not the disabled colour) | none | expands and connects |
| ok | filled green circle | none | expands |
| error | alert circle, red | "Can't connect: password was rejected. Fix" (redacted, one line) | opens the editor on its Test result |
| needs-credentials | key, amber | "Needs a MotherDuck token. Add it" | opens the sign-in prompt |
| extension-missing | download arrow, blue | "Needs the httpfs extension. Install…" | opens extension consent |
| locked-by(pid) | padlock, grey | "In use by `vibe mcp serve` (pid 4182)." | offers Take over (see 7 C) |

**Scope badges and shadowing.** **Team** = defined in `vibe.yaml`, git shares it. **This computer** = only you, this workspace (`.vibe-local/`). **All my workspaces** = only you, every workspace (`~/.config/vibe/`). Precedence is most-specific-wins: This computer beats Team, and Team beats All my workspaces. An All-my-workspaces entry never overrides a more specific one, so it can't shadow a Team connection. When a This-computer entry overrides a Team (or All-my-workspaces) one of the same name, the row gets the layered icon and the editor shows "This is your private copy of warehouse…" (06 D). Tooltips: "Saved in vibe.yaml, shared with the team through git", etc.

**Context menu** (D; right-click, the row's ⋯ button, Shift+F10 or the Menu key). Items depend on state: **Connect / Disconnect**, **Refresh** (F5; clears the schema cache), **New query here** (Ctrl+Alt+N; opens the query editor with this connection selected), **Edit…** (Enter), **Test**, **Duplicate**, **Remove…** (Delete; uses `ConfirmDialog`, destructive: "Remove prod_pg? This removes the connection from {scope}. Your database isn't touched. Saved credentials in your keychain are also deleted."). Table rows: **New query on this table** (inserts `SELECT * FROM conn.schema.table LIMIT 100`), **Open as datasheet**, **Copy name**.

**Empty states** (E). The workspace's own `local` database always exists, attaches automatically on open and starts expanded, as do other local DuckDB files in `data/`; the schema level is hidden while a connection only has `main`, so the tables sit directly under it and **Home also lists them** (the clerk path to a table is two clicks).  If it has no tables: "No tables yet. Import a CSV, or ask the agent to build one." If it is the only connection, a dashed card: "**Your data somewhere else?** Connect Postgres, MotherDuck, a SQLite file or cloud storage. Nothing connects until you use it." with **Add a connection**. The main pane shows a first-run hero with Import a CSV or Excel file · Add a connection · Ask the agent.

**Lazy loading.** Opening a workspace dials nothing, so every connection starts "not connected". Expanding connects (spinner replaces the status icon, `aria-busy`). Schema and column levels load on expand from the fingerprinted schema cache; row counts show "~" when estimated.

**Keyboard and focus (WAI-ARIA tree).** The tree is one tab stop with roving `tabindex`. Up/Down move, Right expands (or moves into children), Left collapses (or moves to the parent), Home/End, Enter activates (connect or open), typing a letter jumps to the next match, `*` expands siblings, Shift+F10 opens the context menu. The filter field is reached with Tab and `/` focuses it from the tree.

**A11y.** `aria-level`, `aria-expanded`, `aria-selected`; status icons have `role="img"` and an `aria-label` (e.g. "Needs credentials"); the detail line is inside the row's description so screen readers hear the state before the name is repeated. The narrow drawer traps focus and closes on Escape.

**Bindings and events** (#117). `ListConnections() []ConnectionStatus{name,type,scope,shadows,readOnly,status,detail,pid}`, `ConnectConnection(name)`, `DisconnectConnection(name)`, `ListCatalog(conn, schema?) → nodes`, `RefreshCatalog(conn?)` **(new; wraps the cache clear)**, `DeleteConnection(name, scope)`, `TestConnection(...)`. `ListCatalog` takes one struct, `CatalogRequest{connection, schema string}`, and returns `[]CatalogNode`; there are no optional parameters on any binding. Detail lines use `--vibe-fg`; only the icon is coloured. **Status precedence** when several apply: `extension-missing` > `needs-credentials` > `error` > `locked-by` > `not connected` > `ok`. Events: `connection:status {name,status,detail}` **(new)**, `catalog:changed {conn}` **(new, after DDL or refresh)**.

**Reuse / new.** Replaces the schema tree in `QueryEditorView.vue` and the Tables nav entry. `TableBrowserView.vue` is reached from the rail with a `conn.schema.table` route. New: `ConnectionRail.vue`, `ConnectionTreeNode.vue`, `StatusIcon.vue`, `ScopeBadge.vue`, `ConnTypeTile.vue`. Reuses `.side-section`, `.side-link`, `.sh-hover`, `ConfirmDialog`.

## 6. Connection editor

**Purpose.** Add or edit a connection. A full-page route inside the shell (`#/connections/new`, `#/connections/<name>`), not a modal, so there is room for explanations and the rail stays visible. (Decision 3.)

**Step 1: type picker** (A). A grid generated from the type registry: DuckDB file, Postgres, MotherDuck, SQLite file, Amazon S3, Google Cloud Storage, Cloudflare R2. MySQL and DuckLake are shown greyed with a "Coming soon" badge, `aria-disabled="true"`, not focusable as buttons and not clickable. Note under the grid: "Just have a CSV, Parquet or Excel file? You don't need a connection: put it in the workspace's `data/` folder or use Import, and query it from there."

**Step 2: generated form.** Each type declares sections and fields (name, label, help, kind, secret, per-person, required). The mock shows Postgres (B, C, D) and MotherDuck (E). Sections:
- **Connection**: Name (read-only after creation; lowercase letters, numbers, underscore; help "How you'll refer to it in SQL: `prod_pg.public.orders`"), Host, Port, Database, **Advanced options** (disclosure: SSL mode). Per type: DuckDB/SQLite = file path with Choose…; MotherDuck = database; storage = region/endpoint and bucket prefix (`scope`).
- **Sign-in** (per person): three choices.
  - *My keychain* (default): "Saved once on this computer."
  - *Environment variables*: "Read from `PG_PROD_USER` and `PG_PROD_PASSWORD` when Vibe starts. Good for scripts and servers." Shows each variable with Set / Not set; **never the value**; prefix editable.
  - *Ask me each time*: "Vibe asks when it connects and forgets it when you close the workspace." The prompt offers "Remember in my keychain", off by default (D9).
- **Safety**: the read-only switch (below).
- **Who can use this connection**: the scope picker (below).
- **Test bar**: Test connection · help "Tries these settings without saving. Stops after 10 seconds." · Cancel · Save.

**Write-only credential fields.** Only the *secret* half is write-only (password, token, secret key). Per-person values that aren't secret (username, S3 key id) are shown, editable text fields. One component (`CredentialField`) has three presentations for the secret:
1. *Set:* `•••••••• ✓ In your keychain · Replace · Clear`. There is no reveal and no copy. The dots are decoration and carry `aria-label="Saved, hidden"`.
2. *Replace / first entry:* a normal `<input>` (`type="password"` for passwords and tokens, `autocomplete="off"` or `new-password`), help "Saved to your keychain when you press Save. Vibe can't show it again after you save. You can only replace or clear it."
3. *Env:* no field, just the variable list.
The username is a normal readable field (D7 decision): it is stored with the keychain entry but is not a secret, and the redacted test error keeps it (`user='rosa'`) and hides only the password. Info note: "These go in your computer's keychain. They're never written to the workspace folder, to git, or to logs." When the keychain is unreachable, saving is blocked with the dialog in 7 A2.
**Clear** asks nothing for a draft, and uses `ConfirmDialog` for a saved one: "Clear the saved password for prod_pg? You'll be asked for it next time you connect."

**Test (success and error).**
- Success banner (`.banner.teal`, `role="status"`): "Connected in 0.4 seconds" / "Found 3 schemas and 41 tables. Read-only is working: Vibe could look but not change."
- Error banner (`.banner.attn`, `role="alert"`): "Couldn't connect" / "The server turned down the username or password. Check them and try again. If you're sure they're right, ask whoever runs the database whether your account can reach `db.staging.internal`." Collapsed **Show details** (code block): the DuckDB error with secrets replaced by `[hidden]`, e.g. `Unable to connect to Postgres at user='[hidden]' password='[hidden]' host='db.staging.internal' port=5432: password authentication failed`, plus "Tested in 0.9 s · secrets are removed from this text". The redactor from #114 runs on every string before it reaches the UI.
- Mapped plain-language lines for the common causes: bad sign-in, host unreachable ("Vibe couldn't reach `host`. Is the VPN on?"), database not found, SSL required, extension missing (links to consent), keychain unavailable, timeout ("No answer after 10 seconds").
- Save stays enabled after a failed test (button reads "Save anyway"). A saved-but-failing connection simply shows the red status in the rail.
- MotherDuck tests run in a separate process; the help line says so.

**Scope picker.** Three radio cards, in this order: **This computer** (default; `.vibe-local/`), **Team** (`vibe.yaml`), **All my workspaces** (`~/.config/vibe/`). New connections always start on This computer; Team is an explicit choice. Scope can be changed later ("Move to Team" / "Move to This computer"); the saved sign-in stays bound to workspace id + connection name + a fingerprint of the server. Choosing **Team** shows the explainer: "**This file is committed to git.** The name, host, port and database are saved in `vibe.yaml`, so anyone who can see the repository can see them. Passwords and tokens are not saved there: they stay in each person's own keychain, and teammates enter their own. Your username is saved on this computer only." `local`: "Saved on this computer only. It isn't committed and teammates won't see it." When the workspace isn't a git repo the shared card adds "(Share it by sharing the folder.)". 

**Shadowing banner** (D): "**This is your private copy of warehouse.** On this computer it overrides the Team `warehouse` in `vibe.yaml`. [Look at the Team one] [Remove my copy]".

**Sign-in method is per person.** The committed definition says what a connection *needs* (which fields). *How* you sign in (keychain, environment variables, ask each time) is chosen per person and stored in `.vibe-local/` (or in the user-scope file for All-my-workspaces connections), so a teammate can use env vars while you use the keychain. Every type offers all three, including MotherDuck (env = an explicit `MOTHERDUCK_TOKEN`; spike finding H). The form says so under the choices: "The team's file only says which details are needed. How you sign in is your own choice and stays on this computer, so teammates can use a different way."

**Credential fingerprint.** A saved sign-in is bound to a fingerprint of (type, host, port, database / bucket scope). If a pull changes any of them, the connection goes to needs-credentials instead of sending your saved password to a different server (7 A3). The Database help line warns about it: "If the host, port or database change, everyone is asked to sign in again to confirm."

**Read-only.** A switch, **on by default** (`role="switch"`, `aria-checked`). On: "**Read-only (recommended).** Vibe will refuse to run changes on this connection, from you or the agent. This is a safety belt inside Vibe, not a lock on the database: a determined query can still get around it." followed by a warning note: "**For production data, also use a read-only database user.** Ask your database admin for a role that can only read. That is the only way to be certain nothing can change." (spike finding K: `READ_ONLY` is client-side, `postgres_execute` multi-statement payloads escaped it). Off: "Changes are allowed. You and the agent can edit data here. The agent will still ask you before every change." Turning it off for a non-local connection also needs a one-line confirm: "Allow changes to prod_pg?".

**Keyboard and focus.** Type grid is a roving group (arrows move, Enter chooses). Focus lands on the Name field in step 2. Errors from Save or Test move focus to the banner (`tabindex="-1"`). Escape on the page asks only if there are unsaved edits ("Discard this connection?"). Ctrl/Cmd+Enter runs Test.

**A11y.** Every input has a visible `<label>`; helps use `aria-describedby`; disabled type cards use `aria-disabled` and say "Coming soon" in text; radio groups use `role="radiogroup"` with a name.

**Bindings** (#117, #114, #115). `ListConnectionTypes() []TypeSpec{id,label,description,sections,fields,requiresExtension,capabilities,soon}` **(new; the registry drives the form)**, `GetConnection(name, scope)`, `SaveConnection(def, scope)`, `DeleteConnection`, `TestConnection(def, draftCredentials) → {ok, ms, summary, error{code,message,details}}`, `SetConnectionCredentials(name, scope, blob)`, `ClearConnectionCredentials`, `GetConnectionCredentialStatus(name) → {method, username, keyId, secretSet, fingerprintMatches, env []{name,set}}` (replaces `HasConnectionCredentials`; the username and key id are returned because they are not secrets, the secret never is), `ProbeKeychain() → ok|unavailable` **(new)**. No binding ever returns a secret.

**Reuse / new.** New `ConnectionEditorView.vue`, `ConnectionTypePicker.vue`, `CredentialField.vue`, `ScopePicker.vue`, `TestResult.vue`. Reuses `.field`, `.card`, `.banner`, `.toggle`, `.seg`, `.btn`.

## 7. States

**A3. Server changed.** Title "Sign in to prod_pg again". "prod_pg's server changed since you signed in (db.internal to db-new.internal). Sign in again to confirm it's the one you mean." Note: "This usually happens after you pull a change from your team. Your saved password is only used with the server it was saved for, so it isn't sent to the new one." Buttons Cancel · **Confirm and connect**. Same key icon in the rail with the detail line "The server changed since you signed in (db.internal to db-new.internal). Sign in again".

**A. Needs credentials: sign-in prompt.** Opened from the key icon or on first use of a connection. Title "Sign in to prod_pg". Body "Vibe doesn't have a username and password for this connection on this computer yet." Username, Password, checkbox "Remember in my keychain" ("Saved on this computer only. Vibe can't show it again, only replace or clear it."), which starts on for keychain mode and **off** for "Ask me each time" (D9). Buttons Cancel · **Connect**. MotherDuck shows a single Token field and the one-login note. Headless clients report *needs-credentials* instead of prompting. Bindings: `ConnectConnection(name)` and, for "ask each time" / first sign-in, a separate `ConnectWithCredentials(name, creds Credentials)` (secrets are used once and kept only if "Remember" is ticked), `SetConnectionCredentials`.

**A2. Keychain unreachable.** Title "Can't reach your keychain". "Vibe stores passwords in this computer's keychain, but it isn't answering. That's common over SSH or when no one is signed in to the desktop." Note: "You can still connect. Set `PG_PROD_USER` and `PG_PROD_PASSWORD` in the environment Vibe starts from, then choose **Environment variables** for this connection. Vibe never writes passwords to a file instead." Buttons Try again · **Edit connection**. (Plan §4.3: no plaintext fallback.)

**B. Extension install consent.** Title "Install the {name} extension?" (mock: `httpfs`, needed by `lake`; a Postgres connection reads "Install the postgres extension?"). Body "To read files from S3, Google Cloud Storage or R2, Vibe's database engine needs a small add-on called the `httpfs` extension. It isn't included, so Vibe needs to download it once." Bullets: downloaded from `extensions.duckdb.org`; kept in Vibe's own folder, shared by all workspaces, never touching an existing DuckDB install (finding G); needed by N connections here. Offline note: "Offline? You can skip this and the connection will wait with an 'Extension needed' icon." Buttons Not now · **Install {name} extension**. During install: progress and "Installing… this can take a moment"; failure: plain message plus Try again. Never downloads without a yes. Bindings: `InstallExtension(name) → progress events`, `ListExtensions()` **(new)**, event `extension:progress {name, state}` **(new)**.

**C. Workspace opened read-only (hub held by `vibe mcp serve`).** A `.ws-banner` across the main pane: "**Opened read-only.** `vibe mcp serve` (pid 4182) is already running this workspace, so Vibe can look at it but not change it." Sub: "Notebooks, queries and skills are plain files and stay editable, and remote connections work. Local database files are in use and can't be edited until it lets go." Buttons **Take over…** · Keep read-only. The top pill stays about the agent ("Agent can only read here"); read-only is explained by the banner, not by the pill. Local `duckdb` connections show the locked-by status; everything that would write is disabled with the reason as a tooltip. **C2 Take over** (`role="alertdialog"`): "Take over Acme Analytics?" "Vibe will ask `vibe mcp serve` (pid 4182) to let go of this workspace, then open it for editing." "Claude Desktop or Claude Code sessions that were using it will need to reconnect, and will then go through Vibe Studio. Nothing is deleted, and open notebooks aren't affected." Note: "After you take over, any change Claude makes needs your approval in Vibe Studio, and closing Studio ends the session it was passing along." Buttons Cancel · **Take over**. Events: `workspace:locked {pid, exe}` **(new)**, binding `TakeOverWorkspace()` **(new, #112)**, `OpenWorkspaceReadOnly(path)` **(new)**.

**D. "Changed on disk" conflict banner** (notebook, plan §4.1 `ErrConflict`). Sticky until resolved, `role="alert"`. "**This notebook changed on disk.** `notebooks/weekly-revenue.vnb` was saved by something else while you had it open: a git pull, another editor, or the agent." Sub: "Your unsaved edit (1 cell) and the new version differ in 2 cells. Pick one. Nothing happens until you do." Buttons: **Reload from disk** (drops your edit) · **Keep mine** (overwrites the disk version) · **Save mine as a copy** (writes `weekly-revenue (copy).vnb`). Each destructive choice confirms what is lost with a one-line `ConfirmDialog`. Typing is not blocked while the banner shows, so nobody loses work by waiting. If there is no local edit, a non-blocking toast "weekly-revenue changed on disk and was reloaded. Undo" replaces the banner. Event `workspace:changed-on-disk {kind, path}` and binding `ResolveConflict(path, choice)` **(new)**.

**E. Read-only datasheet shows the reason.** Banner above the table: "**This table is read-only.** `prod_pg` is set to read-only, so rows can't be added, edited or deleted here." Sub: "To change data, turn off Read-only for the connection. For production data, use a database user that is allowed to write." with **Edit connection**. Header shows a "Read-only" badge; Add row is disabled with a tooltip; cells are not editable (no edit affordance on hover, `aria-readonly`). The same pattern with a different reason covers a view ("This is a view, so it can't be edited") and a locked local file ("In use by `vibe mcp serve`"). The Table Browser gets a `readOnlyReason` prop from `GetTableCapabilities(conn, schema, table)` **(new)**.

**A11y for all dialogs.** `role="dialog"` or `alertdialog` with `aria-labelledby` and (for alertdialog) `aria-describedby`; first focus on the least destructive button for confirms, on the first field for forms; background `inert`.

## 8. Query editor and notebook SQL cell

**Query editor.** The editor's own schema tree is removed (the global rail does that). A saved-queries column stays. A **Run on** selector sits in the toolbar before Save: type tile, connection name, chevron. The menu lists connections with scope badge, status icon and a padlock for read-only, a check on the current one, disabled entries for locked connections, and "Manage connections…". Help line in the menu: "Plain table names run on the connection you pick. To use several at once, write `connection.schema.table`, like `prod_pg.public.customers`." Picking a connection that isn't connected connects it (or opens the sign-in prompt). The result bar says where it ran: "DuckDB · ran across local + prod_pg". Saved queries store the choice in their frontmatter (`-- vibe: {connection: prod_pg}`). The selection is per editor tab and per cell, never session-global. Implementation note for #117: a run must pin one `*sql.Conn`, because `USE` is per connection in the pool.

**Notebook SQL cell.** A compact selector chip in the cell header. Unset reads **Default · local** (workspace default); explicit shows the connection name with a type tile. Menu: Workspace default (first, with the current default underneath), then connections, with the line "Saved with the cell, so a teammate runs it on the same connection." A cell result footnote states the default when nothing was chosen ("no connection chosen, so this ran on the workspace default (local)"). Cells can join across connections by qualifying names; the cell shows no special state for that. A2UI `sql` bindings reuse the same chip in the cell drawer.

**Keyboard.** The selector is a combobox/listbox (`aria-haspopup="listbox"`): Enter or Down opens, arrows move, Enter selects, Escape closes. Ctrl/Cmd+Enter still runs.

**Completion.** Catalog-aware completion (#84) completes `connection.` then schema then table, from the schema cache of connected connections only (never dials).

**Bindings.** `RunQuery(sql, {connection?})`, `RunQuery(req RunQueryRequest{sql, connection string, tabId string})` pins one `*sql.Conn` per run, cell `connection` field in notebook format v2 (`SaveNotebook` unchanged, plan §4.7), `ListConnections()` for the menu. Events: `connection:status`.

**Reuse / new.** Modify `QueryEditorView.vue` (drop the left schema tree, add selector) and `CellDrawer.vue` / `NotebookView.vue` (chip). New `ConnectionSelect.vue` (shared by both) using `usePopoverMenu`.

## 9. Settings split

**Purpose.** Separate what belongs to the folder (travels with git) from what belongs to this computer.

Two tabs, `role="tablist"`, each with a sub-label: **Workspace** ("Acme Analytics · saved in this folder") and **App** ("This computer · every workspace"). The selected tab is in the URL hash. Arrow keys switch tabs.

**Workspace tab** (A).
- **This workspace** (writes `vibe.yaml`): Name; Folder (read-only path, Show in folder); **Default connection** select, help "Notebook cells and queries with no connection chosen run here. `local` is the safest default."; Save settings.
- **The agent** has two layers because a committed file must not be able to loosen anyone's safeguards.
  - **Team limit** (saved in `vibe.yaml`, shared through git): the segmented control Read only / Read and write / Full. "What the agent may do for everyone on the team. This file can only make things **stricter**: a change someone pulls can never switch the agent's safeguards off on your computer."
  - **On this computer** (saved in `.vibe-local/`, just you): your own segmented control and the **"Ask me before it changes data"** switch. This is where you *loosen*: raise the tier up to the team limit, or turn the gate off. Options above the team limit are disabled with the reason.
  - The switch only covers the workspace's own `local` database. For every other connection the agent always asks, and that can't be turned off.
  - A table shows the connections the agent can change (those that are writable, with "Always" and a padlock for non-local ones), then one line: "Everything else: … Read-only. The agent can look but not change." (D8 made visible)
- **Claude and other tools (MCP):** status "Running, only on this computer", address with Copy, **Copy Claude Code config** and **Copy Claude Desktop config** (the latter two produce `vibe mcp serve --workspace <path>` snippets; no token on screen, the proxy reads it from the hub files), and the warning "With Studio closed, tools using `vibe mcp serve` can't change anything while "Ask me before it changes data" is on. Turn it off to let them change the workspace's own `local` database only; other connections always need Studio." Start/Stop stays.

**App tab** (B).
- **Appearance:** the existing `AppearanceSettings` (theme cards, UI scale, reduce motion), moved as is.
- **The agent's model:** provider, model, endpoint where relevant, **API key** as the write-only field ("•••• ✓ In your keychain · Replace · Clear"), "The `VIBE_LLM_API_KEY` environment variable, if set, is used instead." One-time migration note: "Found a key in `~/.vibe.yaml`? Move it to the keychain and blank the file." (#114).
- **Extensions:** one row each (`postgres`, `httpfs`, `sqlite`, `motherduck`): purpose, which connections need it, status badge (Installed / Not installed), Install or Remove. Footnote: "Stored in Vibe's own folder, separate from any DuckDB you installed yourself."

**A11y.** `aria-controls` / `aria-labelledby` link tabs and panels; switches have `role="switch"` with labels and descriptions; the status table has real `<th>` headers.

**Bindings.** `GetWorkspaceInfo`, `SetWorkspaceSettings({name, defaultConnection, sandbox, askBeforeChanges})` (rejects an attempt to clear the gate for a non-local connection), `GetAgentConfig` / `SetAgentConfig` (app-level, key via `SetSecret("llm")`, `HasSecret("llm")`) **(names to align with #114)**, `ListExtensions`, `InstallExtension`, `RemoveExtension` **(new)**, `GetMCPConfigSnippet(client: "claude-code"|"claude-desktop") → string` **(new)**, existing MCP start/stop/status.

**Reuse / new.** Split `SettingsView.vue` into `WorkspaceSettingsPanel.vue` and `AppSettingsPanel.vue`; reuse `AppearanceSettings.vue`, `AgentSettings.vue` (split: sandbox part to workspace, provider part to app). New `ExtensionsPanel.vue`, `AgentWritePolicyTable.vue`.

## 10. Agent approval card (write to a non-local connection)

**Purpose.** The one place an agent write needs a person (D8, plan §4.5 "three gates": tier allows it AND connection writable AND user approves). The card must make clear **where** the change goes.

**Card.** Header "Allow this change to warehouse?" (shield icon, coral accent as in the existing `ApprovalCard`). Body: a **connection chip** (type tile, name, "not your local data"; no scope badge, which would be noise here), a one-sentence summary naming the connection and the rows ("Update 1 row in **warehouse**: set the status of order 10482 to "shipped"."), the exact statement in a code block, and "This is outside your workspace's local data, so Vibe can't undo it for you." Buttons: Cancel · **Allow change to warehouse** (the verb and target are in the button). Destructive statements (DELETE, DROP, TRUNCATE) show a count and use `btn-destructive`: "Delete rows from warehouse?" / "Delete **1,204 rows** from **warehouse**.public.events where…" / "This can't be undone. Check the number of rows before you allow it." / **Delete rows in warehouse**. (The target is a writable connection; read-only ones never produce a card.) The row count comes from the policy's preview, never from model prose.

**States.** pending, approved ("Approved, running…", then "Done."), declined ("Cancelled, nothing changed."), expired ("No answer in time, nothing changed."), cancelled (workspace closed). Refusals: when the target is read-only the agent says so in chat ("**prod_pg** is set to read-only, so I can look but not change it. If you want me to, turn off Read-only on that connection first."), with no card. Headless (`vibe mcp serve`, Studio closed): the tool result is an error the client shows: "Blocked: nobody to ask. A tool tried to change **prod_pg** through `vibe mcp serve`, but this workspace isn't open in Vibe Studio, so there's no one to approve it. Open the workspace in Vibe Studio to allow it, or let the tool read only."

**Keyboard and a11y.** The card is `role="group"` with an accessible name; when it appears the agent panel announces it (`aria-live="polite"`) and focus moves to **Cancel** (safe default), never to Allow. Esc cancels. The status line is `role="status"`.

**Bindings and events.** Existing approval queue (`agents` approval, `ResolveApproval(id, approve)`); the approval payload gains `connection {name,type}`, `rowCount`, `destructive`. Event `approval:requested` (existing name to confirm in #116).

**Reuse / new.** Extend `ApprovalCard.vue` with a `ConnectionChip` (shares `ConnTypeTile` and `ScopeBadge`). The agent panel's footer chips ("Default: local", "Asks before changes") are new, tiny.

## Component inventory

**Reuse as is:** `Icon.vue`, `ConfirmDialog.vue`, `composables/useModal.ts`, `useConfirm`, `usePendingSaves`, `AppearanceSettings.vue`, `ToolStepChip.vue`, `.btn`, `.card`, `.field`, `.badge`, `.banner`, `.toggle`, `.seg`, `.stat`, `.empty-hero`, `.url-tile`, `.brutal-table`, `.status-dot`.
**Modify:** `AppShell.vue` (top bar, sidebar, footer; drop save/revert/close/dirty), `SettingsView.vue` (split), `AgentSettings.vue` (split), `QueryEditorView.vue`, `NotebookView.vue` / `CellDrawer.vue`, `TableBrowserView.vue` (read-only reason, route by connection), `ApprovalCard.vue`.
**Replace:** `UnlockView.vue` → `LauncherView.vue`.
**New:** `WorkspaceSwitcher`, `RelaunchDialog`, `NewWorkspaceDialog`, `ImportVibeDialog`, `ConnectionRail`, `ConnectionTreeNode`, `StatusIcon`, `ScopeBadge`, `ConnTypeTile`, `ConnectionEditorView`, `ConnectionTypePicker`, `CredentialField`, `ScopePicker`, `TestResult`, `ConnectionSelect`, `ExtensionConsentDialog`, `CredentialsPromptDialog`, `WorkspaceBanner`, `ConflictBanner`, `TakeOverDialog`, `ExtensionsPanel`, `AgentWritePolicyTable`, and the `usePopoverMenu` composable.
**New Lucide icons to add to `icons.ts`:** `gitBranch`, `folderOpen`, `plug`, `circle`, `circleAlert`, `circleCheck`, `shieldCheck`, `search`, `chevUpDown`, `unlock`, `externalLink`, `history`, `server`, `more` (paths are in [`lib.mjs`](lib.mjs)).
**New CSS (all role-based):** see the "[new]" blocks in `mock-additions.css` (`.ws-switch`, `.menu`, `.tn`/`.tree`, `.sdot`→`.st`, `.scope`, `.ctype`, `.dialog`, `.note`, `.cred`, `.conn-select`, `.ws-banner`, `.approval`, `.tabs`).

## Binding and event summary

| Area | Bindings | Events | Issue |
|---|---|---|---|
| Launcher / lifecycle | `ListRecentWorkspaces`, `OpenWorkspace`, `CreateWorkspace`, `CloseWorkspace`, `GetWorkspaceInfo`, `SetWorkspaceSettings`, `PreviewVibeImport` + `RunVibeImport` | `workspace:opened`, `workspace:closed`, `workspace:changed-on-disk` | #111 |
| Added by this design (Slice 1) | `RemoveRecentWorkspace`, `RelocateRecentWorkspace`, `ChooseFolder`, `ChooseFile`, `InitWorkspaceInFolder`, `ListStarterTemplates`, `CheckGitAvailable`, `PreviewVibeImport`, `RunVibeImport`, `CancelVibeImport`, `SwitchWorkspace`, `RelaunchInto`, `ResolveConflict`, `OpenWorkspaceReadOnly`, `TakeOverWorkspace` | `workspace:git-changed`, `workspace:locked` | #111, #112 |
| Connections | `ListConnections`, `SaveConnection(scope)`, `DeleteConnection`, `TestConnection`, `SetConnectionCredentials`, `ClearConnectionCredentials`, `GetConnectionCredentialStatus`, `ConnectConnection`, `DisconnectConnection`, `ListCatalog`, `InstallExtension` | none in the plan | #117 |
| Added by this design (Slice 2) | `ListConnectionTypes`, `GetConnection`, `RefreshCatalog`, `ProbeKeychain`, `ListExtensions`, `RemoveExtension`, `GetTableCapabilities`, `GetMCPConfigSnippet`, `ConnectWithCredentials` | `connection:status`, `catalog:changed`, `extension:progress` | #117, #114 |

None of these returns a secret: bindings can set, clear and ask "is it set", never read (plan §4.3).

## Design decisions the plan did not specify

1. **Rail placement.** The Connections rail lives in the sidebar under the nav (one 296px column), not as a second column. The Tables nav item goes away and the editor's own schema tree is removed. Under 1000px the nav becomes icons and the tree a drawer.
2. **Header.** The wordmark shrinks to the mark so the switcher gets the room; the old "Local · Encrypted" pill (no longer true) becomes "Agent asks before changes" (agent trust only; the read-only state is shown in the banner, not the pill, which reads "Agent can only read here" while open read-only). Dashboard is renamed Home.
3. **Editor is a page, not a modal,** with deep-linkable routes.
4. **Open folder on a plain folder** asks before writing `vibe.yaml`; it never auto-initialises.
5. **Import creates a new workspace.** Step 1 only appears for locked files. Importing a bundle into the open workspace is #118's flow.
6. **Status icons use distinct shapes** as well as colour; the detail line only appears when the user can act.
7. **Only secrets are write-only** (orchestrator decision D7): the username and S3 key id are readable per-person fields; password, token and secret key are write-only. The committed config still never carries the username.
8. **"Ask before changes" only covers the workspace's own `local` database.** The table under it makes the forced-on rule for other connections visible.
9. **Connection selector semantics:** it sets the default catalog for plain table names (`USE`); qualified names always work and can span connections.
10. **Save is allowed after a failed Test** ("Save anyway"), since a VPN may simply be off.
11. **Take over behaviour (confirmed in #112):** `vibe mcp serve` closes its session, releases the hub and **exits 0**; it does not turn into a proxy. Claude sessions that were using it see the server end and must reconnect (Claude Code `/mcp`, Claude Desktop restart); the reconnected `vibe mcp serve` then proxies to Studio. The C2 dialog copy "will reconnect through Vibe Studio" should read "will need to reconnect, and will then go through Vibe Studio". A seamless hand-off (the old process replaying `initialize` against Studio) is a possible follow-up.
12. **Conflict banner choice labels:** "Reload from disk" is the visually primary action because it loses the least in the common git-pull case, but each destructive choice states what is lost; editing is never blocked.
13. **Keyboard shortcuts are proposals:** Ctrl/Cmd+O, Ctrl/Cmd+N, Ctrl/Cmd+Shift+O (switcher), F5 (refresh), Ctrl+Alt+N (new query here), Ctrl/Cmd+Enter (run or test). On macOS the labels show ⌘ (the mockups show the Linux/Windows labels). F5 must be intercepted (`preventDefault`) or the WebView reloads the whole app.
14. **Storage connections appear as three cards** (S3, GCS, R2) that share one form, instead of one "cloud storage" card.
15. **Default scope** for a new connection is always **This computer** (orchestrator decision D15). Team is an explicit choice, with the git explainer.
16. **Relative "last opened" times** in the recent list and a git branch chip (read from `.git/HEAD`, no `git` process needed), updated by `workspace:git-changed`.
17. **Approval buttons name their target** ("Allow change to warehouse") and focus starts on Cancel.

## Amendments after review (#106)

Decisions from the design review that touch more than one screen, and the plan sections amended to match (each tagged "(design #106)" in `docs/plans/workspaces-and-connections.md`):

1. **Scope words.** File-level `user` / `shared` / `local` are never shown. The UI says **Team**, **This computer**, **All my workspaces**. "local" in the UI always means the workspace's own `local` database.
2. **Committed `vibe.yaml` can only tighten** (sandbox tier, ask-before-writes). Loosening is stored per person in `.vibe-local/` (plan §4.1, §4.5).
3. **Credentials carry a fingerprint** of (type, host, port, database / bucket scope); a mismatch after a pull needs a fresh sign-in (plan §4.3).
4. **Sign-in method is per person** (keychain / env / ask each time), stored in `.vibe-local/` or the user-scope file; the committed definition lists only the fields needed (plan §4.2).
5. **Status precedence:** extension-missing > needs-credentials > error > locked-by > not connected > ok.
6. **No optional parameters on Wails bindings**; structs for anything with more than two inputs.

## Accessibility checklist (all screens)

- Text contrast: all body, detail-line and badge text uses `--vibe-fg`, `--vibe-fg-muted` or the existing accent roles, which the current `theme/schema.spec.ts` already tests. The **status icon colours are new pairs** (`--vibe-warn`, `--vibe-info` and the muted ring on `--vibe-sidebar-bg` and `--vibe-surface`): #117 must add those pairs (warn, info, fg.disabled-not-used) to `theme/schema.spec.ts` at the 3:1 non-text threshold. The "not connected" ring deliberately uses `--vibe-fg-muted`, not `--vibe-fg-disabled`.
- Focus: rows in clipped lists and trees (rail, menus, launcher list) use an **inset** ring (`box-shadow: inset 0 0 0 2px var(--vibe-focus)`), drawn in the mockups on the `orders` row, the switcher item and the launcher row, so `overflow: hidden` can't cut it.
- No information by colour alone (status shapes, scope text, padlock icons with labels).
- Every interactive control reachable and operable by keyboard; roving focus in tree, menus and radio groups; dialogs trap and return focus.
- Live regions: Test result (`status`/`alert`), conflict and read-only banners, approval card, import step changes.
- Reduced motion respected through the existing `data-reduce-motion` rules (no new animations except a spinner).
- Min target size 32px for icon buttons, 44px for form controls (existing `.field-input`).
