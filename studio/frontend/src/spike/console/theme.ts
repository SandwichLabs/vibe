// SPIKE #257 - never merge. Terminal theme from --vibe-* roles, resolved through the browser.
import type { TermTheme } from './adapter'

let probe: HTMLElement | null = null
let ctx: CanvasRenderingContext2D | null = null

/** Resolve any CSS colour expression (var(), color-mix()) to #rrggbb through the engine. */
export function resolveColor(expr: string): string {
  if (!probe) {
    probe = document.createElement('div')
    probe.style.cssText = 'position:fixed;left:-9999px;top:0;width:1px;height:1px'
    document.body.appendChild(probe)
    const c = document.createElement('canvas'); c.width = c.height = 1
    ctx = c.getContext('2d', { willReadFrequently: true })
  }
  probe.style.color = ''
  probe.style.color = expr
  const computed = getComputedStyle(probe).color
  ctx!.clearRect(0, 0, 1, 1)
  ctx!.fillStyle = '#000'
  ctx!.fillStyle = computed
  ctx!.fillRect(0, 0, 1, 1)
  const [r, g, b] = ctx!.getImageData(0, 0, 1, 1).data
  return '#' + [r, g, b].map((v) => v.toString(16).padStart(2, '0')).join('')
}

const v = (role: string) => resolveColor(`var(--vibe-${role})`)

export function readTheme(): TermTheme {
  return {
    background: v('editor-bg'), foreground: v('editor-fg'), cursor: v('editor-cursor'), cursorAccent: v('editor-bg'),
    selectionBackground: v('selection'), selectionForeground: v('selection-fg'),
    black: v('bg-alt'), red: v('error'), green: v('success'), yellow: v('warn'), blue: v('info'), magenta: v('highlight'), cyan: v('accent'), white: v('fg-muted'),
    brightBlack: v('fg-disabled'), brightRed: v('chart-1'), brightGreen: v('chart-2'), brightYellow: v('chart-3'), brightBlue: v('chart-4'), brightMagenta: v('chart-5'), brightCyan: v('chart-6'), brightWhite: v('fg'),
  }
}

export function readMonoFont(): string {
  const el = document.createElement('span')
  el.style.fontFamily = 'var(--vibe-font-mono)'
  document.body.appendChild(el)
  const f = getComputedStyle(el).fontFamily
  el.remove()
  return f
}

/** Re-run `cb` whenever the document theme changes (apply.ts rewrites style + data-theme). */
export function watchTheme(cb: () => void): () => void {
  let t = 0
  const mo = new MutationObserver(() => { clearTimeout(t); t = window.setTimeout(cb, 30) })
  mo.observe(document.documentElement, { attributes: true, attributeFilter: ['style', 'data-theme'] })
  return () => mo.disconnect()
}
