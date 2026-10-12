# Docs assets

Screenshots and media for the README, the docs and the release page. All of them
come from the **current UI** (Vibe Studio from the `main` branch, a dev build, so
Settings > About says `dev`) against a local demo workspace, "Coffee Shop
Analytics", with no account, token or personal data:

- `local` (DuckDB) with `orders` (about 33,500 rows), `stores` and `products`,
  generated with a seeded DuckDB script and loaded with `vibe import` from CSV.
- `loyalty`, a read-only SQLite connection with 20 made-up members.
- `data/weather.parquet`, a synthetic daily-weather file.
- One notebook ("Q3 sales review": markdown, an A2UI stat row and bar chart, SQL
  cells with line and table views), two saved queries and one skill.

Every name and number is synthetic. Screenshots are 1440 pixels wide at 1x
(Rodney has no device-pixel-ratio option), palette-optimised PNGs under 60 KB each.

| File | Shows |
|---|---|
| `hero-notebook.png` | Notebook with stat cards, bar chart and monthly line chart (README hero) |
| `hero-notebook-dark.png` | Same view in the SchoolHouse dark theme |
| `quickstart-studio-home.png` | Studio Home with the demo workspace open |
| `quickstart-notebook.png` | A SQL cell with its SQL shown and the result table |
| `hero.gif` / `hero.mp4` | 20 s loop: open the notebook, charts render, scroll to the table (GIF 720 px, 8 fps, about 2.4 MB; MP4 1440x900) |
| `social-preview.png` | 1280x640 social card |

## Not shipped

Taken but left out of the repository to keep it small (kept outside the repo, in the build scratch folder, to re-use later):
`notebooks-list.png`, `connections.png`, `query-editor.png`, `table-browser.png`, `agent.png` and `settings-about.png`.
Retake `settings-about.png` from a stamped rc build before shipping it, because a dev build shows version `dev`.
`agent.png` was a **faked** LLM shot (a scripted OpenAI-compatible stub ran one real `run_query` and streamed a fixed answer computed from the demo data); if it is ever used, say so and prefer a real model.

## How they were made

1. `vibe workspace init` for the folder, then the data: a seeded DuckDB
   `generate_series` script wrote `orders.csv`, `stores.csv`, `products.csv` and
   `weather.parquet`; `vibe import` loaded the CSVs. The notebook, queries and
   skill are plain files in the workspace folder.
2. `task studio:dev` with `VIBE_CONFIG_DIR` pointing at a scratch directory, the
   workspace opened through the `OpenWorkspace` binding, then
   [Rodney](../contributing/rodney.md) `screenshot -w 1440 -h 900` per view.
3. The GIF and MP4 are a Rodney screenshot loop (about 13 frames a second)
   while a script clicks the notebook and scrolls smoothly, assembled with
   ffmpeg (`libx264`, then `palettegen`/`paletteuse` for the GIF).
4. `social-preview.png` is an HTML card rendered in Rodney: brand teal gradient, `studio/build/icon/vibe.svg`,
   "Vibe" in Fraunces, the README tagline in Inter (both from `studio/frontend/src/assets/fonts`) and a crop of `hero-notebook.png`.

The top-bar "V" mark in Studio differs from the new app icon (`studio/build/icon/vibe.svg`); a follow-up could use the icon there. The social card uses the icon.

Re-take a shot when the UI around it changes.

## For the release and repository settings

- **Social preview** (`social-preview.png`) can only be uploaded in the GitHub
  web UI: repository Settings > General > Social preview. There is no API.
- **Launch film.** The sandwichlabs.dev project page already has one:
  <https://sandwichlabs.dev/previews/vibe-studio/vibe-studio-launch-v3.mp4>
  (source in the `www` repo, `src/content/projects/vibe-studio.md`). Link it from
  the release page rather than making another.
