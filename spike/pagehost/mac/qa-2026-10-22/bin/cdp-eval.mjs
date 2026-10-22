// cdp-eval.mjs <expr>: evaluate an expression (awaited) in the first page tab of a local headless Chrome on 127.0.0.1:9333.
const tabs = await (await fetch('http://127.0.0.1:9333/json')).json()
const t = tabs.find((x) => x.type === 'page' && x.url.startsWith('http://localhost:34115'))
if (!t) { console.log('no tab on :34115; tabs:', tabs.map((x) => x.url)); process.exit(1) }
const ws = new WebSocket(t.webSocketDebuggerUrl)
await new Promise((r) => ws.addEventListener('open', r))
ws.addEventListener('message', (m) => { const d = JSON.parse(m.data); if (d.id === 1) { console.log(JSON.stringify(d.result?.result?.value ?? d.result ?? d.error)); ws.close(); process.exit(0) } })
ws.send(JSON.stringify({ id: 1, method: 'Runtime.evaluate', params: { expression: `(async () => { try { return JSON.stringify(await (${process.argv[2]})) } catch (e) { return 'THROW ' + (e && e.message || e) } })()`, awaitPromise: true, returnByValue: true } }))
setTimeout(() => { console.log('timeout'); process.exit(2) }, 30000)
