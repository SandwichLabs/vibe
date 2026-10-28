// SPIKE #257 - never merge. Builds the adapter named by the URL, a feed, and the theme watcher.
import type { Kind, TermAdapter } from './adapter'
import { ghosttyAdapter } from './ghosttyAdapter'
import { xtermAdapter } from './xtermAdapter'
import { Feed } from './feed'
import { readMonoFont, readTheme, watchTheme } from './theme'

export interface Params { widget: Kind; wasm: 'inline' | 'url'; auto: boolean; sb: number; pdb: boolean; copyKey: boolean; themeMode: 'inplace' | 'recreate' }

export function readParams(q: Record<string, unknown>): Params {
  const w = String(q.widget ?? 'ghostty')
  return {
    widget: (['ghostty', 'xterm-dom', 'xterm-webgl'].includes(w) ? w : 'ghostty') as Kind,
    wasm: q.wasm === 'url' ? 'url' : 'inline',
    auto: q.auto === '1',
    sb: Number(q.sb) || 20000,
    pdb: q.pdb === '1',
    copyKey: q.copykey === '1',
    themeMode: q.theme === 'recreate' ? 'recreate' : 'inplace',
  }
}

export interface Session { adapter: TermAdapter; feed: Feed; initMs: number; themeEvents: string[]; dispose(): void }

export async function openSession(el: HTMLElement, p: Params, onSubmit?: (c: string) => void): Promise<Session> {
  const adapter = p.widget === 'ghostty' ? ghosttyAdapter(p.wasm, p.sb) : xtermAdapter(p.widget === 'xterm-webgl', p.pdb)
  const { initMs } = await adapter.open(el, readTheme(), readMonoFont())
  const feed = new Feed(adapter, onSubmit)
  adapter.fit()
  feed.start()
  if (p.copyKey) {
    adapter.bindCopyKey(() => {
      const text = adapter.getSelection()
      const log = (m: string) => feed.log.push({ t: Math.round(performance.now()), cmd: 'copykey', result: m })
      const viaExec = () => { const ta = document.createElement('textarea'); ta.value = text; document.body.appendChild(ta); ta.select(); const ok = document.execCommand('copy'); ta.remove(); log('execCommand ' + ok) }
      if (navigator.clipboard?.writeText) navigator.clipboard.writeText(text).then(() => log('navigator.clipboard ok ' + text.length), (e) => { log('navigator.clipboard rejected: ' + String(e)); viaExec() })
      else { log('no navigator.clipboard'); viaExec() }
    })
  }
  const themeEvents: string[] = []
  const stop = watchTheme(() => {
    void adapter.setTheme(readTheme(), p.themeMode).then((r) => themeEvents.push(`${document.documentElement.dataset.theme}: ${r}`))
  })
  return { adapter, feed, initMs, themeEvents, dispose() { stop(); adapter.dispose() } }
}
