// agentDraft: the hand-off from the New dialog's "describe it" row to the
// agent panel (docs/plans/library.md L7). The draft is only text put into the
// panel's own input; it is never sent here (AgentPanel.submit is the one
// caller of send), and no tool or binding is involved.
import type { LibraryType } from './libraryRoutes'

export const OPEN_AGENT_EVENT = 'assay:open-agent'
export const MAX_DESCRIPTION = 2000

/** The unsent request for an artifact kind: names the type and the tool. */
export function agentDraft(type: LibraryType, description: string, withScript = false): string {
  const what = type === 'live'
    ? 'a live view (a data artifact with an A2UI view, view: a2ui)'
    : type === 'page'
      ? (withScript ? 'an HTML page with a script (a data artifact with view: html and an html/app.js)' : 'an HTML page (a data artifact with view: html)')
      : 'a chart script (a data artifact with view: script)'
  return `Build ${what} with write_artifact (read the data-artifacts skill first): ${description.trim()}`
}

/** Opens the agent panel; `draft`, when given, is offered as an unsent request. */
export function openAgent(draft?: string) {
  window.dispatchEvent(new CustomEvent(OPEN_AGENT_EVENT, draft ? { detail: { draft } } : undefined))
}

/** The text a prefill may set, or null to leave the box alone. Never while a
 * turn is sending; never over text the person typed (anything but empty or the
 * previous prefill, unchanged). */
export function prefillText(current: string, lastPrefill: string, incoming: string, sending: boolean): string | null {
  if (sending || !incoming) return null
  return current.trim() === '' || current === lastPrefill ? incoming : null
}
