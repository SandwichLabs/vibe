// @vitest-environment node
// worker.js (the host-owned bootstrap of a custom view's Worker, #160) run in a
// node `vm` context that stands in for a WorkerGlobalScope: the frozen `assay`,
// the closed library list, the captured port, a repeated `loading`, a setter on
// Object.prototype, and the storage APIs removed before the view loads.
import { describe, it, expect } from 'vitest'
import vm from 'node:vm'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { LIB_FILES } from './viewBridge'

const SRC = readFileSync(fileURLToPath(new URL('../../../viewhost/worker.js', import.meta.url)), 'utf8')

function boot(viewDefines = 'self.draw = function (c) { return { svg: "<svg/>" } }') {
  const imported: string[] = []
  const sent: any[] = []
  const intervals: Array<() => void> = []
  const ctx: any = vm.createContext({
    setInterval: (f: () => void) => intervals.push(f),
    setTimeout,
    importScripts: (u: string) => { imported.push(u); if (u === 'view.js') vm.runInContext(viewDefines, ctx) },
  }, { codeGeneration: { strings: false, wasm: false } })
  ctx.self = ctx
  vm.runInContext(`
    function WorkerNavigator() {}
    Object.defineProperty(WorkerNavigator.prototype, 'storage', { get() { return { getDirectory() {} } }, configurable: true });
    Object.defineProperty(WorkerNavigator.prototype, 'locks', { get() { return {} }, configurable: true });
    self.navigator = new WorkerNavigator();
    function WorkerGlobalScope() {}
    self.indexedDB = {}; self.caches = {}; self.BroadcastChannel = function () {}; self.localStorage = {}; self.sessionStorage = {};
  `, ctx)
  vm.runInContext(SRC, ctx, { filename: 'worker.js' })
  const port: any = { onmessage: null, postMessage: (m: unknown) => sent.push(m) }
  const init = () => ctx.onmessage.call(ctx, { data: { t: 'init', theme: { type: 'light' } }, ports: [port] })
  return { ctx, imported, sent, port, intervals, init, run: (code: string) => vm.runInContext(code, ctx) }
}

