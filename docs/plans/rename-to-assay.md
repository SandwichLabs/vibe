# Rename Vibe to Assay, and rewrite the copy

**Status:** accepted 2026-11-01, not started. Settles decision D2 in [`open-source-launch.md`](open-source-launch.md) §7 ("name and trademark").

**Owner decisions (2026-11-01):**

- **One pass.** No backward compatibility and no migration code. There are no external users, so the cutover is immediate.
- **Names.** The app is called just **Assay**. Notebooks are `.asnb` files.
- **Copy.** User-facing copy says **assistant** for the built-in AI.
- **Icon and logo.** New assets, from a design brief ([#311](https://github.com/SandwichLabs/vibe/issues/311)).
- **Feature names.** **AssayBench** for E2 and **`assay verify`** for E5 in [`answers-you-can-check.md`](answers-you-can-check.md).

---

## 1. Why rename

- **The `vibe` binary is taken.** Mistral Vibe, a coding agent, installs a `vibe` command into `~/.local/bin`. The README already has a section working around it, and our users and Mistral's overlap.
- **The word works against the product.** In 2026 "vibe" means "vibe coding": don't check, ship it. The next year of work is *answers you can check*.
- **Why Assay.** To assay something is to test it for quality: a goldsmith assays metal before stamping it. That is the product: every number comes with its evidence, and you can check it.

**Names that were ruled out:**

- **DuckTape.** DuckDB's [trademark guidelines](https://duckdb.org/trademark_guidelines.html) forbid "Duck"-derived names for software built on DuckDB. [hasura/duck-tape](https://github.com/hasura/duck-tape) is also a DuckDB catalog tool with nearly our pitch.
- **Situ** (the first choice). SITU SYSTEMS is a live US registration (Reg. 6160925, 2020, classes 9 and 42) whose goods include "computer software programs for database management". A PyPI package also installs its own `situ` command, with a `situ check` subcommand.

---

## 2. Name checks (step 0, done 2026-11-01)

Run with Firecrawl: Justia's mirror of USPTO records, the npm, PyPI and crates.io registries, Homebrew's formula API, Debian package search, and RDAP for domains.

### Assay

| Check | Result |
|---|---|
| US trademarks, software | No mark on the bare word "ASSAY" in classes 9 or 42. ASSAY EXCHANGE (software for business analysis and audit) was **abandoned** in 2017. The rest are compound marks in other fields: ASSAY CENTRAL (pharma ML), ASSAY PRO (PerkinElmer, fluorescence data), ASSAY 2000 (crude-oil assays, 1990). |
| Homebrew | No `assay` formula or cask |
| Debian | No `assay` package. The only partial match is `r-bioc-multiassayexperiment` |
| PyPI | `assay` is an empty 0.0 placeholder with no downloads |
| npm | `assay` is a 2013 library, about 12 downloads a month, no command |
| Other commands named `assay` | Sand Software Solutions' [Assay](https://sandsoftwaresolutions.com/en/assay): an AGPL Go tool that hashes a folder and reports changes, installed with `go install` or the author's own apt repo. Small, but it puts an `assay` on PATH. The installer must not overwrite a binary it didn't install (it already refuses for `vibe`) |
| Domains | `assay.dev` (2023) and `assay.app` (2026) are registered. Not needed: the installer lives at `sandwichlabs.dev/assay/install.sh` |

### Other names screened

| Name | Why not |
|---|---|
| Daybook | Clear on trademarks, but means "journal". Two small `daybook` CLIs on npm and PyPI |
| Plinth | PLINTH is registered (Reg. 4986425) for education record-keeping software |
| Habitat | HABITAT is registered and maintained (Reg. 4444346) for content software; Chef also ships Habitat |
| Topos | TOPOS (2019) covers software for "marketing insights … business information" |
| Fieldbook, Groundwork | Several live software marks each, some for database and analytics software |
| Ibid, Afield | Old point-of-sale and game marks; "afield" means far from home |
| Plumb, Crosscheck, Footnote, Cairn, Touchstone | Live products in the same space (see the 2026-11-01 naming discussion) |

**Limits.** Justia search is screening, not clearance. It doesn't cover state registrations, common-law use or other countries. If brand spend grows, get a clearance search done.

---

## 3. Names

| What | Now | After |
|---|---|---|
| Product and desktop app | Vibe, Vibe Studio | **Assay**: Dock, window title, app menu, `.desktop` `Name=`, Info.plist, About |
| CLI | `vibe` | `assay` |
| Desktop binary (internal) | `vibe-studio` | `assay-studio`. People see "Assay"; the binary name only shows in `Exec=` lines and process lists. The `studio/` folder keeps its name |
| CLI subcommand | `vibe studio [--gui]` | `assay studio [--gui]` in this pass (see §7) |
| Repository | `SandwichLabs/vibe` | `SandwichLabs/assay` |
| Go module | `github.com/SandwichLabs/vibe` | `github.com/SandwichLabs/assay` (about 1,200 import lines) |
| Installer URL | `sandwichlabs.dev/vibe/install.sh` | `sandwichlabs.dev/assay/install.sh` |
| Workspace file | `vibe.yaml` | `assay.yaml` |
| Machine-local folder | `.vibe-local/` | `.assay-local/` |
| Notebooks | `notebooks/*.vnb` | `notebooks/*.asnb`. Not `.anb`: that is IBM i2 Analyst's Notebook's chart format, which our audience may well have installed. `.asnb` is otherwise only seen inside NCBI genomics pipelines |
| Bundle | `.vibe`, `application/x-vibe-bundle`, UTI `dev.sandwichlabs.vibe-bundle`, `Kind: vibe/bundle` | `.assay`, `application/x-assay-bundle`, `dev.sandwichlabs.assay-bundle`, `assay/bundle` |
| macOS bundle id | `dev.sandwichlabs.vibe-studio` | `dev.sandwichlabs.assay` |
| Linux desktop files | `vibe-studio.desktop`, `Icon=vibe-studio`, `StartupWMClass=vibe-studio`, `vibe-mime.xml`, hicolor `vibe-studio.*` | `assay.*`. Also update `scripts/gen-icons.sh`, `scripts/package.sh` and `scripts/test-install.sh` |
| Keychain service | `vibe` (`$VIBE_KEYRING_SERVICE`) | `assay` |
| Config folder | `os.UserConfigDir()/vibe` | `.../assay` |
| Environment variables | `VIBE_*`: about 40, e.g. `VIBE_LLM_API_KEY`, `VIBE_CONFIG_DIR`, `VIBE_WORKSPACE`, plus test-only ones | `ASSAY_*` |
| Page script API | `vibe.run`, `vibe.state`, `vibe.download`, `vibe.ask` | `assay.*`. Also update the built-in `data-artifacts` skill and `write_artifact`'s docs |
| Page plumbing | `/vibe-view/` (and the `frame-src` emitted at build time), the `vibe-page:` scheme, reserved `html/vibe-lib/`, the `vibe-rules-` temp prefix, the `vibeRange__`/`vibeStep__`/`vibeTick__` guards | `assay-*` |
| Hash domain tags | `vibe-view-hash/1`, `vibe-page-hash/1`, `vibe-deps-hash/1`, `vibe-page-rev/1`, `vibe-conn-v2` | `assay-*`. Golden values in tests change too. Existing allow decisions and keychain bindings reset, which is accepted (§4) |
| MCP | server info `vibe mcp serve`; docs register `vibe-<workspace>` | `assay mcp serve`, `assay-<workspace>` |
| Editors | `editors/nvim/lua/vibe`, Helix `languages.toml` | `lua/assay` |
| Theme tokens | `--vibe-*` (529 in `studio/frontend/src`) | `--assay-*` |
| Identifiers | `runVibe`, `isVibeFile`, `useVibeEditor`, `createVibeEditor`, `VibeEditorInstance`, ... | `runAssay`, ... |
| Plan names | `VibeBench` (E2), `vibe check` (E5) | `AssayBench`, `assay verify` |

**Left alone, because they are history:**

- `CHANGELOG.md` entries for 0.1 to 0.5
- `docs/archive/` (about 1,400 mentions)
- `vibe-app#N` issue references
- the spike findings

The README gets one line instead: "Assay was called Vibe until v0.6.0."

---

## 4. Cutover

There is no migration. Anything keyed on an old name starts fresh: config, keychain entries, workspace file names, allow decisions and MCP registrations.

- **Old installs.** v0.1.2 to v0.5.0 are published.
  - What an old `vibe update` does after the repo rename: it follows the redirect, finds v0.6.0 and runs Assay's checked `install.sh`. Then it fails its own manifest check, because Assay writes a different manifest.
  - That path is unsupported. Uninstall Vibe first.
  - Remove `sandwichlabs.dev/vibe/install.sh` from the website so nobody installs Vibe after the rename. Redirect the `/vibe` page to `/assay`.
- **Your own machine.** One-time steps:
  1. **Before** the repo rename: `curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh -s -- --uninstall`
  2. Install Assay from the new one-liner.
  3. Enter credentials again: connection sign-ins in the app, and `assay config llm-key`.
  4. Rename each workspace. From its root:

     ```sh
     git mv vibe.yaml assay.yaml
     for f in notebooks/*.vnb; do git mv "$f" "${f%.vnb}.asnb"; done
     rm -rf .vibe-local
     sed -i.bak 's/^\.vibe-local\/$/.assay-local\//' .gitignore && rm .gitignore.bak
     ```
  5. Register MCP clients again (`claude mcp add ... assay mcp serve ...`). Remove the old `vibe-*` entries.
  6. Allow your scripted views, pages and `.env` files again the first time you open them.
  7. Optional: delete `~/.config/vibe` (or `~/Library/Application Support/vibe`) and the `vibe` keychain entries.

---

## 5. Order of work

Sizes as in `open-source-launch.md`: **S**, **M**, **L**.

0. **Name checks.** Done (§2).
1. **Design brief.** Icon and logo assets ([#311](https://github.com/SandwichLabs/vibe/issues/311)). It runs in parallel; the release waits for it.
2. **Rename the GitHub repository** to `SandwichLabs/assay`, after step 1 of §4 on your machine.
3. **PR A: the rename, in one pass.** One PR, because the module path touches every import.
   - **Contents:** a script for every row of §3 except the history list, then fixes by hand. Paste the script into the PR description; don't commit it.
   - **No wording changes** apart from the name, plus "agent" → "assistant" for the built-in AI in UI labels (§6, terminology).
   - **Update** CLAUDE.md, `docs/README.md` and the architecture tree, so `internal/docsref` stays green.
   - **Checks:** `task`, `task typecheck`, the ast-grep rules, `check-arity.mjs`, `task build:studio:linux`, and a macOS `workflow_dispatch` run.
   - **L** (mostly mechanical)
4. **PR B: the copy pass** (§6). Wording only. **M**
5. **Release v0.6.0** with the new icon, then:
   - the website: an `/assay` page, the old installer removed, `/vibe` redirected;
   - the repo description and topics;
   - the social preview;
   - the README GIF, re-recorded.

---

## 6. Copy: taking the LLM voice out

### What's wrong now

Most of the app's small UI strings are already good: specific, short, written for the person at the screen. Two examples:

- "Passwords and tokens go in this computer's keychain, not in your folder."
- "Enter the passphrase its author gave you. Vibe can't recover it."

Leave those alone apart from the name. The problems are in the taglines, the README, the CLI help and the assistant's prompt.

1. **One slogan everywhere.** "Data where it (already) lives" is in:
   - the CLI's short and long help;
   - Settings > About, the Linux `.desktop` comment and `wails.json`;
   - the launcher footnote and the connections rail's empty state;
   - the README (twice), CONTRIBUTING and the CHANGELOG;
   - the agent's help text and its system prompt.

   Repeating one phrase on every surface is the strongest tell.
2. **"Agentic."** It's a word for investors and model vendors. Analysts don't search for it. Say what the assistant does.
3. **"A workspace for working with."** The word doubles up.
4. **The README's first paragraph** is 110 words long. It lists thirteen connection types and then makes five more claims, so it reads like a summary of the docs. It also leads with "in the DataGrip / DBeaver mould", which business users won't recognise.
5. **"X, not Y" over and over.** "Questions, not answers", "they store questions, not answers" (twice in the README), "Answers are never frozen", "it reaches data; it doesn't copy it". The idea is good. Say it once, where it explains something: notebooks re-run when you open them.
6. **The front-page feature list** is twelve bullets, each a bold label plus a run of fragments ("Plain files, git-friendly, shareable with a team."). That shape is fine in reference docs. On the front page it reads as generated.
7. **Labels that perform:**
   - "Credentials done properly"
   - "An agent with guardrails"
   - "Three ways in"
   - "the north star"
   - "You are the Vibe wizard"
8. **Em dashes in titles and taglines** ("Vibe — an agentic desktop workspace ...").

### Terminology

- **"Assistant"** is the AI built into Assay. It applies to:
  - the Agent view, which becomes **Assistant**;
  - approval cards and the Settings section;
  - the README, quickstart and CLI help.
- **"Agent"** stays for outside programs that connect over MCP, such as Claude Code, Cursor and Codex. Those really are agents.
- **"Agent" also stays in code and engineering docs:** `internal/agents`, `ToolDef`, `agent-policy.md`, tool names.

### The checklist

Run every user-facing string you touch through this:

- **Show the thing.** Name what someone does, with a real noun: Postgres, a CSV, a chart, a teammate.
- **Keep it short.** One claim per sentence. Taglines under 60 characters.
- **Banned words:** "agentic", "seamless", "powerful", "unlock", "supercharge", "done right", "done properly".
- **No "X, not Y"** in a tagline or heading.
- **Each surface says one thing**, the thing its reader needs there. Don't repeat a slogan across surfaces.
- **Read it aloud** as if to an analyst at the next desk. Rewrite anything you wouldn't say.
- **The engineering docs may keep "data stays where it lives"** as a design principle (`direction.md` §6, `code-mode.md` C6, `html-artifacts.md`). There it is a rule people check designs against, not a pitch.

### Drafts

These are starting points, and they were drafted by Claude: read them with the same checklist and put them in your own words.

| Surface | Now | Draft |
|---|---|---|
| Tagline (README subtitle, About, `.desktop` Comment, `wails.json` comments) | "An agentic data workspace" / "An agentic workspace for working with data where it (already) lives" | **"Query and chart the data you already have"**. Alternative that leans on the name: "Check the numbers before you share them" |
| CLI `Short` (`cmd/root.go`) | "Vibe — an agentic desktop workspace for working with data where it lives" | "Query databases and files together, with notebooks and an AI assistant" |
| CLI `Long` | 9 lines, starting "Vibe is an agentic workspace for data that stays where it already lives: ..." | See below |
| README lede | One 110-word paragraph | See below |
| README "What you get" | 12 bold-label bullets | 6 plain bullets, below |
| README "A note on the name" | A paragraph about the Mistral collision | "Assay was called Vibe until v0.6.0." |
| Launcher footnote (returning user) | "Your data stays where it already lives. Vibe keeps passwords in this computer's keychain, never in the workspace folder, so a workspace is safe to put in git or share with a teammate." | "Passwords stay in this computer's keychain, never in the folder, so you can put a workspace in git or send it to a teammate." |
| Launcher lead | "... so you can back it up or share it with git. Your data stays wherever it is now." | Drop the last sentence |
| Connections rail, empty | "Add one to read data where it lives." | "Add a database, a bucket or a DuckDB file to query it here." |
| Assistant system prompt (`internal/llm/runner.go`) | "You are the Vibe wizard — the agent layer of Vibe, an agentic data workspace. ..." | "You are the assistant in Assay, a desktop app where people query their own databases and files. ..." The rest of the prompt is instructions and stays. Re-run the agent tests after this, because prompt wording shifts behaviour |
| Assistant help (`internal/agents/help.go`) | "... The data itself stays where it already lives." | "... Assay reads the data from its connections and never copies it into the folder." |
| CONTRIBUTING, first line | "Vibe is an agentic desktop workspace for working with data where it already lives." | "Assay is a desktop app for querying the databases and files you already have." |

**README lede (draft)**

> Assay is a desktop app for analysts. Connect Postgres, MotherDuck, S3 or a folder of CSV files and query them together in one SQL statement, without copying anything first.
>
> Your notebooks, saved queries and charts are plain files in a folder, so you can keep them in git and share them with your team. The built-in AI assistant writes queries and builds notebooks for you. It shows the SQL behind every answer, and it asks before it changes any data. Claude Code, Cursor and other MCP clients get the same tools with the same limits.

**What you get (draft)**

> - Query Postgres, MySQL, SQLite, MotherDuck, DuckDB files, S3, GCS, R2, Azure, DuckLake, Iceberg and Delta, and join across them. Connections are read-only unless you change that. [Recipes](docs/connections.md)
> - Notebooks with SQL, Markdown and chart cells. Every cell runs again when you open the notebook, so the numbers are current.
> - Live views and pages: charts and tables with filters, saved as files in `artifacts/`. [Reference](docs/reference/artifacts.md)
> - Definitions of your entities and metrics that you review and keep in git, then use from any query as `metric('revenue', by => 'country')`. The assistant can only propose a definition; you accept it. [Reference](docs/reference/semantics.md)
> - An assistant that reads freely and asks before every write. It works with a local model, Ollama, OpenAI, Anthropic, Gemini or Cloudflare AI Gateway. Your own agent can use the same tools over MCP. [MCP setup](docs/mcp-clients.md)
> - Passwords and tokens stay in your OS keychain or environment, never in the folder, a bundle or a log.

**CLI `Long` (draft)**

> Assay queries your databases and files where they are, through one embedded DuckDB. A workspace is a folder of notebooks, saved queries and an assay.yaml listing its connections. Passwords stay in your OS keychain.
>
> Start with `assay workspace init`, then `assay query`, `assay import` and `assay studio --gui` for the desktop app. `assay mcp serve` lets Claude Code and other MCP clients use the workspace.

---

## 7. Open items

- **The `studio` subcommand.** With "Studio" gone from the product name, `assay studio --gui` reads oddly. A follow-up could split it into `assay app` (desktop) and `assay tui` (terminal), as a CLI change of its own. This pass keeps `studio` so the rename stays mechanical.
- **Design brief** ([#311](https://github.com/SandwichLabs/vibe/issues/311)): icon, wordmark and social preview. The release waits for it.
