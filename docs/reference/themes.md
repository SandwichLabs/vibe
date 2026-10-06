# Themes

Vibe Studio's look is a **theme**: a JSON file that assigns colours to a
fixed set of semantic roles, VS Code style. Four themes ship built in, and
you can add your own by dropping a file in the themes folder.

Themes are an **app preference**, not part of a workspace. Your choice
lives in your user config directory and follows you across every workspace you
open; a workspace you share never carries your colours with it.

## Choosing a theme

Settings → **Appearance**.

- **Pick one** — every available theme, with a live miniature.
- **System** — follow the OS light/dark setting, with one theme chosen for
  each. The top-bar sun/moon button flips between the pair.
- **Reduce motion** and **UI scale** live in the same section.

| Built-in | Type | Notes |
|---|---|---|
| `schoolhouse-light` | light | The default. Warm paper, soft ink, teal. |
| `schoolhouse-dark` | dark | Same ink and paper, lights off. |
| `signal` | light | Black, white and signal yellow. High contrast. |
| `chalkboard` | dark | Green slate, chalk yellow, mint. |

## Where things live

| | Linux | macOS |
|---|---|---|
| Preferences | `~/.config/vibe/settings.json` | `~/Library/Application Support/vibe/settings.json` |
| Custom themes | `~/.config/vibe/themes/*.json` | `~/Library/Application Support/vibe/themes/*.json` |

Set `VIBE_CONFIG_DIR` to move both somewhere else (portable installs, tests).
"Open folder" in Settings creates the themes folder, drops a short README in
it, and opens it in your file manager. "Reload themes" re-reads the folder
without restarting.

`settings.json` looks like:

```json
{
  "theme": "system",
  "systemLight": "schoolhouse-light",
  "systemDark": "chalkboard",
  "reduceMotion": false,
  "uiScale": 100
}
```

`theme` is a theme id or `"system"`. Unknown keys are preserved, so a newer
Studio's settings survive an older one saving the file.

## Writing a theme

A theme file is a JSON object. The **id** is the filename without `.json`,
slugged (`My Theme.json` → `my-theme`); it can't collide with a built-in.

```json
{
  "name": "Nord-ish",
  "type": "dark",
  "description": "A cool-blue dark theme built on SchoolHouse Dark.",
  "extends": "schoolhouse-dark",
  "colors": {
    "accent": "#88C0D0",
    "bg": "#2E3440"
  }
}
```

| Key | Required | Meaning |
|---|---|---|
| `name` | yes | Shown in the picker. |
| `type` | yes | `"light"` or `"dark"`. Drives `color-scheme` (native scrollbars, form controls) and which grid the theme appears in under **System**. |
| `description` | no | One line under the name. |
| `extends` | no* | A theme id whose values fill in every role you leave out. Can be a built-in or another custom theme. *Required unless you set every colour role. |
| `colors` | no | Role → hex colour. `#RGB`, `#RRGGBB` or `#RRGGBBAA`. |
| `fonts` | no | `display`, `body`, `mono` → a font-family list. Families must already be installed or bundled. |

Validation is strict: an unknown role, a non-hex colour, or an unknown
top-level key rejects the file. Every problem is reported at once (Settings
→ Appearance lists them) and the theme appears in the picker greyed out with
its error, so you can fix it and reload. A theme with an `extends` cycle or a
missing parent is rejected the same way.

[`themes/nord-ish.json`](themes/nord-ish.json) is a complete example.

### Colour roles

Surfaces and text:

| Role | Used for |
|---|---|
| `bg` | Page background. |
| `bg.alt` | Alternate rows, panels, gutters, toolbars. |
| `surface` | Card interiors, inputs. |
| `fg` | Primary text. |
| `fg.muted` | Secondary text, metadata, labels. |
| `fg.disabled` | Disabled text. |
| `border` | The load-bearing 2px borders. |
| `border.subtle` | Hairline row dividers. |
| `shadow` | The hard offset shadow behind cards and buttons. |
| `focus` | Keyboard focus ring. |

