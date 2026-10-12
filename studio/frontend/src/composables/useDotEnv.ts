// useDotEnv — the module-singleton mirror of the open workspace's .env state
// (#168): undecided, allowed, changed since allowed, denied, or none. Only
// variable names and the state cross the bridge, never a value. The prompt
// (WorkspaceBanner) and Settings > Workspace read the same ref.
//
// "Not now" is remembered for this open of the workspace only: opening the
// workspace again asks again.
import { ref, computed } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { refreshConnections, relaunch } from './useConnections'
import { workspaceInfo, openWorkspacePath } from './useWorkspace'
import { App } from '../lib/bindings'

export const dotenv = ref<DotEnvStatus | null>(null)
export const dotenvBusy = ref(false)
export const dotenvError = ref('')
const notNowFor = ref('')

/** True while the prompt should show: an undecided or changed .env that wasn't put off. */
export const dotenvPrompt = computed(() => {
  const d = dotenv.value
  if (!d || (d.state !== 'undecided' && d.state !== 'changed')) return false
  return notNowFor.value !== d.state + '|' + d.path
})

export async function refreshDotEnv(): Promise<void> {
  try {
    dotenv.value = await App.GetDotEnvStatus()
  } catch {
    dotenv.value = null
  }
}

export function dotenvNotNow(): void {
  const d = dotenv.value
  if (d) notNowFor.value = d.state + '|' + d.path
}

async function decide(call: () => Promise<DotEnvStatus>): Promise<void> {
  dotenvBusy.value = true
  dotenvError.value = ''
  try {
    dotenv.value = await call()
    await refreshConnections()
    // MotherDuck takes its token only when a workspace opens (one account per
    // workspace): when that is what the .env just supplied, reopen it now.
    const root = workspaceInfo.value?.root
    if (relaunch.value.required && root && dotenv.value?.state === 'allowed') {
      const res = await openWorkspacePath(root)
      if (!res.ok && res.error) dotenvError.value = res.error
      await refreshConnections()
    }
  } catch (e: unknown) {
    dotenvError.value = errorText(e)
  }
  dotenvBusy.value = false
}

export const allowDotEnv = () => decide(() => App.AllowDotEnv())
export const denyDotEnv = () => decide(() => App.DenyDotEnv())
export const revokeDotEnv = () => decide(() => App.RevokeDotEnv())

let started = false
export function startDotEnvTracking(): void {
  if (started || !hasWailsRuntime()) return
  started = true
  EventsOn('workspace:opened', () => { notNowFor.value = ''; dotenvError.value = ''; void refreshDotEnv() })
  EventsOn('workspace:changed', () => { void refreshDotEnv() })
  EventsOn('workspace:closed', () => { dotenv.value = null })
  void refreshDotEnv()
}

/** Test helper: forget everything. */
export function _resetDotEnvForTests(): void {
  dotenv.value = null
  dotenvBusy.value = false
  dotenvError.value = ''
  notNowFor.value = ''
}
