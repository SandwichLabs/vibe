import { describe, it, expect } from 'vitest'
import { themeFromDocument, fontFromDocument } from './xtermTheme'

function docWith(vars: Record<string, string>): Document {
  const doc = document.implementation.createHTMLDocument('t')
  for (const [k, v] of Object.entries(vars)) doc.documentElement.style.setProperty(k, v)
  return doc
}

describe('themeFromDocument', () => {
  it('maps the --slaw-* roles onto the terminal palette', () => {
    const t = themeFromDocument(docWith({
      '--slaw-editor-bg': '#101010', '--slaw-editor-fg': '#eeeeee', '--slaw-editor-cursor': '#00ff00',
      '--slaw-editor-selection': '#334455', '--slaw-error': '#ff0000', '--slaw-success': '#00aa00',
      '--slaw-warn': '#ffaa00', '--slaw-info': '#0000ff', '--slaw-chart-4': '#aa00aa', '--slaw-chart-5': '#00aaaa',
      '--slaw-fg-muted': '#888888',
    }))
    expect(t.background).toBe('#101010')
    expect(t.foreground).toBe('#eeeeee')
    expect(t.cursor).toBe('#00ff00')
    expect(t.cursorAccent).toBe('#101010')
    expect(t.selectionBackground).toBe('#334455')
    expect(t.red).toBe('#ff0000')
    expect(t.green).toBe('#00aa00')
    expect(t.yellow).toBe('#ffaa00')
    expect(t.blue).toBe('#0000ff')
    expect(t.magenta).toBe('#aa00aa')
    expect(t.cyan).toBe('#00aaaa')
    expect(t.brightBlack).toBe('#888888')
    expect(t.brightRed).toBe(t.red)
  })

  it('falls back to the foreground and background when a role is missing', () => {
    const t = themeFromDocument(docWith({ '--slaw-editor-bg': '#000000', '--slaw-editor-fg': '#ffffff' }))
    expect(t.red).toBe('#ffffff')
    expect(t.cursor).toBe('#ffffff')
    expect(t.background).toBe('#000000')
  })

  it('reads the mono font, with a generic fallback', () => {
    expect(fontFromDocument(docWith({ '--slaw-font-mono': 'Fira Code, monospace' }))).toBe('Fira Code, monospace')
    expect(fontFromDocument(docWith({}))).toBe('monospace')
  })
})
