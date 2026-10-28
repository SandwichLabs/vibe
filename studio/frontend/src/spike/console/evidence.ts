// SPIKE #257 - never merge. Evidence goes to Go (/spike/evidence/<name>), which writes files only
// when VIBE_SPIKE_EVIDENCE_DIR is set; every post is also mirrored to the console.
export type Webview = 'chromium' | 'webkitgtk' | 'wkwebview' | 'unknown'

export function webview(): Webview {
  const ua = navigator.userAgent
  if (/Chrome\//.test(ua)) return 'chromium'
  if (/AppleWebKit/.test(ua) && /Linux/.test(ua)) return 'webkitgtk'
  if (/AppleWebKit/.test(ua) && /Macintosh/.test(ua)) return 'wkwebview'
  return 'unknown'
}

export const errors: string[] = []
export function installErrorCapture(): void {
  window.addEventListener('error', (e) => errors.push(`error: ${e.message} @${e.filename}:${e.lineno}`))
  window.addEventListener('unhandledrejection', (e) => errors.push(`unhandledrejection: ${String((e.reason && e.reason.stack) || e.reason)}`))
  const orig = console.error
  console.error = (...a: unknown[]) => { errors.push('console.error: ' + a.map(String).join(' ')); orig.apply(console, a) }
}

function logToApp(file: string, text: string): void {
  try { (window as unknown as { runtime?: { LogInfo(s: string): void } }).runtime?.LogInfo('[spike-evidence] ' + file + ' ' + text.slice(0, 60000)) } catch { /* none */ }
}

let tag = ''
export function setTag(t: string): void { tag = t }

export async function post(name: string, body: unknown): Promise<string> {
  const file = `${tag}-${name}`
  const isPng = typeof body === 'string' && body.startsWith('data:image/png')
  const text = typeof body === 'string' ? body : JSON.stringify(body, null, 2)
  try {
    const r = await fetch('/spike/evidence/' + file, { method: 'POST', body: text })
    if (!r.ok && r.status !== 204) {
      errors.push(`evidence post ${file}: HTTP ${r.status}`)
      logToApp(file, text) // refused (env var missing, body dropped): keep the data in the app log
    }
  } catch (e) {
    errors.push(`evidence post ${file} failed: ${String(e)}`)
    logToApp(file, text) // fetch itself threw: the Wails logger (stdout of the app)
  }
  if (!isPng) console.log('[spike-evidence]', file, text.length > 600 ? text.slice(0, 600) + '...' : text)
  return file
}
