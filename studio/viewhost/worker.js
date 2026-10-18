// The host-owned bootstrap of a custom artifact view's Worker (#160). It is
// embedded in Studio's binary and served as /vibe-view/<token>/worker.js; the
// artifact's own code is /vibe-view/<token>/view.js, loaded below with
// importScripts. The response CSP of this file (no network, no nested workers,
// no eval, scripts only from that view.js and /vibe-view/lib/) is what confines
// the view; see studio/viewserver.go and docs/reference/artifacts.md.
//
// The view's whole world is the `vibe` object:
//   vibe.run(queryName, params) -> Promise of {columns, rows, truncated}
//   vibe.lib(name)              -> the pinned library, loaded by name
//   vibe.theme                  -> chart.1..chart.6 and the text/background roles
// and a global function the view defines:
//   draw({params, theme, width, height}) -> Promise of {svg: string} or {a2ui: page}
//
// All traffic is over one MessagePort handed over in the first message, which is
// kept in this closure; the view cannot reach it. Messages from here are
// {t: ...}: hb, loading, ready, loadFailed, run, drawn, drawFailed.
(function () {
  'use strict'

  // Tables are made with no prototype, so nothing the view puts on
  // Object.prototype (a setter, a getter) can intercept a write or a lookup here.
  function mk(o) {
    var t = Object.create(null)
    for (var k in o) t[k] = o[k]
    return t
  }

  var LIB_DIR = '/vibe-view/lib/'
  // The closed list of libraries vibe.lib() loads, with each one's files in
  // dependency order (each file is /vibe-view/lib/<file>.js) and the global it
  // leaves behind. Nothing else can be named.
  var LIBS = mk({
    'd3-array': { files: ['d3-array'], global: 'd3' },
    'd3-format': { files: ['d3-format'], global: 'd3' },
    'd3-time-format': { files: ['d3-time', 'd3-time-format'], global: 'd3' },
    'd3-scale': { files: ['d3-array', 'd3-color', 'd3-interpolate', 'd3-format', 'd3-time', 'd3-time-format', 'd3-scale'], global: 'd3' },
    'd3-shape': { files: ['d3-path', 'd3-shape'], global: 'd3' },
    'd3-geo': { files: ['d3-array', 'd3-geo'], global: 'd3' },
    'vega': { files: ['vega', 'vega-interpreter'], global: 'vega' },
    'vega-lite': { files: ['vega', 'vega-interpreter', 'vega-lite'], global: 'vegaLite' }
  })
  Object.freeze(LIBS)

  // Everything used after the view has loaded is captured first, so the view
  // cannot swap it out from under this file.
  var scope = self
  var importScripts = scope.importScripts.bind(scope)
  var setIv = scope.setInterval.bind(scope)
  var hasOwn = Object.prototype.hasOwnProperty
  var Prom = Promise
  var loaded = Object.create(null)
  var theme = {}
  var port = null
  var send = null
  var nextId = 1
  var pending = Object.create(null)

  function text(e) {
    var m = e && e.message !== undefined ? e.message : e
    return String(m).slice(0, 300)
  }

  function lib(name) {
    if (typeof name !== 'string' || !hasOwn.call(LIBS, name)) {
      throw new Error('vibe.lib: unknown library')
    }
    var spec = LIBS[name]
    var fresh = []
    for (var i = 0; i < spec.files.length; i++) {
      if (!loaded[spec.files[i]]) fresh.push(spec.files[i])
    }
    if (fresh.length) {
      for (var j = 0; j < fresh.length; j++) {
        // A library parses for a while without a heartbeat; tell the host, once per file.
        send({ t: 'loading', file: fresh[j] })
        importScripts(LIB_DIR + fresh[j] + '.js')
        loaded[fresh[j]] = true
      }
      // vega's CSP-safe expression interpreter, so vega runs without eval:
      //   new vega.View(vega.parse(spec, null, {ast: true}), {expr: vega.expressionInterpreter})
      if (scope.vega && scope.vegaInterpreter && !scope.vega.expressionInterpreter) {
        scope.vega.expressionInterpreter = scope.vegaInterpreter.expressionInterpreter
      }
      send({ t: 'hb' })
    }
    return scope[spec.global]
  }

  function run(query, params) {
    return new Prom(function (resolve, reject) {
      var id = nextId++
      pending[id] = { resolve: resolve, reject: reject }
      try {
        send({ t: 'run', id: id, query: query, params: params === undefined ? {} : params })
      } catch (e) {
        delete pending[id]
        reject(new Error('The query could not be sent.'))
      }
    })
  }

  function onResult(m) {
    var p = pending[m.id]
    if (!p) return
    delete pending[m.id]
    if (m.ok) p.resolve({ columns: m.columns, rows: m.rows, truncated: !!m.truncated })
    else p.reject(new Error(typeof m.error === 'string' ? m.error : 'The query could not be run.'))
  }

  function onDraw(m) {
    var id = m.id
    theme = m.theme || {}
    Prom.resolve()
      .then(function () {
        if (typeof scope.draw !== 'function') throw new Error('The view does not define draw().')
        return scope.draw({ params: m.params, theme: theme, width: m.width, height: m.height })
      })
      .then(function (out) {
        if (out && typeof out.svg === 'string') send({ t: 'drawn', id: id, svg: out.svg })
        else if (out && typeof out.a2ui === 'object' && out.a2ui !== null) send({ t: 'drawn', id: id, a2ui: JSON.stringify(out.a2ui) })
        else throw new Error('draw() must return {svg: string} or {a2ui: page}.')
      })
      .catch(function (e) {
        try { send({ t: 'drawFailed', id: id, text: text(e) }) } catch (_) { /* the host is gone */ }
      })
  }

  // Same-origin storage and cross-context channels the view must not have: a
  // Worker shares Studio's origin, so these would reach Studio's own data or
  // another view. Removed (and locked to undefined) on the global, its prototype
  // chain and navigator, before the view loads. Where WebKit will not let a
  // property go, it is replaced with undefined if it can be.
  function lock(obj, key) {
    try { delete obj[key] } catch (e) { /* not configurable */ }
    try { Object.defineProperty(obj, key, { value: undefined, writable: false, configurable: false }) } catch (e) { /* kept */ }
  }
  function stripStorage() {
    var globals = ['indexedDB', 'caches', 'BroadcastChannel', 'localStorage', 'sessionStorage', 'IDBFactory', 'CacheStorage']
    var navs = ['storage', 'locks', 'serviceWorker']
    var o = scope
    while (o && o !== Object.prototype) {
      for (var i = 0; i < globals.length; i++) if (hasOwn.call(o, globals[i])) lock(o, globals[i])
      o = Object.getPrototypeOf(o)
    }
    var n = scope.navigator
    var np = n ? Object.getPrototypeOf(n) : null
    for (var j = 0; j < navs.length; j++) {
      if (np && hasOwn.call(np, navs[j])) lock(np, navs[j])
      if (n) lock(n, navs[j])
    }
    for (var k = 0; k < globals.length; k++) lock(scope, globals[k])
  }

  function start(p, initialTheme) {
    port = p
    var post = port.postMessage.bind(port)
    send = function (m) { post(m) }
    theme = initialTheme || {}
    port.onmessage = function (e) {
      var m = e.data
      if (!m || typeof m !== 'object') return
      if (m.t === 'draw') onDraw(m)
      else if (m.t === 'result') onResult(m)
    }
    // Alive from here on; the host terminates a Worker that goes quiet.
    setIv(function () { send({ t: 'hb' }) }, 250)
    send({ t: 'hb' })

    var vibe = {
      run: run,
      lib: lib,
      get theme() { return theme }
    }
    Object.defineProperty(scope, 'vibe', { value: Object.freeze(vibe), writable: false, configurable: false })

    stripStorage()
    try {
      importScripts('view.js')
    } catch (e) {
      send({ t: 'loadFailed', text: text(e) })
      return
    }
    if (typeof scope.draw !== 'function') {
      send({ t: 'loadFailed', text: 'The view does not define draw().' })
      return
    }
    send({ t: 'ready' })
  }

  scope.onmessage = function (e) {
    // The one handshake; after it nothing here is reachable from the view.
    scope.onmessage = null
    var m = e.data
    if (!m || m.t !== 'init' || !e.ports || !e.ports[0]) return
    start(e.ports[0], m.theme)
  }
})()
