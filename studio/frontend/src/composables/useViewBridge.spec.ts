// useViewBridge (#160): the host side of a custom view's Worker, tested with a
// fake Worker and a fake channel. The Worker is untrusted, so these are the
// refusals: SQL-shaped, oversized and unknown messages, the 61st call in a
// minute, the in-flight limit, fixed error texts, the 2 MB SVG cap, revoked blob
// URLs, and the watchdog.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { effectScope } from 'vue'
import {
  useViewBridge, STOPPED_TEXT, BEAT_TIMEOUT_MS, STARTUP_TIMEOUT_MS, DRAW_TIMEOUT_MS, LOADING_TIMEOUT_MS,
  type ViewDeps, type PortLike,
} from './useViewBridge'
import { RUN_ERROR_TEXT, MAX_SVG_BYTES } from '../lib/viewBridge'

vi.mock('../lib/bindings', () => ({ App: {} }))

class FakePort implements PortLike {
  onmessage: ((e: { data: unknown }) => void) | null = null
  sent: any[] = []
  closed = false
  postMessage(m: unknown) { this.sent.push(m) }
  close() { this.closed = true }
}

function rig(over: Partial<ViewDeps> = {}) {
  const port = new FakePort()
  const worker = { postMessage: vi.fn(), terminate: vi.fn() }
  const urls: string[] = []
  const deps: Partial<ViewDeps> = {
    openView: vi.fn(async () => ({ token: 't'.repeat(64), hash: 'h' })),
    runQuery: vi.fn(async () => ({ columns: ['a'], types: ['INTEGER'], rows: [[1]], truncated: false })),
    validatePage: vi.fn(async (_n: string, p: string) => p),
    makeWorker: vi.fn((url: string) => { urls.push(url); return worker }),
    makeChannel: () => ({ port1: port, port2: { fake: 'port2' } }),
    origin: () => 'http://localhost:34115',
    ...over,
  }
  const scope = effectScope()
  const b = scope.run(() => useViewBridge(() => 'chart', () => ({ type: 'light', 'chart.1': '#123456' }), deps))!
  const fromWorker = (m: unknown) => port.onmessage?.({ data: m })
  const lastReply = () => port.sent[port.sent.length - 1]
  return { b, port, worker, deps, urls, scope, fromWorker, lastReply }
}

async function ready(r: ReturnType<typeof rig>) {
  await r.b.start()
  r.fromWorker({ t: 'hb' })
  r.fromWorker({ t: 'ready' })
}

let blobs: Blob[]
let revoked: string[]
beforeEach(() => {
  vi.useFakeTimers()
  blobs = []
  revoked = []
  URL.createObjectURL = vi.fn((b: Blob) => { blobs.push(b); return `blob:slaw/${blobs.length}` }) as any
  URL.revokeObjectURL = vi.fn((u: string) => { revoked.push(u) }) as any
})
afterEach(() => { vi.useRealTimers() })

describe('starting', () => {
  it('opens the view for this artifact, loads the worker by token and hands over the port with the theme', async () => {
    const r = rig()
    await r.b.start()
    expect(r.deps.openView).toHaveBeenCalledWith('chart', 'http://localhost:34115')
    expect(r.urls).toEqual([`/slaw-view/${'t'.repeat(64)}/worker.js`])
    expect(r.worker.postMessage).toHaveBeenCalledWith({ t: 'init', theme: { type: 'light', 'chart.1': '#123456' } }, [{ fake: 'port2' }])
    expect(r.b.phase.value).toBe('starting')
    r.fromWorker({ t: 'ready' })
    expect(r.b.phase.value).toBe('ready')
  })

  it('a view that is not allowed never starts a worker', async () => {
    const r = rig({ openView: vi.fn(async () => { throw new Error('this view is not allowed to run') }) })
    await r.b.start()
    expect(r.b.phase.value).toBe('failed')
    expect(r.b.failure.value).toContain('not allowed')
    expect(r.deps.makeWorker).not.toHaveBeenCalled()
  })

  it('a view that fails to load stops with its own (capped) message', async () => {
    const r = rig()
    await r.b.start()
    r.fromWorker({ t: 'loadFailed', text: 'ReferenceError: x is not defined' })
    expect(r.b.phase.value).toBe('failed')
    expect(r.b.failure.value).toBe('ReferenceError: x is not defined')
    expect(r.worker.terminate).toHaveBeenCalled()
  })
})

