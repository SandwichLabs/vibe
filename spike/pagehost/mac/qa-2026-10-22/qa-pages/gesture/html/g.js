// QA page for D1/D2: the download gesture gates (the API's own, and the helper's native one through a raw post).
(async () => {
  const el = document.getElementById('log'), t0 = Date.now()
  const log = (s) => { const l = ((Date.now() - t0) / 1000).toFixed(1) + 's ' + s; el.textContent = l + '\n' + el.textContent; vibe.run('report', { msg: l }).catch(() => {}) }
  const dl = await vibe.use('download')
  let rawId = 900000
  // A download call posted straight to the handler: the API's gesture check is skipped, the helper's is not.
  const raw = (tag) => { rawId++; log('raw post ' + tag + ' id=' + rawId); window.webkit.messageHandlers.vibe.postMessage(JSON.stringify({ t: 'call', id: rawId, cap: 'download', op: 'save', arg: { filename: 'raw-' + tag, type: 'csv', enc: 'text', data: 'raw\n' } })) }
  const api = (tag) => dl.save({ filename: 'api-' + tag, type: 'csv', data: tag + '\n' }).then((r) => log('api ' + tag + ' -> ' + JSON.stringify(r)), (e) => log('api ' + tag + ' rejected: ' + e.message))
  log('loaded; raw post with no input in 1.5s')
  setTimeout(() => raw('noinput'), 1500)
  document.getElementById('twice').onclick = () => { log('twice clicked'); api('twice-1'); api('twice-2') }
  document.getElementById('once').onclick = () => { log('once clicked'); api('once') }
  document.getElementById('rawclick').onclick = () => { log('rawclick clicked'); raw('afterclick') }
  document.getElementById('file').onclick = () => {
    const i = document.createElement('input'); i.type = 'file'
    i.onchange = () => log('file input change: ' + i.files.length + ' file(s)'); i.oncancel = () => log('file input cancel')
    document.body.appendChild(i); i.click(); log('file input clicked')
  }
  addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { log('Escape keydown'); setTimeout(() => { raw('escape'); api('escape') }, 300) }
    else if (e.key === 'r' && !e.repeat) { log('r first press; raw post and api save at +6.5s'); setTimeout(() => { raw('held-r'); api('held-r') }, 6500) }
    else if (e.key === 'r' && e.repeat) { window.__rep = (window.__rep || 0) + 1 }
    else if (e.key === 'k') { log('k pressed'); api('k') }
  })
  addEventListener('keyup', (e) => { if (e.key === 'r') log('r released after ' + (window.__rep || 0) + ' repeat events') })
  let dov = 0; addEventListener('dragover', (e) => { e.preventDefault(); if (!dov++) log('dragover') })
  addEventListener('drop', (e) => { e.preventDefault(); const dt = e.dataTransfer; log('drop: files=' + (dt ? dt.files.length : 'n/a') + ' types=' + (dt ? JSON.stringify(Array.from(dt.types)) : '') + ' uri=' + (dt ? dt.getData('text/uri-list') : '')); if (dt && dt.files.length) { const f0 = dt.files[0]; log('file meta: name=' + f0.name + ' size=' + f0.size + ' type=' + f0.type + ' lastModified=' + f0.lastModified + ' webkitRelativePath=' + f0.webkitRelativePath) } if (dt && dt.files.length) dt.files[0].text().then((t) => log('READ FILE: ' + t.slice(0, 40)), (err) => log('file read failed: ' + err)) })
})()
