// SPIKE #257 - never merge. ghostty-web 0.4.0 behind TermAdapter.
import wasmUrl from 'ghostty-web/ghostty-vt.wasm?url'
import type { Dump, Metrics, TermAdapter, TermTheme } from './adapter'

type GT = import('ghostty-web').Terminal

export function ghosttyAdapter(wasm: 'inline' | 'url', scrollback: number): TermAdapter {
  let term: GT | null = null
  let el: HTMLElement | null = null
  let font = ''
  let fitAddon: import('ghostty-web').FitAddon | null = null
  let dataCb: (s: string) => void = () => {}
  let resizeCb: (d: { cols: number; rows: number }) => void = () => {}
  let mod: typeof import('ghostty-web') | null = null
  let ghosttyObj: import('ghostty-web').Ghostty | null = null
  const notes: string[] = []

  async function create(theme: TermTheme): Promise<void> {
    if (!mod) mod = await import('ghostty-web')
    if (wasm === 'url') {
      if (!ghosttyObj) ghosttyObj = await mod.Ghostty.load(wasmUrl)
    } else if (!ghosttyObj) {
      await mod.init()
    }
    term = new mod.Terminal({ fontSize: 13, fontFamily: font, theme: { ...theme }, scrollback, cursorBlink: false, ...(ghosttyObj ? { ghostty: ghosttyObj } : {}) })
    fitAddon = new mod.FitAddon()
    term.loadAddon(fitAddon)
    term.open(el!)
    term.onData((d) => dataCb(d))
    term.onResize((d) => resizeCb(d))
  }

  const self: TermAdapter = {
    kind: 'ghostty',
    notes,
    async open(e, theme, f) {
      el = e; font = f
      const t0 = performance.now()
      await create(theme)
      notes.push(`scrollbackOption=${scrollback}; wasm=${wasm}${wasm === 'url' ? ' (' + wasmUrl + ')' : ''}; renderer=canvas2d`)
      if (wasm === 'url') {
        try { const r = await fetch(wasmUrl); notes.push(`wasm fetch ${r.status} content-type=${r.headers.get('content-type')} bytes=${(await r.arrayBuffer()).byteLength}`) } catch (e) { notes.push('wasm fetch FAILED ' + String(e)) }
      }
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
      // In place: what the public surface offers. options.theme warns "not yet fully supported";
      // the renderer has setTheme but the wasm terminal keeps the palette it was built with.
      const r = (term as unknown as { renderer?: { setTheme(t: TermTheme): void; render?: unknown } }).renderer
      if (!r) return 'unsupported'
      ;(term!.options as { theme: TermTheme }).theme = { ...theme }
      r.setTheme({ ...theme })
      return 'applied'
    },
    getSelection: () => term!.getSelection(),
    select: (c, r, l) => term!.select(c, r, l),
    selectAll: () => term!.selectAll(),
    paste: (t) => term!.paste(t),
    hasBracketedPaste: () => term!.hasBracketedPaste(),
    scrollLines: (n) => term!.scrollLines(n),
    scrollToBottom: () => term!.scrollToBottom(),
    focus: () => term!.focus(),
    bindCopyKey(cb) { term!.attachCustomKeyEventHandler((e) => { if (e.ctrlKey && e.shiftKey && e.code === 'KeyC') { cb(); return true } return false }) }, // ghostty: true = handled, skip default
    dump(n) {
      const b = term!.buffer.active
      const total = b.length
      const from = n ? Math.max(0, total - n) : 0
      const lines: string[] = []
      for (let i = from; i < total; i++) lines.push(b.getLine(i)?.translateToString(true) ?? '')
      return { cols: term!.cols, rows: term!.rows, viewportY: term!.getViewportY(), scrollback: total - term!.rows, length: total, lines }
    },
    metrics(): Metrics {
      const c = (term as unknown as { renderer: { getCanvas(): HTMLCanvasElement } }).renderer.getCanvas()
      const r = c.getBoundingClientRect()
      return { screenW: r.width, screenH: r.height, containerW: el!.clientWidth, containerH: el!.clientHeight }
    },
    canvasPng() {
      const c = (term as unknown as { renderer: { getCanvas(): HTMLCanvasElement } }).renderer.getCanvas()
      try { return c.toDataURL('image/png') } catch { return null }
    },
    inputEl: () => (term?.textarea as HTMLElement | undefined) ?? el,
    dispose() { term?.dispose(); term = null },
  }
  return self
}
