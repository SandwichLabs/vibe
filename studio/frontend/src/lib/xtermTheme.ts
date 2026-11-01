// The terminal's colours come from the active Slaw theme: the same --slaw-*
// roles the rest of Studio reads (docs/reference/themes.md), resolved to
// concrete values because xterm paints a canvas and cannot use var().
import type { ITheme } from '@xterm/xterm'

function read(style: CSSStyleDeclaration, name: string): string {
  return style.getPropertyValue(name).trim()
}

export function themeFromDocument(doc: Document = document): ITheme {
  const style = getComputedStyle(doc.documentElement)
  const bg = read(style, '--slaw-editor-bg') || read(style, '--slaw-bg')
  const fg = read(style, '--slaw-editor-fg') || read(style, '--slaw-fg')
  const pick = (name: string, fallback = fg) => read(style, name) || fallback
  const red = pick('--slaw-error')
  const green = pick('--slaw-success')
  const yellow = pick('--slaw-warn')
  const blue = pick('--slaw-info')
  const magenta = pick('--slaw-chart-4')
  const cyan = pick('--slaw-chart-5')
  const muted = pick('--slaw-fg-muted')
  return {
    background: bg,
    foreground: fg,
    cursor: pick('--slaw-editor-cursor'),
    cursorAccent: bg,
    selectionBackground: pick('--slaw-editor-selection', muted),
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
  return read(getComputedStyle(doc.documentElement), '--slaw-font-mono') || 'monospace'
}