describe('run', () => {
  it('runs a named query for the grant (the token, never an artifact name from the page) and answers on the port only', async () => {
    const r = rig()
    await ready(r)
    r.fromWorker({ t: 'run', id: 7, query: 'orders', params: { region: 'EU' } })
    await vi.advanceTimersByTimeAsync(0)
    expect(r.deps.runQuery).toHaveBeenCalledWith('t'.repeat(64), 'orders', { region: 'EU' })
    expect(r.lastReply()).toEqual({ t: 'result', id: 7, ok: true, columns: ['a'], rows: [[1]], truncated: false })
  })

  it('refuses a SQL-shaped message with a fixed text and runs nothing', async () => {
    const r = rig()
    await ready(r)
    r.fromWorker({ t: 'run', id: 1, query: 'SELECT * FROM secrets', params: {} })
    expect(r.lastReply()).toEqual({ t: 'result', id: 1, ok: false, error: RUN_ERROR_TEXT.bad })
    r.fromWorker({ t: 'run', id: 2, query: 'orders', params: {}, sql: 'DROP TABLE x' })
    expect(r.lastReply()).toEqual({ t: 'result', id: 2, ok: false, error: RUN_ERROR_TEXT.bad })
    r.fromWorker({ t: 'run', id: 3, query: 'orders', params: {}, name: 'other-artifact' })
    expect(r.lastReply().ok).toBe(false)
    expect(r.deps.runQuery).not.toHaveBeenCalled()
  })

  it('refuses an oversized message and ignores an unknown type', async () => {
    const r = rig()
    await ready(r)
    r.fromWorker({ t: 'run', id: 1, query: 'orders', params: { a: 'x'.repeat(5000) } })
    expect(r.lastReply()).toEqual({ t: 'result', id: 1, ok: false, error: RUN_ERROR_TEXT.bad })
    const sent = r.port.sent.length
    r.fromWorker({ t: 'eval', code: 'alert(1)' })
    r.fromWorker('run')
    expect(r.port.sent.length).toBe(sent)
    expect(r.deps.runQuery).not.toHaveBeenCalled()
    expect(r.b.phase.value).toBe('ready')
  })

  it('refuses the 61st call in a minute, then allows it once the minute has passed', async () => {
    const r = rig()
    await ready(r)
    for (let i = 1; i <= 60; i++) {
      r.fromWorker({ t: 'run', id: i, query: 'orders', params: {} })
      await vi.advanceTimersByTimeAsync(0)
    }
    expect(r.deps.runQuery).toHaveBeenCalledTimes(60)
    r.fromWorker({ t: 'run', id: 61, query: 'orders', params: {} })
    expect(r.lastReply()).toEqual({ t: 'result', id: 61, ok: false, error: RUN_ERROR_TEXT.rate })
    expect(r.deps.runQuery).toHaveBeenCalledTimes(60)
    // keep the worker alive past the minute
    for (let i = 0; i < 61; i++) { r.fromWorker({ t: 'hb' }); await vi.advanceTimersByTimeAsync(1000) }
    r.fromWorker({ t: 'run', id: 62, query: 'orders', params: {} })
    await vi.advanceTimersByTimeAsync(0)
    expect(r.lastReply().ok).toBe(true)
  })

  it('allows 4 in flight and refuses the 5th until one returns', async () => {
    const pending: Array<() => void> = []
    const r = rig({ runQuery: vi.fn(() => new Promise<any>((res) => { pending.push(() => res({ columns: [], types: [], rows: [] })) })) })
    await ready(r)
    for (let i = 1; i <= 5; i++) r.fromWorker({ t: 'run', id: i, query: 'orders', params: {} })
    expect(r.deps.runQuery).toHaveBeenCalledTimes(4)
    expect(r.lastReply()).toEqual({ t: 'result', id: 5, ok: false, error: RUN_ERROR_TEXT.busy })
    pending[0]()
    await vi.advanceTimersByTimeAsync(0)
    r.fromWorker({ t: 'run', id: 6, query: 'orders', params: {} })
    expect(r.deps.runQuery).toHaveBeenCalledTimes(5)
  })

  it('answers a failed query with a fixed text, never the error it got', async () => {
    const r = rig({ runQuery: vi.fn(async () => { throw new Error('read_text(/home/zac/.ssh/id_rsa) refused by policy: password=hunter2') }) })
    await ready(r)
    r.fromWorker({ t: 'run', id: 1, query: 'orders', params: {} })
    await vi.advanceTimersByTimeAsync(0)
    expect(r.lastReply()).toEqual({ t: 'result', id: 1, ok: false, error: RUN_ERROR_TEXT.failed })
    expect(JSON.stringify(r.port.sent)).not.toMatch(/zac|hunter2|policy|ssh/)
  })
})

