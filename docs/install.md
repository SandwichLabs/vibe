# Install Slaw

Slaw is two programs: `slaw` (the command line, the terminal UI and `slaw mcp serve`) and `slaw-studio` (the desktop app). The installer puts both on your machine without `sudo`.

## Install with the script

```sh
curl -fsSL https://sandwichlabs.dev/slaw/install.sh | sh
```

That address redirects to the script attached to the latest GitHub release. If you prefer to skip the redirect, use the release URL directly:

```sh
curl -fsSL https://github.com/SandwichLabs/slaw/releases/latest/download/install.sh | sh
```

Then check it:

```sh
slaw --version
```

The script is plain POSIX `sh`, runs nothing until the whole file has downloaded, never uses `sudo`, and sends no telemetry. Read it first if you like: it is [`install.sh`](https://github.com/SandwichLabs/slaw/blob/main/install.sh) in the repository.

### Options

Pass options after `sh -s --`:

```sh
curl -fsSL https://sandwichlabs.dev/slaw/install.sh | sh -s -- --cli-only
```

| Flag | Environment | What it does |
|---|---|---|
| `--version vX.Y.Z` | `SLAW_VERSION` | Install this release instead of the latest. |
| `--cli-only` | | Install only `slaw`: no desktop app. Right for servers and MCP-only use. |
| `--install-dir DIR` | `SLAW_INSTALL_DIR` (or `INSTALL_DIR`) | Where the binaries go. Default `~/.local/bin`. |
| `--force` | | Replace a `slaw` this installer did not put there, and skip the OS version check. |
| `--uninstall` | | Remove exactly the files the installer recorded. See [Uninstall](#uninstall). |
| `--dry-run` | | Print what would happen. Downloads and changes nothing. |
| `--help` | | Show the script's help. |

Other environment variables: `SLAW_APP_DIR` (macOS app folder, default `~/Applications`), `SLAW_DOWNLOAD_BASE` (a mirror), `NO_COLOR`.

### What gets installed where

| | CLI only | With Studio |
|---|---|---|
| Linux | `~/.local/bin/slaw` | also `~/.local/bin/slaw-studio`, a `.desktop` entry (`~/.local/share/applications`), the `.slaw` MIME type (`~/.local/share/mime/packages`) and icons (`~/.local/share/icons/hicolor`) |
| macOS | `~/.local/bin/slaw` | also `~/Applications/Slaw.app`, (`slaw studio --gui` finds the app itself) |

The installer downloads the archive and `checksums.txt`, verifies the SHA-256 against the archive's exact name, and stops before touching anything if it does not match or the entry is missing. Looking up the latest release needs `curl` (with `--version vX.Y.Z`, `wget` also works). Every file it writes is listed, with its SHA-256, in an install manifest (Linux `~/.local/share/slaw/install-manifest`; macOS `~/Library/Application Support/slaw/install-manifest`).

If `~/.local/bin` is not on your `PATH`, the installer prints the line to add to your shell startup file.

### Name collision with another `slaw`

Another program may already install a command called `slaw`. The installer will not overwrite a `slaw` it did not put there (it recognises its own by the hash it recorded, and never runs an unknown program to find out): it stops and tells you. Either install Slaw somewhere else (`--install-dir DIR`, then put `DIR` first on your `PATH`) or pass `--force` to replace the other program. If another `slaw` comes earlier on your `PATH`, the installer warns; `slaw --version` should print `slaw version ...`. See [Troubleshooting](troubleshooting.md#slaw-starts-a-different-program).

## Supported platforms

| Platform | Supported | Notes |
|---|---|---|
| Linux x86-64 and arm64 | Yes | glibc 2.35 or newer (Ubuntu 22.04, Debian 12, Fedora 36+ and later). The installer's tests run on Ubuntu 22.04 and 24.04, Debian 12 and current Fedora. Studio also needs WebKitGTK 4.1 and GTK 3; the CLI needs neither. Alpine and other musl systems are not supported. |
| macOS on Apple silicon | Yes | macOS 14 (Sonoma) or newer. |
| macOS on Intel | Yes | macOS 13 (Ventura) or newer. |
| Windows | No | Not supported yet. WSL2 with the Linux CLI should work but is untested. |

The macOS minimums come from the DuckDB libraries Slaw links. The builds have been run on macOS 15 only; macOS 13 and 14 are the stated minimum, not something we have tested on a machine.

### Linux libraries for Studio

Studio needs WebKitGTK 4.1 and GTK 3. The installer checks and prints the command for your distribution if they are missing. For reference:

```sh
sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0        # Debian, Ubuntu
sudo dnf install webkit2gtk4.1 gtk3                    # Fedora, RHEL 10+
sudo pacman -S webkit2gtk-4.1 gtk3                     # Arch
sudo zypper install libwebkit2gtk-4_1-0 gtk3           # openSUSE
```

To keep database passwords and tokens in your keychain, you also need a Secret Service provider (GNOME Keyring or KWallet) running in your session. Without one, sign in with environment variables instead (`env:PREFIX`; see [connection recipes](connections.md)).

## macOS and Gatekeeper

The v0.1 builds are **not notarised** (they are ad-hoc signed, which Apple silicon requires). What that means for you:

- **Installed with the script (curl):** no Gatekeeper prompt. `curl` does not mark files as downloaded from the internet, so Slaw opens normally.
- **Downloaded in a browser** (from the releases page): macOS marks the file, and Studio will not open. You see "Slaw" Not Opened / "Apple could not verify...". Fix it one of two ways:
  - System Settings, Privacy & Security, scroll to the message about Slaw, and click **Open Anyway**.
  - Or clear the mark in Terminal: `xattr -dr com.apple.quarantine "$HOME/Applications/Slaw.app"`.
- After an upgrade, macOS may ask again for access to Documents or Desktop, because each unsigned build has a new identity. That is expected.

Why unsigned: see [Packaging](packaging.md#macos-unsigned-path-spike-43). Signing and notarisation are planned.

## Upgrade

Run the installer again. It replaces the files, removes any file the new version no longer ships, and prints the old and new version. Quit Studio first (or restart it afterwards).

Or let Slaw do exactly that for you, after you confirm:

- **`slaw update`** looks up the latest stable release on GitHub, prints the notes link and, on a terminal, asks `Install vX? [y/N]`. `--check` only checks; `--yes` installs without asking (no terminal and no `--yes` means check only). It exits 0 when the check worked (up to date or not) and any requested install succeeded, 1 on an error.
- **Studio, Settings, About, Updates** has **Check now** and, when a newer release exists, **Update to vX** (with a confirmation), then **Restart now** (the old Studio keeps running until you use it). Restart now quits through the usual path (pending saves are flushed and the unsaved-work question is asked) and starts the new Studio, reopening your workspace, once the old one has let go. A running Studio or `slaw mcp serve` keeps its old version until restarted.
- **Weekly checks are opt-in.** Studio asks once; `slaw config updates [off|weekly]` is the same switch. Slaw never checks or installs in the background without it. What is sent is in [Privacy](privacy.md).

Applying an update runs the target release's own `install.sh` (downloaded from the release, verified against that release's `checksums.txt`, never with `--force`), so it behaves exactly like re-running the installer. It only works for a Slaw that the installer put there (the running program must be listed in the install manifest); a build from source, `go install` or a package manager is told to update the way you installed it. A prerelease channel and signed checksums are not part of this yet.

To pin a version: `curl -fsSL https://sandwichlabs.dev/slaw/install.sh | SLAW_VERSION=v0.1.0 sh`.

## Uninstall

```sh
curl -fsSL https://sandwichlabs.dev/slaw/install.sh | sh -s -- --uninstall
```

This removes exactly the files listed in the install manifest, and leaves any that you changed since (their hash differs), saying which. It keeps your configuration, the downloaded DuckDB extension cache, keychain items and your workspaces (those are your folders). To clear saved credentials first, run `slaw conn credentials clear <name>` for each connection.

## Manual install

Every release has archives you can unpack yourself:

- `slaw_<version>_<os>_<arch>.tar.gz`: the CLI.
- `slaw-studio_<version>_<os>_<arch>.tar.gz`: Studio and the CLI (`bin/` and `share/` on Linux; `Slaw.app` on macOS).
- `checksums.txt`: SHA-256 for each archive. Verify before you unpack (`sha256sum -c`, or `shasum -a 256 -c` on macOS).

`<os>` is `linux` or `darwin`; `<arch>` is `amd64` or `arm64`. On macOS, unpack with `tar` from a shell, not by double-clicking in a browser-downloaded folder, so the app is not marked (see above).

## Build from source

See [CONTRIBUTING.md](../CONTRIBUTING.md#build-from-source). You need Go with cgo, Node, Task, and (for Studio) the Wails CLI.

## Licences and build information

The archives include `LICENSE`, `NOTICE` and `THIRD_PARTY_NOTICES.md`. In Studio, **Settings, About** shows the version, commit, build date and platform (with **Copy build info**), the licence and the third-party notices, and **Report a bug**.

## Next

[Quickstart](quickstart.md).
