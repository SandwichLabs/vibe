/**
 * useNewWorkspaceFields — the fields every "make a new workspace" form shares
 * (F6): where to put it (remembered from the last time), whether to start a git
 * repository (only when git exists), the folder name the workspace name becomes
 * (slugify, mirroring the Go side), and the folder picker. Used by the New
 * workspace dialog and by Import .vibe into a new workspace.
 */
import { ref, computed, type Ref } from 'vue'
import { App } from '../lib/bindings'
import { slugify } from './useWorkspace'
import { errorText } from '../lib/errors'

const LAST_PARENT_KEY = 'vibe.newWorkspace.parentDir'

export function useNewWorkspaceFields(name: Ref<string>) {
  const parentDir = ref('')
  const git = ref(true)
  const gitAvailable = ref(true)
  const slug = computed(() => slugify(name.value))

  // reset starts the form over: the last-used location, and git on when it exists.
  async function reset() {
    try { parentDir.value = localStorage.getItem(LAST_PARENT_KEY) || '' } catch { parentDir.value = '' }
    try { gitAvailable.value = await App.CheckGitAvailable() } catch { gitAvailable.value = false }
    git.value = gitAvailable.value
  }

  // remember keeps the location for next time (called after a successful create).
  function remember() {
    try { localStorage.setItem(LAST_PARENT_KEY, parentDir.value.trim()) } catch { /* private window */ }
  }

  async function choose(onError: (message: string) => void) {
    try {
      const p = await App.ChooseFolder('Where should the new workspace go?')
      if (p) parentDir.value = p
    } catch (e: unknown) {
      onError(errorText(e))
    }
  }

  return { parentDir, git, gitAvailable, slug, reset, remember, choose }
}
