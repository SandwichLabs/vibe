// QA isolation of the probe's "open" group: one action per group, each against the target IP.
(async () => {
  const T = JSON.parse(document.getElementById('target').textContent), H = 'http://' + T
  const g = JSON.parse(document.getElementById('group').textContent)
  const log = (s) => { document.getElementById('log').textContent += s + '\n'; vibe.run('probe', { msg: s }).catch(() => {}) }
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
  const link = (attrs) => { const a = document.createElement('a'); for (const k in attrs) a.setAttribute(k, attrs[k]); a.textContent = 'x'; document.body.appendChild(a); return a }
  log('group=' + g)
  if (g === 'popup') { const w = window.open(H + '/v/iso-window-open'); log('window.open=' + (w === null ? 'null' : 'window')) }
  else if (g === 'blank') { link({ href: H + '/v/iso-target-blank', target: '_blank' }).click(); log('blank clicked') }
  else if (g === 'popself') { const w = window.open('vibe-page://' + location.host + '/?group=main'); log('window.open-self=' + (w === null ? 'null' : 'window')) }
  else if (g === 'idle') { log('idle: no action') }
  await sleep(2500); log(g + ' done')
})()
