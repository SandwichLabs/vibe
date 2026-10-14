/**
 * useUpdates — the module-singleton state behind the first-run consent prompt, the weekly startup
 * check and Settings > About > Updates. Nothing is requested until the person answered (or pressed
 * "Check now"), and nothing is installed without a confirmation. Failures of the unprompted weekly
 * check are silent; an explicit "Check now" shows its error.
 */
import { ref, computed } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { askConfirm } from './useConfirm'
import { appNotice } from './useWorkspace'

export const updateState = ref<UpdateState | null>(null)
export const updateResult = ref<UpdateResult | null>(null)
export const updateChecking = ref(false)
export const updateError = ref('')
/** A failed save of the person's choice (consent, toggle, skip): shown apart from check errors. */
export const updateSettingsError = ref('')
export const updateApplying = ref(false)
export const updateApplyError = ref('')
export const updateApplied = ref<UpdateApplyResult | null>(null)
/** Set only by the unprompted weekly check: a newer release the person has not skipped. */
export const updateNotice = ref<UpdateResult | null>(null)
const noticeDismissed = ref('')
const consentLater = ref(false)

/**
 * An install that finished in this session and awaits a restart. Backend-backed (UpdateState.appliedVersion),
 * so a reload, Check now or "Later" never re-offers the version just installed.
 */
export const appliedInfo = computed(() => {
  if (updateApplied.value) return { to: updateApplied.value.to, output: updateApplied.value.output, canRestart: !!updateApplied.value.restart }
  const st = updateState.value
  return st?.appliedVersion ? { to: st.appliedVersion, output: '', canRestart: st.canRestart } : null
})

/** The first-run question shows once: never asked, a release build, and not put off this session. */
export const showConsent = computed(() => !!updateState.value?.needsConsent && !consentLater.value)
export const showNotice = computed(() => !!updateNotice.value && updateNotice.value.latest !== noticeDismissed.value && !appliedInfo.value)

export function resetUpdates(): void {
  updateState.value = null; updateResult.value = null; updateChecking.value = false; updateError.value = ''
  updateSettingsError.value = ''; updateApplying.value = false; updateApplyError.value = ''; updateApplied.value = null; updateNotice.value = null
  noticeDismissed.value = ''; consentLater.value = false
}

export async function refreshUpdateState(): Promise<void> {
  try { updateState.value = await App.GetUpdateState() } catch { updateState.value = null }
}

/** Answer the first-run question: "weekly" (Check weekly) or "off" (No thanks). Either is saved. */
export async function answerUpdateConsent(mode: 'weekly' | 'off'): Promise<void> {
  try {
    await App.SetUpdateCheck(mode)
    await refreshUpdateState()
    if (mode === 'weekly') await startupUpdateCheck()
  } catch (e: unknown) {
    updateSettingsError.value = errorText(e)
    consentLater.value = true
  }
}

/** Run once at startup (after the launcher or a workspace is up). Quiet unless a new release is found. */
export async function startupUpdateCheck(): Promise<void> {
  if (!hasWailsRuntime()) return
  await refreshUpdateState()
  if (updateState.value?.check !== 'weekly') return
  try {
    const res = await App.AutoCheckForUpdate()
    if (!res) return
    updateResult.value = res
    if (res.updateAvailable && !res.skipped) updateNotice.value = res
  } catch {
    // Silent by design: an unprompted check never shows an error.
  }
}

export async function setUpdateCheck(on: boolean): Promise<void> {
  try {
    await App.SetUpdateCheck(on ? 'weekly' : 'off')
    await refreshUpdateState()
  } catch (e: unknown) { updateSettingsError.value = errorText(e) }
}

/** "Check now": explicit consent for this one request, whatever the saved choice is. */
export async function checkUpdateNow(): Promise<void> {
  updateChecking.value = true
  updateError.value = ''
  try {
    updateResult.value = await App.CheckForUpdate()
    await refreshUpdateState()
  } catch (e: unknown) {
    updateResult.value = null
    updateError.value = errorText(e)
  }
  updateChecking.value = false
}

export async function skipUpdate(tag: string): Promise<void> {
  try {
    await App.SkipUpdate(tag)
    await refreshUpdateState()
    if (updateResult.value?.latest === tag) updateResult.value = { ...updateResult.value, skipped: true }
    dismissNotice()
  } catch (e: unknown) { updateSettingsError.value = errorText(e) }
}

export function dismissNotice(): void { noticeDismissed.value = updateNotice.value?.latest ?? '' }
export function putOffConsent(): void { consentLater.value = true }

/** Ask, then install `tag` with that release's own installer. Nothing happens on "Cancel". */
export async function installUpdate(tag: string): Promise<void> {
  const yes = await askConfirm({
    title: `Install ${tag}?`,
    message: `Vibe will download ${tag}'s installer, check it against the release's checksums and run it. Your workspaces, settings and credentials are not touched. This running Vibe keeps its current version until you restart it.`,
    variant: 'primary',
    confirmLabel: `Install ${tag}`,
  })
  if (yes) await applyUpdateTo(tag)
}

/** The note App.vue shows when a quit is held because an update is installing. */
export const INSTALLING_NOTICE = 'An update is installing. Close Vibe again when it finishes, in a minute or so.'

async function applyUpdateTo(tag: string): Promise<void> {
  updateApplying.value = true
  updateApplyError.value = ''
  try {
    updateApplied.value = await App.ApplyUpdate(tag)
    await refreshUpdateState()
  } catch (e: unknown) {
    updateApplyError.value = errorText(e)
  }
  updateApplying.value = false
  if (appNotice.value === INSTALLING_NOTICE) appNotice.value = ''
}

/** Quit through the normal path (saves flushed, unsaved-work question asked) and start the new Studio. */
export async function restartAfterUpdate(): Promise<void> {
  try { await App.RestartAfterUpdate() } catch (e: unknown) { updateApplyError.value = errorText(e) }
}
