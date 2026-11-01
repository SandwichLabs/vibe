# Rename Assay to Slaw

**Status:** accepted 2026-11-01. Replaces the name chosen in [`rename-to-assay.md`](rename-to-assay.md), which stays as the record of that pass and of the name checks behind it.

**Owner decision (2026-11-01):** the product is called **Slaw**, short for **SandwichLabs analysis workspace**. "Assay" read as a laboratory test of a biological sample to the first people who heard it, and its search results belong to biotech. Assay never shipped: PR A (#315) merged, but v0.6.0 was never tagged and the website was never deployed. So this pass has no users to migrate and no Assay compatibility to keep.

---

## 1. What stays from the Assay pass

Everything in `rename-to-assay.md` §1, §3 (the list of surfaces), §5 and §6 still applies, with the word "Assay" replaced by "Slaw":

- **One pass, no migration.** The Assay names never reached a release, so nothing reads them. The Vibe-era legacy handling that PR A kept stays as it is: legacy `_vibe_seq_` autonumber sequences are still detected and bumped (new ones become `_slaw_seq_`), and an old SQLCipher `.vibe` container is still refused by name ("This is an older Vibe file that holds data. Slaw doesn't open these.").
- **History is left alone.** `CHANGELOG.md` entries for 0.1 to 0.5, `docs/archive/`, `docs/reviews/`, the spike findings, `open-source-launch.md`, `vibe-app#N` references and `rename-to-assay.md` itself.
- **PR B, the copy pass** (§6 of the Assay plan), is still to do after this PR, with the same checklist. Its drafts change "Assay" to "Slaw" and remain the owner's to put in his own words.
- **v0.6.0 waits for the icon** ([#311](https://github.com/SandwichLabs/vibe/issues/311)), whose brief now names Slaw.

---

## 2. Name checks (done 2026-11-01)

| Check | Result |
|---|---|
| US trademarks (Justia) | No SLAW mark for software (classes 9 or 42). Live and pending SLAW marks cover travel publications (RedSlaw, 2025), a music act, online advertising (SLAW MEDIA, 2025) and food (SLAW DOG, SLAW BURGER, SLAWESOME!). |
| Homebrew | No `slaw` formula or cask |
| Debian | No `slaw` source package |
| PyPI, npm, crates.io | No `slaw` package on any of them |
| Go module proxy | `github.com/SandwichLabs/slaw` unused |
| GitHub | `SandwichLabs/slaw` free. Other repos named slaw: [laws-africa/slaw](https://github.com/laws-africa/slaw) (Python, Akoma Ntoso legal documents, not on PyPI), zamboni-lab/SLAW (LC-MS workflow), a mobility simulator. None installs a `slaw` command through a package manager. The installer still refuses to overwrite a `slaw` it didn't install, as it did for `vibe` and `assay`. |

**Limits.** Screening, not clearance, as in the Assay plan.

---

## 3. Names

Every row of `rename-to-assay.md` §3, with `assay` → `slaw`:

| What | Assay pass | Now |
|---|---|---|
| Product and desktop app | Assay | **Slaw**: Dock, window title, app menu, `.desktop` `Name=`, Info.plist, About. The long form "SandwichLabs analysis workspace" appears once, in the README's first line and in Settings > About |
| CLI | `assay` | `slaw` |
| Desktop binary | `assay-studio` | `slaw-studio`, `StartupWMClass=slaw-studio` |
| Repository | `SandwichLabs/assay` | `SandwichLabs/slaw` (GitHub keeps redirects from `vibe` and `assay`) |
| Go module | `github.com/SandwichLabs/assay` | `github.com/SandwichLabs/slaw` |
| Installer URL | `sandwichlabs.dev/assay/install.sh` | `sandwichlabs.dev/slaw/install.sh` |
| Workspace file, local folder | `assay.yaml`, `.assay-local/` | `slaw.yaml`, `.slaw-local/` |
| Notebooks | `notebooks/*.asnb` | `notebooks/*.slnb` |
| Bundle | `.assay`, `application/x-assay-bundle`, `dev.sandwichlabs.assay-bundle`, `assay/bundle` | `.slaw`, `application/x-slaw-bundle`, `dev.sandwichlabs.slaw-bundle`, `slaw/bundle` |
| macOS bundle id, Linux desktop files, hicolor icons, mime XML | `dev.sandwichlabs.assay`, `assay.desktop`, `assay-mime.xml`, `assay.*` | `dev.sandwichlabs.slaw`, `slaw.desktop`, `slaw-mime.xml`, `slaw.*` |
| Keychain service, config folder, DuckDB extension cache | `assay` | `slaw` |
| Environment variables | `ASSAY_*` | `SLAW_*` |
| Page script API and plumbing | `assay.run` / `.state` / `.download` / `.ask` / `.use` / `.lib`, `/assay-view/`, `assay-page:`, `html/assay-lib/`, `assay-rules-`, `assayRange__` / `assayStep__` / `assayTick__` | `slaw.*`, `/slaw-view/`, `slaw-page:`, `html/slaw-lib/`, `slaw-rules-`, `slawRange__` ... |
| Hash domain tags | `assay-view-hash/1` ... `assay-conn-v2` | `slaw-*`; golden test values change |
| MCP | `assay mcp serve`, `assay-<workspace>` | `slaw mcp serve`, `slaw-<workspace>` |
| Editors | `editors/nvim/lua/assay` | `lua/slaw` |
| Theme tokens | `--assay-*` | `--slaw-*` |
| Identifiers | `runAssay`, `isAssayFile`, `useAssayEditor`, ... | `runSlaw`, `isSlawFile`, `useSlawEditor`, ... |
| Plan names | `AssayBench` (E2), `assay verify` (E5) | `SlawBench`, `slaw verify` |
| Icon source | `studio/build/icon/assay.svg` | `slaw.svg` |

**Mentions of Assay that change by hand:**

- The unreleased CHANGELOG entry "**Vibe is now Assay.**" becomes "**Vibe is now Slaw.**", with the new names and a pointer to this plan's §4. Assay never shipped, so no user-facing text mentions it.
- The README's "Assay was called Vibe until v0.6.0" becomes "Slaw was called Vibe Studio until v0.6.0."
- Documents that link `rename-to-assay.md` as the current plan (`docs/README.md`, `direction.md`, `answers-you-can-check.md`) link this one instead, and `docs/README.md` lists both.
- `rename-to-assay.md` gets one status line at the top: "Superseded by rename-to-slaw.md: the product is called Slaw." Nothing else in it changes.

---

## 4. Cutover

- **Old Vibe installs** (v0.1.2 to v0.5.0): as in the Assay plan. Uninstall Vibe first; an old `vibe update` that follows the repo redirect to v0.6.0 is unsupported.
- **A workspace made with Vibe.** From its root:

  ```sh
  git mv vibe.yaml slaw.yaml
  for f in notebooks/*.vnb; do git mv "$f" "${f%.vnb}.slnb"; done
  rm -rf .vibe-local
  sed -i.bak 's/^\.vibe-local\/$/.slaw-local\//' .gitignore && rm .gitignore.bak
  ```

  Then enter credentials again (`slaw config llm-key`, connection sign-ins), register MCP clients again (`slaw-*`), and allow scripted views, pages and `.env` files again.
- **A workspace made with a main build between #315 and this PR** (`assay.yaml`, `*.asnb`): the same steps with `assay` in place of `vibe` and `.asnb` in place of `.vnb`.

---

## 5. Order of work

1. **Rename the GitHub repository** `SandwichLabs/assay` → `SandwichLabs/slaw`, and point local remotes at it.
2. **PR C: the rename, in one pass** (this plan plus the code). The Assay rename script (`rename.sh`, kept outside the repo) with `assay` → `slaw` and `.asnb` → `.slnb`, run over everything outside the history list, then fixes by hand. The script goes in the PR description. Checks as in the Assay plan: `task`, `task typecheck`, the ast-grep rules, `check-arity.mjs`, `task build:studio:linux`, a macOS `workflow_dispatch` run, and a `git grep -i assay` that leaves only history, this plan, and the deliberate lines above.
3. **Issues:** retitle #308 to "slaw verify --notebooks"; note the new name on #311.
4. **Website** (on hold until v0.6.0, as before): the Assay page becomes `/projects/slaw/`; `/vibe`, `/assay`, `/projects/vibe-studio` and `/projects/assay` redirect to it; the installer is `sandwichlabs.dev/slaw/install.sh`.
5. **The pitch film** names the product in the VO ("Assay." twice, "Assay says so", "The built-in Assay Agent", "Assay. By SandwichLabs.") and in the wordmark. Those clips and the end card are redone with Slaw before the film goes on the page.
6. **PR B (copy pass), then v0.6.0** with the icon.
