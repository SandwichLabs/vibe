/**
 * useViewBridge — runs one custom artifact view (view: script, #160) in a
 * dedicated Worker and is its only line to the rest of the app.
 *
 * The Worker comes from /assay-view/<token>/worker.js (a token Go issues only for
 * a view that is allowed right now) and gets a MessagePort in its first message;
 * every later message uses that port, never a broadcast. It can ask for exactly
 * one thing, `run(query, params)`, which goes through the RunArtifactQuery
 * binding with the artifact name pinned HERE (a view cannot name another
 * artifact), at most 4 in flight and 60 a minute. Anything it sends is shape-
 * checked and size-capped (lib/viewBridge.ts); anything sent back is a fixed
 * text. What it draws is either an SVG, shown only as <img src="blob:…"> (an
 * image runs no script and loads nothing), or an A2UI page that Go validates
 * first.
 *
 * A watchdog stops a view that goes quiet: the Worker beats every 250 ms, and
 * 1 s of silence (or a draw over 30 s) terminates it. `stop()` terminates and
 * revokes every blob URL. Latest wins: a newer draw makes an older one's result
 * be dropped.
 */
import { ref, shallowRef, onScopeDispose } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import {
  checkMessage, RateWindow, RUN_ERROR_TEXT, MAX_IN_FLIGHT, MAX_RUNS_PER_MINUTE, MAX_MESSAGES_PER_SECOND,
  MAX_BYTES_PER_SECOND, READY_DEADLINE_MS, ByteWindow, messageBytes,
  MAX_SVG_BYTES, MAX_PAGE_BYTES, type RunErrorCode, type RunParams, type WorkerMessage,
} from '../lib/viewBridge'
import type { A2UIPageT } from '../a2ui/types'

export const BEAT_TIMEOUT_MS = 1000
export const LOADING_TIMEOUT_MS = 5000
export const STARTUP_TIMEOUT_MS = 4000
export const DRAW_TIMEOUT_MS = 30_000
const TICK_MS = 100

interface WorkerLike { postMessage(msg: unknown, transfer?: Transferable[]): void; terminate(): void }
export interface PortLike { onmessage: ((e: { data: unknown }) => void) | null; postMessage(msg: unknown): void; close(): void }
interface ChannelLike { port1: PortLike; port2: unknown }

export interface ViewDeps {
  openView(name: string, origin: string): Promise<ArtifactViewGrant>
  /** Runs a query for the grant `token` names (the artifact is the grant's, not ours). */
  runQuery(token: string, query: string, values: Record<string, unknown>): Promise<A2UIQueryResult>
  validatePage(name: string, page: string): Promise<string>
  makeWorker(url: string): WorkerLike
  makeChannel(): ChannelLike
  origin(): string
}

const realDeps: ViewDeps = {
  openView: (n, o) => App.OpenArtifactView(n, o),
  runQuery: (t, q, v) => App.RunArtifactViewQuery(t, q, v),
  validatePage: (n, p) => App.ValidateArtifactViewPage(n, p),
  makeWorker: (url) => new Worker(url),
  makeChannel: () => new MessageChannel() as unknown as ChannelLike,
  origin: () => `${location.protocol}//${location.host}`,
}

export type ViewPhase = 'idle' | 'starting' | 'ready' | 'stopped' | 'failed'
const SVG_TOO_BIG = 'The view drew an image over the 2 MB limit, so it is not shown.'
export const STOPPED_TEXT = 'This view stopped responding'

