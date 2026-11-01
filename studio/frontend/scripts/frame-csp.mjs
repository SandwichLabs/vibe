// The frame policy of Studio's own page (docs/plans/html-artifacts.md, H3, "a1").
//
// A static HTML artifact is shown in a sandbox="" iframe. The sandbox stops its
// script, but a person's click on an outside link in it would still navigate the
// frame, and a <meta refresh> would too. Studio's page forbids that with a meta
// CSP of one directive, `frame-src`, narrowed to the view server's own route on
// the window's origin: <origin>/slaw-view/. Studio's boot is untouched (no other
// directive), and a frame navigating to / or /index.html loads nothing.
//
// The origin is the window's, which depends on the platform, so the meta is
// emitted into index.html at build time:
//   build  Linux and macOS: wails://wails   Windows: http://wails.localhost
//          (SLAW_STUDIO_ORIGIN overrides, for a cross build)
//   dev    wails://wails.localhost:34115, the dev window, plus
//          http://localhost:34115, the same server in a browser (Rodney QA).
//          The dev meta is only ever emitted by `vite` serving, never by a build.

export const DEV_ORIGINS = ['wails://wails.localhost:34115', 'http://localhost:34115']

const ORIGIN = /^(https?|wails):\/\/[A-Za-z0-9.-]+(:[0-9]{1,5})?$/

/** The origins Studio's window can have, for this command, environment and platform. */
export function studioOrigins(command, env = {}, platform = 'linux') {
  if (command === 'serve') return DEV_ORIGINS
  const forced = env.SLAW_STUDIO_ORIGIN
  if (forced) {
    if (!ORIGIN.test(forced)) throw new Error(`SLAW_STUDIO_ORIGIN ${JSON.stringify(forced)} is not a plain origin`)
    return [forced]
  }
  return [platform === 'win32' ? 'http://wails.localhost' : 'wails://wails']
}

/** The meta element, with one frame-src source per origin. */
export function frameCspMeta(origins) {
  if (!origins.length || !origins.every((o) => ORIGIN.test(o))) throw new Error('frame policy: every origin must be a plain origin')
  const sources = origins.map((o) => `${o}/slaw-view/`).join(' ')
  return `<meta http-equiv="Content-Security-Policy" content="frame-src ${sources}" />`
}

/** index.html with the meta placed first in <head> (a CSP applies to what follows it). */
export function injectFrameCsp(html, origins) {
  if (/http-equiv=["']Content-Security-Policy["']/i.test(html)) throw new Error('index.html already has a CSP meta')
  const i = html.search(/<head[^>]*>/i)
  if (i < 0) throw new Error('index.html has no <head>')
  const end = html.indexOf('>', i) + 1
  return `${html.slice(0, end)}\n  ${frameCspMeta(origins)}${html.slice(end)}`
}
