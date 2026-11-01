# Rename Vibe to Situ, and rewrite the copy

**Status:** proposal, 2026-11-01. Not started. It settles decision D2 in [`open-source-launch.md`](open-source-launch.md) §7 ("name and trademark").

**Scope:** the product, binary, module, file and format names, how to move existing installs and workspaces, and a pass over the user-facing copy. Engineering docs keep their wording apart from the name.

---

## 1. Why rename

- **The `vibe` binary is taken.** Mistral Vibe, a coding agent, installs a `vibe` command into `~/.local/bin`. The README already has a section working around it, and our users and Mistral's overlap.
- **The word works against the product.** In 2026 "vibe" means "vibe coding": don't check, ship it. The next year of work is [answers you can check](answers-you-can-check.md). The name should not argue with that.
- **DuckTape is out.** DuckDB's [trademark guidelines](https://duckdb.org/trademark_guidelines.html) say names derived from "Duck" may not be used for software built on DuckDB. [hasura/duck-tape](https://github.com/hasura/duck-tape) is also a DuckDB catalog tool with nearly our pitch.
- **Why Situ.** From *in situ*, "in its original place". The name says what "data where it already lives" has been saying, so the copy can stop saying it (§6).

**Checked so far:** web searches found no `situ` command and no data product called Situ. Zetaris uses "in-situ analytics" as a tagline.
**Not checked yet (step 0):**
- a USPTO search in classes 9 and 42
- domains
- the `situ` name on GitHub, Homebrew, winget, apt and PyPI

---

## 2. Names

| What | Now | After | Notes |
|---|---|---|---|
| Product | Vibe | **Situ** | |
| Desktop app | Vibe Studio | **Situ Studio** | Keeps `studio/`, `situ studio --gui` and the word "Studio" in the docs (decision R1) |
| CLI | `vibe` | `situ` | No two-letter alias |
| Desktop binary | `vibe-studio` | `situ-studio` | `cmd/studio_gui.go` `guiBinName`, Wails `name`/`outputfilename` |
| Repository | `SandwichLabs/vibe` | `SandwichLabs/situ` | GitHub redirects the old URLs, including release downloads |
| Go module | `github.com/SandwichLabs/vibe` | `github.com/SandwichLabs/situ` | About 1,200 import lines |
| Installer URL | `sandwichlabs.dev/vibe/install.sh` | `sandwichlabs.dev/situ/install.sh` | Redirect the old URL |
| Workspace file | `vibe.yaml` | `situ.yaml` | Legacy name read in 0.6.x (§4) |
| Machine-local folder | `.vibe-local/` | `.situ-local/` | Update the generated `.gitignore` line |
| Notebook files | `notebooks/*.vnb` | `notebooks/*.snb` | Decision R2 |
| Bundle | `.vibe`, `application/x-vibe-bundle`, UTI `dev.sandwichlabs.vibe-bundle`, `Kind: vibe/bundle` | `.situ`, `application/x-situ-bundle`, `dev.sandwichlabs.situ-bundle`, `situ/bundle` | Import accepts the old ones until 1.0 |
| macOS bundle id | `dev.sandwichlabs.vibe-studio` | `dev.sandwichlabs.situ-studio` | macOS treats it as a new app: expect one keychain prompt per secret and fresh privacy prompts |
| Linux desktop entry | `vibe-studio.desktop`, `Icon=vibe-studio`, `StartupWMClass=vibe-studio`, `vibe-mime.xml` | `situ-studio.*` | `scripts/gen-icons.sh`, `scripts/package.sh`, `scripts/test-install.sh` |
| Keychain service | `vibe` (`$VIBE_KEYRING_SERVICE`) | `situ` | Fallback read (§4) |
| Config folder | `os.UserConfigDir()/vibe` | `.../situ` | Copied on first start (§4) |
| Environment variables | `VIBE_*` (about 40, e.g. `VIBE_LLM_API_KEY`, `VIBE_CONFIG_DIR`, `VIBE_WORKSPACE`) | `SITU_*` | Warn when a `VIBE_*` variable is set; don't read it |
| Page script API | `vibe.run`, `vibe.state`, `vibe.download`, `vibe.ask` | `situ.*` | Keep `vibe` as an alias object until 1.0: the agent has written pages that call it |
| Page plumbing | `/vibe-view/` (frame-src emitted at build time), `vibe-page:` scheme, reserved `html/vibe-lib/`, `vibe-rules-` temp prefix, `vibeRange__` guards | `situ-*` | Keep `vibe-lib` reserved as well |
| MCP | server info `vibe mcp serve`; docs register `vibe-<workspace>` | `situ mcp serve`, `situ-<workspace>` | Users re-run `claude mcp add` |
| Editors | `editors/nvim/lua/vibe`, Helix `languages.toml` | `lua/situ` | |
| Theme tokens | `--vibe-*` (529 in `studio/frontend/src`) | `--situ-*` | Mechanical. If user theme files can name roles, accept both until 1.0 (check `theme/schema.ts`) |
| Identifiers | `runVibe`, `isVibeFile`, `useVibeEditor`, `createVibeEditor`, ... | `runSitu`, ... | Mechanical |
| Plan names | `VibeBench`, `vibe check` (E2, E5 in answers-you-can-check) | Decision R6 | |

