// The P5 capability page: state across a reopen, download without a gesture (and
// with one, when a person presses the button), ask, and onChange. It reports to
// Studio through vibe.run("report", {msg}).
(async () => {
  const log = []
  const show = (s) => { log.push(s); document.getElementById('log').textContent = log.join('\n') }
  let sent = 0
  const report = async () => { try { await vibe.run('report', { msg: log.slice(sent).join(' | ').slice(0, 990) }); sent = log.length } catch (e) {} }
  const st = await vibe.use('state'), dl = await vibe.use('download'), ask = await vibe.use('ask'), nope = await vibe.use('db')
  show('caps state=' + !!st + ' download=' + !!dl + ' ask=' + !!ask + ' db=' + !!nope)
  show('userActivation.type=' + typeof navigator.userActivation)
  const n = ((await st.get('count')) || 0) + 1
  await st.set('count', n)
  show('count=' + n)
  show('activeInsideReply=' + navigator.userActivation.isActive)
  for (const ms of [0, 50, 500]) setTimeout(() => show('active+' + ms + 'ms=' + navigator.userActivation.isActive), ms)
  st.onChange((e) => { show('change ' + JSON.stringify(e)); report() })
  try { await dl.save({ filename: 'live', type: 'csv', data: 'a,b\n1,2\n' }); show('save resolved') } catch (e) { show('save rejected: ' + e.message) }
  // A page that skips the injected API and posts a save straight to the handler.
  // The helper's own record of the person's input refuses it, so Studio is never asked.
  window.webkit.messageHandlers.vibe.postMessage(JSON.stringify({ t: 'call', id: 987654, cap: 'download', op: 'save', arg: { filename: 'raw', type: 'csv', enc: 'text', data: 'x' } }))
  show('raw save posted')
  const a = await ask.ask('say hi', { system: 'be brief' })
  show('ask=' + JSON.stringify(a))
  const big = 'x'.repeat(70000)
  try { await st.set('big', big); show('big ok') } catch (e) { show('big rejected: ' + e.message) }
  await report()
  document.getElementById('save').addEventListener('click', async () => {
    show('clicked active=' + navigator.userActivation.isActive)
    try { show('saved=' + JSON.stringify(await dl.save({ filename: '../x/clicked', type: 'csv', data: 'c\n1\n' }))) } catch (e) { show('click save rejected: ' + e.message) }
    report()
  })
  show('ready')
  await report()
})()
