// SPIKE P1 (throwaway): drives the static-host vectors in a sandbox="" frame.
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const TOK = '5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a'

export async function runSpike() {
  const App = (window as any).go?.main?.App
  if (!App) return
  const mode: string = await App.SpikeMode()
  if (!mode) return
  const probe = (t: string) => App.SpikeProbe(t)
  if (window.top !== window) {
    probe('BOOT-IN-FRAME href=' + location.href)
    return
  }
  const control = mode.includes('control')
  const sbonly = mode.includes('sbonly') // sandbox attribute, no response CSP
  await sleep(2500)
  probe(`BOOT top origin=${location.origin} mode=${mode} app-children=${document.querySelector('#app')?.children.length}`)
  for (const path of [`page?v=inert&o=${encodeURIComponent(location.origin)}`, 'page/', 'index.html', 'assets/ok.css', 'nope']) {
    try {
      const r = await fetch(`/vibe-view/${TOK}/${path}`)
      const body = await r.text()
      probe(`FETCH ${path.split('&')[0]} status=${r.status} csp=${r.headers.get('content-security-policy')} nosniff=${r.headers.get('x-content-type-options')} cache=${r.headers.get('cache-control')} ctype=${r.headers.get('content-type')} wails-runtime-injected=${/wails\/(ipc|runtime)\.js|window\.wails/.test(body)} bytes=${body.length}`)
    } catch (e) { probe('FETCH ' + path + ' ERR ' + e) }
  }
  const only = (mode.match(/only=([a-z0-9,-]+)/) || [])[1]?.split(',')
  const variants: [string, boolean][] = [
    ['inert', false], ['ipc', false], ['refresh-outside', false], ['refresh-js', false], ['refresh-self', false], ['refresh-root', false],
    ['click-outside', true], ['click-js', true], ['click-form-get', true], ['click-form-post', true], ['click-rerender', true],
    ['lnk-preconnect', false], ['lnk-dns-prefetch', false], ['lnk-prefetch', false], ['lnk-prerender', false], ['lnk-icon', false], ['lnk-stylesheet', false], ['lnk-preload', false], ['lnk-modulepreload', false],
    ['click-root', true], ['click-index', true], ['click-ping', true], ['click-blank', true], ['click-download', true],
  ]
  for (const [v, click] of variants) {
    if (only && !only.includes(v)) continue
    probe('BEGIN ' + v)
    const f = document.createElement('iframe')
    if (!control) f.setAttribute('sandbox', '')
    f.style.cssText = 'position:fixed;inset:0;width:100vw;height:100vh;border:0;z-index:99999;background:#fff'
    f.onload = () => probe('FRAME-LOAD ' + v)
    f.src = `/vibe-view/${TOK}/page?v=${v}&o=${encodeURIComponent(location.origin)}${control || sbonly ? '&c=1' : ''}`
    document.body.appendChild(f)
    await sleep(2500)
    if (click) {
      probe('READY_CLICK ' + v)
      await sleep(3500)
    }
    f.remove()
    probe('END ' + v)
    await sleep(300)
  }
  probe('ALLDONE app-children=' + document.querySelector('#app')?.children.length)
}
