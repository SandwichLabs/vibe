/**
 * Vibe Studio theme schema — the contract between a theme file and the UI.
 *
 * A theme is a JSON document (VS Code style) that assigns hex colours to a
 * fixed set of semantic ROLES. The UI never references a hue ("teal") — it
 * references a role ("accent") through a `--vibe-*` CSS custom property, and
 * the SchoolHouse `--sh-*` tokens the existing stylesheets use are bridged
 * onto those roles in theme/bridge.css. Swap the theme, every surface follows.
 *
 * Validation is strict on purpose: unknown keys are rejected (so a typo never
 * silently does nothing), and every colour must be hex. Theme files are
 * user-authored, not model output, but the same "closed vocabulary" rule the
 * A2UI validator enforces applies — nothing from a file reaches a style
 * attribute unparsed.
 */

/** Every colour role a theme can set. Dotted names group related roles. */
export const COLOR_ROLES = [
  // Surfaces
  'bg',            // page background (SchoolHouse "paper")
  'bg.alt',        // secondary surface: alternate rows, sidebar, gutters
  'surface',       // card interior
  // Text
  'fg',            // primary text
  'fg.muted',      // secondary text, metadata
  'fg.disabled',   // disabled text, dividers on dark
  // Lines
  'border',        // load-bearing 2px borders
  'border.subtle', // hairline row dividers
  'shadow',        // hard offset shadow colour
  'focus',         // keyboard focus ring
  // Brand accent (buttons, links, active nav)
  'accent',
  'accent.hover',
  'accent.active',
  'accent.fg',     // text on an accent fill
  // Secondary accents
  'highlight',     // in-progress, badges, user avatar (SchoolHouse marigold)
  'highlight.fg',
  'attention',     // needs attention — NOT destructive (SchoolHouse coral)
  'splash',        // brand flourish, pull quotes (SchoolHouse ember)
  // Status
  'success',
  'warn',
  'error',
  'info',
  // Selection + overlays
  'selection',     // text selection background
  'selection.fg',
  'scrim',         // modal backdrop (may carry alpha)
  // App chrome (top bar)
  'chrome.bg',
  'chrome.fg',
  'chrome.border',
  // Sidebar
  'sidebar.bg',
  'sidebar.fg',
  'sidebar.active.bg',
  'sidebar.active.fg',
  // Code blocks in rendered content
  'code.bg',
  'code.fg',
  // CodeMirror editor
  'editor.bg',
  'editor.fg',
  'editor.gutter.bg',
  'editor.gutter.fg',
  'editor.line',       // active line background (may carry alpha)
  'editor.selection',  // (may carry alpha)
  'editor.cursor',
  // Syntax highlighting
  'syntax.keyword',
  'syntax.string',
  'syntax.number',
  'syntax.comment',
  'syntax.link',
  'syntax.heading',
  'syntax.meta',
  // Data-viz categorical palette (A2UI charts)
  'chart.1',
  'chart.2',
  'chart.3',
  'chart.4',
  'chart.5',
  'chart.6',
] as const

export type ColorRole = (typeof COLOR_ROLES)[number]

export const FONT_ROLES = ['display', 'body', 'mono'] as const
export type FontRole = (typeof FONT_ROLES)[number]

export type ThemeType = 'light' | 'dark'

/** A theme as authored: partial, may extend another theme. */
export interface ThemeSpec {
  /** Stable id used in preferences. Built-ins set it; user files get theirs from the filename. */
  id: string
  /** Human name shown in the picker. */
  name: string
  type: ThemeType
  /** Optional one-line description for the picker. */
  description?: string
  /** Id of a theme whose values fill in every role this one leaves unset. */
  extends?: string
  colors: Partial<Record<ColorRole, string>>
  fonts?: Partial<Record<FontRole, string>>
  /** Where this theme came from (set by the loader, never by the file). */
  source?: 'builtin' | 'user'
  /** Absolute path for user themes (set by the loader). */
  path?: string
}

/** A theme with every role filled in — what the UI actually applies. */
export interface ResolvedTheme {
  id: string
  name: string
  type: ThemeType
  description?: string
  source: 'builtin' | 'user'
  path?: string
  colors: Record<ColorRole, string>
  fonts: Record<FontRole, string>
}

