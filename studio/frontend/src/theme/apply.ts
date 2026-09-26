import { themeToCssVars, type ResolvedTheme } from './schema'

/**
 * Write a resolved theme onto <html>. Everything downstream is plain CSS:
 * bridge.css maps the `--sh-*` tokens onto these `--vibe-*` properties, the
 * CodeMirror theme reads `var(--vibe-editor-*)`, and `color-scheme` lets
 * native scrollbars / form controls follow the theme's light/dark type.
 */
export function applyThemeToDocument(theme: ResolvedTheme, doc: Document = document): void {
  const root = doc.documentElement
  const vars = themeToCssVars(theme)
  for (const [k, v] of Object.entries(vars)) root.style.setProperty(k, v)
  root.dataset.theme = theme.id
  root.dataset.themeType = theme.type
  root.style.colorScheme = theme.type
}

export function applyUiScale(percent: number, doc: Document = document): void {
  const clamped = Math.min(150, Math.max(75, percent || 100))
  doc.documentElement.style.setProperty('--vibe-ui-scale', String(clamped / 100))
}

export function applyReduceMotion(on: boolean, doc: Document = document): void {
  doc.documentElement.dataset.reduceMotion = on ? 'true' : 'false'
}
