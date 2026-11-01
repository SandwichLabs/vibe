# Vibe: open-source and first public release plan

**Audit date:** 2026-10-11 · **Audited:** `vibe-app` `main` @ `5d6f3c0` · **Baseline:** [`../packaging.md`](../packaging.md) (2026-09-23) and the packaging epic (`vibe-app#77`).

## Status

**Tracking:** epic [#51](https://github.com/SandwichLabs/vibe/issues/51), children #36 to #50. The audit below records what was found on 2026-10-11 and why each step exists; it is not rewritten as work lands. Findings that are already fixed are marked **(done)** or covered by the status list here.

Decisions made (2026-10-11), recorded one per line in [`../direction.md`](../direction.md) §9:

- **Name:** keep **Vibe**; the CLI stays `vibe`. The installer will ship a collision guard (§4, #47) and the README says so. Revisit before 1.0.
- **Repo and module:** `SandwichLabs/vibe`, module `github.com/SandwichLabs/vibe` (#37, done). Private until #50.
- **Licence:** Apache-2.0 (#38).
- **History:** a filtered copy of `vibe-app` `main`, done. `landing/`, `docs/reviews/ux-panel-2026-09-26/` and `docs/archive/*.txt` are gone from all history, the 88 MB binary was never on `main`, and a work-email author was mailmapped. gitleaks on the result found only known test fixtures.
- **Reporting and contact:** GitHub only. Vulnerabilities go through GitHub private vulnerability reporting, everything else through Issues. There is no email address.
- **Contributions:** DCO sign-off (`git commit -s`), no CLA.
- **Old repo:** `SandwichLabs/vibe-app` is archived (read-only, pointer in its README) and stays private. References to `vibe-app#N` in these docs are history, not links an outside reader needs.
- **Telemetry:** none, and none planned.
- **Build and release:** unsigned v0.1.0 tarballs, no GoReleaser. CI (#45) and the release workflow (#46) are done.

| Issue | Step | State |
|---|---|---|
| #36 | Naming and launch decisions, this document | done with this change |
| #37 | Module path, binary and product strings | done |
| #38 | LICENSE, community files, templates | done |
| #39 | Prune internal-only content | done with this change |
| #40 | Third-party notices, Settings > About | open |
| #41 | Dependency and security sweep | open |
| #42 | Release-blocking bugs | open |
| #43 | macOS unsigned spike | open |
| #44 | Studio build hygiene, app icon | open |
| #45, #46 | CI, release workflow | done |
| #47 | `install.sh` | open |
| #48 | Newcomer docs, README, CHANGELOG | open |
| #49 | Screenshots and website | open |
| #50 | Go public, cut v0.1.0 | held for the owner |

The sections below keep the audit's original wording. Where they say "decision" or "recommendation", the list above is the outcome. The numbered items in the "Proposed issue breakdown" map to issues #36 to #50 in a different order; use the table above for current numbers.

Effort: **S** is under half a day, **M** is half a day to two days, **L** is more than two days. Items marked **DECISION** need the owner.

---

## Executive summary

The app itself is close. Release *infrastructure* and *open-source packaging* are not started, and three findings block publication outright:

1. **The Go module path points at someone else's GitHub org.** `go.mod` says `github.com/sandwich-labs/vibe-app`. GitHub resolves `sandwich-labs` to **`Sandwich-Labs`**, a different organisation (created 2022, 0 public repos, unrelated to this project). The intended org is `SandwichLabs`. At audit time `go install` 404ed. If that org ever creates `vibe-app`, `go install` would fetch *their* code. The module path had to change before going public (206 Go files plus ldflags; mechanical). **(done, #37)**
2. **Name collision on the `vibe` binary.** Mistral's open-source coding agent **Mistral Vibe** installs a `vibe` command (and `vibe-acp`) into `~/.local/bin` with `curl -LsSf https://mistral.ai/vibe/install.sh | bash`. That is the same install pattern and the same directory this plan would use. "Vibe Studio" is also the name of an AI Flutter app builder, and `vibe.co` has a "Vibe Studio" product. Settle the name **before** the module rename, so the repo, module path, bundle ID and binary change only once. **(decided: keep Vibe, ship the collision guard)**
3. **No LICENSE.** (Apache-2.0 chosen, #38.) Without one the code isn't open source. Every dependency is permissive (details in §3), so any licence works.

What is in good shape:
- **History scan is clean of real secrets.** gitleaks 8.30.1 ran over all 181 commits of `vibe-app` and every ref. All 11 hits are test fixtures: the well-known Azurite dev key, base64 `"foo"`/`"key"` strings, and ephemeral PG harness passwords. A custom pattern scan (JWT/MotherDuck, `sk-`, `ANTHROPIC_API_KEY=`, AWS, private keys, GitHub/Slack/Google/HF tokens) found only placeholders. No `.env` was ever committed.
- **No telemetry.** No analytics SDK. Outbound traffic is only what the user configures: the LLM provider, connections, and DuckDB extensions downloaded after consent.
- **All third-party licences are permissive.** Go (122 modules): MIT 53, Apache-2.0 37, BSD-3 29, BSD-2 3. Shipped npm: MIT/ISC/BSD/Apache, with dompurify `MPL-2.0 OR Apache-2.0`.
- The file-open wiring, Info.plist, `.desktop` and MIME files are done (vibe-app#105).

What has to be built: CI (none exists), a release workflow (`.goreleaser.yaml` still fails `goreleaser check`), an app icon (none exists anywhere, and `studio/build/appicon.png` is gitignored), an About screen, `install.sh`, newcomer docs, community files and a third-party notices file.

**Recommended shape for v0.1.0:** unsigned, ad-hoc-signed tarballs for macOS (arm64, x86_64) and Linux (amd64, arm64) from a plain GitHub Actions native-runner matrix, plus `checksums.txt` and build-provenance attestations. One POSIX `install.sh` installs into `~/.local/bin` (plus `~/Applications/Vibe Studio.app` on macOS). **Drop GoReleaser for v0.1.0.** Defer Homebrew, deb/rpm, dmg and notarisation to #4 once the Apple account exists.

**History:** keep it, but publish a **filtered copy into a new public repo** **(done: `SandwichLabs/vibe`)**. Strip the 88 MB `vibe-app` binary committed in 72f220e/0e2718b/146d82e, and decide about one commit authored with a former-work email address. Rewriting the *existing* repo is a trap: GitHub keeps old objects reachable through `refs/pull/*`. The full option comparison is in §2.

**Rough total (audit estimate):** about 2–3 focused weeks for one person. Most of it is CI, release, install and docs; the bug and dependency work runs alongside.

### Decisions the owner must make (all collected here; each also appears in its section)

Outcomes are in the Status section above; the table keeps the audit's recommendations.

| # | Decision | Recommendation |
|---|---|---|
| D1 | Licence | **Apache-2.0** (explicit patent grant, fits the DuckDB/charm/Wails ecosystem). MIT is the simpler alternative. FSL/BUSL only to stop a hosted competitor, which matters less for a desktop app. |
| D2 | Product and CLI name (Mistral Vibe collision, "Vibe Studio" collisions, trademark) | Run a USPTO search (classes 9 and 42) and make a deliberate call now. At minimum, rename the **CLI binary**, or ship the installer's collision guard (§4) and say so in the README. *Outcome: keep the name, ship the guard.* |
| D3 | History: as-is, filtered, or squashed | **Filtered history into a new public repo**; the old repo stays as the archive (§2). *Outcome: done.* |
| D4 | Repo and module path | `github.com/SandwichLabs/<name>` with exact casing, or a vanity path `sandwichlabs.dev/<name>` (survives any future repo move). |
| D5 | One commit authored with a former-work email address (March 2026, Taskfile/go.mod dependency bumps) | Confirm there's no employer IP claim on the project as a whole, then rewrite the author with a mailmap during the filter. *Outcome: mailmapped.* |
| D6 | Contribution model | DCO sign-off (no CLA), PRs welcome; or "issues only" for the first month. *Outcome: DCO.* |
| D7 | Bundle DuckDB extensions offline? | **No** for v0.1.0. Keep consent-then-download. Note the `motherduck` extension is proprietary, so it could not be redistributed anyway. |
| D8 | Install location default | `~/.local/bin` (no sudo), with `VIBE_INSTALL_DIR=/usr/local/bin` as an option. |
| D9 | Install script URL | `https://sandwichlabs.dev/vibe/install.sh`, a redirect to the latest release's `install.sh` asset. |
| D10 | Which open bugs block v0.1.0 | Block on #2 and #19. Fix #33 (cheap) and #34/#35 (they gate contributor CI; since fixed). Triage #21 (§8). |
| D11 | Public scope of `docs/` | Prune `docs/reviews/ux-panel-*`, `landing/` and the AI research dumps in `docs/archive/`; keep plans and reference (§6). |
| D12 | Security contact and support channel | GitHub private vulnerability reporting plus an email address; GitHub Discussions for questions. *Outcome: GitHub only, no email.* |

---

## 1. Open-source hygiene

**Current state:** no `LICENSE`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `.github/` (no workflows, no issue or PR templates), `CHANGELOG.md` or privacy statement. The README is written for insiders: status blocks about epics, "Go 1.24+" while `go.mod` says 1.26.3, no install section, and a docs table that points at `CLAUDE.md`.

### The module path problem

- `module github.com/sandwich-labs/vibe-app`. GitHub resolves `sandwich-labs` to the **`Sandwich-Labs`** organisation, which is not `SandwichLabs` and has no visible repos. The import path is wrong, not merely mis-cased.
- After the fix, `go install github.com/SandwichLabs/vibe@latest` would work only if:
  - the module path matches the repo exactly (mixed case is fine; the proxy escapes it as `!sandwich!labs`);
  - the user has CGO and a C++ toolchain (DuckDB static libraries are fetched as Go modules, so no system DuckDB is needed; the CLI built in about 12 s here);
  - they accept that the binary is named **`vibe-app`**, because `main` is at the module root. To get `vibe`, move `main.go` to `cmd/vibe/` and update the Taskfile and ldflags paths.
- Studio can't be `go install`ed: it embeds the gitignored `frontend/dist` and needs `wails build`. Document `go install` as CLI only.

### Checklist

- [ ] **D1 (Apache-2.0)** Add `LICENSE`. Add an SPDX line to `wails.json` `info.copyright`, Info.plist (via wails.json) and the README badge. Optionally add SPDX headers to source files (not required). **S**
- [ ] **DECISION D2/D4** Rename the module path to the real org (or a vanity path), and move `main` to `cmd/vibe` for `go install …/cmd/vibe@latest`. Update `Taskfile.yml` `CLI_LDFLAGS`, `.goreleaser.yaml` (or delete it), docs and `CLAUDE.md`. Run `go mod edit -module`, a sed over imports, then `go build ./... && go test ./...`. **M**
- [ ] Rewrite `README.md` for outsiders:
  - a one-paragraph pitch and screenshot or GIF;
  - **Install** (the curl one-liner, a manual download, and building from source);
  - a 5-minute quickstart;
  - supported platforms and a privacy statement;
  - links to the docs;
  - licence and contributing.
  Move the epic and status prose into `docs/direction.md`. **M**
- [ ] `CONTRIBUTING.md` covering:
  - prerequisites: Go 1.26.x, Node 22, Wails CLI v2.11.0, `libwebkit2gtk-4.1-dev` on Linux, Xcode CLT on macOS;
  - the `task` targets and the `GOTOOLCHAIN` caveat (vibe-app#92);
  - conventions from `CLAUDE.md` (no hex, focus ring, one tool surface, docs move with code);
  - tests, commit style (`feat(#n): …`) and DCO (**DECISION D6**). **M**
- [ ] `SECURITY.md`: supported versions (latest minor only), private reporting through GitHub Security Advisories only, no email address (**D12**), scope (credential handling, agent sandbox and SQL allow-list, MCP token, bundle encryption), and the response SLA. **S**
- [ ] `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1), with GitHub's reporting tools as the contact route (no email address). **S**
- [ ] `.github/ISSUE_TEMPLATE/` with `bug.yml` (asks for the `vibe --version` / `vibe-studio --version` output, OS and arch, webkit version, install method), `feature.yml`, and `config.yml` pointing questions to Discussions and security reports to the advisory form. Add `.github/pull_request_template.md` (what, why, tests, docs, screenshots for UI). **S**
- [ ] `CLAUDE.md` stays (useful to AI-assisted contributors), but move the architecture section into `docs/architecture.md` and have `CLAUDE.md` link to it. Personal memory files aren't tracked, which is good. **S**
- [ ] Repo settings once public: branch protection on `main` (required CI checks, linear history), private vulnerability reporting **on**, Dependabot security updates **on**, Discussions **on**, a description, topics, and the social preview (§7). The new repo starts with only `main`; the stale branches stayed behind in the archived repo. **S**

---

## 2. History and content scan

### Secrets: clean

- **gitleaks 8.30.1** (release binary, sha256-verified), `git --log-opts=--all`, redacted. 181 commits and about 11 MB scanned; 11 findings, all false positives:

  | Commit | Path | What it is |
  |---|---|---|
  | 9803c5b1 | `internal/connections/azure_integration_test.go` (lines 38, 244) | Azurite's **publicly documented** dev account key; a fake account key |
  | 9803c5b1 | `internal/connections/newtypes_test.go` (6 lines) | Fixture credentials (base64 of `key…`/`foo`, `s3cr…`) and a synthetic Azure connection string |
  | 0bb817a2 / d3b12ec2 | `internal/{connections,session}/pg_harness_integration_test.go` | Throwaway password for a local Postgres test container |
  | 0bb817a2 | `internal/connections/test.go:321` | A comment mentioning access-key error names |

- **Custom diff-addition scan** over all refs: MotherDuck/JWT `eyJ…`, `sk-`/`sk-ant-`, `ANTHROPIC_API_KEY=`, `OPENAI_API_KEY=`, `MOTHERDUCK_TOKEN=`, AWS `AKIA/ASIA`, PEM private keys, `ghp_`/`github_pat_`, `xox?-`, `AIza…`, `hf_…`, and Postgres URLs with passwords. Hits:
  - `sk-test-N…`, `sk-ant-NE…` in `internal/llm/openai_test.go` (d3aaab8f), `studio/secrets_test.go` and `AgentModelSettings.spec.ts` (6de46cef): obvious fixtures;
  - `ANTHROPIC_API_KEY=sk-ant-...` in `docs/llm-config.md` (9d816e44): a literal `...` placeholder.
- **Paths ever committed:** no `.env*`, `.pem/.p12/.p8/.key`, `id_rsa`, `.vibe`, `.duckdb` or `.db` files. `.claude/plan.md` and four `.claude/worktrees/agent-*` gitlinks exist in early history (removed by 4c4b98b); they're empty submodule pointers, so harmless.
- **Issue and PR bodies** (124 issues, 46 PRs) were scanned for tokens, personal paths and the work email: none.

### Internal or unwanted material

| Item | Where | Severity | Action |
|---|---|---|---|
| **88 MB compiled `vibe-app` binary** | Commits 72f220e, 0e2718b, 146d82e (March 2026); not on `main` | Clone weight (pack is 88 MB of 123 MB total blobs) | Strip with `git filter-repo --path vibe-app --invert-paths`. **(done)** |
| One commit authored with a former-work email address | March 2026 dependency bump (Taskfile/go.mod/duckdb.go) | Employer email in public history; possible IP question | **D5**: confirm, then mailmap rewrite during the filter. **(done)** |
| A Cloudflare account identifier | `landing/wrangler.toml`, **on `main`** | Not a secret, but no reason to publish | Delete `landing/` (superseded by the website repo). **(done)** |
| Access-era landing screenshots (`01-unlock.png`, SQLCipher era) | `landing/public/screenshots/` | Misleading | Delete with `landing/`. **(done)** |
| Personal home-directory paths and a session scratch path | `docs/reviews/ux-panel-2026-09-26/` (protocol, dev script, persona transcript) | Internal | Remove `docs/reviews/ux-panel-*` (kept in the private archive). **(done)** |
| A personal home-directory path in a test case | `internal/agents/sql_policy_test.go` | Cosmetic | Change to `/home/user/.bashrc`. **(done, #39)** |
| AI deep-research dumps (Access 97, Office UX) | `docs/archive/*.txt` | Low value, unclear provenance | Delete from the tree. **(done: dropped from all history)** |
| UX persona reviews | `docs/reviews/ux-panel-2026-09-26/` | Internal process; frank critique | Remove or move private. **(done)** |
| `RODNEY.md`, `tools/qa-video/` | Root and tools | QA tooling notes (Rodney is public) | Move to `docs/contributing/`; keep the scripts. **(done, #39: `docs/contributing/rodney.md`)** |
| 24 mockup PNGs (≈2.5 MB) | `docs/design/workspaces-connections/shots/` | Fine, but not the real UI | Keep as the design record, or drop the shots and keep the HTML |
| Co-author trailers naming an AI assistant on about 120 commits | History | None (transparent AI use) | Keep |

### Recommendation on history (**DECISION D3**)

| Option | Pros | Cons |
|---|---|---|
| **A. Flip `SandwichLabs/vibe-app` public as-is** | Zero work. Issue, PR and commit links all survive. | Everyone clones the 88 MB binary. The work email is public. The Access-era and AI-research material stays one `git log` away. |
| **B. Filter in place, force-push, then flip** | Keeps the repo and its issues. | **Doesn't work cleanly:** GitHub keeps the old commits reachable via `refs/pull/*` and cached views. Purging them needs a GitHub Support request. Every SHA in issue and PR comments breaks. |
| **C. Filtered history into a NEW public repo** (chosen) | Clean 35 MB history with authorship and narrative intact. A natural moment to fix the name and module path (D2/D4). The old repo stays as the archive. | The open issues move with `gh issue transfer` (same org; comments are kept). Done: the 51 open issues moved. Closed issues and PRs stay private. Old commit links in transferred issues point at the private repo. |
| **D. Squash to one commit in a new repo** | Simplest, smallest | Loses the history that explains the design. Not needed: nothing secret was found. |

Steps for C:
1. `git clone --mirror` into a scratch directory.
2. Run `git filter-repo --path vibe-app --invert-paths --mailmap mailmap.txt`, also removing `landing`, `docs/reviews/ux-panel-2026-09-26` and `docs/archive/*.txt`.
3. Re-run gitleaks on the result.
4. Push to the new repo.
5. Transfer the open issues.
6. Archive the old repo.

Effort: **M**. **Done** (see Status).

---

## 3. Third-party notices

### Go (go-licenses v2 over `./` and `./studio`, 122 modules)

- MIT 53, Apache-2.0 37, BSD-3-Clause 29, BSD-2-Clause 3. **No GPL, AGPL, MPL or unknown licences.**
- Notable modules:
  - `marcboeker/go-duckdb/v2` and `duckdb/duckdb-go-bindings/*` (MIT);
  - `wailsapp/wails/v2` (MIT), `charm.land/fantasy` (Apache-2.0);
  - `filippo.io/age` (BSD-3), `mark3labs/mcp-go` (MIT);
  - `zalando/go-keyring` (MIT), `godbus/dbus` (BSD-2).
- **DuckDB is statically linked** (`libduckdb_static.a` plus bundled components). DuckDB is MIT, but its bundled third-party code needs its own notices. That code is ICU (Unicode licence), RE2 (BSD-3), fmt (MIT), zstd (BSD), Mbed TLS (Apache-2.0), utf8proc (MIT), jemalloc (BSD-2), FastPFor (Apache-2.0), FSST (MIT), HyperLogLog (BSD-3, from Redis), miniz (MIT), yyjson (MIT), and libpg_query (BSD-3 plus the PostgreSQL licence). Copy DuckDB's `LICENSE` and its third-party notice section into the NOTICE file.
- **WebKitGTK** (LGPL-2.1) on Linux and **WebKit** on macOS are dynamically linked system libraries. They aren't redistributed, so there's no obligation beyond not bundling them.

### npm (from `studio/frontend/package-lock.json`)

- Shipped (`prod`, 52 packages): MIT 46, plus `entities` (BSD-2), `source-map-js` (BSD-3), `picocolors` (ISC), `typescript` (Apache-2.0, a build-time dependency of Vue's compiler) and **dompurify (`MPL-2.0 OR Apache-2.0`; elect Apache-2.0)**. Only vue, vue-router, @codemirror/*, @lezer/*, marked and dompurify end up in the bundle.
- Dev-only: MIT 251, ISC 16, **MPL-2.0 12 (`lightningcss*`, a Tailwind build tool, not shipped)**, BlueOak 4, Apache 3. None constrain the licence.

### Assets

- **Fonts** (`studio/frontend/src/assets/fonts/`): Fraunces, Inter and JetBrains Mono, all **SIL OFL 1.1**. `LICENSES.md` names the licence and the sources, but **OFL requires the copyright notice and the full licence text to accompany redistributed fonts**. Add `OFL-Fraunces.txt`, `OFL-Inter.txt` and `OFL-JetBrainsMono.txt` with each project's copyright line. **S**
- **Icons** (`studio/frontend/src/icons.ts`): described as "Lucide-style", and the paths are copied from **Lucide (ISC)**, some of which derive from Feather (MIT). Add the Lucide ISC notice to NOTICE and a header comment. **S**
- **DuckDB extensions** downloaded at runtime (excel, httpfs, postgres, mysql, sqlite, azure, ducklake, iceberg, delta, motherduck) are fetched by the user from `extensions.duckdb.org` after consent, so Vibe doesn't redistribute them. Most are MIT or Apache. The **`motherduck` extension is proprietary** (MotherDuck terms). Mention this in the privacy/third-party section, and it's another reason **not to bundle** the full set (D7).

### Checklist

- [ ] `THIRD_PARTY_NOTICES.md` (or `NOTICE`), generated by a `task notices` target:
  - `go-licenses report ./ ./studio --template notices.tpl` (or `go-licenses save` for full texts);
  - `npx license-checker-rseidelsohn --production --json`, or `rollup-plugin-license` in `vite.config` so the notices match the bundle exactly;
  - plus hand-written DuckDB-bundled, OFL and Lucide sections.

  Ship the file in every release archive. **M**
- [ ] CI check: fail the build if a new dependency has a licence outside an allow-list (MIT, ISC, BSD-2/3, Apache-2.0, MPL-2.0 dev-only, OFL, Unicode). Use `go-licenses check --allowed_licenses=…`. **S**
- [ ] **About screen** (Settings ▸ About): the version, commit and date. Studio already has `main.version/commit/date` ldflags, but **no binding exposes them and no About UI exists**. Add a "Third-party licences" view that renders the embedded NOTICE, plus links to the repo, the licence, the privacy statement and "Report a bug" (pre-filled with the version). **M**

---

## 4. Install script design

### What the existing doc says

`docs/packaging.md` mentions `install.sh` only as a script **inside the Linux Studio tarball**. It would put the `.desktop` file, the MIME XML and the hicolor icons into `/usr/share` and run `update-desktop-database`/`update-mime-database`, alongside deb/rpm. There's no curl-pipe installer, nothing for macOS outside a notarised dmg, and unsigned macOS builds were "prereleases for testers only". This plan **replaces** that for v0.1.0.

### Release assets the script consumes

Asset names carry **no version**, so `releases/latest/download/<name>` works without an API call. The version is in a `VERSION` file inside each archive.

| Asset | Contents |
|---|---|
| `vibe-darwin-arm64.tar.gz`, `vibe-darwin-x86_64.tar.gz` | `Vibe Studio.app/` (ad-hoc signed, `vibe-studio` inside), `vibe` (CLI), `LICENSE`, `THIRD_PARTY_NOTICES.md`, `VERSION` |
| `vibe-linux-amd64.tar.gz`, `vibe-linux-arm64.tar.gz` | `bin/vibe`, `bin/vibe-studio`, `share/applications/vibe-studio.desktop`, `share/icons/hicolor/*/apps/vibe-studio.png` (+ scalable SVG), `share/mime/packages/vibe-bundle.xml`, `LICENSE`, `THIRD_PARTY_NOTICES.md`, `VERSION` |
| `vibe-cli-linux-amd64.tar.gz`, `vibe-cli-linux-arm64.tar.gz` | `vibe` only, for servers, CI and MCP-only use; no WebKit needed |
| `checksums.txt` | sha256 of every asset |
| `install.sh` | The script, versioned with the release |
| Attestations | `actions/attest-build-provenance` on every asset (verify with `gh attestation verify`) |

**Per-arch macOS archives rather than universal:** each is half the download, and the script picks the right one. If `sysctl -in sysctl.proc_translated` returns `1` (a shell under Rosetta), use arm64.

### Behaviour

```
curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh
curl -fsSL …/install.sh | sh -s -- --cli-only          # Linux servers, MCP-only
curl -fsSL …/install.sh | VIBE_VERSION=v0.1.0 sh       # pin
curl -fsSL …/install.sh | sh -s -- --uninstall
```

1. **Safety:** POSIX `sh`, `set -eu`. The whole body sits in `main() { … }; main "$@"`, so a truncated download runs nothing. Use `mktemp -d` with a cleanup `trap`, never call `sudo` implicitly, honour `NO_COLOR`, and require `curl` (or fall back to `wget`) and `tar`.
2. **Detect:**
   - `uname -s`: Darwin or Linux, otherwise a friendly "unsupported; build from source" message.
   - `uname -m`: `x86_64|amd64` → amd64 (`x86_64` on darwin), `arm64|aarch64` → arm64.
   - On Linux, refuse musl (Alpine): "glibc ≥ 2.35 needed".
   - Parse the glibc version from `ldd --version`, or `getconf GNU_LIBC_VERSION`, and require ≥ 2.35. (A local build on Fedora 44 needs `GLIBC_2.43`, which is why CI must build on Ubuntu 22.04.)
   - On macOS, check `sw_vers -productVersion` against the minimum (§8: **14.0 for arm64, 13.0 for Intel** per the DuckDB static libraries' `LC_BUILD_VERSION`).
3. **Download and verify:**
   - Base URL: `https://github.com/SandwichLabs/<repo>/releases/latest/download` or `/releases/download/$VIBE_VERSION`.
   - Download the asset and `checksums.txt`, and verify with `sha256sum` (Linux) or `shasum -a 256` (macOS) by matching the asset's line exactly. Abort on a mismatch.
   - Optional stronger check: if `gh` is installed and `VIBE_VERIFY=attestation`, run `gh attestation verify <file> --repo SandwichLabs/<repo>`. (A cosign keyless signature on `checksums.txt` is the alternative; defer it.)
4. **Collision guard (D2):** if `command -v vibe` resolves outside the install directory, or `$PREFIX/vibe --version` doesn't start with `vibe version`, stop. Print "Another program named `vibe` is installed at … (for example Mistral Vibe). Re-run with `--force` to replace it, or set `VIBE_INSTALL_DIR`." Never silently overwrite.
5. **Install, macOS:**
   - `PREFIX=${VIBE_INSTALL_DIR:-$HOME/.local/bin}` and `APPDIR=${VIBE_APP_DIR:-$HOME/Applications}`.
   - Copy the app to `"$APPDIR/Vibe Studio.app.new"` with `ditto` (preserves signatures and xattrs), `rm -rf` the old one, then `mv`. That gives an atomic swap.
   - `xattr -dr com.apple.quarantine "$APPDIR/Vibe Studio.app"`. This is defensive; curl doesn't set it.
   - Install `vibe` to `$PREFIX/vibe` (temp file plus `mv`).
   - Write `$PREFIX/vibe-studio` as a two-line wrapper: `exec "$APPDIR/Vibe Studio.app/Contents/MacOS/vibe-studio" "$@"`. That makes `vibe studio --gui` work **without code changes**, because `launchGUI` finds `vibe-studio` beside `vibe` and `exec`s it. (#4's EvalSymlinks / `open -a` change is still worth doing later; see §5.)
6. **Install, Linux:**
   - Install `vibe` and `vibe-studio` to `$PREFIX`.
   - Write `~/.local/share/applications/vibe-studio.desktop` with `Exec=$PREFIX/vibe-studio %U`. Use an absolute path: desktop sessions often don't have `~/.local/bin` on `PATH`.
   - Install icons to `~/.local/share/icons/hicolor/{16..512}/apps/` and the MIME file to `~/.local/share/mime/packages/vibe-bundle.xml`.
   - Run `update-mime-database ~/.local/share/mime`, `update-desktop-database ~/.local/share/applications` and `gtk-update-icon-cache` **only if present**.
   - `--cli-only` skips everything except `vibe`.
7. **Runtime dependency check (Linux GUI):** run `ldd "$PREFIX/vibe-studio" | grep 'not found'`, which is more reliable than `ldconfig -p`. If anything is missing, keep the install (the CLI works) and print a distro-specific line from `/etc/os-release` `ID`/`ID_LIKE`:
   - Debian/Ubuntu: `sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0`
   - Fedora/RHEL 9+: `sudo dnf install webkit2gtk4.1 gtk3`
   - Arch: `sudo pacman -S webkit2gtk-4.1`
   - openSUSE: `sudo zypper install libwebkit2gtk-4_1-0`
   - otherwise: "needs WebKitGTK 4.1 (webkit2gtk-4.1)".

   Also warn when no Secret Service is reachable (`busctl --user status org.freedesktop.secrets` fails): "credentials need a keyring or `env:` references".
8. **PATH:** if `$PREFIX` isn't on `PATH`, print the exact line for the user's `$SHELL` (zsh, bash or fish). Don't edit rc files unless `--modify-path` is given.
9. **Manifest:** write `${XDG_DATA_HOME:-~/.local/share}/vibe/install-manifest` (macOS: `~/Library/Application Support/vibe/install-manifest`) listing every installed path and the version.
10. **Upgrade** is re-running the script. It's idempotent, swaps atomically, and prints `old → new`. If `pgrep -x vibe-studio` finds Studio running, say "restart Vibe Studio to use the new version". No in-app update check for v0.1.0: it would contradict "doesn't phone home". An opt-in check can come later.
11. **Uninstall** (`--uninstall`): remove the paths in the manifest, refresh the MIME and desktop databases, and print what is **left on purpose**:
    - config (`~/.config/vibe` or `~/Library/Application Support/vibe`);
    - the extension cache (`<user cache>/vibe/duckdb/extensions`);
    - keychain items (`vibe conn credentials clear …`);
    - workspaces, which are the user's own folders.
12. **Exit:** print the installed version (`vibe --version`, `vibe-studio --version`) and next steps: `vibe workspace init demo --template …`, `vibe studio --gui demo`, and the docs URL.

### Gatekeeper for unsigned builds

- **curl doesn't set `com.apple.quarantine`.** Only LSQuarantine-aware apps (browsers, Mail, Messages, AirDrop) do, and `tar` and `ditto` run from a shell don't add it. Gatekeeper's first-launch check happens only for quarantined files, so a curl-installed app **launches with no "unidentified developer" dialog** and no App Translocation.
- **Apple silicon requires every arm64 executable to be signed**, ad-hoc is enough. The Go linker ad-hoc signs darwin/arm64 binaries automatically. CI should still run `codesign --force --deep --sign - "Vibe Studio.app"` **after** placing all files in the bundle, then `codesign --verify --deep --strict`. That seals Info.plist and resources and keeps the signature consistent. The installer must not modify anything inside the bundle afterwards, or the signature breaks and the kernel kills the process.
- **Don't enable the hardened runtime** (`--options runtime`) for ad-hoc builds. Without it, **library validation isn't enforced**, so DuckDB extension dylibs that DuckDB downloads into the cache (over its own HTTP client, so not quarantined) load fine. `com.apple.security.cs.disable-library-validation` matters only once you notarise, which requires the hardened runtime. That's #4's spike. Verify that the `osx_arm64` extension binaries carry an (ad-hoc) signature; arm64 macOS refuses to map unsigned code. DuckDB's own CLI loads them on Apple silicon, so this is expected to work. Check it in the Mac smoke job (`LOAD excel`).
- **A browser-downloaded tarball is quarantined.** On macOS 15 the old right-click ▸ Open bypass is gone, so the user must go to System Settings ▸ Privacy & Security ▸ **Open Anyway**, or run `xattr -dr com.apple.quarantine "~/Applications/Vibe Studio.app"`. Put this in the troubleshooting doc and on the release page.
- **Keychain.** go-keyring calls `/usr/bin/security`, so keychain item ACLs trust Apple's signed `security` tool, not Vibe's code hash. Re-signed upgrades *should not* re-prompt. **Verify:** save a credential, upgrade to a rebuilt version, and test it.
- **TCC (privacy prompts).** An ad-hoc identity changes with every build, so grants for folders in Documents/Desktop may re-prompt after each upgrade. That's acceptable for v0.1; note it in the docs.

### Hosting (**D9**)

- Canonical URL: `https://sandwichlabs.dev/vibe/install.sh`. Serve it as a 302 to `https://github.com/SandwichLabs/<repo>/releases/latest/download/install.sh` (a Cloudflare redirect rule or an Astro redirect in the website repo). The script is then versioned and checksummed with each release, and the short URL never changes.
- Document the GitHub URL as a fallback. Don't point at `raw.githubusercontent.com/.../main/install.sh`: an unreviewed `main` would become the install path.

### Checklist

- [ ] `scripts/install.sh` as specified, with `shellcheck` in CI. **M**
- [ ] Installer tests in CI after the release build:
  - `ubuntu:22.04`, `ubuntu:24.04`, `debian:12`, `fedora:latest` containers: `--cli-only`, then `vibe --version` and a `workspace init`/`query` round trip;
  - Ubuntu 22.04 runner: the full install, `vibe-studio --version` under `xvfb-run`, and an uninstall;
  - `macos-15`: the full install, `vibe-studio --version`, `codesign --verify`, `LOAD excel`, and an uninstall.

  Run against a local file server (`VIBE_BASE_URL` override) so the release candidate is tested before publishing. **M**
- [ ] The `sandwichlabs.dev/vibe/install.sh` redirect. **S**

---

## 5. Build, CI and release state

| Item | State on `main` |
|---|---|
| `.github/workflows/` | **None.** (The only Actions runs ever were Dependabot graph updates.) |
| `.goreleaser.yaml` | **Still broken.** `goreleaser check` (local v2) fails on `before.hooks` with `dir:` (Pro syntax). It still cross-compiles CGO for darwin from one runner and builds Studio as a plain `go build` without Wails' `desktop,production` tags. `archives.format` is deprecated. |
| `task release:*` | Tags and runs `goreleaser release` locally. Unusable. |
| CLI version | ldflags `cmd.version/commit/date` work (Taskfile `install:cli`). `vibe --version` prints them. |
| Studio version | `main.version/commit/date` exist with `vibe-studio --version` (prints before the GUI starts, which is good for CI). Stamped only by `task install:studio`. **Not exposed to the UI.** `wails.json` `info.productVersion` is hard-coded `0.1.0`, and that is what Info.plist's `CFBundleShortVersionString` gets. |
| `appicon.png` | **Missing**, and `studio/build/appicon.png` is listed in `.gitignore`. No logo SVG exists anywhere in the repo. |
| `wails.json` | `info` filled in. `name`/`outputfilename` is `vibe-studio`, so the macOS bundle is `vibe-studio.app`, not `Vibe Studio.app`. `frontend:install` is `npm install` (use `npm ci` for reproducible releases). |
| Info.plist | `LSMinimumSystemVersion` **10.13.0**. The DuckDB static libraries are built for **macOS 14.0 (arm64) / 13.0 (x86_64)**, so the real floor is higher and the plist is wrong. |
| `launchGUI` | No `EvalSymlinks` and no `open -a` fallback. That's fine with the install.sh wrapper (§4); needed later for Homebrew symlinks. |
| Linux tags | `build:studio:linux` uses `-tags webkit2_41` **without `duckdb_arrow`**, but `install:studio` and `studio:dev` include it. Make them consistent. |
| Toolchain | Wails v2.11 can't load Go 1.27 (vibe-app#92). Pin `GOTOOLCHAIN=go1.26.x` (latest patch; see §8) in CI. |

### Minimum pipeline for an unsigned v0.1.0

**`ci.yml`** (PRs and pushes to `main`). Reuse #4 §5.2 nearly as written:

- `test` on `ubuntu-22.04`:
  - setup-go `1.26.x`, setup-node 22;
  - `apt install libwebkit2gtk-4.1-dev libgtk-3-dev`;
  - `go test ./...`;
  - `npm ci && npm run test && npm run build && npx vue-tsc --noEmit` (after #34/#35);
  - golangci-lint, `govulncheck ./...`, `go-licenses check`, `shellcheck scripts/*.sh`.
- `build-smoke` matrix on `ubuntu-22.04`, `ubuntu-22.04-arm` and `macos-15`: `wails build` with the release tags, `vibe --version`, and a `workspace init` → `import` → `query` round trip.
- Public repos get standard, arm64 and macOS runners free, so #4's billing concern goes away.

**`release.yml`** (on `v*` tags; `-rc` tags become prereleases):

```
build-linux (ubuntu-22.04, ubuntu-22.04-arm) ─┐
build-macos (macos-15: arm64 + x86_64 slices) ─┼─► publish (ubuntu-latest)
                                               │     checksums.txt, attest-build-provenance,
                                               │     gh release create --draft --notes-file <CHANGELOG section>
installer-test (needs build-*, before publish) ┘     upload install.sh; a human undrafts
```

- **build-linux:**
  - stamp `wails.json` `info.productVersion` from the tag (`jq`);
  - `wails build -tags webkit2_41,duckdb_arrow -ldflags "$STUDIO_LDFLAGS"`;
  - `go build -ldflags "$CLI_LDFLAGS" -o vibe .`;
  - stage the tarball layout in §4, plus the CLI-only tarball;
  - smoke test.
- **build-macos:**
  - `wails build -platform darwin/arm64` and `darwin/amd64` (CGO with `-arch`; the DuckDB bindings ship both archs);
  - `go build` `vibe` for each arch;
  - rename the bundle to `Vibe Studio.app` (or set `outputfilename` and `productName`);
  - `codesign --force --deep -s -` and `codesign --verify --deep --strict`;
  - tar;
  - smoke test the arm64 build (`vibe-studio --version`, `LOAD excel`). Optionally smoke x86_64 on `macos-15-intel` (available until August 2027).
- **publish:** download the artifacts, `sha256sum * > checksums.txt`, attest, create a **draft** release, upload. Release notes come from `CHANGELOG.md`.

**Reuse from #4's plan:**
- the native-runner matrix and the Ubuntu 22.04 / webkit2gtk-4.1 baseline;
- the `GOTOOLCHAIN` note;
- the smoke tests;
- provenance attestation;
- draft-then-publish;
- `-rc` prereleases;
- the Info.plist, `.desktop` and MIME work already done;
- the keychain checklist (§8 of the plan) and the extension decision.

**Drop or defer:**
- GoReleaser entirely for v0.1.0. Delete `.goreleaser.yaml` and `task release:*`, and replace them with "push a tag". GoReleaser adds nothing when Studio needs a native matrix anyway; revisit for Homebrew.
- Homebrew formula and cask.
- deb/rpm (nfpm).
- dmg.
- Developer ID signing, notarisation, the hardened runtime and entitlements.
- Flatpak, AppImage, Snap and Windows.
- Auto-update.

### Checklist

- [ ] `ci.yml`. **M**
- [ ] `release.yml` plus artifact staging scripts (`scripts/package-linux.sh`, `scripts/package-macos.sh`). **L**
- [ ] Delete or replace `.goreleaser.yaml` and `task release:*`, and add `task release:check` (a local dry run of the staging scripts). **S**
- [ ] Studio build hygiene:
  - un-ignore and commit `appicon.png`;
  - `LSMinimumSystemVersion` → `13.0` and document "Apple silicon needs macOS 14+", or build per arch with separate plists;
  - `npm ci`;
  - consistent `duckdb_arrow` tag;
  - version stamping of `wails.json`;
  - the bundle name `Vibe Studio`. **S–M**
- [ ] `launchGUI`: `EvalSymlinks(os.Executable())` plus a darwin `open -a "Vibe Studio" <path>` fallback (#4; not required for v0.1 thanks to the wrapper). **S**
- [ ] Mac spike on a `macos-15` runner (no Apple account needed):
  - an ad-hoc signed, non-hardened app launches;
  - `LOAD excel`/`postgres`/`httpfs` work;
  - a keychain round trip survives a re-signed upgrade;
  - `OnFileOpen` fires from Finder for a folder and for a `.vibe` file. **M**

---

## 6. Documentation for outside users

**What exists** (`docs/`): good reference material in `reference/` (workspaces, connections, agent-policy, mcp, llm-config, notebooks/A2UI, import-office, templates, themes), plus `direction.md`, plans, spikes, design mockups, reviews and archive. All of it is written for the builder; there is no entry point for a newcomer.

| A newcomer needs | Status | Work |
|---|---|---|
| Install (script, manual, from source, uninstall, upgrade) | Missing | `docs/install.md` **S** |
| Quickstart: first workspace, a connection, a query, a notebook, the agent with a key or a local model | Missing (the README has a CLI cheat sheet) | `docs/quickstart.md`, using a template and a public dataset (for example a Parquet URL over httpfs) so it works with no accounts **M** |
| Connections | `reference/connections.md` is good | Add a "common recipes" page (MotherDuck token, Postgres read-only user, S3 with env credentials) **S** |
| MCP setup | `reference/mcp.md` covers serve, SSE and the Claude config. BYO docs are #27 (open) | Add a short "Use Vibe from Claude Code / Claude Desktop / Cursor" page; fold in #27 if it lands **S** |
| LLM provider setup | `reference/llm-config.md` | Make sure it says what the default provider is and that nothing is sent anywhere without a configured provider **S** |
| Troubleshooting | Missing | `docs/troubleshooting.md` **M**. Cover: Gatekeeper/quarantine; missing WebKitGTK; glibc too old; no Secret Service on headless Linux (use `env:`); "workspace owned by another process" (hub lock); extension download blocked by a proxy or air gap; MotherDuck can't be read-only; TCC re-prompts after an upgrade; Wayland/X11 (`GDK_BACKEND`); collision with another `vibe` on `PATH`. |
| Privacy / what leaves your machine | Missing | `docs/privacy.md` plus a README section (§8) **S** |
| Building from source | Scattered across `CLAUDE.md`, the README and the Taskfile | `CONTRIBUTING.md` (§1) **M** |
| Architecture for contributors | In `CLAUDE.md` and the README | `docs/architecture.md`, with the package map and data flow: hub, sessions, tool registry, MCP **S** |
| Changelog and release notes | Missing | `CHANGELOG.md` (Keep a Changelog). The v0.1.0 entry is a feature summary, not a commit log. Conventional `feat(#n)` commits allow `git-cliff` later **S** |
| Supported platforms | Missing | In the README and install.md (§8) **S** |

**Move, prune or keep (D11):**
- **Delete from the public tree:**
  - `landing/` (superseded by the website repo; had a Cloudflare account identifier and Access-era screenshots);
  - `docs/reviews/ux-panel-2026-09-26/` (persona transcripts, personal paths, dev scripts);
  - `docs/archive/*.txt` (AI research dumps).
- **Keep but relabel:**
  - `docs/archive/*.md`, as history. It could be pruned to a short `HISTORY.md` instead.
  - `docs/plans/*`: accepted plans give contributors context. Add a "status: delivered/accepted" line to each.
  - `docs/reviews/cleanup-2026-10/`: move `rules/*.yml` and `sgconfig.yml` to `tools/ast-grep/`, wire them into CI if useful, and drop `FINDINGS.md` or keep it as history.
  - `docs/design/`: keep the HTML and SPEC; drop the 24 PNGs if size matters.
- **Rewrite `docs/packaging.md`:** a "v0.1 unsigned" section on top that describes install.sh and the workflows; keep the signed/Homebrew plan below it as "next".
- Update `docs/README.md` with a "Using Vibe" section first and "Contributing / design records" after. `internal/docsref` must stay green.

---

## 7. Assets

- [ ] **App icon.** There is none: no `appicon.png`, no SVG mark, no favicon; the website has its own `favicon.svg`. Design the SchoolHouse "V" mark as a master SVG, then export:
  - a 1024² `studio/build/appicon.png`, from which Wails generates `iconfile.icns` (un-ignore it in `.gitignore`);
  - Linux hicolor PNGs at 16, 32, 48, 64, 128, 256 and 512 plus `scalable/apps/vibe-studio.svg`;
  - optionally a `.vibe` document icon.

  Check that it reads at 16 px and in dark docks. **M**
- [ ] **Screenshots.** None are usable: `landing/public/screenshots` are from the Access/SQLCipher era, and `docs/design/.../shots` are mockups. Capture 4–6 real shots with Rodney against `:34115`: launcher, notebook with a live chart, connections rail, agent approval card, query editor, plus one dark-theme shot. Store them in `docs/images/`. **S**
- [ ] **Demo video/GIF.** The 56 s launch film already exists in the website repo. Reuse it on the release page and the README (GitHub renders an uploaded mp4). Make a ≤15 s GIF of "query → chart → ask the agent" for the README hero. **S**
- [ ] **Social preview** at 1280×640 (logo, tagline, one screenshot), uploaded in repo settings. **S**
- [ ] **Website.** The project page in the website repo is **already published** (no `draft` flag). It has "Watch the trailer" and "Try it early ↗" (mailto) CTAs, and a comment saying to add Source CTAs when the repo goes public. Work: replace the mailto with the install one-liner, add the GitHub link, add the `/vibe/install.sh` redirect, and add a docs link. **S**
- [ ] **DECISION D2: name and trademark.**
  - **Mistral Vibe**: an open-source CLI, `vibe` and `vibe-acp` binaries, installed into `~/.local/bin` via `uv tool` or `curl … | bash`. A direct PATH collision with a well-known AI developer tool; users of both overlap heavily, especially with the planned ACP support (`vibe-app#153`).
  - **"Vibe Studio"**: an AI Flutter app builder (listed on Capterra and SoftwareAdvice), and vibe.co's "Vibe Studio" (ad creative).
  - "vibe" is also the generic word in "vibe coding".

  Search USPTO (classes 9 and 42) before launch. If you keep the brand, ship a **distinct CLI binary name** and keep "Vibe" as the product name; otherwise rebrand once, together with the module and repo rename. A later rename also touches the `.vibe` extension, the `vibe.yaml` name, `application/x-vibe-bundle`, `dev.sandwichlabs.vibe-*` and the keychain service names. Decide what is brand and what is file format.

---

## 8. Product readiness blockers

### Open issues (36 open; bugs and release-relevant items)

| Issue | What | Block v0.1.0? |
|---|---|---|
| **#2** | Navigating away from Agent mid-turn hides the conversation while the turn keeps running; a retry **duplicates writes** | **Yes.** A trust bug in the headline feature. **M** |
| **#19** | Auto-number off-by-one after reopen; `BumpAutoNumber` can make the **next default insert collide** with an existing key (local DuckDB connections) | **Yes.** Data-integrity bug on the default local path. **M** |
| **#33** | A reload after closing reopens the launch workspace | Fix, it's cheap (in flight next to vibe-app#164). **S** |
| **#34, #35** | 315 vue-tsc errors; type-check isn't a gate | Fix before inviting contributors: CI must be green on a fresh clone. **M** |
| **#21** | Security follow-ups: 11 hand-rolled SQL quoting sites, 75 `Sprintf`-built SQL sites, 20 `EvalSymlinks` containment checks | **Triage before going public.** Audit the agent-reachable and MCP-reachable SQL paths (those face model input); the rest can follow. **M–L** |
| #3 | Empty SQL cell shows a misleading error | No (polish). **S** |
| #24 | xlsx/table export bindings have no UI | No; remove the dead bindings or defer. |
| #4 | Packaging epic | Superseded for v0.1 by the new epic; keep it for signed/Homebrew |
| #12 and children vibe-app#84 to vibe-app#90 and vibe-app#96 | UX "table stakes" epic | Not blockers; list the known rough edges in the release notes |
| #32 children vibe-app#150 to vibe-app#156 | BYO agent | Not blockers. Advertise MCP (shipped) and leave ACP out of the v0.1 pitch |

### Telemetry and privacy (**it doesn't phone home**)

- No analytics or crash-reporting SDKs in Go or the frontend; fonts are vendored (no CDN). There are three `http.Client` uses in the Go code: `internal/llm/models.go` (lists models from the *configured* provider) and `internal/mcp/proxy.go` (loopback to the local MCP server). MCP SSE binds `127.0.0.1` with a fresh bearer token.
- Other outbound traffic happens only through:
  - DuckDB extension installs from `extensions.duckdb.org`, after consent; autoinstall and autoload are off;
  - the connections you define (MotherDuck, Postgres, S3, …);
  - the LLM provider you configure. The agent sends schema, query results and skills to it, which is the one data-egress point worth stating plainly.
- [ ] Write `docs/privacy.md` and a README section: "Vibe sends nothing to Sandwich Labs. Network traffic goes only to the data sources you connect, to the LLM provider you configure (which sees the prompts, schemas and query results the agent uses), and to extensions.duckdb.org when you approve an extension download." **S**

### Supported-platform statement (draft)

- **macOS:** Apple silicon on **macOS 14 Sonoma or later**; Intel on **macOS 13 or later** (from the DuckDB static libraries' `minos`, 14.0 for arm64 and 13.0 for x86_64; confirm in the spike). Unsigned for now, installed with the script.
- **Linux:** x86_64 and arm64, **glibc ≥ 2.35** with **WebKitGTK 4.1** for Studio: Ubuntu 22.04+, Debian 12+, Fedora 38+, and recent Arch or openSUSE Tumbleweed. Not supported: RHEL 8, Debian 11 and Alpine/musl. The CLI-only tarball needs glibc ≥ 2.35 and no GUI libraries.
- **Windows:** not supported yet. WSL2 with the Linux CLI should work, untested.
- **Credentials** need the macOS Keychain or a Secret Service (GNOME Keyring or KWallet); otherwise use `env:` references.

### Dependabot (live: **34 open alerts**, not 37; 3 may have been closed since)

| Severity | Count | Packages |
|---|---|---|
| Critical | 3 | npm dev: `tinypool` ×2, `vitest` (UI server) |
| High | 17 | Go: `google.golang.org/grpc` ×3, `labstack/echo/v4`, `buger/jsonparser`. npm: `brace-expansion` ×5 (dev), `form-data` (dev), `vite` (dev), `nanoid` ×2, `postcss`, `source-map-js` (build-time), `@vue/server-renderer` |
| Medium | 10 | grpc, dompurify ×2, postcss, vite ×2, vitest, @vitest/mocker, esbuild, brace-expansion |
| Low | 4 | dompurify ×4 |

By ecosystem: npm 28, Go 6. **What matters:**

1. **dompurify** (6 alerts; fixed in **≥ 3.4.16**). It's **shipped** and sanitises rendered markdown, including model output. Bump it. **S**
2. **Go, reachable per govulncheck:**
   - `google.golang.org/grpc` v1.81.1 → **≥ 1.83.1** (an indirect dependency via Google Cloud auth, on the LLM provider path);
   - `golang.org/x/net` v0.55.0 → ≥ 0.60.0;
   - `golang.org/x/text` v0.37.0 → ≥ 0.39.0;
   - `buger/jsonparser` → ≥ 1.1.2 (DoS; reached via MCP/jsonschema parsing).

   `labstack/echo` comes in through Wails' dev server. Bumping Wails or forcing the version is cheap; verify that it's not in production builds. **S**
3. **Go toolchain.** govulncheck also flags 12 standard-library vulnerabilities (net/http, HTTP/2, crypto/tls, html/template) fixed in the latest point release. Build releases with the **latest Go 1.26.x patch**. **S**
4. **Dev-only npm** (vitest, vite, tinypool, brace-expansion, esbuild, form-data): no user impact, but contributors run them. Bump vitest to 4.1.11+ and vite to 6.4.3+ (or 7). **S–M**
5. `@vue/server-renderer`, `nanoid`, `postcss` and `source-map-js` aren't in the client bundle (SSR unused; the rest are build tools). Bump `vue` anyway. **S**
6. Enable Dependabot version updates (weekly, grouped) and run govulncheck in CI so this doesn't pile up again. **S**

---

## Proposed issue breakdown

**Epic: Open-source launch and v0.1.0 (unsigned, install script).** Make the repo public under a clear licence and name, and ship v0.1.0 for macOS (arm64, x86_64) and Linux (amd64, arm64) as ad-hoc-signed tarballs installed by `curl … | sh`, with checksums and provenance. This supersedes #4 for v0.1.0. #4 keeps signing, notarisation, Homebrew and deb/rpm for v0.2.
*Done when* v0.1.0 is published from CI, `curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh` installs a working CLI and Studio on a clean macOS 14+ Mac and an Ubuntu 22.04+ machine, and the public repo passes the hygiene checklist.

Children, in order:

1. **Decide licence, name and repo layout (D1–D5, D11, D12).** Record the decisions in `docs/direction.md` §9:
   - the licence;
   - the product and CLI names, after a USPTO search (Mistral Vibe and "Vibe Studio" collisions);
   - the public repo name and module path;
   - the history option;
   - the work-email question;
   - the security contact.

   *Done when* each decision has a one-line record and the follow-up issues reference it.

2. **Rename the module path (and binary, if decided) and fix `go install`.**
   - `go mod edit -module` to the real org or vanity path; update imports (206 files), Taskfile ldflags and docs;
   - optionally move `main` to `cmd/vibe`;
   - if the CLI is renamed, update cobra `Use`, `launchGUI`, `hub.LocateVibe`, docs and MCP config snippets.

   *Done when* `go build ./...` and `go test ./...` pass, and `GOFLAGS=-mod=mod go install <path>@<branch>` builds the CLI from a clean GOPATH.

3. **Add licence, community files and repo settings.**
   - `LICENSE`, `CONTRIBUTING.md` (build from source, DCO), `SECURITY.md`, `CODE_OF_CONDUCT.md`;
   - issue and PR templates, `config.yml`;
   - SPDX in `wails.json` copyright.

   *Done when* GitHub's Community Standards checklist is all green on the public repo.

4. **Prune internal content.**
   - Delete `landing/`, `docs/reviews/ux-panel-2026-09-26/` and `docs/archive/*.txt`;
   - move ast-grep rules to `tools/ast-grep/` and `RODNEY.md` into `docs/contributing/`;
   - replace the personal home-directory path in `sql_policy_test.go`;
   - delete stale remote branches.

   *Done when* no personal paths remain (`account_id` hits are the legitimate R2/Cloudflare field name), `internal/docsref` passes, and `docs/README.md` has no dead links.

5. **Third-party notices and an About screen.**
   - A `task notices` target generating `THIRD_PARTY_NOTICES.md` (go-licenses, the frontend bundle's licences, DuckDB's bundled components, OFL texts, the Lucide ISC notice);
   - OFL licence files beside the fonts;
   - a `GetBuildInfo` binding and Settings ▸ About with the version/commit/date, licence, NOTICE viewer, privacy link and "Report a bug";
   - `go-licenses check` in CI.

   *Done when* the About screen shows the stamped version in a release build and the NOTICE ships in every archive.

6. **Dependency and security sweep.**
   - Bump dompurify ≥ 3.4.16, grpc ≥ 1.83.1, x/net, x/text, jsonparser, vue, vite and vitest;
   - pin the latest Go 1.26.x;
   - enable Dependabot version updates and add govulncheck to CI;
   - triage #21: audit SQL quoting on agent- and MCP-reachable paths, and file the rest as post-launch.

   *Done when* there are no open critical/high Dependabot alerts on shipped code, govulncheck is clean (or every exception is documented), and #21 has a written triage.

7. **Fix release-blocking bugs: #2, #19, #33, #34/#35.** Studio keeps (or cancels and says so) an in-flight agent turn across navigation. Auto-number gaps and next values are correct after reopen. The launch target is consumed once. `vue-tsc --noEmit` passes. *Done when* each issue closes with a test, and a Rodney check where it touches UI.

8. **macOS unsigned spike** (`macos-15` runner):
   - `wails build` for arm64 and amd64;
   - ad-hoc signing without the hardened runtime;
   - a curl-style (non-quarantined) launch;
   - `LOAD excel`/`postgres`/`httpfs`;
   - a keychain credential survives a re-signed upgrade;
   - `OnFileOpen` from Finder for a folder and a `.vibe` file;
   - confirm the minimum macOS from the binary's `LC_BUILD_VERSION`.

   *Done when* findings are written into `docs/packaging.md` and any blocker has an issue.

9. **Studio build hygiene and app icon.**
   - The master SVG and the icon set: `appicon.png` (un-ignored), hicolor PNGs and SVG;
   - bundle name `Vibe Studio.app`;
   - `LSMinimumSystemVersion` per the spike;
   - `npm ci` in `wails.json`;
   - consistent `duckdb_arrow` tags;
   - a script that stamps `wails.json` `productVersion` from the tag;
   - `launchGUI` `EvalSymlinks` plus the darwin `open -a` fallback.

   *Done when* a local `task build:studio*` produces a correctly named and iconed app that reports the right version on both platforms.

10. **CI workflow (`ci.yml`).** The test job (Go, vitest, vue-tsc, lint, govulncheck, licences, shellcheck) and the build-smoke matrix (ubuntu-22.04, ubuntu-22.04-arm, macos-15) with the CLI round trip. Require the checks on `main`. *Done when* a PR shows green checks on all three platforms and branch protection requires them.

11. **Release workflow (`release.yml`).**
    - Tag-triggered native builds;
    - staging scripts for the tarball layouts (§4), including Linux CLI-only;
    - macOS ad-hoc codesign and verify;
    - `checksums.txt`, build-provenance attestations, a draft release with notes from `CHANGELOG.md`, and `install.sh` uploaded;
    - `-rc` tags become prereleases;
    - delete `.goreleaser.yaml` and `task release:*`.

    *Done when* pushing `v0.1.0-rc1` produces a complete draft release with eight archives, the checksums, the attestations and the script.

12. **`install.sh`, plus uninstall, upgrade and installer tests.** The script per §4: detection, checksum verification, the collision guard, macOS app and wrapper, Linux desktop/icon/MIME, the WebKit and Secret Service checks, the manifest, `--uninstall`, `--cli-only` and pinning. A CI job installs from the draft's artifacts into Ubuntu 22.04/24.04, Debian 12 and Fedora containers and onto a macOS runner, then uninstalls. Add the `sandwichlabs.dev/vibe/install.sh` redirect. *Done when* the installer tests pass against rc1 and a fresh Mac and Linux machine run Studio after the one-liner.

13. **Newcomer docs and changelog.**
    - `docs/install.md`, `docs/quickstart.md` (works with no accounts), `docs/troubleshooting.md`, `docs/privacy.md`, `docs/architecture.md`;
    - a connection-recipes page and an MCP client setup page;
    - the README rewritten for outsiders, with supported platforms;
    - `docs/packaging.md` updated for the unsigned path;
    - `CHANGELOG.md` with the v0.1.0 entry and known limitations.

    *Done when* someone who has never seen the repo can go from the README to a running query and an MCP-connected Claude Code without asking.

14. **Launch assets and website.** Real screenshots, the README hero GIF, the launch film on the release page, the social preview image, and the website project page updated (install one-liner, GitHub link, docs). *Done when* the README, release page and sandwichlabs.dev page all show current UI and the install command.

15. **Go public and cut v0.1.0.**
    - Execute the history decision (filter-repo into the new repo; re-run gitleaks on the result);
    - transfer open issues;
    - set visibility, branch protection, private vulnerability reporting, Discussions and topics;
    - run rc1 → install tests → v0.1.0;
    - archive the old private repo.

    *Done when* v0.1.0 is public with a working one-liner and the private repo is archived.

---

### Appendix: how the scans were run

- `gitleaks git --log-opts=--all --redact` (v8.30.1, sha256-verified release binary). Findings were triaged by masking values to lengths and prefixes only.
- A custom Python scan over `git log --all -p` additions (patterns listed in §2), reporting rule, path and commit only.
- `git rev-list --objects --all | git cat-file --batch-check` for blob sizes.
- `go-licenses report ./ ./studio` on a `git archive main` export; a licence census from `package-lock.json`.
- `govulncheck ./...` (tags `webkit2_41,duckdb_arrow,desktop,production`, local Go 1.27.1).
- `gh api …/dependabot/alerts` (open alerts only).
- A test build of the CLI on Fedora 44: 82 MB, links only libc/libstdc++/libm/libgcc_s/libresolv, needs `GLIBC_2.43`. This confirms releases must be built on Ubuntu 22.04.
- `llvm-otool -l` on the darwin DuckDB static libraries: `minos` 14.0 (arm64) and 13.0 (amd64).
