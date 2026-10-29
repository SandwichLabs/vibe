// useProposalBadge — the number of proposed definitions waiting for a decision,
// for the Semantics nav item's badge. One shared count, kept live: it is read
// when the shell starts and again when a proposal is filed (the in-app agent
// announces it with `semantics:proposed`; a file from another process arrives as
// a watcher event), accepted or rejected (`data:changed`). The shell reads it
// again when another workspace opens.
import { ref } from 'vue'
import { App } from '../lib/bindings'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

export const openProposals = ref(0)

let timer: ReturnType<typeof setTimeout> | undefined

/** Re-list soon: a burst of events (an agent turn) makes one read. */
function refreshSoon() {
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => { timer = undefined; void refreshProposalBadge() }, 250)
}

export async function refreshProposalBadge() {
  try {
    const open = (await App.ListProposals(false)).filter((p) => p.status === 'open')
    openProposals.value = open.length
  } catch {
    openProposals.value = 0
  }
}

/** Start keeping the count live; returns the function that stops it. */
export function startProposalBadge(): () => void {
  void refreshProposalBadge()
  if (!hasWailsRuntime()) return () => {}
  const offs = [
    EventsOn('semantics:proposed', refreshSoon),
    EventsOn('data:changed', refreshSoon),
    EventsOn('workspace:changed-on-disk', (p?: { kind?: string }) => { if (p?.kind === 'proposal') refreshSoon() }),
  ]
  return () => { if (timer) clearTimeout(timer); for (const off of offs) off() }
}