### Frozen: these strings stay `vibe-` forever

They are domain-separation tags inside hashes, not branding. Changing one silently invalidates every decision keyed on it.

| Constant | Where | What breaks if it changes |
|---|---|---|
| `vibe-view-hash/1` | `internal/artifacts/viewhash.go` | Every allowed script view goes back to "not allowed" |
| `vibe-page-hash/1` | `internal/artifacts/pagehash.go` | Every allowed HTML page |
| `vibe-deps-hash/1` | `internal/artifacts/depshash.go` | Every allow of a view or page that calls `metric()` |
| `vibe-page-rev/1` | `internal/artifacts/rev.go` | Compare-and-write revisions of page files |
| `vibe-conn-v2` | `internal/workspace/fingerprint.go` | Whatever is keyed on a connection's fingerprint |

PR 1 adds a test that pins each value, with a comment saying why. The rename script skips these lines.

### Left alone

- `CHANGELOG.md` entries for 0.1 to 0.5
- `docs/archive/` (about 1,400 mentions)
- `vibe-app#N` issue references
- the spike findings

They are history. The README gets one line instead: "Situ was called Vibe until v0.6.0."

---

## 3. Size

About 8,900 mentions in 950 files:

| Area | Mentions |
|---|---|
| docs | 3,560 |
| internal | 2,078 |
| studio | 1,783 |
| cmd | 810 |
| root files | 366 |
| scripts | 172 |
| editors | 73 |
| .github | 52 |

Most are mechanical: import paths, `vibe.yaml` in strings and docs, test helpers. The careful work is in §2's frozen list, the migration in §4, and the copy in §6.

---

## 4. Existing installs and workspaces

v0.1.2 to v0.5.0 are published GitHub releases (v0.5.0 on 2026-10-30), and `vibe update` can install updates. So there are installs to move, even if they're few.

### What an old `vibe update` will do after the rename

Read from `internal/selfupdate`:

1. **It finds the new release.** `check.go` reads the redirect of `github.com/SandwichLabs/vibe/releases/latest`. After the repo rename that resolves to Situ's latest release, so v0.5.0 offers "update to v0.6.0".
2. **It runs Situ's installer.** `apply.go` downloads that release's `install.sh`, checks it against `checksums.txt`, and runs it with the `VIBE_*` install variables scrubbed.
3. **It reports a failure anyway.** It then reads **its own** install manifest and expects version 0.6.0. Situ's installer writes a different manifest, so the old binary reports "the installer finished but the install manifest says 0.5.0".

**Recommendation:** give Situ's `install.sh` a legacy block for 0.6.x. When it finds a Vibe install manifest, it:

- installs Situ;
- runs `situ migrate --from-vibe`;
- removes only the files that manifest lists (`vibe`, `vibe-studio`, the desktop entry, icons and MIME file). It never removes a `vibe` it didn't install, such as Mistral's;
- updates the old manifest's version and Studio path so the old updater exits cleanly and restarts into Situ Studio;
- prints "Vibe is now Situ. Use the `situ` command."

Add a case to `scripts/test-install.sh` that installs v0.5.0, runs `vibe update` against the new release, and checks the result. Remove the block in 0.7.

If this gets fiddly, the fallback is a line in the release notes and the README saying "reinstall with the new one-liner". Old `vibe update` then fails once with the manifest message.

### Machine state: `situ migrate`

Studio and the CLI also run it on first start when `.../situ` is missing and `.../vibe` exists.

- **Config folder.** Copy `.../vibe` to `.../situ`: preferences, recents, LLM settings, `dotenv.json`, `views.json`. Copy rather than move, so the old install keeps working until it's removed.
- **Keychain.** go-keyring can't list entries, so migrate lazily. When a lookup under `situ` misses, try `vibe`; on a hit, write it under `situ`. Leave the old entry alone. On macOS the new bundle id means one access prompt per secret; say so in the release notes.
- **Trust decisions.** They survive as long as the hash tags stay frozen. They are keyed by the workspace's path, which doesn't change.
- **Environment.** If any `VIBE_*` variable is set, print one warning naming the `SITU_*` replacement. Don't read the old variable: a key silently picked up from an old name is the kind of surprise this product promises not to have.

