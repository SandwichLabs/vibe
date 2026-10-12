# Troubleshooting

Find the symptom, then the fix. For anything not here, see [Report a bug](#report-a-bug).

## Studio will not start on Linux (webkit missing)

Symptom: `vibe-studio` exits at once with an error about `libwebkit2gtk-4.1.so`, `libgtk-3.so` or a missing shared library. Studio needs WebKitGTK **4.1** and GTK 3. Install them:

```sh
sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0      # Debian, Ubuntu
sudo dnf install webkit2gtk4.1 gtk3                  # Fedora
sudo pacman -S webkit2gtk-4.1 gtk3                   # Arch
sudo zypper install libwebkit2gtk-4_1-0 gtk3         # openSUSE
```

Older distributions that only have `webkit2gtk-4.0` (Ubuntu 20.04, Debian 11, RHEL 8) are not supported. The `vibe` command line works without any of this.

On a blank or glitchy window under Wayland, try `GDK_BACKEND=x11 vibe-studio`. If the window is blank under a VM or remote desktop, `WEBKIT_DISABLE_COMPOSITING_MODE=1 vibe-studio` may help (a general WebKitGTK workaround, not specific to Vibe).

## "glibc is too old" or musl

The installer needs glibc 2.35 or newer (Ubuntu 22.04, Debian 12, Fedora 36+). RHEL 8, Debian 11 and Alpine are not supported. Build from source ([CONTRIBUTING](../CONTRIBUTING.md#build-from-source)) or pass `--force` to try anyway.

## Vibe starts a different program

Symptom: `vibe` opens a coding agent with a different interface, or `vibe --version` does not print `vibe version ...`. Mistral Vibe also installs a `vibe` command.

1. `command -v vibe` shows which one runs. `type -a vibe` lists all of them.
2. Put Vibe's folder first on your `PATH` (the installer printed the line), or call it by path (`~/.local/bin/vibe`).
3. If the installer refused to continue because `~/.local/bin/vibe` was another program, install elsewhere with `--install-dir DIR`, or replace the other with `--force`.

## `vibe: command not found`

The install folder (`~/.local/bin` by default) is not on your `PATH`. Add it to your shell startup file: `export PATH="$HOME/.local/bin:$PATH"`, then open a new terminal.

## macOS says Vibe Studio cannot be opened (Gatekeeper)

Symptom: "Vibe Studio" Not Opened, or "Apple could not verify ...". The v0.1 builds are not notarised. This appears only if the app was downloaded by a browser (which marks it); the install script does not trigger it. Either:

- System Settings, Privacy & Security, scroll down, click **Open Anyway** next to the Vibe Studio message, or
- `xattr -dr com.apple.quarantine "$HOME/Applications/Vibe Studio.app"` (adjust the path if you put it elsewhere).

If you want to avoid the prompt, reinstall with the script. If macOS asks again for access to Documents or Desktop after an upgrade, allow it: each unsigned build is a new identity to macOS.

## The keychain is unavailable (Secret Service)

Symptom: saving a password or token fails with a message that the keychain is "not reachable" or locked, or you are on a server, over SSH or in a container with no desktop session. Vibe never falls back to a plain file.

- **Linux with a desktop:** unlock GNOME Keyring or KWallet (log in normally, or start `gnome-keyring-daemon`), and make sure `DBUS_SESSION_BUS_ADDRESS` is set in the shell that starts Vibe.
- **Headless or CI:** sign in with environment variables instead: `vibe conn add NAME ... --sign-in env:PREFIX` and set `PREFIX_USER`, `PREFIX_PASSWORD` and so on (`env:MOTHERDUCK` reads `MOTHERDUCK_TOKEN`). Or keep them in the workspace `.env` and run `vibe workspace env allow`. See [connection recipes](connections.md#sign-in-methods).
- Or choose **Ask me each time** (`--sign-in prompt`).

## Extension download blocked or offline

Symptom: a connection shows `extension-missing`, or an install fails with a network error. Vibe downloads a DuckDB extension only when you ask for a feature that needs one (Install, `--install`, or an Excel import or export, which fetches the `excel` extension, about 2 MB, the first time); connecting never installs.

- Press **Install** in Studio (Settings, App, Extensions, or on the connection), or run `vibe conn test NAME --install`.
- The download comes from `https://extensions.duckdb.org`. Behind a proxy or firewall, allow that host.
- **Offline or air-gapped:** on a machine that has network access and the same Vibe version, install the extensions you need, then copy `~/.cache/vibe/duckdb/extensions` (Linux) or `~/Library/Caches/vibe/duckdb/extensions` (macOS) to the offline machine. The version must match: extensions are built per DuckDB version.
- Vibe ships no extensions inside the app yet, so the first use of each needs the network once.

## MotherDuck will not connect

- **No token.** The status is `needs-credentials`. Get a token at app.motherduck.com, then either enter it in the connection editor (it goes to your keychain), or `export MOTHERDUCK_TOKEN=...` and add the connection with `--sign-in env:MOTHERDUCK`. A `MOTHERDUCK_TOKEN` that is merely in your environment is ignored unless the connection signs in with `env:MOTHERDUCK`.
- **"Reopen this workspace to finish connecting."** The MotherDuck extension or the token arrived after the workspace opened. Reopen the workspace (Studio's button does it).
- **"can't be attached read-only."** MotherDuck has no read-only attach. Use a read-scaling token for read-only access.
- **Two MotherDuck accounts.** One account per workspace; the second connection is reported and not registered.
- Run `vibe conn test NAME --install` for a plain, redacted message.

## Workspace is in use by another process

Symptom: `vibe query` fails with "workspace NAME is open in vibe mcp serve (pid N), which owns its database", or Studio shows the workspace read-only. DuckDB lets one process write a database at a time, so one process (Studio or `vibe mcp serve`) owns a workspace.

- Close the owner, or use `--read-only` on `query`/`export`/`studio` (it reads connections such as MotherDuck or Postgres but cannot reach the workspace's own `.duckdb` files while they are locked).
- In Studio, **Take over** asks a headless `vibe mcp serve` to let go; a client using it must reconnect.
- A crash does not leave a stuck lock: it is released when the process dies.

## MCP: "port in use", or the client cannot connect

- Over stdio (the normal setup) there is no port. The client starts `vibe mcp serve` itself, so check the command path in its config is **absolute**, and that the workspace path exists. Run the same command in a terminal: it should wait silently for input.
- With `--transport sse`, the default address `127.0.0.1:0` picks a free port. If you chose a fixed one with `--addr` and it is taken, pick another. Non-loopback addresses are refused.
- If Studio has the workspace with MCP off, `vibe mcp serve` stops and says to turn it on (Settings, Workspace, Claude and other tools) or close Studio.
- After a take-over, reconnect the client (Claude Code: `/mcp`).
- stdout is the protocol channel and must stay clean; any log you see in a terminal is on stderr and is expected.

## The agent refuses to write

By design, a write needs the tier, a writable connection and your approval. Over headless MCP with "Ask me before it changes data" on, every write is refused; non-local connections (Postgres, MotherDuck and so on) are always refused there. See [what the client may do](mcp-clients.md#what-the-client-may-do) and the [agent policy](reference/agent-policy.md#writes-need-three-gates).

## The agent says it has no model

The chat agent needs a model provider. Set one in Studio (Settings, App) or with `vibe config llm set ...` and `vibe config llm-key`. The default points at a local server on `http://localhost:8080/v1`; if nothing is running there, the agent cannot answer. See [LLM configuration](reference/llm-config.md). Everything else works without a model.

## Where the logs are

Vibe writes no log file. The command line and Studio log structured JSON lines to **stderr**, and `vibe mcp serve` prints plain-text notices there. The command line and Studio log through the same secret redactor: it scrubs every secret value they have seen and credential-shaped text such as `password=…` or the password in `user:pass@host`. It cannot know every value, so read the log before you share it. Run Studio from a terminal (`vibe studio --gui` or `vibe-studio`) to see them; add `--verbose` to the CLI for more. Settings live in `~/.vibe.yaml` and the per-user config directory (`~/.config/vibe` on Linux, `~/Library/Application Support/vibe` on macOS); set `VIBE_CONFIG_DIR` to use a different one, for example when testing. Per-workspace local state is in `.vibe-local/` inside the workspace folder (git-ignored).

## Report a bug

1. In Studio open **Settings, About** (version, commit, build date, platform), press **Copy build info**, and paste it into the report. From a terminal, `vibe --version` prints the version, commit and build date.
2. Press **Report a bug** (it opens GitHub's issue chooser in your browser, with nothing prefilled), or go to <https://github.com/SandwichLabs/vibe/issues/new/choose>.
3. Say what you did, what you expected and what happened. Add stderr output if there is any.
4. **Do not paste credentials, tokens, connection strings or private data.** The command line and Studio redact known secrets from their logs, but the redactor cannot know every value, so check before you post.

For security problems, do not open a public issue: use GitHub private vulnerability reporting as described in [SECURITY.md](../SECURITY.md).
