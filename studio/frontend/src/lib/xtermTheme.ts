// The terminal's colours come from the active Assay theme: the same --assay-*
// roles the rest of Studio reads (docs/reference/themes.md), resolved to
// concrete values because xterm paints a canvas and cannot use var().
import type { ITheme } from '@xterm/xterm'

function read(style: CSSStyleDeclaration, name: string): string {
  return style.getPropertyValue(name).trim()
}

export function themeFromDocument(doc: Document = document): ITheme {
  const style = getComputedStyle(doc.documentElement)
  const bg = read(style, '--assay-editor-bg') || read(style, '--assay-bg')
  const fg = read(style, '--assay-editor-fg') || read(style, '--assay-fg')
  const pick = (name: string, fallback = fg) => read(style, name) || fallback
  const red = pick('--assay-error')
  const green = pick('--assay-success')
  const yellow = pick('--assay-warn')
  const blue = pick('--assay-info')
  const magenta = pick('--assay-chart-4')
  const cyan = pick('--assay-chart-5')
  const muted = pick('--assay-fg-muted')
  return {
    background: bg,
    foreground: fg,
    cursor: pick('--assay-editor-cursor'),
    cursorAccent: bg,
    selectionBackground: pick('--assay-editor-selection', muted),
    black: bg,
    red,
    green,
    yellow,
    blue,
    magenta,
    cyan,
    white: fg,
    brightBlack: muted,
    brightRed: red,
    brightGreen: green,
    brightYellow: yellow,
    brightBlue: blue,
    brightMagenta: magenta,
    brightCyan: cyan,
    brightWhite: fg,
  }
}

export function fontFromDocument(doc: Document = document): string {
  return read(getComputedStyle(doc.documentElement), '--assay-font-mono') || 'monospace'
}