### Workspaces: `situ workspace migrate`

Studio offers it as a banner when a folder has `vibe.yaml` and no `situ.yaml`.

- **Renames.** `vibe.yaml` to `situ.yaml`, `notebooks/*.vnb` to `*.snb`, `.vibe-local/` to `.situ-local/`, and the `.gitignore` line. Uses `git mv` when the folder is a git repository, a plain rename otherwise. It doesn't commit; it tells you to.
- **In 0.6.x without migrating.** Discovery still finds `vibe.yaml` and `*.vnb`, and everything works, with the banner shown. 0.7 drops the legacy names.
- **Bundles.** Import accepts `.vibe` files and `Kind: vibe/bundle` until 1.0. Export writes `.situ`.
- **Pages.** Pages that call `vibe.run` keep working through the alias. The built-in `data-artifacts` skill and `write_artifact`'s docs teach `situ.*`.

---

## 5. Order of work

Sizes as in `open-source-launch.md`: **S**, **M**, **L**.

0. **Gate (owner).** USPTO classes 9 and 42; domains; the `situ` name on GitHub, Homebrew, winget, apt and PyPI. Pick the icon mark (R5). Stop here if the search turns up a conflict. **S**
1. **PR: freeze the wire constants.** The pinning test from §2, and the bundle importer reading `vibe/bundle`. No renames. **S**
2. **Rename the GitHub repository** to `SandwichLabs/situ`. Redirects keep old clones, release URLs and `go install` of old tags working.
3. **PR: the mechanical rename.**
   - **One PR**, because the module path touches every import.
   - **Contents:** a script for the module path, imports, identifiers, binaries, file names, env vars, CSS tokens and docs, skipping §2's frozen lines and history. Then fixes by hand. Paste the script into the PR description; don't commit it.
   - **No wording changes** other than the name, so review is "did the name change and nothing else".
   - **Checks:** `task`, `task typecheck`, the ast-grep rules, `internal/docsref`, `check-arity.mjs`, `task build:studio` on Linux, and a macOS `workflow_dispatch` run.
   - **L** (mostly mechanical)
4. **PR: compatibility and migration.** Legacy discovery, `situ migrate`, `situ workspace migrate` and the Studio banner, the keychain fallback, the `VIBE_*` warning, the `vibe` page alias, `.vibe` import. **M**
5. **PR: installer bridge.** The legacy block in `install.sh` and the `test-install.sh` case from §4. **M**
6. **PR: copy pass** (§6). Wording only. **M**
7. **Release v0.6.0**, then:
   - the website: the `/situ` page, and redirects for `/vibe` and `/vibe/install.sh`;
   - the repo description and topics;
   - the social preview;
   - the icon (the current mark is a "V" on a wave);
   - a README GIF re-recorded with the new name in the window title.
8. **v0.7: remove the legacy readers** from step 4, except bundle import and the page alias, which stay until 1.0. **S**

---

## 6. Copy: taking the LLM voice out

### What's wrong now

Most of Studio's small UI strings are already good: specific, short, written for the person at the screen. Two examples:

- "Passwords and tokens go in this computer's keychain, not in your folder."
- "Enter the passphrase its author gave you. Vibe can't recover it."

Leave those alone apart from the name. The problems are in the taglines, the README, the CLI help and the agent prompt.

1. **One slogan everywhere.** "Data where it (already) lives" is in:
   - the CLI's short and long help;
   - Settings > About, the Linux `.desktop` comment and `wails.json`;
   - the launcher footnote and the connections rail's empty state;
   - the README (twice), CONTRIBUTING and the CHANGELOG;
   - the agent's help text and its system prompt.

   Repeating one phrase on every surface is the strongest tell. With the name Situ, the name carries that idea.
