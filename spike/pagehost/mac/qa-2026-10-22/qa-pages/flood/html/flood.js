// QA: vibe.run in a tight loop; count answers by outcome.
(async () => {
  const out = document.getElementById('log'), n = { ok: 0 }, t0 = Date.now()
  const tally = (k) => { n[k] = (n[k] || 0) + 1 }
  const calls = []
  for (let i = 0; i < 1000; i++) calls.push(vibe.run('probe', { msg: 'x' }).then(() => tally('ok'), (e) => tally('err:' + e.message)))
  await Promise.all(calls)
  out.textContent = 'after 1000 calls in ' + (Date.now() - t0) + 'ms: ' + JSON.stringify(n)
})()
