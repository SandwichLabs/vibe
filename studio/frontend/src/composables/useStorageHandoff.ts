/**
 * useStorageHandoff — "Set up storage" from a lake form (#130 step 3). The lake
 * form is stashed (useConnectionDraft), the same editor opens on a prefilled
 * storage connection, and Save or Cancel there returns to the lake with every
 * field intact. The storage connection is entered through the one sign-in
 * section and credential bridge of the editor; nothing here handles a secret.
 * The route change stays inside the one editor view, so the unsaved-edits guard
 * does not fire for it; leaving for anywhere else forgets the draft.
 */
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { askConfirm } from './useConfirm'
import { currentDraft, stashDraft, clearDraft } from './useConnectionDraft'
import { RETURN_LAKE } from '../lib/storagePrefill'
import type { ConnectionForm } from './useConnectionForm'

// One-shot: set by the two moves that ARE the hand-off, consumed by the editor's
// guards, so a destination query alone never skips the unsaved-edits check.
let passing = false
export function consumeHandoff(): boolean { const p = passing; passing = false; return p }
async function handOff(go: () => Promise<unknown>): Promise<void> {
  passing = true
  try { await go() } finally { passing = false }
}

export function useStorageHandoff(form: ConnectionForm) {
  const route = useRoute()
  const router = useRouter()

  // On a storage form that was opened from a lake form still held in memory.
  const draft = computed(() => (route.query.return === RETURN_LAKE ? currentDraft.value : null))
  const returning = computed(() => draft.value !== null)
  const lakeName = computed(() => draft.value?.def.name || 'the lake')
  // The whole bucket, offered next to the folder the form was prefilled with.
  const wholeBucket = computed(() => {
    const b = draft.value?.suggest?.bucket_scope ?? ''
    return b && b !== form.f.scope ? b : ''
  })

  function setup(need: StorageNeedView): void {
    const s = need.suggest
    const prefix = s?.scope || s?.bucket_scope || ''
    if (!s || !prefix) return
    stashDraft({
      def: form.buildDef(),
      connScope: form.connScope.value,
      advancedOpen: form.advancedOpen.value,
      editName: form.isNew.value ? '' : form.editName.value,
      editScope: typeof route.query.scope === 'string' ? route.query.scope : '',
      focus: form.f.path.trim() === need.url || !form.f.options['data_path'] ? 'f-path' : 'f-data_path',
      fingerprint: form.detail.value?.fingerprint ?? '',
      suggest: s,
    })
    const own = form.connScope.value === 'user' ? 'user' : 'local'
    // replace, not push: history back must not land on a blank form with the lake lost
    void handOff(() => router.replace({ path: '/connections/new', query: { type: s.type, prefix, name: s.name, in: own, return: RETURN_LAKE } }))
  }

  // backToLake routes to the lake form it came from. The lake being edited is
  // reopened in the scope it was opened in; a new one at /connections/new.
  async function backToLake(): Promise<void> {
    const d = draft.value
    if (!d) { await router.replace('/'); return }
    if (!d.editName) { await handOff(() => router.replace({ path: '/connections/new', query: { resume: '1' } })); return }
    const query: Record<string, string> = { resume: '1' }
    if (d.editScope) query.scope = d.editScope
    await handOff(() => router.replace({ path: `/connections/${d.editName}`, query }))
  }

  // cancel leaves the storage form for the lake. Typed values are asked about
  // first, like any unsaved edit.
  async function cancel(): Promise<void> {
    if (form.dirty.value) {
      const ok = await askConfirm({ title: 'Discard this storage connection?', message: "It hasn't been saved yet. You will go back to the lake.", variant: 'destructive', confirmLabel: 'Discard' })
      if (!ok) return
    }
    await backToLake()
  }

  // guard asks before a move that is not the hand-off leaves a storage form that
  // was opened for a lake, naming the lake, and forgets the draft only on a yes.
  // (The editor lets the hand-off itself through with consumeHandoff.)
  async function guard(): Promise<boolean> {
    if (!returning.value) return true
    const ok = await askConfirm({
      title: 'Leave without finishing?',
      message: `Your lake ${lakeName.value} and this storage connection are not saved.`,
      variant: 'destructive',
      confirmLabel: 'Leave',
    })
    if (ok) clearDraft()
    return ok
  }

  function useWholeBucket(): void {
    if (wholeBucket.value) form.f.scope = wholeBucket.value
  }

  return { returning, lakeName, wholeBucket, setup, backToLake, cancel, useWholeBucket, guard }
}