const HEX_RE = /^#(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$/i
const ID_RE = /^[a-z0-9][a-z0-9-]{0,63}$/
// Font stacks are a comma-separated list of quoted or bare family names and
// generic keywords. No url(), no semicolons, no braces — nothing that could
// terminate the declaration or smuggle in another one.
const FONT_RE = /^[A-Za-z0-9 '",\-]{1,200}$/

export function isHexColor(v: unknown): v is string {
  return typeof v === 'string' && HEX_RE.test(v.trim())
}

export function isThemeId(v: unknown): v is string {
  return typeof v === 'string' && ID_RE.test(v)
}

/** Turn a filename like "My Theme.json" into a stable id "my-theme". */
export function idFromFilename(filename: string): string {
  const base = filename.replace(/\.json$/i, '')
  const id = base
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64)
  return id || 'theme'
}

export class ThemeError extends Error {
  constructor(message: string, public readonly path?: string) {
    super(path ? `${path}: ${message}` : message)
    this.name = 'ThemeError'
  }
}

const KNOWN_TOP_KEYS = new Set(['$schema', 'id', 'name', 'type', 'description', 'extends', 'colors', 'fonts'])

/**
 * Parse and validate one theme document. `id` overrides / supplies the id
 * (user themes are keyed by filename so two files can't collide on a "name").
 * Throws ThemeError listing EVERY problem, not just the first, so a hand
 * author fixes the file in one pass.
 */
export function parseTheme(input: unknown, opts: { id?: string; source: 'builtin' | 'user'; path?: string }): ThemeSpec {
  const problems: string[] = []
  if (!input || typeof input !== 'object' || Array.isArray(input)) {
    throw new ThemeError('theme must be a JSON object', opts.path)
  }
  const raw = input as Record<string, unknown>

  for (const k of Object.keys(raw)) {
    if (!KNOWN_TOP_KEYS.has(k)) problems.push(`unknown top-level key "${k}"`)
  }

  const id = opts.id ?? raw.id
  if (!isThemeId(id)) problems.push(`id must match ${ID_RE} (got ${JSON.stringify(id)})`)

  const name = typeof raw.name === 'string' && raw.name.trim() ? raw.name.trim().slice(0, 80) : ''
  if (!name) problems.push('name is required')

  const type = raw.type
  if (type !== 'light' && type !== 'dark') problems.push('type must be "light" or "dark"')

  let description: string | undefined
  if (raw.description !== undefined) {
    if (typeof raw.description !== 'string') problems.push('description must be a string')
    else description = raw.description.trim().slice(0, 200)
  }

  let ext: string | undefined
  if (raw.extends !== undefined) {
    if (!isThemeId(raw.extends)) problems.push('extends must be a theme id')
    else ext = raw.extends
  }

  const colors: Partial<Record<ColorRole, string>> = {}
  if (raw.colors !== undefined) {
    if (!raw.colors || typeof raw.colors !== 'object' || Array.isArray(raw.colors)) {
      problems.push('colors must be an object')
    } else {
      for (const [k, v] of Object.entries(raw.colors as Record<string, unknown>)) {
        if (!(COLOR_ROLES as readonly string[]).includes(k)) {
          problems.push(`colors.${k}: unknown role`)
          continue
        }
        if (!isHexColor(v)) {
          problems.push(`colors.${k}: must be a hex colour like #1F2330 (got ${JSON.stringify(v)})`)
          continue
        }
        colors[k as ColorRole] = v.trim().toUpperCase()
      }
    }
  }

  let fonts: Partial<Record<FontRole, string>> | undefined
  if (raw.fonts !== undefined) {
    if (!raw.fonts || typeof raw.fonts !== 'object' || Array.isArray(raw.fonts)) {
      problems.push('fonts must be an object')
    } else {
      fonts = {}
      for (const [k, v] of Object.entries(raw.fonts as Record<string, unknown>)) {
        if (!(FONT_ROLES as readonly string[]).includes(k)) {
          problems.push(`fonts.${k}: unknown role (display, body, mono)`)
          continue
        }
        if (typeof v !== 'string' || !FONT_RE.test(v)) {
          problems.push(`fonts.${k}: must be a font-family list (letters, digits, quotes, commas)`)
          continue
        }
        fonts[k as FontRole] = v.trim()
      }
    }
  }

  if (!ext && opts.source === 'user') {
    // A user theme without `extends` must be complete, otherwise we'd be
    // inventing colours. Report the missing roles by name.
    const missing = COLOR_ROLES.filter((r) => !(r in colors))
    if (missing.length) problems.push(`no "extends" and missing colours: ${missing.join(', ')}`)
  }

  if (problems.length) throw new ThemeError(problems.join('; '), opts.path)

  return {
    id: id as string,
    name,
    type: type as ThemeType,
    description,
    extends: ext,
    colors,
    fonts,
    source: opts.source,
    path: opts.path,
  }
}

