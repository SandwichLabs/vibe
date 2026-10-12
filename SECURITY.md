# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub: open this repository's **Security** tab, choose **Report a vulnerability**, and fill in the form. This is the only reporting channel; there is no security email address. Please do not open a public issue or discussion for a vulnerability.

Include the affected version (`vibe --version`), your OS, steps to reproduce, and the impact you see. Do not include real credentials in a report.

This is a small project, so we cannot promise a fixed response time. We will acknowledge a report as soon as we can, keep you updated in the advisory thread, and credit you in the advisory if you wish.

## Supported versions

Vibe is pre-1.0. Only the latest release receives security fixes.

## Scope

Reports that matter most:

- **Credential handling.** Credentials live in the OS keychain or environment variables and must never reach a config file, a `.vibe` bundle, a log line, an error shown to the UI or agent, or anything the agent can read. They are write-only across the Wails bridge and MCP.
- **Agent policy.** The sandbox tiers, write approvals, and the SQL allow-list described in [`docs/reference/agent-policy.md`](docs/reference/agent-policy.md). A way to run a statement the policy should refuse (views, macros, table functions, comments, `PRAGMA`, `CALL`) or to write without approval is in scope.
- **MCP server.** Authentication and exposure of `vibe mcp serve` over stdio and SSE ([`docs/reference/mcp.md`](docs/reference/mcp.md)), and parity of gating with the in-app agent.
- **Workspace and bundle handling.** Path traversal or code execution from opening a malicious workspace folder, `.env` file, or `.vibe` bundle; bundle encryption weaknesses.

Out of scope: issues that need an already-compromised local account, vulnerabilities in dependencies with no impact on Vibe (report those upstream), and social engineering.
