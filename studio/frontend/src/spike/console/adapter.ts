// SPIKE #257 - throwaway harness, never merge. The seam both widgets implement.
export type Kind = 'ghostty' | 'xterm-dom' | 'xterm-webgl'

export interface TermTheme {
  background: string; foreground: string; cursor: string; cursorAccent: string
  selectionBackground: string; selectionForeground: string
  black: string; red: string; green: string; yellow: string; blue: string; magenta: string; cyan: string; white: string
  brightBlack: string; brightRed: string; brightGreen: string; brightYellow: string; brightBlue: string; brightMagenta: string; brightCyan: string; brightWhite: string
}

export interface Dump { cols: number; rows: number; viewportY: number; scrollback: number; length: number; lines: string[] }
export interface Metrics { screenW: number; screenH: number; containerW: number; containerH: number }

export interface TermAdapter {
  kind: Kind
  /** Free-form note: renderer actually used, wasm path, fallbacks. */
  notes: string[]
  open(el: HTMLElement, theme: TermTheme, font: string): Promise<{ initMs: number }>
  write(data: string | Uint8Array, cb?: () => void): void
  onData(cb: (s: string) => void): void
  onResize(cb: (d: { cols: number; rows: number }) => void): void
  fit(): { cols: number; rows: number }
  /** inplace: change the live widget; recreate: dispose and open again (content is lost). */
  setTheme(theme: TermTheme, mode: 'inplace' | 'recreate'): Promise<'applied' | 'recreated' | 'unsupported'>
  getSelection(): string
  select(col: number, row: number, len: number): void
  selectAll(): void
  paste(text: string): void
  hasBracketedPaste(): boolean
  scrollLines(n: number): void
  scrollToBottom(): void
  focus(): void
  /** Bind Ctrl+Shift+C to `cb` through attachCustomKeyEventHandler (the two widgets use opposite return conventions). */
  bindCopyKey(cb: () => void): void
  /** Bottom `n` lines of the active buffer plus counters; whole screen when n omitted. */
  dump(n?: number): Dump
  metrics(): Metrics
  canvasPng(): string | null
  /** The element key events and paste events land on. */
  inputEl(): HTMLElement | null
  dispose(): void
}
