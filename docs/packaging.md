# Packaging and release plan: macOS and Linux

> **Slice 3 wiring (epic #105) is done; shipping is not.** The app side of "open a workspace folder, import a `.vibe`" is built and tested (see §1a): launch arguments, macOS `OnFileOpen`, the `Info.plist` document types, the Linux `.desktop` and MIME files. Signing, notarisation, CI, the release pipeline, the extension-bundling decision and the signed-build keychain check are still epic #77 (§5, §8).

**Date:** 2026-09-23
**Status:** Proposed, except §1a (the app-side file-open wiring, built under #105). The pipeline, signing and CI are not built. Tracking: epic #77.

Vibe ships two binaries: `vibe` (CLI, TUI, `vibe mcp serve`) and
`vibe-studio` (the Wails desktop app). Both are CGO (DuckDB, and
for Studio, WebKit), which decides almost everything below.

## 1. Where things stand

| Finding | Consequence |
|---|---|
| `.goreleaser.yaml` fails `goreleaser check` (v2.13.3): `before.hooks` with `dir:` is GoReleaser Pro syntax. | No release has ever run from it. |
| Its `vibe-studio` build is a plain `go build ./studio`, without the `desktop,production` tags `wails build` adds. | The binary would refuse to start, and there is no `.app` bundle, icon, or `.desktop` file. |
| It cross-compiles CGO for darwin and linux/arm64 from one runner with no cross toolchain. | Can't link DuckDB for those targets. |
| ~~`studio/build/` is gitignored wholesale.~~ Fixed in the #105 wrap-up (only `bin/` and Wails-generated defaults are ignored). | `Info.plist` and the Linux files are committed. **No `appicon.png` exists in the repo yet**, so Wails generates a default one; committing the real icon is a #77 item (§5.1). |
| No `.github/workflows/` at all. | No CI on PRs either. |
| The repo is private. | Release assets aren't downloadable by Homebrew or the website without a token. |
| ~~`studio/main.go` read the path to open from `os.Args[1]` only.~~ Fixed in #105 wrap-up. | On macOS an opened folder or `.vibe` arrives as an Apple Event (`mac.Options.OnFileOpen`), not argv; Studio now handles both (§1a). |
| `vibe studio --gui` looks for `vibe-studio` beside the executable, then on `PATH`. | Needs an `open -a "Vibe Studio"` path on macOS, and symlink resolution when `vibe` is a Homebrew link. |
| DuckDB extensions (`excel`, `postgres`, `httpfs`, ...) download from extensions.duckdb.org on first use, after the person agrees, into Vibe's own directory (§8). | The first use of each needs network access. On macOS, loading a `.duckdb_extension` dylib under the hardened runtime likely needs `com.apple.security.cs.disable-library-validation`. Verify in the spike. |
| Studio has no version ldflags. | The About screen and bug reports can't say which build they came from. |

## 1a. File opening, as built (#105 Slice 3)

What the app does, whichever way the OS hands it a path. All of it is `studio/workspace.go` (`classifyLaunchArg`, `setInitialArg`, `handleOpenPath`, `GetInitialWorkspace`) and `studio/frontend/src/App.vue` (`handleOpenRequest`).

| Path is | Studio does |
|---|---|
| a directory | opens it as a workspace (asks "Make this folder a workspace?" when it has no `vibe.yaml`; asks before leaving an open workspace) |
| a `*.vibe` file (any case) | opens the **Import .vibe…** dialog with the file already chosen: into the open workspace, or as a new one from the launcher. The file is read, verified and previewed first; nothing is written until **Import**. A v1 container or any non-bundle gets the plain "not a Vibe bundle" message from the preview |
| anything else, or a missing path | a plain-language notice (on the launcher, or a dismissible banner when a workspace is open) |
| a flag (`-psn_...`, `--x`) | ignored |

- **Linux and argv.** `vibe-studio <path>` and `vibe studio --gui <path>` (which execs `vibe-studio` with the absolute path; a `.vibe` is passed through to Import, only the terminal UI refuses one). A desktop entry's `Exec=vibe-studio %U` passes `file://` URIs, which are percent-decoded; only the first path is used.
- **macOS and Apple Events.** `mac.Options.OnFileOpen` (Wails v2.11) calls the same handler. Finder launches the app with no path in argv, so a path can arrive **before** the frontend is ready (cold launch by double-click) or **after** (the app is running and a folder is dropped on the Dock icon). Before: the path is queued and the frontend collects it with `GetInitialWorkspace()` on mount (the last one wins). After: the backend emits an `app:open-path` event with the same `{path, bundlePath, message}` shape, and the frontend handles it exactly like the startup case. The first `GetInitialWorkspace()` call is the moment the backend switches from queueing to emitting.
- **Verification.** The classification, the `file://` handling and the queue-then-emit switch are unit-tested (`studio/workspace_test.go`), the frontend routing in `App.spec.ts`. **Not verified on a Mac**: the darwin build needs CGO and a Mac toolchain, so `GOOS=darwin go vet ./studio/...` does not run on Linux, and whether Finder really fires `OnFileOpen` for a folder is one of the spike's questions (§6, step 1). The Wails option is plain Go and compiles on every platform.

### The macOS bundle

`studio/build/darwin/Info.plist` (committed; `studio/build/` is un-ignored except `bin/`) sets `CFBundleIdentifier` `dev.sandwichlabs.vibe-studio` and declares:

- `CFBundleDocumentTypes`, **Viewer** role for both:
  - **folders** (`public.folder`, `LSHandlerRank` Alternate): Vibe appears in *Open With* for a folder and can be chosen, but never becomes the default handler for folders;
  - **Vibe bundle** (`dev.sandwichlabs.vibe-bundle`, rank Owner).
- `UTExportedTypeDeclarations`: `dev.sandwichlabs.vibe-bundle`, conforming to `public.data`, extension `vibe`, MIME `application/x-vibe-bundle`, description "Vibe bundle".

Wails still fills in the product name, version and copyright from `wails.json` (`info`). The description everywhere says "Vibe bundle", not "Vibe database".

### The Linux files

- `studio/build/linux/vibe-studio.desktop`: `Exec=vibe-studio %U`, `MimeType=application/x-vibe-bundle;`, `Icon=vibe-studio`.
- `studio/build/linux/vibe-mime.xml`: `*.vibe` → `application/x-vibe-bundle`, comment "Vibe bundle".
- **Decision: the `.desktop` file does not claim `inode/directory`.** That MIME type would add Vibe to *Open With* for every folder on the machine and, on some desktops, compete for the "open folder" default, which is the file manager's job. A workspace folder is opened with `vibe-studio <dir>` or `vibe studio --gui <dir>`, or from Studio's own launcher (Open folder…, recent workspaces). People who want a file-manager entry can add one themselves (a Nautilus script or a Dolphin service menu that runs `vibe-studio "$@"`); this is documented, not shipped. Revisit if testers ask.
- **Install steps** (for the nfpm package, §5.3, and `install.sh` in the tarball): `/usr/share/applications/vibe-studio.desktop`, `/usr/share/mime/packages/vibe-bundle.xml` (installed under that name), hicolor icons; post-install runs `update-desktop-database` and `update-mime-database /usr/share/mime`. The nfpm config itself is part of the pipeline work in #77.

## 2. What we ship

One install per platform gives you **both** binaries, so `vibe studio --gui`
and `vibe mcp serve` work without a second download.

| Platform | Artifact | Contents | Channel |
|---|---|---|---|
| macOS (universal: arm64 + x86_64) | `Vibe-Studio-<ver>-macos-universal.dmg`, signed, notarized and stapled | `Vibe Studio.app`, with `vibe` at `Contents/MacOS/vibe` | GitHub Release, plus the Homebrew **cask** `sandwichlabs/tap/vibe-studio` (`binary` stanza links `vibe` onto `PATH`) |
| Linux amd64 and arm64 | `vibe_<ver>_<arch>.deb`, `vibe-<ver>.<arch>.rpm` | `/usr/bin/vibe`, `/usr/bin/vibe-studio`, `.desktop` file, hicolor icons, MIME type for `.vibe` bundles | GitHub Release |
| Linux amd64 and arm64 | `vibe-studio_<ver>_linux_<arch>.tar.gz` | both binaries, `.desktop`, icon, `install.sh` | GitHub Release |
| Linux amd64 and arm64, CLI only | `vibe_<ver>_linux_<arch>.tar.gz` | `vibe` | GitHub Release, plus the Homebrew **formula** `sandwichlabs/tap/vibe` (Linuxbrew, servers, MCP-only users) |
| All | `checksums.txt`, plus SBOM and build provenance attestation | | GitHub Release |

Linux baseline: built on **Ubuntu 22.04** (glibc 2.35), against
**webkit2gtk-4.1**. That covers Ubuntu 22.04+, Debian 12+, and Fedora 38+.
RHEL 8 / Debian 11 have no webkit2gtk-4.1 and are out.

Deferred, not rejected:
- **Flatpak.** The GNOME runtime ships WebKitGTK 4.1, so this is the best path
  to "any distro". Flathub review adds lead time.
- **AppImage.** Bundling WebKitGTK's helper processes is fragile and large.
- **Snap.**
- **In-app auto-update.** Sparkle on macOS; Linux relies on the package manager.
- **Windows.** The DuckDB bindings support it; there is no signing story yet.

## 3. Toolchain choice

GoReleaser OSS can't do most of the macOS half. Checked against goreleaser.com
2026-09: `app_bundles`, `dmg`, `pkg`, native sign+notarize, versioned casks,
the `prebuilt` builder, and split/merge builds are **Pro-only**. OSS
notarization uses quill, which is binary-only. The docs warn that an `.app`
where only the binary is signed "is deemed damaged by macOS."

Studio also needs a real Mac (`wails build`, WebKit/Cocoa frameworks,
`codesign`, `notarytool`) and a native Linux per arch (WebKitGTK dev headers).
So:

**Recommended: GoReleaser OSS for the Linux CLI and the release itself;
a native-runner matrix for Studio.**

- **GoReleaser OSS** (one `ubuntu-22.04` job):
  - changelog
  - Linux CLI builds (amd64 native, arm64 via `aarch64-linux-gnu-g++`)
  - CLI tarballs, `checksums.txt`, and the Homebrew formula
  - creates the GitHub release **as a draft**
- **Studio matrix** (`macos-15`, `ubuntu-22.04`, `ubuntu-22.04-arm`):
  - `wails build`, plus a native `go build` of `vibe` on the same runner
  - packaging: `.app` → dmg on the Mac, nfpm on Linux
  - uploads to the draft with `gh release upload`
- **Publish job**: re-checksums every asset, pushes the cask to the tap,
  flips the release from draft to published.

Alternatives considered:

| Option | Pros | Cons |
|---|---|---|
| **A. Recommended** (above) | $0 beyond the Apple account. Every build is native, so no osxcross. GoReleaser still owns changelog, formula, and release. | Two packaging systems: GoReleaser, and the scripts in `packaging/`. The cask is templated by hand. |
| **B. GoReleaser Pro** | One config: split builds per runner, merge, `app_bundles`, `dmg`, native notarize, versioned cask, nfpm, all in `.goreleaser.yaml`. | Paid licence. Still needs the same three runners. Wails' own bundle step would be replaced by GoReleaser's. |
| **C. goreleaser-cross** (osxcross Docker image, one Linux job builds everything) | One job for the CLI. | Can't build Studio (WebKit on Linux-for-mac is not realistic). Cross-linking DuckDB's static lib through osxcross is a risk. The Mac notarization problem remains. |

Pick B only if the two-system split in A gets annoying. The workflow shape is
the same, so switching later is cheap.

## 4. Prerequisites (outside the repo)

1. **Where downloads live.** Homebrew and the website need public asset
   URLs. The likely path is open-sourcing `SandwichLabs/vibe`. The
   fallback is publishing releases to a public `SandwichLabs/vibe` repo from
   this one's workflow, using a fine-grained token secret.
2. **A new Apple Developer Program account.** The existing one is tied to a
   former client and can't be unlinked, so this starts from scratch:
   - Enroll as an **organization** (Sandwich Labs), so Gatekeeper and the
     notarization record show the company, not a personal name. That needs a
     D-U-N-S number for the legal entity. Apple's verification can take days
     to weeks, so start it before anything else here.
   - Once enrolled: a **Developer ID Application** certificate exported as
     `.p12`, and an **App Store Connect API key** (`.p8`, key ID, issuer ID)
     for `notarytool`.
   - **Until then:** everything in §5 except signing can land and run. The
     macOS job builds the universal `.app` and dmg **unsigned**, and
     `release.yml` skips the sign/notarize steps when the cert secrets are
     absent. Unsigned macOS builds go out only as prereleases for testers,
     who must clear quarantine (`xattr -dr com.apple.quarantine`). No cask
     is published and the website offers no Mac download until notarization
     works. Linux isn't blocked at all.
3. **`SandwichLabs/homebrew-tap`** public repo, plus a token with write
   access to it (`HOMEBREW_TAP_TOKEN`).
4. **Actions secrets:**
   - `MACOS_CERT_P12_BASE64`, `MACOS_CERT_PASSWORD`, `MACOS_KEYCHAIN_PASSWORD`
   - `APPLE_API_KEY_P8_BASE64`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID`, `APPLE_TEAM_ID`
   - `HOMEBREW_TAP_TOKEN`
   - later, `RELEASES_REPO_TOKEN`, if option 1 goes that way
5. **Runner billing.** On a private repo, macOS minutes bill at a multiple of
   Linux, and arm64 Linux runners may be billed differently. Check the org
   plan. `macos-15-intel` exists until Aug 2027 and is the only way to smoke
   the x86_64 slice. After that, the universal build's Intel half is untested
   in CI.

### Open-sourcing checklist (if prerequisite 1 goes public)
- Pick a licence and add `LICENSE`. Put an SPDX line in the release
  artifacts' metadata (`nfpm`, the cask, `Info.plist` copyright).
- Scan the full history for secrets (`gitleaks detect`, `trufflehog git`)
  before flipping visibility. A public repo exposes every past commit, not
  just `main`.
- Review what's tracked that was written for internal use (`docs/archive/`,
  `.claude/`, `landing/` with its
  Cloudflare account ID) and decide keep, move, or delete.
- `README.md` install section pointing at Releases and the tap, plus
  `CONTRIBUTING.md` and `SECURITY.md` (the credential-handling claims invite reports;
  give them a private channel).
- Branch protection on `main` requiring the §5.2 CI checks.

## 5. Repo changes

In order. Each step is one small PR.

### 5.1 Build hygiene
Done in #105's wrap-up:
- `studio/build/` is un-ignored except `studio/build/bin/`, and holds `darwin/Info.plist` (§1a) and `linux/vibe-studio.desktop` and `linux/vibe-mime.xml` (§1a).
- `wails.json` has `info` (product name, version, copyright, company).
- `studio/main.go` has `Mac: &mac.Options{OnFileOpen: …}` routed to the same open path as argv, handling it arriving before and after startup.
- `vibe workspace info` shows the DuckDB version and installed extensions (§8).

Still to do (#77):
- Commit `appicon.png` (none exists today; Wails generates and the repo ignores a default) (1024², from the SchoolHouse "V" mark) and `darwin/entitlements.plist` (hardened runtime; `disable-library-validation` only if the spike proves DuckDB extensions need it).
- Rename the output to `Vibe Studio` for the bundle, and add `version`/`commit`/`date` vars to `studio/main.go`, shown in Settings → About.
- `cmd/studio.go` `launchGUI`:
  - `filepath.EvalSymlinks(os.Executable())` first, so a Homebrew-linked `vibe` finds its sibling inside the `.app`
  - on darwin, fall back to `open -a "Vibe Studio" --args <path>`
- The DuckDB extensions decision (§8).
- Fix `.goreleaser.yaml` (sketch in §7). Replace `task release:*`'s local `goreleaser release` with "push a tag".

### 5.2 CI: `.github/workflows/ci.yml` (PRs and pushes to `main`)
- `test` (`ubuntu-22.04`):
  - setup-go from `go.mod` and setup-node 22
  - `apt install libwebkit2gtk-4.1-dev libgtk-3-dev`
  - `go test ./...`, `npm ci && npm run test && npm run build` in `studio/frontend`
  - `golangci-lint`
- `build-smoke` matrix (`ubuntu-22.04`, `ubuntu-22.04-arm`, `macos-15`):
  - `wails build` with the release tags
  - `vibe --version`
  - `vibe workspace init` + `vibe import` + `vibe query` round-trip in a temp folder, a real CGO smoke test of DuckDB (the #72 CLI bug is fixed, #113)
- Cache: Go module/build cache, the npm cache, and `~/.duckdb/extensions` if vendoring.
- Wails CLI v2.11 fails on Go 1.27 (#92). Run the `wails build` steps with `GOTOOLCHAIN` set to the
  Taskfile's `WAILS_GOTOOLCHAIN` (or install that Go via setup-go) until Wails ships a fixed loader.

### 5.3 Release: `.github/workflows/release.yml` (on `push` tag `v*`)

```
goreleaser (ubuntu-22.04) ──┬──► studio-macos (macos-15) ──────┐
  draft release,            ├──► studio-linux (ubuntu-22.04) ──┼──► publish (ubuntu-latest)
  CLI tarballs, formula     └──► studio-linux (22.04-arm) ─────┘      checksums, cask, undraft
```

- **goreleaser**
  - `goreleaser release --clean`, with `release.draft: true`
  - outputs the release tag for the downstream jobs
- **studio-macos** (see §5.2's `GOTOOLCHAIN` note for the `wails build` steps below)
  - import the cert into a throwaway keychain
  - `wails build -platform darwin/universal -tags duckdb_arrow -ldflags "-X main.version=…"`
  - `go build` of `vibe` for arm64 and amd64 (`CC="clang -arch x86_64"`), then `lipo -create` into `Contents/MacOS/vibe`
  - `codesign --force --options runtime --timestamp --entitlements …`, inner binaries first, then the bundle (no `--deep`)
  - `create-dmg` or `hdiutil`, then codesign the dmg
  - `xcrun notarytool submit --wait` with the API key, then `xcrun stapler staple`
  - `spctl -a -vvv -t install` as a gate
  - `gh release upload`
- **studio-linux** (per arch; see §5.2's `GOTOOLCHAIN` note)
  - apt deps, `wails build -tags webkit2_41,duckdb_arrow`
  - `go build` of `vibe`
  - `nfpm pkg` twice (deb, rpm), using `packaging/nfpm.yaml`:
    - deb deps: `libwebkit2gtk-4.1-0`, `libgtk-3-0`
    - rpm deps: `webkit2gtk4.1`, `gtk3`
    - postinstall: `update-desktop-database`, `update-mime-database`
  - tarball, `gh release upload`
- **publish**
  - download all assets, regenerate `checksums.txt`
  - `actions/attest-build-provenance` on every artifact
  - render `packaging/homebrew/vibe-studio.rb.tmpl` (version, dmg sha256) and push it to the tap
  - `gh release edit --draft=false`
- An install-test job after publish (optional, later):
  - `brew install --cask` on `macos-15`
  - `apt install ./vibe_*.deb` in `ubuntu:24.04` and `debian:12` containers
  - `dnf install` in `fedora:latest`
  - each runs `vibe --version`

Tags `v*-rc*` run the same pipeline but publish as a prerelease and skip the
tap push.

### 5.4 Website
- `sandwichlabs.dev/projects/vibe` (draft entry in `../www`) gets a Download
  CTA to `…/releases/latest`, and flips `draft: false`, when the first
  release is public.

## 6. Order of work

0. **Start Apple organization enrollment and the D-U-N-S request.** Everything
   else can proceed while Apple verifies; only notarization (and so the cask
   and a public Mac download) waits on it.
1. **Spike (half a day, local Mac or `macos-15` runner):**
   - `wails build -platform darwin/universal` links DuckDB for both slices
   - hand-sign, notarize, and staple once (after step 0 lands; until then, ad-hoc sign with `codesign -s -` to exercise the hardened runtime and entitlements)
   - confirm `LOAD excel` (and `postgres`, `httpfs`) work under the hardened runtime
   - confirm `OnFileOpen` fires from Finder for a `.vibe` file **and** for a folder dropped on the app or its Dock icon, both cold and while running (§1a)
   - run the keychain check (§8)

   These answer the only open technical risks.
2. 5.1 build hygiene PR.
3. 5.2 CI PR. It catches regressions from here on.
4. Prerequisites 1, 3 and 4 are done. Then 5.3 release workflow, cut `v0.1.0-rc1`.
5. Install-test on clean machines/containers. Cut `v0.1.0`, update the tap.
6. Project page goes live.

## 7. `.goreleaser.yaml` sketch (OSS, Linux CLI only)

```yaml
version: 2
project_name: vibe
before:
  hooks:
    - go mod download
builds:
  - id: vibe
    main: .
    binary: vibe
    env: [CGO_ENABLED=1]
    goos: [linux]
    goarch: [amd64, arm64]
    overrides:
      - goos: linux
        goarch: arm64
        env: [CC=aarch64-linux-gnu-gcc, CXX=aarch64-linux-gnu-g++]
    ldflags:
      - -s -w
      - -X github.com/SandwichLabs/vibe/cmd.version={{.Version}}
      - -X github.com/SandwichLabs/vibe/cmd.commit={{.Commit}}
      - -X github.com/SandwichLabs/vibe/cmd.date={{.Date}}
archives:
  - formats: [tar.gz]
    name_template: "vibe_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
checksum:
  name_template: checksums-cli.txt
release:
  draft: true
  replace_existing_draft: true
brews:
  - name: vibe
    repository: { owner: SandwichLabs, name: homebrew-tap, token: "{{ .Env.HOMEBREW_TAP_TOKEN }}" }
    homepage: https://sandwichlabs.dev/projects/vibe/
    description: Agentic data workspace — CLI and MCP server
    test: system "#{bin}/vibe", "--version"
changelog:
  use: github
  filters:
    exclude: ['^docs:', '^test:', '^chore:']
```

`brews` is deprecated in recent GoReleaser in favour of `homebrew_casks`.
A cask is macOS-oriented and wouldn't give Linuxbrew users a formula. Keep
`brews` until that changes, and watch the deprecation notice in
`goreleaser check`.

## 8. Decisions and checks that stay open in #77

### DuckDB extensions: pinning and bundling

- **Pinned engine.** go-duckdb is pinned to a DuckDB version MotherDuck supports (v2.4.3 = DuckDB 1.4.1 today). Bumping it is a deliberate change, checked against MotherDuck, never a routine `go get -u`. Extension binaries are per DuckDB version and platform, so a bump also changes what has to be downloaded or bundled.
- **Download on consent, into the app-private directory.** Autoinstall and autoload are off; `extension_directory` is `<user cache dir>/vibe/duckdb/extensions`; a missing extension is a visible `extension-missing` state and the download happens only on the person's yes (Studio's Install, or `--install` on the CLI). The user's own `~/.duckdb` is never loaded. Details: [reference/connections.md](reference/connections.md#duckdb-version-and-extensions). `vibe workspace info` shows the running version and what is installed.
- **Open decision: bundle an offline set?** Options:
  - bundle nothing (today): smallest download, needs network the first time each extension is used;
  - bundle `excel` only (the one a non-technical person meets first, for xlsx import and export);
  - bundle the full set (`excel`, `httpfs`, `postgres`, `mysql`, `sqlite`, `azure`, `ducklake`, `iceberg`, `delta`, `motherduck`): works air-gapped, adds tens of MB per platform and a per-version rebuild, and every bundled dylib must be signed (macOS) or packaged with its checksum.
  Whichever is chosen, point `extension_directory` at a read-only bundled directory first and the user cache second, and keep the consent step for anything not bundled. Decide in the spike, with the hardened-runtime result in hand.

### Keychain access from signed builds

go-keyring talks to the macOS Keychain by running `/usr/bin/security`, and to the Secret Service over D-Bus on Linux. Both are exercised in development, but not from a signed, notarised, hardened-runtime `Vibe Studio.app`. Checklist item for #77, before the first public Mac build:

- [ ] On a signed and notarised build: save a connection credential in Studio, quit, relaunch, and read it back (Test the connection). Confirm no unexpected keychain prompt appears on every launch, and that the item is visible to a **re-signed** build of the next version (the keychain item's access list can be tied to the signing identity; if each release re-prompts, the secrets store needs an explicit access policy or the Data Protection keychain).
- [ ] Same for the LLM API key, and for `vibe` (the CLI inside the bundle) reading a credential Studio saved, since both must see one item.
- [ ] On Linux: a headless box with no Secret Service. Confirm the "no keychain" message is clear and that `env:` references (the CI path) still work.

If the check fails, the fallback is **not** a plaintext file (see the direction doc's non-goals); it is `env:` references plus a clear message.
