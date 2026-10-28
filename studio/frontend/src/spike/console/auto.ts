// SPIKE #257 - never merge. The automated check sequence; one results JSON per run.
import { resolveTheme } from '../../theme/schema'
import { applyThemeToDocument } from '../../theme/apply'
import { BUILTIN_THEMES } from '../../theme/builtin'
import { errors, post, webview } from './evidence'
import { readTheme, resolveColor } from './theme'
import type { Session } from './session'
import { esc } from './feed'

type Status = 'pass' | 'partial' | 'fail' | 'info' | 'human'
interface Check { id: number; name: string; status: Status; data: unknown }

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const registry = new Map(BUILTIN_THEMES.map((t) => [t.id, t]))
const useThemeId = (id: string) => applyThemeToDocument(resolveTheme(registry.get(id)!, registry))

const progress = (n: string) => { void post('progress.txt', `${new Date().toISOString()} ${n}`) }
async function flush(s: Session): Promise<void> { await new Promise<void>((r) => s.adapter.write('\x1b[0m', r)) }
async function cmd(s: Session, c: string): Promise<void> { progress('cmd ' + c + ' run'); await s.feed.run(c); progress('cmd ' + c + ' flush'); await flush(s); progress('cmd ' + c + ' done') }

function pixel(png: string, x: number, y: number): Promise<string> {
  return new Promise((res) => {
    const img = new Image()
    img.onload = () => {
      const c = document.createElement('canvas'); c.width = img.width; c.height = img.height
      const g = c.getContext('2d', { willReadFrequently: true })!; g.drawImage(img, 0, 0)
      const d = g.getImageData(Math.round(x), Math.round(y), 1, 1).data
      res('#' + [d[0], d[1], d[2]].map((v) => v.toString(16).padStart(2, '0')).join(''))
    }
    img.onerror = () => res('decode-failed')
    img.src = png
  })
}

/** Background of terminal cell (0,0) painted ANSI red, and the default background: via canvas pixels or DOM styles. */
async function sampleColors(s: Session, el: HTMLElement): Promise<{ via: string; bg: string; red: string }> {
  s.adapter.write('\x1b[2J\x1b[H\x1b[41m   \x1b[0m'); await flush(s); await sleep(120)
  const png = s.adapter.canvasPng()
  if (png) {
    const m = s.adapter.metrics(); const d = s.adapter.dump(); const cw = m.screenW / d.cols; const ch = m.screenH / d.rows
    const img = new Image(); img.src = png; await img.decode()
    const k = img.width / m.screenW
    return { via: 'canvas-pixel', bg: await pixel(png, (m.screenW - 2) * k, (m.screenH - 2) * k), red: await pixel(png, cw * 1.5 * k, ch * 0.5 * k) }
  }
  const red = el.querySelector('.xterm-bg-1') as HTMLElement | null
  const rowBg = el.querySelector('.xterm-scrollable-element') as HTMLElement | null
  return { via: 'dom-style', bg: rowBg ? resolveColor(getComputedStyle(rowBg).backgroundColor) : 'none', red: red ? resolveColor(getComputedStyle(red).backgroundColor) : 'none' }
}

