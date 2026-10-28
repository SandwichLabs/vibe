<script setup lang="ts">
// SPIKE #257 - throwaway harness, never merge. See spike/console/README.md.
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { BUILTIN_THEMES } from '../../theme/builtin'
import { resolveTheme } from '../../theme/schema'
import { applyThemeToDocument } from '../../theme/apply'
import { installErrorCapture, post, setTag, webview } from './evidence'
import { openSession, readParams, type Session } from './session'
import { runAuto } from './auto'
import { esc } from './feed'

const route = useRoute()
const p = readParams(route.query as Record<string, unknown>)
const host = ref<HTMLElement | null>(null)
const status = ref('starting')
const done = ref(false)
const rxText = ref('')
const seed = ref('copy this text then press Ctrl+C in this box')
const seedEl = ref<HTMLTextAreaElement | null>(null)
const commands = ['help', 'table', 'colors', 'unicode', 'wide', 'lines 10000', 'paste on', 'paste off', 'size', 'clear']
let session: Session | null = null
let snaps = 0

onMounted(async () => {
  installErrorCapture()
  const tag = `${p.widget}${p.widget === 'ghostty' ? '-' + p.wasm + (route.query.sb ? '-sb' + p.sb : '') : ''}${p.pdb ? '-pdb' : ''}-${webview()}-${location.protocol === 'wails:' ? (location.host === 'wails' ? 'prod' : 'dev') : 'http'}${route.query.lbl ? '-' + String(route.query.lbl) : ''}`
  setTag(tag)
  // rAF heartbeat: ghostty-web fires write callbacks from requestAnimationFrame, so record whether frames run at all.
  let raf = 0; const t0 = performance.now(); const tick = () => { raf++; if (performance.now() - t0 < 1000) requestAnimationFrame(tick) }
  requestAnimationFrame(tick)
  setTimeout(() => { void post('raf.json', { framesIn1s: raf, visibility: document.visibilityState, hasFocus: document.hasFocus(), innerWidth: window.innerWidth, innerHeight: window.innerHeight, dpr: window.devicePixelRatio }) }, 1500)
  try {
    session = await openSession(host.value!, p, (c) => { status.value = 'ran: ' + c })
    session.feed.log.length = 0
    ;(window as unknown as { spikeFeed: unknown }).spikeFeed = session
    let posted = 0; let ptr = { x: -1, y: -1 }; let ptrPosted = ''
    window.addEventListener('mousemove', (e) => { ptr = { x: e.clientX, y: e.clientY } })
    setInterval(() => {
      const s = session!
      rxText.value = s.feed.rx.slice(-12).map((r) => esc(r.raw)).join('  ')
      // keys run (no auto): mirror every received byte sequence plus the screen to evidence
      if (!p.auto && (s.feed.rx.length !== posted || JSON.stringify([ptr, seed.value, s.adapter.dump(1).viewportY]) !== ptrPosted)) { posted = s.feed.rx.length; ptrPosted = JSON.stringify([ptr, seed.value, s.adapter.dump(1).viewportY]); const rect = (e: Element | null) => { const r = e?.getBoundingClientRect(); return r ? { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) } : null }
        void post('keys-rx.json', { cell: { w: s.adapter.metrics().screenW / s.adapter.dump(1).cols, h: s.adapter.metrics().screenH / s.adapter.dump(1).rows }, ptr, vy: s.adapter.dump(1).viewportY, sbLines: s.adapter.dump(1).scrollback, seed: seed.value, rects: { term: rect(host.value), seed: rect(seedEl.value) }, innerW: window.innerWidth, innerH: window.innerHeight, rx: s.feed.rx.map((r) => esc(r.raw)), selection: s.adapter.getSelection(), log: s.feed.log.filter((l) => l.cmd === 'copykey'), screen: s.adapter.dump(24).lines }) }
    }, 400)
    status.value = `open (${p.widget}) init ${session.initMs} ms`
    if (p.auto) { await runAuto(session, host.value!, tag); status.value = 'auto done' }
  } catch (e) {
    status.value = 'FAILED: ' + String(e)
    await post('open-failed.txt', String((e as Error)?.stack ?? e))
  }
  done.value = true
})
onBeforeUnmount(() => session?.dispose())

function setTheme(id: string) {
  const reg = new Map(BUILTIN_THEMES.map((t) => [t.id, t]))
  applyThemeToDocument(resolveTheme(reg.get(id)!, reg))
}
async function snapshot() {
  if (!session) return
  const n = ++snaps
  const png = session.adapter.canvasPng(); if (png) await post(`snap${n}.png`, png)
  await post(`snap${n}.json`, { dump: session.adapter.dump(), selection: session.adapter.getSelection(), rx: session.feed.rx.map((r) => esc(r.raw)), themeEvents: session.themeEvents })
  status.value = `snapshot ${n} saved`
}
</script>

<template>
  <div class="spike" data-testid="spike-console">
    <div class="bar">
      <strong>SPIKE #257 (throwaway)</strong>
      <span data-testid="spike-widget">{{ p.widget }}{{ p.widget === 'ghostty' ? ' / wasm=' + p.wasm : '' }}</span>
      <span data-testid="spike-status">{{ status }}</span>
      <button v-for="c in commands" :key="c" class="btn" type="button" @click="session?.feed.run(c)">{{ c }}</button>
      <select aria-label="theme" @change="setTheme(($event.target as HTMLSelectElement).value)">
        <option v-for="t in BUILTIN_THEMES" :key="t.id" :value="t.id">{{ t.id }}</option>
      </select>
      <button class="btn" type="button" data-testid="spike-snapshot" @click="snapshot">snapshot</button>
      <textarea ref="seedEl" v-model="seed" rows="2" class="seed" data-testid="spike-seed" aria-label="seed text for clipboard checks" />
    </div>
    <div ref="host" class="term" data-testid="spike-term" />
    <div class="rx" data-testid="spike-rx">rx: {{ rxText }}</div>
    <div v-if="done" data-testid="spike-done" />
  </div>
</template>

<style scoped>
.spike { display: flex; flex-direction: column; gap: 6px; padding: 8px; height: 100%; box-sizing: border-box; background: var(--vibe-bg); color: var(--vibe-fg); }
.bar { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; font-size: 12px; }
.seed { min-width: 280px; min-height: 2.6em; background: var(--vibe-surface); color: var(--vibe-fg); border: 1px solid var(--vibe-border); }
.term { flex: 1; min-height: 200px; overflow: hidden; background: var(--vibe-editor-bg); border: 2px solid var(--vibe-border); }
.rx { font-family: var(--vibe-font-mono); font-size: 11px; color: var(--vibe-fg-muted); white-space: pre-wrap; min-height: 2.4em; }
</style>
