# How the screenshots were taken

Taken with [rodney](../../contributing/rodney.md) (headless Chrome) against the static pages in this folder. Nothing in the real app was run.

1. `node docs/design/workspaces-connections/build.mjs` (regenerates `mock.css` and the HTML).
2. For each page, `rodney open "file://…/<page>.html?theme=light|dark"` (the page's own light/dark toggle reads `?theme=`), read `document.documentElement.scrollHeight`, then `rodney screenshot -w <1280|900> -h <that height> <file>` for a full-page capture.
3. Light and dark at 1280px for all ten pages; pages 04, 05 and 07 also at 900px (light) to check the narrow layout (icon rail, connections drawer, stacked banners).
4. Files named `<page>-<theme>-<width>.png` were then palette-quantised to 24 colours with Pillow (`optimize=True`, no dithering) to keep the folder small (roughly 25% smaller than the previous 128-colour set although the pages grew). Flat UI colours survive this, photographs would not; text edges are slightly coarser than the live app.

Each page stacks several states, so the shots are tall. `index-light-1280.png` is the gallery.
