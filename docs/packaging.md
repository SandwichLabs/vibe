# Packaging and release plan: macOS and Linux

**Date:** 2026-09-23
**Status:** Proposed. Nothing here is built yet. Tracking: epic #77.

Vibe ships two binaries: `vibe` (CLI, TUI, `vibe mcp serve`) and
`vibe-studio` (the Wails desktop app). Both are CGO (SQLCipher, DuckDB, and
for Studio, WebKit), which decides almost everything below.

## 1. Where things stand

| Finding | Consequence |
|---|---|
| `.goreleaser.yaml` fails `goreleaser check` (v2.13.3): `before.hooks` with `dir:` is GoReleaser Pro syntax. | No release has ever run from it. |
| Its `vibe-studio` build is a plain `go build ./studio`, without the `desktop,production` tags `wails build` adds. | The binary would refuse to start, and there is no `.app` bundle, icon, or `.desktop` file. |
| It cross-compiles CGO for darwin and linux/arm64 from one runner with no cross toolchain. | Can't link SQLCipher/DuckDB for those targets. |
| `studio/build/` is gitignored wholesale. | App icon, `Info.plist`, and entitlements can't be committed. Wails regenerates defaults. |
| No `.github/workflows/` at all. | No CI on PRs either. |
| The repo is private. | Release assets aren't downloadable by Homebrew or the website without a token. |
| `studio/main.go` reads the file to open from `os.Args[1]` only. | On macOS, double-clicking a `.vibe` file arrives as an Apple Event (`mac.Options.OnFileOpen`), not argv, so it would do nothing. |
| `vibe studio --gui` looks for `vibe-studio` beside the executable, then on `PATH`. | Needs an `open -a "Vibe Studio"` path on macOS, and symlink resolution when `vibe` is a Homebrew link. |
| xlsx import runs `INSTALL excel`, which downloads from extensions.duckdb.org on first use. | It needs network access the first time (at odds with local-first). On macOS, loading a `.duckdb_extension` dylib under the hardened runtime likely needs `com.apple.security.cs.disable-library-validation`. Verify in the spike. |
| Studio has no version ldflags. | The About screen and bug reports can't say which build they came from. |

## 2. What we ship

One install per platform gives you **both** binaries, so `vibe studio --gui`
and `vibe mcp serve` work without a second download.

| Platform | Artifact | Contents | Channel |
|---|---|---|---|
| macOS (universal: arm64 + x86_64) | `Vibe-Studio-<ver>-macos-universal.dmg`, signed, notarized and stapled | `Vibe Studio.app`, with `vibe` at `Contents/MacOS/vibe` | GitHub Release, plus the Homebrew **cask** `sandwichlabs/tap/vibe-studio` (`binary` stanza links `vibe` onto `PATH`) |
| Linux amd64 and arm64 | `vibe_<ver>_<arch>.deb`, `vibe-<ver>.<arch>.rpm` | `/usr/bin/vibe`, `/usr/bin/vibe-studio`, `.desktop` file, hicolor icons, MIME type for `.vibe` | GitHub Release |
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
   URLs. The likely path is open-sourcing `SandwichLabs/vibe-app`. The
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
- Review what's tracked that was written for internal use (`PLAN.md`,
  `PROJECT.md`, `motherduck-research.md`, `.claude/`, `landing/` with its
  Cloudflare account ID) and decide keep, move, or delete.
- `README.md` install section pointing at Releases and the tap, plus
  `CONTRIBUTING.md` and `SECURITY.md` (the encryption claims invite reports;
  give them a private channel).
- Branch protection on `main` requiring the §5.2 CI checks.

## 5. Repo changes

In order. Each step is one small PR.

### 5.1 Build hygiene
- Un-ignore `studio/build/` except `studio/build/bin/`. Commit:
  - `appicon.png` (1024², from the SchoolHouse "V" mark)
  - `darwin/Info.plist`, with `CFBundleIdentifier` `dev.sandwichlabs.vibe-studio`, plus `CFBundleDocumentTypes` and `UTExportedTypeDeclarations` for `.vibe` → `dev.sandwichlabs.vibe`
  - `darwin/entitlements.plist` (hardened runtime; `disable-library-validation` only if the spike proves DuckDB extensions need it)
  - `linux/vibe-studio.desktop`, `linux/vibe-mime.xml`
- `wails.json`:
  - `"info"`: product name, version, copyright
  - `"fileAssociations"` for `.vibe`
  - rename output to `Vibe Studio` for the bundle
- `studio/main.go`:
  - add `Mac: &mac.Options{OnFileOpen: …}`, routed to the same open-file path as argv, and handle it arriving before and after startup
  - add `version`/`commit`/`date` vars, shown in Settings → About
- `cmd/studio.go` `launchGUI`:
  - `filepath.EvalSymlinks(os.Executable())` first, so a Homebrew-linked `vibe` finds its sibling inside the `.app`
  - on darwin, fall back to `open -a "Vibe Studio" --args <file>`
- DuckDB extensions. Decide between two options:
  - vendor the `excel` extension per platform into the bundle (`Contents/Resources/duckdb_extensions/…`, `/usr/lib/vibe/…`) and point DuckDB's `extension_directory` at it, which keeps xlsx import offline;
  - or document that the first xlsx import needs network access.
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
  - `vibe init` + `vibe info` round-trip on a temp file with `VIBE_PASSPHRASE`, a real CGO smoke test of SQLCipher and DuckDB (fixing #72 first makes this more meaningful)
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
   - `wails build -platform darwin/universal` links SQLCipher and DuckDB for both slices
   - hand-sign, notarize, and staple once (after step 0 lands; until then, ad-hoc sign with `codesign -s -` to exercise the hardened runtime and entitlements)
   - confirm `LOAD excel` works under the hardened runtime
   - confirm `OnFileOpen` fires from Finder

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
      - -X github.com/sandwich-labs/vibe-app/cmd.version={{.Version}}
      - -X github.com/sandwich-labs/vibe-app/cmd.commit={{.Commit}}
      - -X github.com/sandwich-labs/vibe-app/cmd.date={{.Date}}
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
    description: Encrypted single-file data workspace — CLI and MCP server
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