export async function runAuto(s: Session, el: HTMLElement, tag: string): Promise<{ checks: Check[] }> {
  const checks: Check[] = []
  const add = (id: number, name: string, status: Status, data: unknown) => { checks.push({ id, name, status, data }) }
  const wv = webview()

  // 11 init
  add(11, 'WASM / init', 'pass', { initMs: s.initMs, origin: location.origin, webview: wv, ua: navigator.userAgent, dpr: window.devicePixelRatio, notes: s.adapter.notes })
  await post('init.json', checks[0])

  // 1 renders
  progress('1 render')
  useThemeId('schoolhouse-light'); await sleep(200)
  await cmd(s, 'clear'); await cmd(s, 'table'); await cmd(s, 'colors'); await cmd(s, 'unicode'); await cmd(s, 'size'); await sleep(250)
  const d1 = s.adapter.dump()
  const txt = d1.lines.join('\n')
  const ok1 = txt.includes('┌') && txt.includes('12 rows (3 shown)') && txt.includes('你好')
  add(1, 'Renders (text-level; glyph alignment needs the png)', ok1 ? 'pass' : 'fail', { cols: d1.cols, rows: d1.rows })
  await post('render-dump.txt', txt)
  const png1 = s.adapter.canvasPng(); if (png1) await post('render-canvas.png', png1)

  // 2 theme from roles
  progress('2 theme roles')
  const themes: Record<string, unknown> = {}
  for (const t of BUILTIN_THEMES) { useThemeId(t.id); await sleep(60); themes[t.id] = readTheme() }
  const bgs = new Set(Object.values(themes).map((x) => (x as { background: string }).background))
  add(2, 'Theme from --vibe-* roles', bgs.size >= 3 ? 'pass' : 'fail', { distinctBackgrounds: bgs.size, themes })
  await post('theme.json', themes)
  useThemeId('schoolhouse-light'); await sleep(250)

  // 3 theme switch live. Start from a clean widget (recreate = the known-good path), then switch light -> dark -> light
  // through the same MutationObserver path the app would use, sampling real pixels / DOM styles after each.
  progress('3 theme switch')
  useThemeId('schoolhouse-light'); await sleep(400)
  await s.adapter.setTheme(readTheme(), 'recreate'); s.adapter.fit(); await sleep(200)
  const matches = (smp: { bg: string; red: string }, t: { background: string; red: string }) => smp.bg.toLowerCase() === t.background.toLowerCase() && smp.red.toLowerCase() === t.red.toLowerCase()
  const expLight = readTheme()
  const before = await sampleColors(s, el)
  const pngBefore = s.adapter.canvasPng(); if (pngBefore) await post('theme-before.png', pngBefore)
  useThemeId('schoolhouse-dark'); await sleep(600)
  const expDark = readTheme()
  const afterDark = await sampleColors(s, el)
  const pngAfter = s.adapter.canvasPng(); if (pngAfter) await post('theme-after.png', pngAfter)
  useThemeId('schoolhouse-light'); await sleep(600)
  const afterLight = await sampleColors(s, el)
  const inPlaceOk = matches(before, expLight) && matches(afterDark, expDark) && matches(afterLight, expLight)
  let recreatedOk: boolean | null = null; let recreatedResult = ''
  if (!inPlaceOk) {
    useThemeId('schoolhouse-dark'); await sleep(300)
    recreatedResult = await s.adapter.setTheme(readTheme(), 'recreate'); s.adapter.fit(); await sleep(200)
    recreatedOk = matches(await sampleColors(s, el), expDark)
    const pr = s.adapter.canvasPng(); if (pr) await post('theme-recreated.png', pr)
    useThemeId('schoolhouse-light'); await sleep(300)
    await s.adapter.setTheme(readTheme(), 'recreate'); s.adapter.fit()
  }
  add(3, 'Theme switch live', before.red === 'none' ? 'info' : inPlaceOk ? 'pass' : recreatedOk ? 'partial' : 'fail', { watcherEvents: s.themeEvents.slice(-6), before, afterDark, afterLight, expectLight: { bg: expLight.background, red: expLight.red }, expectDark: { bg: expDark.background, red: expDark.red }, inPlaceOk, recreatedOk, recreatedResult, note: inPlaceOk ? 'applied in place (light->dark->light)' : 'in place did NOT recolour reliably; recreate (content lost) ' + (recreatedOk ? 'works' : 'also failed or not measurable') })
  useThemeId('schoolhouse-light'); await sleep(300)

  // 7 selection
  progress('7 copy')
  await cmd(s, 'clear'); s.adapter.write('copy-me 12345 你好'); await flush(s); await sleep(100)
  s.adapter.select(0, 0, 13); const sel = s.adapter.getSelection()
  add(7, 'Copy: getSelection equals selected text (programmatic; clipboard is a keyboard/human check)', sel === 'copy-me 12345' ? 'pass' : 'fail', { got: sel })

  // 8 bracketed paste
  progress('8 paste')
  const rx0 = s.feed.rx.length
  await cmd(s, 'paste on'); s.adapter.paste('a\nb'); await sleep(50)
  await cmd(s, 'paste off'); s.adapter.paste('c'); await sleep(50)
  const input = s.adapter.inputEl() as HTMLElement
  const dt = new DataTransfer(); dt.setData('text/plain', 'synthetic-paste')
  await cmd(s, 'paste on')
  input.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true })); await sleep(80)
  const rxs = s.feed.rx.slice(rx0).map((r) => esc(r.raw))
  const wrapped = rxs.some((x) => x.startsWith('\\x1b[200~a') && x.endsWith('\\x1b[201~'))
  const plain = rxs.includes('c')
  add(8, 'Bracketed paste (adapter.paste; synthetic DOM paste event is labelled synthetic)', wrapped && plain ? 'pass' : 'fail', { received: rxs, syntheticPasteArrived: rxs.some((x) => x.includes('synthetic-paste')) })
  await cmd(s, 'paste off')

  // 9 resize / fit
  progress('9 resize')
  const resizes: unknown[] = []
  el.style.flex = 'none'
  for (const [w, h] of [[900, 300], [620, 430]]) {
    el.style.width = w + 'px'; el.style.height = h + 'px'; await sleep(450)
    const f = s.adapter.fit(); await sleep(200); await cmd(s, 'size')
    const m = s.adapter.metrics(); const cellW = m.screenW / f.cols; const cellH = m.screenH / f.rows
    const fits = m.screenW <= m.containerW + 1 && m.screenH <= m.containerH + 1
    const gapW = m.containerW - m.screenW; const gapH = m.containerH - m.screenH
    // a gap under one cell is fine; ghostty's FitAddon also reserves 15 px for its scrollbar
    const filled = gapW < cellW + 16 && gapH < cellH + 1
    resizes.push({ gapW, gapH, w, h, fit: f, onResize: { ...s.feed.lastResize }, metrics: m, cellW, cellH, fits, filled })
    const pr = s.adapter.canvasPng(); if (pr) await post(`resize-${w}x${h}.png`, pr)
  }
  const ok9 = (resizes as { fit: { cols: number; rows: number }; onResize: { cols: number; rows: number }; fits: boolean; filled: boolean }[]).every((r) => r.fit.cols === r.onResize.cols && r.fit.rows === r.onResize.rows && r.fits && r.filled)
  add(9, 'Resize / fit', ok9 ? 'pass' : 'fail', resizes)
  await post('resize.json', resizes)
  el.style.width = ''; el.style.height = ''; el.style.flex = ''; await sleep(400); s.adapter.fit()

  // 10 10k lines
  progress('10 lines')
  await cmd(s, 'clear')
  const run = s.feed.log.length
  await s.feed.run('lines 10000'); await flush(s)
  const lr = s.feed.log[run].result as { drainMs: number; combined: { p95: number; over50: number; frames: number }; lastLinePresent: boolean; scrollbackAfter: number; lengthAfter: number }
  const ok10 = lr.drainMs <= 2000 && lr.combined.p95 <= 33.4 && lr.combined.over50 / Math.max(1, lr.combined.frames) <= 0.05 && lr.lastLinePresent && lr.lengthAfter >= 10000
  add(10, '10k lines (drain, rAF frame times during drain and a 2 s programmatic scroll)', ok10 ? 'pass' : 'fail', lr)
  await post('lines.json', lr)
  const png10 = s.adapter.canvasPng(); if (png10) await post('lines.png', png10)
  await post('lines-tail.txt', s.adapter.dump(30).lines.join('\n'))

  // human-only rows
  add(4, 'Dead keys', 'human', 'not automatable here')
  add(5, 'IME composition', 'human', 'not automatable here')
  add(6, 'Shortcuts', 'human', 'XTEST run (X11) / owner on macOS; rx log attached to this run')

  // 14 errors
  progress('14 errors')
  add(14, 'Errors', errors.length === 0 ? 'pass' : 'fail', errors)
  await post('errors.txt', errors.join('\n'))
  await post('rx.json', s.feed.rx.map((r) => ({ t: r.t, raw: esc(r.raw) })))
  await post('feed-log.json', s.feed.log)
  const out = { tag, webview: wv, origin: location.origin, widget: s.adapter.kind, notes: s.adapter.notes, dpr: window.devicePixelRatio, checks: checks.sort((a, b) => a.id - b.id).map((c) => ({ id: c.id, name: c.name, status: c.status })), detail: checks }
  await post('results.json', out)
  return { checks }
}
