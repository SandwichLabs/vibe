# Privacy: what leaves your machine

**Vibe sends nothing to Sandwich Labs.** There is no telemetry, no analytics, no crash reporting, no update check and no account. Fonts and icons are bundled with the app (no CDN).

Vibe makes network connections only in the cases below. Everything else stays on your computer.

| Traffic | When | Where to |
|---|---|---|
| **Your connections** | When something uses a connection (a query that names it, `vibe conn test`, expanding it in Studio). Opening a workspace dials nothing remote. | The servers you configured: MotherDuck, Postgres, MySQL, S3, GCS, R2, Azure, and so on. DuckDB makes these connections. |
| **Your LLM provider** | Only if you configure one and use the agent (Studio's chat panel or a notebook AI cell). | The provider you chose in Settings, App (or `vibe config llm`): a local server on your own machine by default, or Ollama, OpenAI, Anthropic, Google Gemini or Cloudflare AI Gateway. Listing models also asks that provider (or your local server) which models it has. |
| **DuckDB extensions** | Only when you ask for a feature that needs one: the Install button, `--install` on `vibe conn test` or `vibe conn connect`, or the first Excel import or export (about 2 MB, with no separate prompt). Autoinstall and autoload are off, and community extensions are off. | `extensions.duckdb.org`, over HTTPS, into Vibe's own folder. The `motherduck` extension comes from there too. |
| **GitHub, in your browser** | Only when you click Report a bug, Apache-2.0 licence or Privacy in Settings, About, or confirm an `http(s)` link in a notebook's Markdown cell. Studio opens the link in your default browser; Vibe itself does not talk to GitHub. | `github.com` (or the link's own site). Nothing is prefilled: no workspace names, paths or data. |
| **Your own SQL** | When you write SQL that reads a URL (`read_csv('https://...')`) or a storage path. | That URL or storage. This is your query, run as written. Reading an `https://` URL needs the `httpfs` extension, which a storage connection loads; your own SQL cannot `INSTALL` or `LOAD` extensions. |
| **Loopback only** | MCP over SSE and Studio's MCP proxy. | `127.0.0.1` on your machine, with a random bearer token. Nothing is exposed to the network. |

## The LLM provider sees your data

This is the one place your data can leave your computer by design, so be clear about it. When you ask the agent a question, the provider you configured receives what the agent needs to answer: your prompt, table and column names and types, the rows your queries return (within a result-size limit), and the skills it reads. Vibe never sends credentials or the contents of your keychain to the model.

- If you use a **local model** (the default endpoint is `http://localhost:8080/v1`, or Ollama on localhost), nothing leaves your machine.
- If you use a **hosted provider**, its own terms and retention rules apply to those prompts and results.
- If you configure **no provider**, the agent does nothing and nothing is sent. SQL, notebooks, connections and `vibe mcp serve` all work without one.
- **External agents** (Claude Code and others over MCP) are your agent talking to your model provider. What they read through Vibe's tools is governed by [the same limits](mcp-clients.md#what-the-client-may-do), and by that client's own privacy terms.

Credentials stay in your OS keychain or environment variables. They never appear in the workspace folder, a `.vibe` bundle, log lines, error messages shown to the UI or the agent, or anything the agent can read.

## How to check this yourself

This page was checked against the code (pre-0.1.0 source). The Go code makes outbound requests itself in exactly two places, both to addresses you configured:

- `internal/llm/models.go`: lists models from the configured provider endpoint.
- `internal/mcp/proxy.go`: connects to the local MCP server on loopback.

Everything else goes through DuckDB (connections, extension downloads, URL reads) or the LLM provider SDK for the provider you selected, or the system browser. There are no analytics or crash-reporting libraries in the Go code or in the Studio frontend. You can confirm with `grep -rn "http\.\(Get\|NewRequest\|Client\)" internal cmd studio` and by running Vibe under a firewall or `strace -e trace=connect`.

An air-gapped machine works: the only first-use download is a DuckDB extension, and you can copy a populated extension folder from another machine running the same Vibe version (`~/.cache/vibe/duckdb/extensions` on Linux, `~/Library/Caches/vibe/duckdb/extensions` on macOS). See [troubleshooting](troubleshooting.md#extension-download-blocked-or-offline).

## Reporting a privacy problem

If you find Vibe sending something this page does not list, that is a bug. Report it privately as described in [SECURITY.md](../SECURITY.md).
