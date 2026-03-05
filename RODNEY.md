---
name: rodney
description: Use the Rodney CLI to automate a headless Chrome browser. Invoke this skill when the user asks to browse a website, scrape content, take screenshots, test accessibility, interact with web pages, or perform any browser automation task.
allowed-tools: Bash, Read, Write, Grep, Glob
---

# Rodney — Chrome automation from the command line

Rodney is a Go CLI that drives a **persistent headless Chrome** instance. Each
command is a short-lived process that connects to the same long-running browser
over WebSocket, so tabs, cookies, and page state persist between calls.

The `rodney` binary is installed globally on PATH. Run commands as `rodney <command>`.

---

## Command reference

### Browser lifecycle

```
rodney start                    Launch headless Chrome
rodney stop                     Shut down Chrome and clean up
rodney status                   Show PID, WebSocket URL, active page
```

### Navigation

```
rodney open <url>               Navigate (auto-adds http:// if missing)
rodney back                     History back
rodney forward                  History forward
rodney reload                   Reload current page
```

### Page info / extraction

```
rodney url                      Print current URL
rodney title                    Print page title
rodney html [selector]          Full page HTML, or element outer HTML
rodney text <selector>          Element text content
rodney attr <selector> <name>   Element attribute value
rodney pdf [file]               Save page as PDF (default: page.pdf)
```

### JavaScript

```
rodney js <expression>          Evaluate JS expression
```

The expression is auto-wrapped as `() => { return (<expr>); }`.
Output: strings printed raw, objects/arrays pretty-printed as JSON.

### Interaction

```
rodney click <selector>         Click element
rodney input <selector> <text>  Clear field then type text
rodney clear <selector>         Clear input
rodney file <selector> <path|-> Set file on <input type="file"> (- = stdin)
rodney download <sel> [file|-]  Download href/src target (- = stdout)
rodney select <selector> <val>  Select dropdown option by value
rodney submit <selector>        Submit form
rodney hover <selector>         Hover
rodney focus <selector>         Focus
```

### Waiting / synchronization

```
rodney wait <selector>          Wait for element to appear (default 30s)
rodney waitload                 Wait for page load event
rodney waitstable               Wait for DOM to stop changing
rodney waitidle                 Wait for network idle
rodney sleep <seconds>          Sleep N seconds (decimals OK)
```

### Screenshots

```
rodney screenshot [-w N] [-h N] [file]   Page screenshot (default 1280×720, full page)
rodney screenshot-el <sel> [file]        Element screenshot
```

### Tab management

```
rodney pages                    List tabs (* = active)
rodney page <index>             Switch to tab by index
rodney newpage [url]            Open new tab
rodney closepage [index]        Close tab (default: active; can't close last)
```

### Element queries

```
rodney exists <selector>        Exit 0 if exists, 1 if not
rodney count <selector>         Print match count
rodney visible <selector>       Exit 0 if visible, 1 if not
```

### Accessibility

```
rodney ax-tree [--depth N] [--json]              Full accessibility tree
rodney ax-find [--name N] [--role R] [--json]    Find nodes by name/role
rodney ax-node <selector> [--json]               A11y info for one element
```

---

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `ROD_CHROME_BIN` | `/usr/bin/google-chrome` | Chrome binary path |
| `ROD_TIMEOUT` | `30` | Element query timeout (seconds) |
| `HTTPS_PROXY` / `HTTP_PROXY` | — | Authenticated proxy (auto-detected) |

State file: `~/.rodney/state.json`
Chrome profile: `~/.rodney/chrome-data/`

---

## Essential patterns

### Always start before use, stop when done

```bash
rodney start
# ... commands ...
rodney stop
```

If the browser is already running, `rodney status` will confirm. Always
call `rodney stop` when finished to clean up the Chrome process.

### Wait after navigation

Pages with JavaScript need time to render. After `rodney open`, use one of:

```bash
rodney open https://example.com
rodney waitstable        # best default — waits for DOM to settle
```

Choose the right wait:
- `waitload` — fires on the load event (fast, but JS may not be done)
- `waitstable` — waits for DOM mutations to stop (good for SPAs)
- `waitidle` — waits for network requests to finish
- `wait <selector>` — waits for a specific element to appear

### Extract data

```bash
title=$(rodney title)
link=$(rodney attr "a.primary" href)
content=$(rodney text ".article-body")
data=$(rodney js 'JSON.stringify(window.__DATA__)')
```

### Conditional logic with exit codes

`exists` and `visible` return exit code 0 (true) or 1 (false):

```bash
if rodney exists ".error-message"; then
    echo "Error: $(rodney text '.error-message')"
fi
```

### Multi-step form interaction

```bash
rodney input "#username" "user@example.com"
rodney input "#password" "secret"
rodney click "#login-button"
rodney waitstable
```

### File upload

```bash
rodney file "#file-input" /path/to/document.pdf
```

### Download files

```bash
rodney download "a.download-link" ./output.zip    # save to file
rodney download "img.hero" -                       # pipe to stdout
rodney download "a#report" ./report.pdf            # from href
```

Handles both regular URLs and data: URLs. Uses the page's cookies/session.

### Screenshot with custom viewport

```bash
rodney screenshot -w 1920 -h 1080 wide.png
rodney screenshot-el ".chart" chart.png
```

### Multi-tab workflows

```bash
rodney newpage https://other-site.com
rodney page 0          # switch back to first tab
rodney click ".btn"
rodney page 1          # switch to second tab
rodney text "h1"
rodney closepage 1     # close second tab
```

### Accessibility testing

```bash
# Dump full tree
rodney ax-tree --depth 3

# Find all buttons and check they have names
rodney ax-find --role button --json | jq '.[] | select(.name.value == "")'

# Inspect a specific element
rodney ax-node "#submit-btn" --json
```

---

## Important notes

- **Selectors** are CSS selectors (e.g., `#id`, `.class`, `div > p`, `[data-attr="val"]`).
- **Timeouts** default to 30 seconds for element queries. Override with `ROD_TIMEOUT`.
- **Chrome must be installed** — Rodney does not bundle it. Set `ROD_CHROME_BIN` if it's not at the default path.
- **One browser at a time** — `rodney start` fails if Chrome is already running. Use `rodney status` to check.
- **Quotes matter** — wrap selectors and JS expressions in quotes to prevent shell expansion.
- The `js` command auto-wraps expressions. Don't write `() => { ... }` yourself — just pass the expression directly.
- `input` selects all existing text before typing, effectively replacing the field content.
- `download` works with `href` attributes on links and `src` attributes on images. It fetches using the page's JS context so cookies and auth are preserved.
