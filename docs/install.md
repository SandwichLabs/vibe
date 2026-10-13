# Install Vibe

Vibe is two programs: `vibe` (the command line, the terminal UI and `vibe mcp serve`) and `vibe-studio` (the desktop app). The installer puts both on your machine without `sudo`.

## Install with the script

```sh
curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh
```

That address redirects to the script attached to the latest GitHub release. If you prefer to skip the redirect, use the release URL directly:

```sh
curl -fsSL https://github.com/SandwichLabs/vibe/releases/latest/download/install.sh | sh
```

Then check it:

```sh
vibe --version
```

The script is plain POSIX `sh`, runs nothing until the whole file has downloaded, never uses `sudo`, and sends no telemetry. Read it first if you like: it is [`install.sh`](https://github.com/SandwichLabs/vibe/blob/main/install.sh) in the repository.

### Options

Pass options after `sh -s --`:

```sh
curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh -s -- --cli-only
```

| Flag | Environment | What it does |
|---|---|---|
| `--version vX.Y.Z` | `VIBE_VERSION` | Install this release instead of the latest. |
| `--cli-only` | | Install only `vibe`: no desktop app. Right for servers and MCP-only use. |
| `--install-dir DIR` | `VIBE_INSTALL_DIR` (or `INSTALL_DIR`) | Where the binaries go. Default `~/.local/bin`. |
| `--force` | | Replace a `vibe` this installer did not put there, and skip the OS version check. |
| `--uninstall` | | Remove exactly the files the installer recorded. See [Uninstall](#uninstall). |
| `--dry-run` | | Print what would happen. Downloads and changes nothing. |
| `--help` | | Show the script's help. |

Other environment variables: `VIBE_APP_DIR` (macOS app folder, default `~/Applications`), `VIBE_DOWNLOAD_BASE` (a mirror), `NO_COLOR`.

### What gets installed where

| | CLI only | With Studio |
|---|---|---|
| Linux | `~/.local/bin/vibe` | also `~/.local/bin/vibe-studio`, a `.desktop` entry (`~/.local/share/applications`), the `.vibe` MIME type (`~/.local/share/mime/packages`) and icons (`~/.local/share/icons/hicolor`) |
| macOS | `~/.local/bin/vibe` | also `~/Applications/Vibe Studio.app`, (`vibe studio --gui` finds the app itself) |

The installer downloads the archive and `checksums.txt`, verifies the SHA-256 against the archive's exact name, and stops before touching anything if it does not match or the entry is missing. Looking up the latest release needs `curl` (with `--version vX.Y.Z`, `wget` also works). Every file it writes is listed, with its SHA-256, in an install manifest (Linux `~/.local/share/vibe/install-manifest`; macOS `~/Library/Application Support/vibe/install-manifest`).

If `~/.local/bin` is not on your `PATH`, the installer prints the line to add to your shell startup file.

### Name collision with Mistral Vibe

Mistral Vibe, an unrelated coding agent, also installs a command called `vibe`. The installer will not overwrite a `vibe` it did not put there (it recognises its own by the hash it recorded, and never runs an unknown program to find out): it stops and tells you. Either install Vibe somewhere else (`--install-dir DIR`, then put `DIR` first on your `PATH`) or pass `--force` to replace the other program. If another `vibe` comes earlier on your `PATH`, the installer warns; `vibe --version` should print `vibe version ...`. See [Troubleshooting](troubleshooting.md#vibe-starts-a-different-program).

## Supported platforms

| Platform | Supported | Notes |
|---|---|---|
| Linux x86-64 and arm64 | Yes | glibc 2.35 or newer (Ubuntu 22.04, Debian 12, Fedora 36+ and later). The installer's tests run on Ubuntu 22.04 and 24.04, Debian 12 and current Fedora. Studio also needs WebKitGTK 4.1 and GTK 3; the CLI needs neither. Alpine and other musl systems are not supported. |
| macOS on Apple silicon | Yes | macOS 14 (Sonoma) or newer. |
| macOS on Intel | Yes | macOS 13 (Ventura) or newer. |
| Windows | No | Not supported yet. WSL2 with the Linux CLI should work but is untested. |

The macOS minimums come from the DuckDB libraries Vibe links. The builds have been run on macOS 15 only; macOS 13 and 14 are the stated minimum, not something we have tested on a machine.

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

- **Installed with the script (curl):** no Gatekeeper prompt. `curl` does not mark files as downloaded from the internet, so Vibe Studio opens normally.
- **Downloaded in a browser** (from the releases page): macOS marks the file, and Studio will not open. You see "Vibe Studio" Not Opened / "Apple could not verify...". Fix it one of two ways:
  - System Settings, Privacy & Security, scroll to the message about Vibe Studio, and click **Open Anyway**.
  - Or clear the mark in Terminal: `xattr -dr com.apple.quarantine "$HOME/Applications/Vibe Studio.app"`.
- After an upgrade, macOS may ask again for access to Documents or Desktop, because each unsigned build has a new identity. That is expected.

Why unsigned: see [Packaging](packaging.md#macos-unsigned-path-spike-43). Signing and notarisation are planned.

## Upgrade

Run the installer again. It replaces the files, removes any file the new version no longer ships, and prints the old and new version. Quit Studio first (or restart it afterwards).

Or let Vibe do exactly that for you, after you confirm:

- **`vibe update`** looks up the latest stable release on GitHub, prints the notes link and, on a terminal, asks `Install vX? [y/N]`. `--check` only checks; `--yes` installs without asking (no terminal and no `--yes` means check only). It exits 0 when the check worked (up to date or not) and any requested install succeeded, 1 on an error.
- **Studio, Settings, About, Updates** has **Check now** and, when a newer release exists, **Update to vX** (with a confirmation), then **Restart now** (the old Studio keeps running until you use it). Restart now quits through the usual path (pending saves are flushed and the unsaved-work question is asked) and starts the new Studio, reopening your workspace, once the old one has let go. A running Studio or `vibe mcp serve` keeps its old version until restarted.
- **Weekly checks are opt-in.** Studio asks once; `vibe config updates [off|weekly]` is the same switch. Vibe never checks or installs in the background without it. What is sent is in [Privacy](privacy.md).

Applying an update runs the target release's own `install.sh` (downloaded from the release, verified against that release's `checksums.txt`, never with `--force`), so it behaves exactly like re-running the installer. It only works for a Vibe that the installer put there (the running program must be listed in the install manifest); a build from source, `go install` or a package manager is told to update the way you installed it. A prerelease channel and signed checksums are not part of this yet.

To pin a version: `curl -fsSL https://sandwichlabs.dev/vibe/install.sh | VIBE_VERSION=v0.1.0 sh`.

## Uninstall

```sh
curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh -s -- --uninstall
```

This removes exactly the files listed in the install manifest, and leaves any that you changed since (their hash differs), saying which. It keeps your configuration, the downloaded DuckDB extension cache, keychain items and your workspaces (those are your folders). To clear saved credentials first, run `vibe conn credentials clear <name>` for each connection.

## Manual install

Every release has archives you can unpack yourself:

- `vibe_<version>_<os>_<arch>.tar.gz`: the CLI.
- `vibe-studio_<version>_<os>_<arch>.tar.gz`: Studio and the CLI (`bin/` and `share/` on Linux; `Vibe Studio.app` on macOS).
- `checksums.txt`: SHA-256 for each archive. Verify before you unpack (`sha256sum -c`, or `shasum -a 256 -c` on macOS).

`<os>` is `linux` or `darwin`; `<arch>` is `amd64` or `arm64`. On macOS, unpack with `tar` from a shell, not by double-clicking in a browser-downloaded folder, so the app is not marked (see above).

## Build from source

See [CONTRIBUTING.md](../CONTRIBUTING.md#build-from-source). You need Go with cgo, Node, Task, and (for Studio) the Wails CLI.

## Licences and build information

The archives include `LICENSE`, `NOTICE` and `THIRD_PARTY_NOTICES.md`. In Studio, **Settings, About** shows the version, commit, build date and platform (with **Copy build info**), the licence and the third-party notices, and **Report a bug**.

## Next

[Quickstart](quickstart.md).
