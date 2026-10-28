// SPIKE #257 - never merge. @xterm/xterm 6.0.0 (+ fit, + webgl) behind TermAdapter.
import '@xterm/xterm/css/xterm.css'
import type { Terminal as XT } from '@xterm/xterm'
import type { FitAddon as XFit } from '@xterm/addon-fit'
import type { Dump, Metrics, TermAdapter, TermTheme } from './adapter'

// xterm's webgl canvas is cleared after compositing, so toDataURL() is blank. With pdb=1 the harness
// asks WebGL for preserveDrawingBuffer so pixels can be sampled; leave it off for timing runs.
function preserveDrawingBuffer(): void {
  const orig = HTMLCanvasElement.prototype.getContext as (this: HTMLCanvasElement, id: string, o?: unknown) => unknown
  HTMLCanvasElement.prototype.getContext = function (this: HTMLCanvasElement, id: string, o?: unknown) {
    return orig.call(this, id, id === 'webgl2' || id === 'webgl' ? { ...(o as object), preserveDrawingBuffer: true } : o)
  } as typeof HTMLCanvasElement.prototype.getContext
}

export function xtermAdapter(webgl: boolean, pdb: boolean): TermAdapter {
  if (pdb) preserveDrawingBuffer()
  let term: XT | null = null
  let fitAddon: XFit | null = null
  let el: HTMLElement | null = null
  let font = ''
  let dataCb: (s: string) => void = () => {}
  let resizeCb: (d: { cols: number; rows: number }) => void = () => {}
  let canvasSource: (() => HTMLCanvasElement | null) | null = null
  const notes: string[] = []

  async function create(theme: TermTheme): Promise<void> {
    const [{ Terminal }, { FitAddon }] = await Promise.all([import('@xterm/xterm'), import('@xterm/addon-fit')])
    term = new Terminal({ fontSize: 13, fontFamily: font, theme: { ...theme }, scrollback: 20000, cursorBlink: false, allowProposedApi: true })
    fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(el!)
    if (webgl) {
      try {
        const { WebglAddon } = await import('@xterm/addon-webgl')
        const w = new WebglAddon()
        w.onContextLoss(() => notes.push('webgl context lost'))
        term.loadAddon(w)
        if (pdb) canvasSource = () => el!.querySelector<HTMLCanvasElement>('.xterm-screen canvas:not(.xterm-link-layer)')
        notes.push('renderer=webgl' + (pdb ? ' (preserveDrawingBuffer for pixel sampling)' : ''))
      } catch (e) {
        notes.push('webgl FAILED, DOM fallback: ' + String(e))
      }
    } else notes.push('renderer=dom')
    term.onData((d) => dataCb(d))
    term.onResize((d) => resizeCb(d))
  }

  return {
    kind: webgl ? 'xterm-webgl' : 'xterm-dom',
    notes,
    async open(e, theme, f) {
      el = e; font = f
      const t0 = performance.now()
      await create(theme)
      return { initMs: Math.round(performance.now() - t0) }
    },
    write: (d, cb) => term!.write(d, cb),
    onData: (cb) => { dataCb = cb },
    onResize: (cb) => { resizeCb = cb },
    fit() { fitAddon!.fit(); return { cols: term!.cols, rows: term!.rows } },
    async setTheme(theme, mode) {
      if (mode === 'recreate') {
        term!.dispose(); el!.innerHTML = ''
        await create(theme)
        return 'recreated'
      }
      term!.options.theme = { ...theme }
      return 'applied'
    },
    getSelection: () => term!.getSelection(),
    select: (c, r, l) => term!.select(c, r, l),
    selectAll: () => term!.selectAll(),
    paste: (t) => term!.paste(t),
    hasBracketedPaste: () => term!.modes.bracketedPasteMode,
    scrollLines: (n) => term!.scrollLines(n),
    scrollToBottom: () => term!.scrollToBottom(),
    focus: () => term!.focus(),
    bindCopyKey(cb) { term!.attachCustomKeyEventHandler((e) => { if (e.type === 'keydown' && e.ctrlKey && e.shiftKey && e.code === 'KeyC') { cb(); return false } return true }) }, // xterm: false = skip default
    dump(n): Dump {
      const b = term!.buffer.active
      const total = b.length
      const from = n ? Math.max(0, total - n) : 0
      const lines: string[] = []
      for (let i = from; i < total; i++) lines.push(b.getLine(i)?.translateToString(true) ?? '')
      return { cols: term!.cols, rows: term!.rows, viewportY: b.viewportY, scrollback: total - term!.rows, length: total, lines }
    },
    metrics(): Metrics {
      const s = el!.querySelector<HTMLElement>('.xterm-screen')!.getBoundingClientRect()
      return { screenW: s.width, screenH: s.height, containerW: el!.clientWidth, containerH: el!.clientHeight }
    },
    canvasPng() {
      const c = canvasSource?.()
      if (!c) return null
      try { return c.toDataURL('image/png') } catch { return null }
    },
    inputEl: () => term?.textarea ?? el,
    dispose() { term?.dispose(); term = null },
  }
}