2. **"Agentic."** It's a word for investors and model vendors. Analysts don't search for it. Say what the agent does.
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
| Tagline (README subtitle, About, `.desktop` Comment, `wails.json` comments) | "An agentic data workspace" / "An agentic workspace for working with data where it (already) lives" | **"Query and chart the data you already have"** |
| CLI `Short` (`cmd/root.go`) | "Vibe — an agentic desktop workspace for working with data where it lives" | "Query databases and files together, with notebooks and an AI agent" |
| CLI `Long` | 9 lines, starting "Vibe is an agentic workspace for data that stays where it already lives: ..." | See below |
| README lede | One 110-word paragraph | See below |
| README "What you get" | 12 bold-label bullets | 6 plain bullets, below |
| README "A note on the name" | A paragraph about the Mistral collision | "Situ was called Vibe until v0.6.0." |
| Launcher footnote (returning user) | "Your data stays where it already lives. Vibe keeps passwords in this computer's keychain, never in the workspace folder, so a workspace is safe to put in git or share with a teammate." | "Passwords stay in this computer's keychain, never in the folder, so you can put a workspace in git or send it to a teammate." |
| Launcher lead | "... so you can back it up or share it with git. Your data stays wherever it is now." | Drop the last sentence |
| Connections rail, empty | "Add one to read data where it lives." | "Add a database, a bucket or a DuckDB file to query it here." |
| Settings > About | "An agentic workspace for working with data where it already lives." | The tagline |
| Agent system prompt (`internal/llm/runner.go`) | "You are the Vibe wizard — the agent layer of Vibe, an agentic data workspace. ..." | "You are the agent in Situ, a desktop app where people query their own databases and files. ..." The rest of the prompt is instructions and stays. Re-run the agent tests after this, because prompt wording shifts behaviour |
| Agent help (`internal/agents/help.go`) | "... The data itself stays where it already lives." | "... Situ reads the data from its connections and never copies it into the folder." |
| CONTRIBUTING, first line | "Vibe is an agentic desktop workspace for working with data where it already lives." | "Situ is a desktop app for querying the databases and files you already have." |

**README lede (draft)**

> Situ is a desktop app for analysts. Connect Postgres, MotherDuck, S3 or a folder of CSV files and query them together in one SQL statement, without copying anything first.
>
> Your notebooks, saved queries and charts are plain files in a folder, so you can keep them in git and share them with your team. The built-in AI agent writes queries and builds notebooks for you. It shows the SQL behind every answer, and it asks before it changes any data. Claude Code, Cursor and other MCP clients get the same tools with the same limits.

**What you get (draft)**

> - Query Postgres, MySQL, SQLite, MotherDuck, DuckDB files, S3, GCS, R2, Azure, DuckLake, Iceberg and Delta, and join across them. Connections are read-only unless you change that. [Recipes](docs/connections.md)
> - Notebooks with SQL, Markdown and chart cells. Every cell runs again when you open the notebook, so the numbers are current.
> - Live views and pages: charts and tables with filters, saved as files in `artifacts/`. [Reference](docs/reference/artifacts.md)
> - Definitions of your entities and metrics that you review and keep in git, then use from any query as `metric('revenue', by => 'country')`. The agent can only propose a definition; you accept it. [Reference](docs/reference/semantics.md)
> - An agent that reads freely and asks before every write. It works with a local model, Ollama, OpenAI, Anthropic, Gemini or Cloudflare AI Gateway, and over MCP with your own agent. [MCP setup](docs/mcp-clients.md)
> - Passwords and tokens stay in your OS keychain or environment, never in the folder, a bundle or a log.

**CLI `Long` (draft)**

> Situ queries your databases and files where they are, through one embedded DuckDB. A workspace is a folder of notebooks, saved queries and a situ.yaml listing its connections. Passwords stay in your OS keychain.
>
> Start with `situ workspace init`, then `situ query`, `situ import` and `situ studio --gui` for the desktop app. `situ mcp serve` lets Claude Code and other MCP clients use the workspace.

---

## 7. Decisions for the owner

| # | Question | Recommendation |
|---|---|---|
| R1 | Desktop app name: "Situ" or "Situ Studio" | **Situ Studio.** The brand is Situ. "Studio" names the desktop app, as it does now, which keeps `studio/`, `situ studio --gui` and the docs |
| R2 | Notebook extension | **`.snb`.** One extension, same code paths as `.vnb`. (Samsung's discontinued S Note also used `.snb`; that doesn't matter on a desktop.) The alternative is a name with no brand in it, such as `.nb.json`, so the next rename never touches notebooks. That gives editors JSON highlighting, but the double extension complicates the stem logic in `notebooks/file_store.go` |
| R3 | How long legacy names are read | Workspace files, `VIBE_*` warning, keychain fallback and installer bridge: **0.6.x only.** Bundle import and the `vibe` page alias: **until 1.0** |
| R4 | "Agent" or "assistant" in user-facing copy | **Agent everywhere.** It matches the Agent view, the tool and MCP docs, and what Claude Code users call it. Pick one and don't mix them |
| R5 | Icon mark | Keep the teal tile and the wave; replace the "V" |
| R6 | `VibeBench` and `vibe check` (E2, E5) | `SituBench` doesn't read well. The epic owners pick, before E2 ships any public numbers. Don't call E5 `situ check`: `situ semantics check` already exists |
