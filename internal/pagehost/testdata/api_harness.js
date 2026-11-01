// Runs the injected page API (read from argv[2]) in a vm with a fake webview,
// plays one scenario (argv[3]) and prints a JSON result.
'use strict'
const fs = require('fs')
const vm = require('vm')
const [, , file, scenario, capsArg] = process.argv
const src = fs.readFileSync(file, 'utf8')
const posted = []
const handlers = {}
let clock = 1000000
const sandbox = {
  webkit: { messageHandlers: { assay: { postMessage: (s) => posted.push(s) } } },
  setInterval: () => 0,
  btoa: (s) => Buffer.from(s, 'latin1').toString('base64'),
  navigator: {},
  Date: { now: () => clock },
  addEventListener: (type, fn, capture) => { handlers[type] = { fn, capture } },
  TextEncoder, Promise, ArrayBuffer, Blob, Uint8Array, JSON, String, Object, Map, Error, Math,
}
sandbox.window = sandbox
vm.createContext(sandbox)
vm.runInContext(src, sandbox)
const assay = sandbox.assay
const out = {}
const reply = (id, ok, v) => sandbox.__assayReply(id, ok, v)
const flush = () => new Promise((r) => setImmediate(r))
async function main() {
  if (scenario === 'use') {
    out.state = (await assay.use('state')) !== null
    out.download = (await assay.use('download')) !== null
    out.ask = (await assay.use('ask')) !== null
    out.other = (await assay.use('constructor')) !== null
    out.proto = (await assay.use('__proto__')) !== null
  }
  if (scenario === 'gesture') {
    const d = await assay.use('download')
    const tryIt = async () => { try { await d.save({ filename: 'a', type: 'csv', data: 'x' }); return 'resolved' } catch (e) { return e.message } }
    out.noGesture = await tryIt()
    handlers.click.fn({ type: 'click', isTrusted: false })
    out.forged = await tryIt()
    handlers.keydown.fn({ type: 'keydown', isTrusted: true, key: 'Escape' })
    out.escape = await tryIt()
    out.listenersAreCapture = Object.values(handlers).every((h) => h.capture === true)
    out.postedWithoutGesture = posted.length
    handlers.click.fn({ type: 'click', isTrusted: true })
    clock += 6000
    out.expired = await tryIt()
    handlers.pointerdown.fn({ type: 'pointerdown', isTrusted: true })
    clock += 100
    const p = d.save({ filename: 'a', type: 'csv', data: 'x' })
    await flush()
    out.posted = posted.map(JSON.parse)
    reply(out.posted[0].id, true, { saved: false })
    out.result = await p
    out.consumed = await tryIt()
  }
  if (scenario === 'bytes') {
    const d = await assay.use('download')
    handlers.click.fn({ type: 'click', isTrusted: true })
    const p = d.save({ filename: 'a', type: 'png', data: new Uint8Array([137, 80, 78, 71, 255, 0]) })
    await flush()
    out.posted = posted.map(JSON.parse)
    reply(out.posted[0].id, true, { saved: true })
    await p
  }
  if (scenario === 'chunks') {
    const s = await assay.use('state')
    const value = { t: '"\\\n' + 'é😀'.repeat(15000) }
    const p = s.set('big', value)
    let n = 0
    for (;;) {
      await flush()
      if (posted.length === n) break
      const m = JSON.parse(posted[n])
      out.maxBytes = Math.max(out.maxBytes || 0, Buffer.byteLength(posted[n]))
      n++
      if (m.t === 'put') reply(m.id, true, {})
      else { out.call = m; reply(m.id, true, true); break }
    }
    await p
    const puts = posted.map(JSON.parse).filter((m) => m.t === 'put')
    out.puts = puts.length
    out.ordered = puts.every((m, i) => (m.seq || 0) === i) && puts[puts.length - 1].last === true
    const text = puts.map((m) => m.s).join('')
    out.roundTrips = JSON.stringify({ key: 'big', value }) === text
  }
  if (scenario === 'event') {
    const s = await assay.use('state')
    const got = []
    const off = s.onChange((e) => got.push(e))
    sandbox.__assayEvent('state', { key: 'a', value: 1 })
    off()
    sandbox.__assayEvent('state', { key: 'b', value: 2 })
    out.got = got
  }
  if (scenario === 'frozen') {
    try { assay.run = null } catch (e) {}
    try { sandbox.assay = null } catch (e) {}
    out.runIsFunction = typeof sandbox.assay.run === 'function'
    const s = await assay.use('state')
    try { s.set = null } catch (e) {}
    out.setIsFunction = typeof s.set === 'function'
  }
  console.log(JSON.stringify(out))
}
main().catch((e) => { console.log(JSON.stringify({ error: String(e && e.stack) })); process.exit(1) })