describe('worker.js', () => {
  it('takes the port in one handshake, keeps it in its closure, beats, loads the view and says ready', () => {
    const w = boot()
    expect(typeof w.ctx.onmessage).toBe('function')
    w.init()
    expect(w.ctx.onmessage).toBeNull()
    expect(w.sent[0]).toEqual({ t: 'hb' })
    expect(w.sent.at(-1)).toEqual({ t: 'ready' })
    expect(w.imported).toEqual(['view.js'])
    expect(w.intervals.length).toBe(1)
    // The view can find the port nowhere on the global.
    for (const k of Object.getOwnPropertyNames(w.ctx)) expect(w.ctx[k], k).not.toBe(w.port)
    // A second handshake does nothing.
    expect(w.run('typeof onmessage')).toBe('object')
  })

  it('ignores a handshake without a port or of another shape', () => {
    const w = boot()
    w.ctx.onmessage.call(w.ctx, { data: { t: 'init' }, ports: [] })
    expect(w.sent).toEqual([])
    expect(w.imported).toEqual([])
  })

  it('exposes a frozen, non-replaceable assay', () => {
    const w = boot()
    w.init()
    expect(w.run('Object.isFrozen(assay)')).toBe(true)
    expect(w.run(`'use strict'; var r = []; try { assay.run = 1 } catch (e) { r.push('run') } try { assay = {} } catch (e) { r.push('assay') } try { delete self.assay } catch (e) {} r.push(typeof assay); r.join()`)).toBe('run,assay,object')
    expect(w.run('typeof assay.run + typeof assay.lib + typeof assay.theme')).toBe('functionfunctionobject')
  })

  it('loads only names on its closed list, never an inherited one', () => {
    const w = boot()
    w.init()
    for (const bad of ['nope', 'constructor', '__proto__', 'toString', 'hasOwnProperty', '', 'd3', '../d3-array', 'https://x/y.js']) {
      expect(w.run(`try { assay.lib(${JSON.stringify(bad)}); 'loaded' } catch (e) { e.message }`), bad).toBe('assay.lib: unknown library')
    }
    expect(w.imported).toEqual(['view.js'])
    // And the table itself cannot be extended or changed from the view.
    expect(w.run(`'use strict'; var r; try { assay.lib('x'); } catch (e) {} r = Object.keys(assay).join(); r`)).toBe('run,lib,theme')
  })

  it('loads a library chain in order, announcing each file once', () => {
    const w = boot()
    w.init()
    w.run("self.d3 = {}; assay.lib('d3-scale'); assay.lib('d3-scale'); assay.lib('d3-array')")
    expect(w.imported.slice(1)).toEqual(['d3-array', 'd3-color', 'd3-interpolate', 'd3-format', 'd3-time', 'd3-time-format', 'd3-scale'].map((f) => `/assay-view/lib/${f}.js`))
    const loading = w.sent.filter((m) => m.t === 'loading').map((m) => m.file)
    expect(loading).toEqual(['d3-array', 'd3-color', 'd3-interpolate', 'd3-format', 'd3-time', 'd3-time-format', 'd3-scale'])
  })

  it('every file a library loads is one the host knows how to expect (and the build vendors)', () => {
    const w = boot()
    w.init()
    for (const name of ['d3-array', 'd3-format', 'd3-time-format', 'd3-scale', 'd3-shape', 'd3-geo', 'vega', 'vega-lite']) w.run(`assay.lib('${name}')`)
    const files = w.imported.slice(1).map((u) => u.replace('/assay-view/lib/', '').replace(/\.js$/, ''))
    expect(files.length).toBeGreaterThan(8)
    for (const f of files) expect(LIB_FILES, f).toContain(f)
    for (const f of w.sent.filter((m) => m.t === 'loading').map((m) => m.file)) expect(LIB_FILES).toContain(f)
  })

  it('is not fooled by a setter on Object.prototype', () => {
    const w = boot()
    w.init()
    w.run(`var hits = 0; ['d3-array', 'd3-scale', '1', 'vega'].forEach(function (k) { Object.defineProperty(Object.prototype, k, { set: function () { hits++ }, get: function () { return true }, configurable: true }) })`)
    w.run("assay.lib('d3-array'); assay.lib('d3-array')")
    expect(w.run('hits')).toBe(0)
    // the getter a view planted does not make an unloaded file look loaded
    expect(w.imported.filter((u) => u.endsWith('d3-array.js')).length).toBe(1)
    w.run("assay.lib('vega')")
    expect(w.imported.some((u) => u.endsWith('/vega.js'))).toBe(true)
    // and a run id is not intercepted either
    w.run("assay.run('q', {})")
    const run = w.sent.find((m) => m.t === 'run')
    expect(run.query).toBe('q')
  })

  it('removes the storage APIs and cross-context channels before the view loads', () => {
    const w = boot("self.seen = [typeof indexedDB, typeof caches, typeof BroadcastChannel, typeof localStorage, typeof sessionStorage, typeof navigator.storage, typeof navigator.locks].join()")
    w.init()
    expect(w.run('self.seen')).toBe('undefined,undefined,undefined,undefined,undefined,undefined,undefined')
    expect(w.run(`try { new BroadcastChannel('x'); 'made' } catch (e) { 'blocked' }`)).toBe('blocked')
    expect(w.run(`try { navigator.storage.getDirectory(); 'got' } catch (e) { 'blocked' }`)).toBe('blocked')
  })

  it('stringifies an A2UI page for the host and rejects a draw that returns neither shape', async () => {
    const w = boot('self.draw = function (c) { return c.params.kind === "page" ? { a2ui: { title: "T", components: [] } } : c.params.kind === "bad" ? 5 : { svg: "<svg/>" } }')
    w.init()
    const draw = (id: number, kind: string) => w.port.onmessage({ data: { t: 'draw', id, params: { kind }, theme: {}, width: 1, height: 1 } })
    draw(1, 'page'); draw(2, 'bad'); draw(3, 'svg')
    await new Promise((r) => setTimeout(r, 10))
    expect(w.sent.find((m) => m.id === 1)).toEqual({ t: 'drawn', id: 1, a2ui: '{"title":"T","components":[]}' })
    expect(w.sent.find((m) => m.id === 2).t).toBe('drawFailed')
    expect(w.sent.find((m) => m.id === 3)).toEqual({ t: 'drawn', id: 3, svg: '<svg/>' })
  })

  it('reports a view that throws while loading or defines no draw', () => {
    const a = boot('throw new Error("boom")')
    a.init()
    expect(a.sent.at(-1)).toEqual({ t: 'loadFailed', text: 'boom' })
    const b = boot('self.x = 1')
    b.init()
    expect(b.sent.at(-1).t).toBe('loadFailed')
  })
})
