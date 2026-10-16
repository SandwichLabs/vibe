// useConnectionDraft — the lake form stashed while its storage connection is set
// up in the same editor (#130 step 3). In memory only, never localStorage: a
// draft may hold a local path. It holds the definition (a lake has no
// credential; nothing typed into a sign-in section is ever stashed), is tied to
// the workspace it was made in, and is dropped when a workspace opens or closes.
import { shallowRef, computed } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { hasWailsRuntime } from '../lib/wails'
import { workspaceInfo } from './useWorkspace'

export interface LakeDraft {
  def: ConnectionDef
  connScope: ConnScope
  advancedOpen: boolean
  // The saved connection being edited ('' for a new one), and the scope it was opened in.
  editName: string
  editScope: string
  // The id of the location field whose storage line was followed (focused on return).
  focus: string
  // The saved definition's fingerprint when the draft was made, to notice a change on disk.
  fingerprint: string
  // The suggestion that was followed, kept for the "whole bucket" choice.
  suggest?: StorageSuggestionView
}

interface Stored extends LakeDraft { root: string }
const stored = shallowRef<Stored | null>(null)
const rootNow = () => workspaceInfo.value?.root ?? ''

// The draft, only while the workspace it came from is the open one.
export const currentDraft = computed<LakeDraft | null>(() => (stored.value && stored.value.root === rootNow() ? stored.value : null))

let listening = false
export function clearDraft(): void { stored.value = null }

export function stashDraft(d: LakeDraft): void {
  stored.value = { ...d, root: rootNow() }
  if (!listening && hasWailsRuntime()) {
    listening = true
    EventsOn('workspace:opened', clearDraft)
    EventsOn('workspace:closed', clearDraft)
  }
}

// takeDraft returns the draft for the connection being edited ('' for a new one)
// and forgets it. A draft for another connection is left alone.
export function takeDraft(editName: string): LakeDraft | null {
  const d = currentDraft.value
  if (!d || d.editName !== editName) return null
  stored.value = null
  return d
}