export const DEFAULT_FONTS: Record<FontRole, string> = {
  display: "'Fraunces', Georgia, 'Times New Roman', serif",
  body: "'Inter', system-ui, -apple-system, sans-serif",
  mono: "'JetBrains Mono', 'SF Mono', Menlo, monospace",
}

/**
 * Resolve a theme against a registry by walking its `extends` chain
 * (child wins). Throws on an unknown parent, a cycle, or a role that is still
 * unset at the end of the chain.
 */
export function resolveTheme(spec: ThemeSpec, registry: ReadonlyMap<string, ThemeSpec>): ResolvedTheme {
  const chain: ThemeSpec[] = []
  const seen = new Set<string>()
  let cur: ThemeSpec | undefined = spec
  while (cur) {
    if (seen.has(cur.id)) throw new ThemeError(`extends cycle through "${cur.id}"`, spec.path)
    seen.add(cur.id)
    chain.push(cur)
    if (!cur.extends) break
    const parent: ThemeSpec | undefined = registry.get(cur.extends)
    if (!parent) throw new ThemeError(`extends unknown theme "${cur.extends}"`, spec.path)
    cur = parent
  }

  const colors: Partial<Record<ColorRole, string>> = {}
  const fonts: Partial<Record<FontRole, string>> = {}
  // Parent first, child last so the child overrides.
  for (const t of [...chain].reverse()) {
    Object.assign(colors, t.colors)
    if (t.fonts) Object.assign(fonts, t.fonts)
  }
  const missing = COLOR_ROLES.filter((r) => !colors[r])
  if (missing.length) throw new ThemeError(`missing colours after resolving: ${missing.join(', ')}`, spec.path)

  return {
    id: spec.id,
    name: spec.name,
    type: spec.type,
    description: spec.description,
    source: spec.source ?? 'builtin',
    path: spec.path,
    colors: colors as Record<ColorRole, string>,
    fonts: { ...DEFAULT_FONTS, ...fonts },
  }
}

/** CSS custom-property name for a role: "accent.hover" → "--vibe-accent-hover". */
export function cssVarName(role: ColorRole | `font.${FontRole}`): string {
  return '--vibe-' + role.replace(/\./g, '-')
}

/** Flatten a resolved theme to the custom properties the UI reads. */
export function themeToCssVars(theme: ResolvedTheme): Record<string, string> {
  const out: Record<string, string> = {}
  for (const role of COLOR_ROLES) out[cssVarName(role)] = theme.colors[role]
  for (const f of FONT_ROLES) out[cssVarName(`font.${f}`)] = theme.fonts[f]
  return out
}

/**
 * Relative luminance + WCAG contrast ratio — used by the picker to warn when a
 * user theme's text/background pair is unreadable, and by tests to keep the
 * built-ins honest.
 */
export function contrastRatio(a: string, b: string): number {
  const L = (hex: string) => {
    const [r, g, b] = hexToRgb(hex).map((c) => {
      const s = c / 255
      return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
    })
    return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!
  }
  const la = L(a)
  const lb = L(b)
  const [hi, lo] = la > lb ? [la, lb] : [lb, la]
  return (hi + 0.05) / (lo + 0.05)
}

export function hexToRgb(hex: string): [number, number, number] {
  let h = hex.trim().replace('#', '')
  if (h.length === 3 || h.length === 4) h = h.split('').map((c) => c + c).join('')
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}