export function useViewBridge(name: () => string, theme: () => Record<string, unknown>, overrides: Partial<ViewDeps> = {}) {
  const deps: ViewDeps = { ...realDeps, ...overrides }
  const phase = ref<ViewPhase>('idle')
  /** Why the view is not running (stopped, failed to open or to load). */
  const failure = ref('')
  /** The view's last draw failed, or drew something that cannot be shown. */
  const drawError = ref('')
  const svgUrl = ref('')
  const page = shallowRef<A2UIPageT | null>(null)
  const drawing = ref(false)

  let gen = 0
  let worker: WorkerLike | null = null
  let port: PortLike | null = null
  let timer: ReturnType<typeof setInterval> | undefined
  let lastMsg = 0
  let allowance = STARTUP_TIMEOUT_MS
  let drawSince = 0
  let drawId = 0
  let pageSeq = 0
  let inFlight = 0
  let queued: { params: RunParams; width: number; height: number } | null = null
  let runs = new RateWindow(MAX_RUNS_PER_MINUTE, 60_000)
  let flood = new RateWindow(MAX_MESSAGES_PER_SECOND, 1000)
  let bytes = new ByteWindow(MAX_BYTES_PER_SECOND, 1000)
  let token = ''
  let startedAt = 0
  let settled = true
  const seenLibs = new Set<string>()

  function clearOutput() {
    if (svgUrl.value) URL.revokeObjectURL(svgUrl.value)
    svgUrl.value = ''
    page.value = null
    drawError.value = ''
  }

  function teardown() {
    if (timer !== undefined) clearInterval(timer)
    timer = undefined
    worker?.terminate()
    try { port?.close() } catch { /* already closed */ }
    if (port) port.onmessage = null
    worker = null
    port = null
    inFlight = 0
    drawSince = 0
    drawing.value = false
    queued = null
  }

  function stop() {
    gen++
    teardown()
    clearOutput()
    phase.value = 'idle'
    failure.value = ''
  }

  function halt(text: string, to: ViewPhase) {
    gen++
    teardown()
    clearOutput()
    phase.value = to
    failure.value = text
  }

  function tick(mine: number) {
    if (mine !== gen) return
    const now = Date.now()
    const late = phase.value === 'starting' && now - startedAt > READY_DEADLINE_MS
    if (late || now - lastMsg > allowance || (drawSince && now - drawSince > DRAW_TIMEOUT_MS)) halt(STOPPED_TEXT, 'stopped')
  }

  async function start() {
    stop()
    const mine = gen
    phase.value = 'starting'
    let grant: ArtifactViewGrant
    try {
      grant = await deps.openView(name(), deps.origin())
    } catch (e) {
      if (mine === gen) { phase.value = 'failed'; failure.value = errorText(e) }
      return
    }
    if (mine !== gen) return
    const channel = deps.makeChannel()
    port = channel.port1
    port.onmessage = (e) => onMessage(mine, e.data)
    worker = deps.makeWorker(`/assay-view/${grant.token}/worker.js`)
    worker.postMessage({ t: 'init', theme: theme() }, [channel.port2 as Transferable])
    lastMsg = Date.now()
    allowance = STARTUP_TIMEOUT_MS
    runs = new RateWindow(MAX_RUNS_PER_MINUTE, 60_000)
    flood = new RateWindow(MAX_MESSAGES_PER_SECOND, 1000)
    bytes = new ByteWindow(MAX_BYTES_PER_SECOND, 1000)
    seenLibs.clear()
    token = grant.token
    startedAt = Date.now()
    settled = true
    timer = setInterval(() => tick(mine), TICK_MS)
  }

  function reply(mine: number, msg: Record<string, unknown>) {
    if (mine === gen) port?.postMessage(msg)
  }

  function onMessage(mine: number, data: unknown) {
    if (mine !== gen) return
    const now = Date.now()
    lastMsg = now
    if (!flood.take(now)) return halt(STOPPED_TEXT, 'stopped')
    const v = checkMessage(data)
    if (!v.ok) {
      if (v.code !== 'ignore' && v.id !== undefined) reply(mine, { t: 'result', id: v.id, ok: false, error: RUN_ERROR_TEXT[v.code] })
      return
    }
    if (!bytes.add(now, messageBytes(v.msg))) return halt(STOPPED_TEXT, 'stopped')
    handle(mine, v.msg, now)
  }

  function handle(mine: number, m: WorkerMessage, now: number) {
    switch (m.t) {
      case 'hb': allowance = BEAT_TIMEOUT_MS; break
      case 'loading': if (!seenLibs.has(m.file)) { seenLibs.add(m.file); allowance = LOADING_TIMEOUT_MS } break
      case 'ready': onReady(); break
      case 'loadFailed': halt(m.text, 'failed'); break
      case 'run': onRun(mine, m, now); break
      case 'drawn': onDrawn(mine, m); break
      case 'drawFailed': if (settle(m.id)) drawError.value = m.text; break
    }
  }

  function onReady() {
    phase.value = 'ready'
    if (queued) { const q = queued; queued = null; draw(q.params, q.width, q.height) }
  }

  function onRun(mine: number, m: Extract<WorkerMessage, { t: 'run' }>, now: number) {
    const refuse = (code: RunErrorCode) => reply(mine, { t: 'result', id: m.id, ok: false, error: RUN_ERROR_TEXT[code] })
    if (inFlight >= MAX_IN_FLIGHT) return refuse('busy')
    if (!runs.take(now)) return refuse('rate')
    inFlight++
    // The artifact is the grant's (Go resolves it from the token), not the view's.
    deps.runQuery(token, m.query, m.params).then(
      (res) => reply(mine, { t: 'result', id: m.id, ok: true, columns: res.columns, rows: res.rows, truncated: !!res.truncated }),
      () => refuse('failed'),
    ).finally(() => { if (mine === gen) inFlight-- })
  }

  /** Ask the view to draw. Before it is ready the latest request waits; a newer one replaces it. */
  function draw(params: RunParams, width: number, height: number) {
    if (phase.value !== 'ready' || !port) {
      if (phase.value === 'starting') queued = { params, width, height }
      return
    }
    drawId++
    settled = false
    drawSince = Date.now()
    drawing.value = true
    drawError.value = ''
    port.postMessage({ t: 'draw', id: drawId, params, theme: theme(), width, height })
  }

  /** Takes the one answer a draw may have: false for an older id or one already answered. */
  function settle(id: number): boolean {
    if (id !== drawId || settled) return false
    settled = true
    drawSince = 0
    drawing.value = false
    return true
  }

  function onDrawn(mine: number, m: Extract<WorkerMessage, { t: 'drawn' }>) {
    if (!settle(m.id)) return // an older draw (latest wins) or a repeat: ignored
    if ('svg' in m) showSvg(m.svg)
    else showPage(mine, m.a2ui)
  }

  function showSvg(svg: string) {
    pageSeq++
    // Length first: nothing is built from a string over the cap.
    if (svg.length > MAX_SVG_BYTES) {
      clearOutput()
      drawError.value = SVG_TOO_BIG
      return
    }
    const blob = new Blob([svg], { type: 'image/svg+xml' })
    if (blob.size > MAX_SVG_BYTES) {
      clearOutput()
      drawError.value = SVG_TOO_BIG
      return
    }
    clearOutput()
    svgUrl.value = URL.createObjectURL(blob)
  }

  function showPage(mine: number, json: string) {
    const seq = ++pageSeq
    if (!json || json.length > MAX_PAGE_BYTES) {
      clearOutput()
      drawError.value = 'The view returned a page that is empty or over the 1 MB limit.'
      return
    }
    deps.validatePage(name(), json).then(
      (ok) => {
        if (mine !== gen || seq !== pageSeq) return
        clearOutput()
        page.value = JSON.parse(ok) as A2UIPageT
      },
      (e) => {
        if (mine !== gen || seq !== pageSeq) return
        clearOutput()
        drawError.value = errorText(e)
      },
    )
  }

  onScopeDispose(stop)

  return { phase, failure, drawError, svgUrl, page, drawing, start, stop, draw }
}
