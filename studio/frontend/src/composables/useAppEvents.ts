/**
 * useAppEvents — what the app root does when the shell or the OS asks for
 * something (F10): the quit flow (flush every pending save, ask before losing one),
 * the workspace shortcuts that work on the launcher too, and an open request
 * (a folder or .vibe passed on the command line, or by the OS).
 */
import { App } from '../lib/bindings'
import { openBundleImport } from './useBundle'
import { askConfirm } from './useConfirm'
import { flushAll } from './usePendingSaves'
import {
  workspaceOpen, openWorkspacePath, openFolderDialog, launcherNotice, appNotice, newWorkspaceOpen, switcherOpen,
} from './useWorkspace'

// beforeCloseTimeoutMs bounds how long the quit flow waits for flushAll() — a
// hung frontend (a stuck save, a broken promise) must never be able to block
// the app from quitting forever.
const BEFORE_CLOSE_TIMEOUT_MS = 3000

// Wails' OnBeforeClose (studio/main.go's app.beforeClose) intercepts the FIRST
// window-close/quit attempt and emits this event instead of letting the process
// exit, so every view with a pending debounced save (an open notebook) gets a
// chance to flush before the workspace (and the process) goes away. Then Quit()
// sets a server-side flag so the NEXT close attempt goes through. There is no
// Save / Don't save prompt any more: data writes go straight to the connection.
export async function handleBeforeClose() {
  const res = await Promise.race([
    flushAll(),
    new Promise<{ failed: { label: string; error: string }[] }>((resolve) => setTimeout(() => resolve({ failed: [] }), BEFORE_CLOSE_TIMEOUT_MS)),
  ])
  if (res.failed.length > 0) {
    // An edit that could not be written (say, the file changed on disk and the
    // conflict is unresolved) must not vanish silently with the window.
    // A frontend that can ask a question is not hung: stop the quit watchdog so
    // it cannot quit the app while the person is deciding.
    try { await App.HoldQuit() } catch { /* an older backend */ }
    const quit = await askConfirm({
      title: 'Unsaved changes',
      message: `${res.failed[0].label} couldn't be saved: ${res.failed[0].error}. Quit anyway?`,
      variant: 'destructive',
      confirmLabel: 'Quit anyway',
    })
    if (!quit) {
      try { await App.CancelQuit() } catch { /* nothing to undo */ }
      return
    }
  }
  try {
    await App.Quit()
  } catch {
    // Nothing left to do — the window is on its way down either way.
  }
}

// Workspace shortcuts work on the launcher too. F5 is intercepted, or the
// WebView reloads the whole app (and every unsaved edit with it).
export function onKeydown(e: KeyboardEvent) {
  if (e.key === 'F5') {
    e.preventDefault()
    return
  }
  const mod = e.metaKey || e.ctrlKey
  if (!mod || e.altKey) return
  const k = e.key.toLowerCase()
  if (k === 'o' && e.shiftKey) {
    if (!workspaceOpen.value) return
    e.preventDefault()
    switcherOpen.value = !switcherOpen.value
  } else if (k === 'o' && !e.shiftKey) {
    e.preventDefault()
    void openFolderDialog()
  } else if (k === 'n' && !e.shiftKey) {
    e.preventDefault()
    newWorkspaceOpen.value = true
  }
}

export interface OpenRequest { path?: string; message?: string; bundlePath?: string }

// handleOpenRequest acts on something the OS or command line asked Vibe to open:
// a folder opens as a workspace, a .vibe file opens the bundle Import dialog,
// anything else shows the backend's plain-language note.
export async function handleOpenRequest(req: OpenRequest | null | undefined) {
  if (!req) return
  if (req.message) {
    if (workspaceOpen.value) appNotice.value = req.message
    else launcherNotice.value = req.message
  } else if (req.path) {
    const res = await openWorkspacePath(req.path)
    if (!res.ok && res.error) {
      if (workspaceOpen.value) appNotice.value = res.error
      else launcherNotice.value = res.error
    }
  } else if (req.bundlePath) {
    openBundleImport(workspaceOpen.value ? 'here' : 'new', req.bundlePath)
  }
}