describe('drawing', () => {
  it('sends a draw with the theme and size, and queues one made before the view is ready', async () => {
    const r = rig()
    await r.b.start()
    r.b.draw({ region: 'EU' }, 600, 400)
    r.b.draw({ region: 'US' }, 700, 400) // replaces the queued one
    expect(r.port.sent).toEqual([])
    r.fromWorker({ t: 'hb' })
    r.fromWorker({ t: 'ready' })
    expect(r.port.sent).toEqual([{ t: 'draw', id: 1, params: { region: 'US' }, theme: { type: 'light', 'chart.1': '#123456' }, width: 700, height: 400 }])
  })

  it('shows an SVG only as an image/svg+xml blob URL and revokes the old one on replace and on stop', async () => {
    const r = rig()
    await ready(r)
    r.b.draw({}, 600, 400)
    r.fromWorker({ t: 'drawn', id: 1, svg: '<svg xmlns="http://www.w3.org/2000/svg"/>' })
    expect(r.b.svgUrl.value).toBe('blob:slaw/1')
    expect(blobs[0].type).toBe('image/svg+xml')
    r.b.draw({}, 600, 400)
    r.fromWorker({ t: 'drawn', id: 2, svg: '<svg xmlns="http://www.w3.org/2000/svg"/>' })
    expect(r.b.svgUrl.value).toBe('blob:slaw/2')
    expect(revoked).toEqual(['blob:slaw/1'])
    r.scope.stop()
    expect(revoked).toEqual(['blob:slaw/1', 'blob:slaw/2'])
    expect(r.worker.terminate).toHaveBeenCalled()
    expect(r.port.closed).toBe(true)
  })

  it('refuses an SVG over 2 MB and shows nothing', async () => {
    const r = rig()
    await ready(r)
    r.b.draw({}, 600, 400)
    r.fromWorker({ t: 'drawn', id: 1, svg: '<svg>' + 'x'.repeat(MAX_SVG_BYTES) + '</svg>' })
    expect(r.b.svgUrl.value).toBe('')
    expect(r.b.drawError.value).toContain('2 MB')
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })

  it('refuses an oversized SVG by its length, before any Blob is built', async () => {
    const r = rig()
    await ready(r)
    const RealBlob = globalThis.Blob
    const made = vi.fn()
    globalThis.Blob = class extends RealBlob { constructor(...a: any[]) { made(); super(...a) } } as any
    try {
      r.b.draw({}, 600, 400)
      r.fromWorker({ t: 'drawn', id: 1, svg: 'x'.repeat(MAX_SVG_BYTES + 1) })
      expect(made).not.toHaveBeenCalled()
      expect(r.b.drawError.value).toContain('2 MB')
    } finally { globalThis.Blob = RealBlob }
  })

  it('takes exactly one drawn per draw id: a repeat is ignored, whatever it carries', async () => {
    const r = rig()
    await ready(r)
    r.b.draw({}, 600, 400)
    r.fromWorker({ t: 'drawn', id: 1, svg: '<svg id="first"/>' })
    r.fromWorker({ t: 'drawn', id: 1, svg: '<svg id="second"/>' })
    r.fromWorker({ t: 'drawn', id: 1, a2ui: '{"title":"x","components":[]}' })
    r.fromWorker({ t: 'drawFailed', id: 1, text: 'late' })
    expect(URL.createObjectURL).toHaveBeenCalledTimes(1)
    expect(r.deps.validatePage).not.toHaveBeenCalled()
    expect(r.b.drawError.value).toBe('')
  })

  it('refuses an oversized A2UI page by its length before calling Go', async () => {
    const r = rig()
    await ready(r)
    r.b.draw({}, 600, 400)
    r.fromWorker({ t: 'drawn', id: 1, a2ui: 'x'.repeat(1024 * 1024 + 1) })
    expect(r.deps.validatePage).not.toHaveBeenCalled()
    expect(r.b.drawError.value).toContain('1 MB')
  })

  it('stops a worker that sends more bytes in a second than the budget', async () => {
    const q = rig()
    await ready(q)
    for (let i = 1; i <= 9; i++) {
      q.b.draw({}, 600, 400)
      q.fromWorker({ t: 'drawn', id: i, svg: 'x'.repeat(2 * 1024 * 1024) })
    }
    expect(q.b.phase.value).toBe('stopped')
  })

  it('latest wins: the result of an older draw is dropped', async () => {
    const r = rig()
    await ready(r)
    r.b.draw({ a: 1 }, 600, 400)
    r.b.draw({ a: 2 }, 600, 400)
    r.fromWorker({ t: 'drawn', id: 1, svg: '<svg id="old"/>' })
    expect(r.b.svgUrl.value).toBe('')
    r.fromWorker({ t: 'drawn', id: 2, svg: '<svg id="new"/>' })
    expect(r.b.svgUrl.value).not.toBe('')
  })

  it('sends an A2UI page to Go for validation and renders only what comes back', async () => {
    const validated = '{"title":"T","components":[{"id":"a","type":"text","text":"checked"}]}'
    const r = rig({ validatePage: vi.fn(async () => validated) })
    await ready(r)
    r.b.draw({}, 600, 400)
    r.fromWorker({ t: 'drawn', id: 1, a2ui: JSON.stringify({ title: 'T', components: [{ id: 'a', type: 'text', text: 'raw' }] }) })
    await vi.advanceTimersByTimeAsync(0)
    expect(r.deps.validatePage).toHaveBeenCalledWith('chart', '{"title":"T","components":[{"id":"a","type":"text","text":"raw"}]}')
    expect(r.b.page.value).toEqual(JSON.parse(validated))
  })

  it('shows the validation error for an invalid page', async () => {
    const r = rig({ validatePage: vi.fn(async () => { throw new Error('components[0].type: "iframe" is not a component') }) })
    await ready(r)
    r.b.draw({}, 600, 400)
    r.fromWorker({ t: 'drawn', id: 1, a2ui: JSON.stringify({ title: 'T', components: [{ id: 'a', type: 'iframe' }] }) })
    await vi.advanceTimersByTimeAsync(0)
    expect(r.b.page.value).toBeNull()
    expect(r.b.drawError.value).toContain('iframe')
  })

  it('shows what a failed draw says', async () => {
    const r = rig()
    await ready(r)
    r.b.draw({}, 600, 400)
    r.fromWorker({ t: 'drawFailed', id: 1, text: 'TypeError: rows is undefined' })
    expect(r.b.drawError.value).toBe('TypeError: rows is undefined')
    expect(r.b.phase.value).toBe('ready')
  })
})

