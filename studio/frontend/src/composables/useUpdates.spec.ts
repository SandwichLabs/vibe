// useUpdates — ask once, weekly check is quiet, Check now shows its error, install needs a confirmation.
import { describe, it, expect, beforeEach, vi } from 'vitest'

import {
  updateState, updateResult, updateError, updateSettingsError, updateNotice, updateApplied, updateApplyError, showConsent, showNotice,
  resetUpdates, answerUpdateConsent, startupUpdateCheck, checkUpdateNow, skipUpdate, installUpdate, restartAfterUpdate,
  dismissNotice, putOffConsent, setUpdateCheck,
} from './useUpdates'
import { confirmVisible, resolveConfirmChoice } from './useConfirm'

const STATE: UpdateState = { check: '', lastChecked: '', skippedVersion: '', version: 'v0.1.1', isDev: false, needsConsent: true, managed: 'installer', updatable: true, managedNote: '', appliedVersion: '', canRestart: false }
const RESULT: UpdateResult = { current: 'v0.1.1', latest: 'v0.1.2', updateAvailable: true, skipped: false, releaseUrl: 'https://github.com/SandwichLabs/vibe/releases/tag/v0.1.2', notes: 'Fixes.', managed: 'installer', updatable: true, managedNote: '', checkedAt: '2026-10-13T00:00:00Z' }

function makeApp(over: Partial<Record<string, any>> = {}) {
  let state = { ...STATE }
  const app = {
    GetUpdateState: vi.fn(async () => state),
    SetUpdateCheck: vi.fn(async (m: string) => { state = { ...state, check: m as UpdateState['check'], needsConsent: false } }),
    CheckForUpdate: vi.fn(async () => RESULT),
    AutoCheckForUpdate: vi.fn(async (): Promise<UpdateResult | null> => RESULT),
    SkipUpdate: vi.fn(async () => {}),
    ApplyUpdate: vi.fn(async (): Promise<UpdateApplyResult> => ({ from: '0.1.1', to: '0.1.2', output: 'ok', studio: true, restart: '/x/vibe-studio' })),
    RestartAfterUpdate: vi.fn(async () => {}),
    ...over,
  }
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {}
  return app
}

describe('useUpdates', () => {
  beforeEach(() => { resetUpdates(); delete (window as any).runtime })

  it('asks once: either answer is saved and the prompt goes away', async () => {
    const app = makeApp()
    expect(showConsent.value).toBe(false)
    updateState.value = { ...STATE }
    expect(showConsent.value).toBe(true)
    await answerUpdateConsent('off')
    expect(app.SetUpdateCheck).toHaveBeenCalledWith('off')
    expect(showConsent.value).toBe(false)
    expect(app.AutoCheckForUpdate).not.toHaveBeenCalled()

    resetUpdates()
    await answerUpdateConsent('weekly')
    expect(app.SetUpdateCheck).toHaveBeenLastCalledWith('weekly')
    expect(app.AutoCheckForUpdate).toHaveBeenCalled()
  })

  it('"Ask later" hides the prompt for this session without saving anything', async () => {
    const app = makeApp()
    updateState.value = { ...STATE }
    putOffConsent()
    expect(showConsent.value).toBe(false)
    expect(app.SetUpdateCheck).not.toHaveBeenCalled()
  })

  it('the startup check does nothing unless the person chose weekly', async () => {
    const app = makeApp()
    await startupUpdateCheck()
    expect(app.AutoCheckForUpdate).not.toHaveBeenCalled()
    await app.SetUpdateCheck('off')
    await startupUpdateCheck()
    expect(app.AutoCheckForUpdate).not.toHaveBeenCalled()
  })

  it('a weekly check that finds a release raises a notice; a skipped one stays quiet', async () => {
    const app = makeApp()
    await app.SetUpdateCheck('weekly')
    await startupUpdateCheck()
    expect(updateNotice.value?.latest).toBe('v0.1.2')
    expect(showNotice.value).toBe(true)
    dismissNotice()
    expect(showNotice.value).toBe(false)

    resetUpdates()
    app.AutoCheckForUpdate.mockResolvedValue({ ...RESULT, skipped: true })
    await startupUpdateCheck()
    expect(updateNotice.value).toBeNull()
    expect(updateResult.value?.skipped).toBe(true)
  })

  it('a failed weekly check is silent; null means none was due', async () => {
    const app = makeApp()
    await app.SetUpdateCheck('weekly')
    app.AutoCheckForUpdate.mockRejectedValue(new Error('offline'))
    await startupUpdateCheck()
    expect(updateError.value).toBe('')
    expect(updateNotice.value).toBeNull()
    app.AutoCheckForUpdate.mockResolvedValue(null)
    await startupUpdateCheck()
    expect(updateResult.value).toBeNull()
  })

  it('Check now shows its result and its error', async () => {
    const app = makeApp()
    await checkUpdateNow()
    expect(updateResult.value?.latest).toBe('v0.1.2')
    app.CheckForUpdate.mockRejectedValue(new Error('failed to look up the latest release: offline'))
    await checkUpdateNow()
    expect(updateResult.value).toBeNull()
    expect(updateError.value).toBe('offline')
  })

  it('a failed save is reported apart from a failed check', async () => {
    const app = makeApp({ SetUpdateCheck: vi.fn(async () => { throw new Error('read-only config') }) })
    await setUpdateCheck(true)
    expect(updateSettingsError.value).toBe('read-only config')
    expect(updateError.value).toBe('')
    expect(app.SetUpdateCheck).toHaveBeenCalled()
  })

  it('setUpdateCheck maps the toggle to weekly/off', async () => {
    const app = makeApp()
    await setUpdateCheck(true)
    expect(app.SetUpdateCheck).toHaveBeenLastCalledWith('weekly')
    await setUpdateCheck(false)
    expect(app.SetUpdateCheck).toHaveBeenLastCalledWith('off')
  })

  it('skipping marks the shown result skipped', async () => {
    const app = makeApp()
    await checkUpdateNow()
    await skipUpdate('v0.1.2')
    expect(app.SkipUpdate).toHaveBeenCalledWith('v0.1.2')
    expect(updateResult.value?.skipped).toBe(true)
  })

  it('installing asks first; Cancel installs nothing, Install applies', async () => {
    const app = makeApp()
    const p = installUpdate('v0.1.2')
    await Promise.resolve()
    expect(confirmVisible.value).toBe(true)
    resolveConfirmChoice('cancel')
    await p
    expect(app.ApplyUpdate).not.toHaveBeenCalled()

    const q = installUpdate('v0.1.2')
    await Promise.resolve()
    resolveConfirmChoice('confirm')
    await q
    expect(app.ApplyUpdate).toHaveBeenCalledWith('v0.1.2')
    expect(updateApplied.value?.to).toBe('0.1.2')
  })

  it('an install failure is kept for display', async () => {
    const app = makeApp()
    app.ApplyUpdate.mockRejectedValue(new Error('install.sh does not match the release\'s checksums.txt; refusing to run it'))
    const p = installUpdate('v0.1.2')
    await Promise.resolve()
    resolveConfirmChoice('confirm')
    await p
    expect(updateApplyError.value).toContain('refusing to run it')
    expect(updateApplied.value).toBeNull()
  })

  it('Restart now calls the quit-through-normal-path binding', async () => {
    const app = makeApp()
    await restartAfterUpdate()
    expect(app.RestartAfterUpdate).toHaveBeenCalled()
  })
})
