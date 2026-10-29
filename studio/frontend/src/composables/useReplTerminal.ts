/**
 * useReplTerminal — one Data Console tab: an xterm.js terminal wired to a Go
 * console session (internal/repl) through the Repl* bindings and the
 * "repl:out:<id>" frame events (docs/reference/console.md).
 *
 * Order matters: ReplOpen, then subscribe, then ReplAck(id, 0) to attach (Go
 * holds output until then, so the banner is never lost), then fit and resize.
 * Frames are acknowledged once xterm has rendered them, which is the
 * backpressure that keeps a huge result from flooding the webview.
 */
import { ref, shallowRef } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { createReplTransport, type ReplTransport } from '../lib/replTransport'
import { themeFromDocument, fontFromDocument } from '../lib/xtermTheme'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'

type Renderer = 'webgl' | 'dom' | ''

export function useReplTerminal() {
  const id = ref('')
  const ended = ref('') // why the session closed ('' = running)
  const renderer = ref<Renderer>('')
  const error = ref('')
  // current is the console's actual connection (from ReplOpen, ReplUse and the
  // repl:conn:<id> event, so a hand-typed .conn counts); refusal is why the
  // last picker change did not move it ('' = none).
  const current = ref('')
  const refusal = ref('')
  const refusedFor = ref<string | null>(null) // the picker value that was refused
  const term = shallowRef<Terminal | null>(null)

  let fit: FitAddon | null = null
  let transport: ReplTransport | null = null
  let off: (() => void) | null = null
  let offConn: (() => void) | null = null
  let wanted: string | null = null
  let switching = false
  let ro: ResizeObserver | null = null
  let mo: MutationObserver | null = null
  let fitTimer: ReturnType<typeof setTimeout> | undefined
  let gen = 0 // bumps on close, so an open that finishes late disposes itself

  function doFit() {
    const el = term.value?.element?.parentElement
    if (!fit || !el || el.clientWidth === 0 || el.clientHeight === 0) return
    try { fit.fit() } catch { /* a hidden or detached container */ }
  }

  function refit() {
    clearTimeout(fitTimer)
    fitTimer = setTimeout(doFit, 50)
  }

  function onFrame(f: ReplFrame) {
    const t = term.value
    if (!t) return
    if (f.closed) {
      ended.value = f.reason || 'closed'
      t.write(`\r\n[console closed: ${ended.value}]\r\n`)
      off?.(); off = null
      transport?.stop()
      return
    }
    t.write(f.data, () => transport?.acked(f.seq))
  }

  async function loadRenderer(t: Terminal, webgl: typeof import('@xterm/addon-webgl')): Promise<void> {
    renderer.value = 'dom'
    try {
      if (!document.createElement('canvas').getContext('webgl2')) return
      const gl = new webgl.WebglAddon()
      gl.onContextLoss(() => { gl.dispose(); renderer.value = 'dom' })
      t.loadAddon(gl)
      renderer.value = 'webgl'
    } catch {
      renderer.value = 'dom' // the DOM renderer is the fallback
    }
  }

  async function open(container: HTMLElement, connection: string): Promise<void> {
    const mine = ++gen
    error.value = ''
    ended.value = ''
    const [xterm, fitMod, uni, webgl] = await Promise.all([
      import('@xterm/xterm'), import('@xterm/addon-fit'),
      import('@xterm/addon-unicode11'), import('@xterm/addon-webgl'),
      import('@xterm/xterm/css/xterm.css'),
    ])
    if (mine !== gen) return
    const t = new xterm.Terminal({
      scrollback: 10000, allowProposedApi: true, macOptionIsMeta: true, customGlyphs: true,
      fontFamily: fontFromDocument(), fontSize: 13, theme: themeFromDocument(),
    })
    fit = new fitMod.FitAddon()
    t.loadAddon(fit)
    t.loadAddon(new uni.Unicode11Addon())
    t.unicode.activeVersion = '11'
    t.open(container)
    term.value = t // from here close() owns disposing it
    await loadRenderer(t, webgl)
    if (mine !== gen) return
    let res: ReplOpenResult
    try {
      res = await App.ReplOpen(connection)
    } catch (e) {
      if (mine === gen) {
        error.value = errorText(e)
        t.dispose(); term.value = null
      }
      return
    }
    if (mine !== gen) { void App.ReplClose(res.id).catch(() => {}); return }
    id.value = res.id
    transport = createReplTransport(
      { input: (d) => App.ReplInput(res.id, d), ack: (s) => App.ReplAck(res.id, s) },
      (m) => { if (!ended.value) error.value = m },
    )
    current.value = res.connection
    refusal.value = ''
    if (hasWailsRuntime()) {
      off = EventsOn('repl:out:' + res.id, onFrame)
      offConn = EventsOn('repl:conn:' + res.id, (name: string) => { current.value = name; refusal.value = ''; refusedFor.value = null })
    }
    t.onData((d) => transport?.send(d))
    t.onResize(({ cols, rows }) => { void App.ReplResize(res.id, cols, rows).catch(() => {}) })
    t.attachCustomKeyEventHandler((e) => {
      // Ctrl+Shift+C copies the selection and is never sent to the console (it
      // would arrive as Ctrl+C and cancel a statement); the rest is the terminal's.
      if (e.ctrlKey && e.shiftKey && e.code === 'KeyC') {
        if (e.type === 'keydown' && t.hasSelection()) void navigator.clipboard?.writeText(t.getSelection())
        return false
      }
      return true
    })
    try {
      await App.ReplAck(res.id, 0)
    } catch (e) {
      error.value = errorText(e)
    }
    if (mine !== gen) return // closed while attaching: close() already cleaned up
    doFit()
    void App.ReplResize(res.id, t.cols, t.rows).catch(() => {})
    ro = new ResizeObserver(refit)
    ro.observe(container)
    mo = new MutationObserver(() => { if (term.value) term.value.options.theme = themeFromDocument() })
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['style', 'data-theme'] })
  }

  // useConnection asks Go to move the console to a connection ("" = the
  // workspace default). Calls are serialised and the latest wins: a change that
  // arrives while one is in flight is sent when it resolves. Go refuses while a
  // statement runs or text is typed; the console then stays where it was, and
  // refusal says why. current always holds where the console really is.
  async function useConnection(name: string): Promise<void> {
    wanted = name
    if (switching) return
    switching = true
    try {
      while (wanted !== null && id.value && !ended.value) {
        const n = wanted
        const mine = gen
        wanted = null
        try {
          const cur = await App.ReplUse(id.value, n)
          if (mine !== gen) return // closed or reopened meanwhile: this is not that console
          current.value = cur
          refusal.value = ''
          refusedFor.value = null
        } catch (e) {
          if (mine !== gen) return
          refusal.value = errorText(e)
          refusedFor.value = n
        }
      }
    } finally {
      switching = false
    }
  }

  function close() {
    gen++
    clearTimeout(fitTimer)
    ro?.disconnect(); ro = null
    mo?.disconnect(); mo = null
    off?.(); off = null
    offConn?.(); offConn = null
    wanted = null
    transport?.stop(); transport = null
    if (id.value && !ended.value) void App.ReplClose(id.value).catch(() => {})
    term.value?.dispose(); term.value = null
    fit = null
    id.value = ''
    renderer.value = ''
  }

  return { open, close, refit: doFit, useConnection, ended, id, renderer, error, current, refusal, refusedFor }
}
