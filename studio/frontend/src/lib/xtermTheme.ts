// The terminal's colours come from the active Vibe theme: the same --vibe-*
// roles the rest of Studio reads (docs/reference/themes.md), resolved to
// concrete values because xterm paints a canvas and cannot use var().
import type { ITheme } from '@xterm/xterm'

function read(style: CSSStyleDeclaration, name: string): string {
  return style.getPropertyValue(name).trim()
}

export function themeFromDocument(doc: Document = document): ITheme {
  const style = getComputedStyle(doc.documentElement)
  const bg = read(style, '--vibe-editor-bg') || read(style, '--vibe-bg')
  const fg = read(style, '--vibe-editor-fg') || read(style, '--vibe-fg')
  const pick = (name: string, fallback = fg) => read(style, name) || fallback
  const red = pick('--vibe-error')
  const green = pick('--vibe-success')
  const yellow = pick('--vibe-warn')
  const blue = pick('--vibe-info')
  const magenta = pick('--vibe-chart-4')
  const cyan = pick('--vibe-chart-5')
  const muted = pick('--vibe-fg-muted')
  return {
    background: bg,
    foreground: fg,
    cursor: pick('--vibe-editor-cursor'),
    cursorAccent: bg,
    selectionBackground: pick('--vibe-editor-selection', muted),
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
  return read(getComputedStyle(doc.documentElement), '--vibe-font-mono') || 'monospace'
}