describe('watchdog', () => {
  it('terminates a worker that goes silent for a second and says so', async () => {
    const r = rig()
    await ready(r)
    for (let i = 0; i < 8; i++) { await vi.advanceTimersByTimeAsync(250); r.fromWorker({ t: 'hb' }) }
    expect(r.b.phase.value).toBe('ready')
    await vi.advanceTimersByTimeAsync(BEAT_TIMEOUT_MS + 200)
    expect(r.b.phase.value).toBe('stopped')
    expect(r.b.failure.value).toBe(STOPPED_TEXT)
    expect(STOPPED_TEXT).toBe('This view stopped responding')
    expect(r.worker.terminate).toHaveBeenCalledTimes(1)
  })

  it('gives a worker that never speaks the startup time, then terminates it', async () => {
    const r = rig()
    await r.b.start()
    await vi.advanceTimersByTimeAsync(STARTUP_TIMEOUT_MS - 300)
    expect(r.b.phase.value).toBe('starting')
    await vi.advanceTimersByTimeAsync(600)
    expect(r.b.phase.value).toBe('stopped')
    expect(r.worker.terminate).toHaveBeenCalled()
  })

  it('terminates a draw that takes over 30 seconds even while it beats', async () => {
    const r = rig()
    await ready(r)
    r.b.draw({}, 600, 400)
    for (let t = 0; t < DRAW_TIMEOUT_MS - 1000; t += 250) { await vi.advanceTimersByTimeAsync(250); r.fromWorker({ t: 'hb' }) }
    expect(r.b.phase.value).toBe('ready')
    for (let t = 0; t < 2000; t += 250) { await vi.advanceTimersByTimeAsync(250); r.fromWorker({ t: 'hb' }) }
    expect(r.b.phase.value).toBe('stopped')
    expect(r.worker.terminate).toHaveBeenCalled()
  })

  it('allows a longer quiet spell after the worker says it is loading a library, but not forever', async () => {
    const r = rig()
    await ready(r)
    r.fromWorker({ t: 'loading', file: 'vega' })
    await vi.advanceTimersByTimeAsync(LOADING_TIMEOUT_MS - 500)
    expect(r.b.phase.value).toBe('ready')
    await vi.advanceTimersByTimeAsync(800)
    expect(r.b.phase.value).toBe('stopped')
  })

  it('honours a loading announcement once per library file, and only for known files', async () => {
    const r = rig()
    await ready(r)
    r.fromWorker({ t: 'loading', file: 'vega' })
    r.fromWorker({ t: 'hb' })
    await vi.advanceTimersByTimeAsync(200)
    r.fromWorker({ t: 'loading', file: 'vega' }) // a repeat buys nothing
    r.fromWorker({ t: 'loading', file: '../../etc/passwd' }) // not a known file
    r.fromWorker({ t: 'loading' })
    await vi.advanceTimersByTimeAsync(BEAT_TIMEOUT_MS + 300)
    expect(r.b.phase.value).toBe('stopped')
  })

  it('stops a worker that is not ready 15 seconds after it started, however it keeps beating', async () => {
    const r = rig()
    await r.b.start()
    for (let t = 0; t < 14_000; t += 250) { await vi.advanceTimersByTimeAsync(250); r.fromWorker({ t: 'hb' }) }
    expect(r.b.phase.value).toBe('starting')
    for (let t = 0; t < 2000; t += 250) { await vi.advanceTimersByTimeAsync(250); r.fromWorker({ t: 'hb' }) }
    expect(r.b.phase.value).toBe('stopped')
  })

  it('stops a worker that floods the host with messages', async () => {
    const r = rig()
    await ready(r)
    for (let i = 0; i < 120; i++) r.fromWorker({ t: 'run', id: 1 + (i % 5), query: 'SELECT', params: {} })
    expect(r.b.phase.value).toBe('stopped')
  })

  it('ignores everything from a worker that was already stopped', async () => {
    const r = rig()
    await ready(r)
    r.scope.stop()
    r.fromWorker({ t: 'run', id: 1, query: 'orders', params: {} })
    expect(r.deps.runQuery).not.toHaveBeenCalled()
  })

  it('Reload starts a fresh worker with a fresh token', async () => {
    const tokens = ['a'.repeat(64), 'b'.repeat(64)]
    const r = rig({ openView: vi.fn(async () => ({ token: tokens.shift()!, hash: 'h' })) })
    await ready(r)
    await vi.advanceTimersByTimeAsync(2000)
    expect(r.b.phase.value).toBe('stopped')
    await r.b.start()
    expect(r.b.phase.value).toBe('starting')
    expect(r.urls[1]).toBe(`/slaw-view/${'b'.repeat(64)}/worker.js`)
  })
})
