// useProposals — the inbox of semantic definitions an agent proposed (E4). The
// person reads each proposal beside the file it would replace and accepts or
// rejects it; nothing in semantics/ changes until they accept. A target that
// changed since the proposal was made comes back as a conflict, and accepting
// it then needs the person's explicit "accept anyway".
import { computed, ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText, isConflict } from '../lib/errors'

export function useProposals() {
  const proposals = ref<Proposal[]>([])
  const preview = ref<ProposalPreview | null>(null)
  const busy = ref(false)
  const error = ref('')
  // Set when accepting was refused because the file changed since the proposal:
  // the reason to show beside "Accept anyway".
  const stale = ref('')
  // True when the open proposal was read again and came back different (the file it
  // would replace, or the proposal, changed while it was on screen).
  const updated = ref(false)

  const open = computed(() => proposals.value.filter((p) => p.status === 'open'))

  async function load(all = false) {
    error.value = ''
    try {
      proposals.value = await App.ListProposals(all)
    } catch (e) {
      proposals.value = []
      error.value = errorText(e)
    }
  }

  async function select(id: string) {
    error.value = ''
    stale.value = ''
    const before = preview.value
    try {
      const next = await App.GetProposal(id)
      updated.value = !!before && before.proposal.id === id && before.shown_rev !== next.shown_rev
      preview.value = next
    } catch (e) {
      preview.value = null
      updated.value = false
      error.value = errorText(e)
    }
  }

  /** Accept the selected proposal; true when it was written. */
  async function accept(overwrite = false): Promise<boolean> {
    const id = preview.value?.proposal.id
    const shown = preview.value?.shown_rev ?? ''
    if (!id) return false
    busy.value = true
    error.value = ''
    try {
      // The rev of what is on screen: the write is refused if the file or the
      // proposal changed after the person looked, even with "accept anyway".
      await App.AcceptProposal(id, shown, overwrite)
      stale.value = ''
      preview.value = null
      updated.value = false
      await load()
      return true
    } catch (e) {
      if (isConflict(e)) {
        const why = errorText(e)
        await select(id) // show the file as it is now beside the proposal
        stale.value = why
      } else {
        error.value = errorText(e)
      }
      return false
    } finally {
      busy.value = false
    }
  }

  async function reject(): Promise<boolean> {
    const id = preview.value?.proposal.id
    if (!id) return false
    busy.value = true
    error.value = ''
    try {
      await App.RejectProposal(id)
      preview.value = null
      updated.value = false
      await load()
      return true
    } catch (e) {
      error.value = errorText(e)
      return false
    } finally {
      busy.value = false
    }
  }

  return { proposals, open, preview, busy, error, stale, updated, load, select, accept, reject }
}
