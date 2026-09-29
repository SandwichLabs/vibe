// useFileDirty — a module-singleton mirror of the open file's dirty state
// (#81): DuckDB table changes since the last Save. Backend of record is
// Container.Dirty() (studio.App.IsDirty/MarkDirty); this composable just
// keeps a shared `fileDirty` ref in sync with it via the "file:dirty" event
// so AppShell (sidebar pill, Save button, window title) and the app-quit
// flow (App.vue) read the same value without each polling the binding.
import { ref } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { askChoice, askConfirm } from './useConfirm'

export const fileDirty = ref(false)

const hasWailsRuntime = () => typeof window !== 'undefined' && !!(window as any).runtime

let started = false

// startFileDirtyTracking wires the shared listeners exactly once per page
// load (idempotent — AppShell and App.vue may both call it). Subscribes
// "file:dirty" (the authoritative edge notification), resets to false on
// "file:opened"/"file:closed" (a fresh or closed file starts/ends clean),
// and calls IsDirty() once to sync in case this session's listeners
// attached after the file was already dirty (e.g. a reload mid-session).
export function startFileDirtyTracking(): void {
  if (started || !hasWailsRuntime()) return
  started = true

  EventsOn('file:dirty', (d: boolean) => { fileDirty.value = !!d })
  EventsOn('file:opened', () => { fileDirty.value = false })
  EventsOn('file:closed', () => { fileDirty.value = false })

  window.go.main.App.IsDirty().then((d) => { fileDirty.value = d }).catch(() => { /* no file open yet */ })
}

// _resetForTests clears the module singleton (the `started` guard and the
// shared ref) between component tests that each mount their own AppShell
// against a fresh mocked runtime — mirrors usePendingSaves' own
// _resetForTests. Not used in production.
export function _resetForTests(): void {
  started = false
  fileDirty.value = false
}

export type SaveChoice = 'save' | 'discard' | 'cancel'

// askSaveChanges shows the one shared "Save changes to <file>?" prompt used
// by closeWithPrompt below, so the copy never drifts between Close and
// quit. "Don't save" is styled destructive — it's the choice that discards data, even though "Save" is the
// (primary) confirm button here.
export function askSaveChanges(fileName: string): Promise<SaveChoice> {
  return askChoice({
    title: `Save changes to ${fileName}?`,
    message: "Table changes since your last save will be lost if you don't save. Notebooks, saved queries and skills are already saved.",
    confirmLabel: 'Save',
    altLabel: "Don't save",
    altDestructive: true,
    variant: 'primary',
  }).then((result) => {
    if (result === 'confirm') return 'save'
    if (result === 'alt') return 'discard'
    return 'cancel'
  })
}

// closeWithPrompt is the shared "close this file, prompting if it's dirty"
// logic. Callers pass
// the already-derived display name (AppShell has it from GetFileInfo;
// App.vue derives it the same way) and keep only their own reaction:
// AppShell shows a toast and emits 'file-closed'; App.vue calls Quit() or
// CancelQuit(). IsDirty() is read fresh here (authoritative at call time,
// not whatever `fileDirty` last mirrored) for both the prompt-or-not
// decision and the actual close. Resolves false on Cancel, true once the
// file is closed (saved or discarded), and REJECTS if the CloseFile/
// CloseFileWithoutSaving binding itself throws — callers decide how to
// surface that (AppShell's toast; App.vue's error dialog + CancelQuit).
export async function closeWithPrompt(fileName: string): Promise<boolean> {
  const dirty = await window.go.main.App.IsDirty()
  if (!dirty) {
    await window.go.main.App.CloseFile()
    return true
  }
  const choice = await askSaveChanges(fileName)
  if (choice === 'cancel') return false
  if (choice === 'save') await window.go.main.App.CloseFile()
  else await window.go.main.App.CloseFileWithoutSaving()
  return true
}

// revertWithPrompt shows the shared destructive confirm for "Revert to last
// save" (#98) and, on confirm, calls the RevertFile binding — the copy lives
// here, next to askSaveChanges, so the two related prompts don't drift.
// Resolves false on Cancel without touching the file. Resolves true after
// RevertFile succeeds; `fileDirty` is cleared by the "file:dirty" false
// event RevertFile emits, not set locally here. Rejects if RevertFile
// itself throws — the caller (AppShell) toasts.
export async function revertWithPrompt(): Promise<boolean> {
  const confirmed = await askConfirm({
    title: 'Discard unsaved table changes?',
    message: 'Tables go back to how they were at your last save. Notebooks, saved queries, skills and settings are saved as you go and stay as they are.',
    confirmLabel: 'Discard changes',
    variant: 'destructive',
  })
  if (!confirmed) return false
  await window.go.main.App.RevertFile()
  return true
}
