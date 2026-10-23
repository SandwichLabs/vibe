/**
 * usePendingAsk: the one-shot hand-off from the New dialog to a freshly
 * created notebook (docs/plans/library.md L7). The dialog sets the description
 * before it routes; the notebook view takes it once it has loaded and opens its
 * AI cell dialog with it. It lives in memory, never in the URL, is taken once,
 * and is dropped when the workspace closes.
 */
import { EventsOn } from '../wailsjs/runtime/runtime'
import { hasWailsRuntime } from '../lib/wails'

let pending: { id: string; description: string } | null = null
let tracking = false

function track() {
  if (tracking || !hasWailsRuntime()) return
  tracking = true
  EventsOn('workspace:closed', clearPendingAsk)
}

export function setPendingAsk(id: string, description: string) {
  track()
  pending = { id, description }
}

/** The description set for this notebook, once; null for any other notebook. */
export function takePendingAsk(id: string): string | null {
  if (!pending || pending.id !== id) return null
  const d = pending.description
  pending = null
  return d
}

export function clearPendingAsk() { pending = null }