Accents:

| Role | Used for |
|---|---|
| `accent`, `accent.hover`, `accent.active` | Primary buttons, links, active nav, selected rows. |
| `accent.fg` | Text on an accent fill. |
| `highlight`, `highlight.fg` | Badges, in-progress markers, the user avatar, text selection. |
| `attention` | "Needs attention" markers. Not destructive. |
| `splash` | Brand flourishes, pull quotes. |
| `success`, `warn`, `error`, `info` | Status colours. `error` must read as text on `bg`. |
| `selection`, `selection.fg` | Text selection. |
| `scrim` | Modal backdrop (usually carries alpha). |

App chrome:

| Role | Used for |
|---|---|
| `chrome.bg`, `chrome.fg`, `chrome.border` | The top bar and agent-panel header. |
| `sidebar.bg`, `sidebar.fg` | The navigation rail. |
| `sidebar.active.bg`, `sidebar.active.fg` | The active nav item. |
| `code.bg`, `code.fg` | Code blocks in rendered markdown and A2UI. |

Editor (CodeMirror) and syntax:

| Role | Used for |
|---|---|
| `editor.bg`, `editor.fg`, `editor.cursor` | The SQL/markdown editor. |
| `editor.gutter.bg`, `editor.gutter.fg` | Line-number gutter. |
| `editor.line`, `editor.selection` | Active line and selection (usually carry alpha). |
| `syntax.keyword`, `syntax.string`, `syntax.number`, `syntax.comment`, `syntax.link`, `syntax.heading`, `syntax.meta` | Token colours. |

Charts:

| Role | Used for |
|---|---|
| `chart.1` … `chart.6` | Categorical series palette for A2UI charts and stripes. |

### Contrast

The built-ins are tested to WCAG AA (4.5:1) on their core text/background
pairs and 3:1 on borders and focus rings. Keep the same bar for a custom
theme: `fg` on `bg`/`surface`/`bg.alt`, `fg.muted` on `bg`, `accent.fg` on
`accent`, `chrome.fg` on `chrome.bg`, `code.fg` on `code.bg`, every
`syntax.*` on `editor.bg`, and `error`/`success` on `bg`.

## How it works (for contributors)

- Roles are the contract: `studio/frontend/src/theme/schema.ts` lists them
  (`COLOR_ROLES`), parses and validates a file (`parseTheme`), walks
  `extends` (`resolveTheme`) and flattens to CSS custom properties
  (`themeToCssVars`). Built-ins live in `theme/builtin.ts` and must be
  complete, so anything can extend them.
- `theme/apply.ts` writes `--vibe-<role>` (dots become dashes:
  `sidebar.active.fg` → `--vibe-sidebar-active-fg`) onto `<html>`, plus
  `data-theme`, `data-theme-type` and `color-scheme`.
- `theme/bridge.css` maps the SchoolHouse `--sh-*` tokens the stylesheets use
  onto those roles, derives translucent tints with `color-mix()`, and owns the
  app-wide `:focus-visible` ring. New CSS should reference `--vibe-*` directly.
- The CodeMirror theme (`composables/useVibeEditor.ts`) uses `var(--vibe-editor-*)`
  and `var(--vibe-syntax-*)`, so it follows a theme switch with no re-render.
- Persistence is Go-side in `internal/prefs` (atomic writes, 0600, unknown
  keys round-trip). Studio exposes `GetPreferences`, `SetPreferences`,
  `ListUserThemes` and `OpenThemesDir`. Without the Wails runtime (vitest,
  `vite dev` in a browser) `theme/useTheme.ts` falls back to `localStorage`.
- Fonts are vendored under `studio/frontend/src/assets/fonts/` (Fraunces,
  Inter, JetBrains Mono; OFL) so the app renders identically offline.
