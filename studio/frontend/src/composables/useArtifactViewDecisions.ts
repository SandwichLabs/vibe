// useArtifactViewDecisions — the stored allow and deny decisions about custom
// artifact views (#159), for Settings > Workspace. Decisions live in the user
// config directory; this lists the ones for the open workspace and revokes them.
import { ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'

export function useArtifactViewDecisions() {
  const decisions = ref<ArtifactViewDecision[]>([])
  // Recent uses of the model by pages: names, times and sizes, never text.
  const activity = ref<AgentActivityEntry[]>([])
  const busy = ref('')
  const error = ref('')

  async function refresh() {
    try {
      decisions.value = await App.ListArtifactViewDecisions()
    } catch {
      decisions.value = []
    }
    await refreshActivity()
  }

  async function refreshActivity() {
    try {
      activity.value = (await App.ListAgentActivity()).slice().reverse()
    } catch {
      activity.value = []
    }
  }

  async function revoke(name: string) {
    busy.value = name
    error.value = ''
    try {
      await App.RevokeArtifactView(name)
    } catch (e) {
      error.value = errorText(e)
    }
    busy.value = ''
    await refresh()
  }

  /** Turn a scripted page's `ask` capability on or off, separately from its allow. */
  async function setAsk(name: string, on: boolean) {
    busy.value = name
    error.value = ''
    try {
      await App.SetPageAsk(name, on)
    } catch (e) {
      error.value = errorText(e)
    }
    busy.value = ''
    await refresh()
  }

  return { decisions, activity, busy, error, refresh, refreshActivity, revoke, setAsk }
}
