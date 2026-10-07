<script setup lang="ts">
import { onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { EventsOn } from './wailsjs/runtime/runtime'
import LauncherView from './views/LauncherView.vue'
import AppShell from './components/AppShell.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import NewWorkspaceDialog from './components/NewWorkspaceDialog.vue'
import TakeOverDialog from './components/TakeOverDialog.vue'
import BundleExportDialog from './components/BundleExportDialog.vue'
import BundleImportDialog from './components/BundleImportDialog.vue'
import { openBundleImport } from './composables/useBundle'
import { confirmVisible, confirmTitle, confirmMessage, confirmVariant, confirmLabel, confirmAltLabel, confirmAltDestructive, resolveConfirm, resolveConfirmChoice } from './composables/useConfirm'
import { flushAll } from './composables/usePendingSaves'
import { askConfirm } from './composables/useConfirm'
import {
  workspaceOpen, workspaceInfo, startWorkspaceTracking, syncWorkspaceState, openWorkspacePath, openFolderDialog,
  launcherNotice, appNotice, newWorkspaceOpen, switcherOpen,
} from './composables/useWorkspace'

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
async function handleBeforeClose() {
  const res = await Promise.race([
    flushAll(),
    new Promise<{ failed: { label: string; error: string }[] }>((resolve) => setTimeout(() => resolve({ failed: [] }), BEFORE_CLOSE_TIMEOUT_MS)),
  ])
  if (res.failed.length > 0) {
    // An edit that could not be written (say, the file changed on disk and the
    // conflict is unresolved) must not vanish silently with the window.
    // A frontend that can ask a question is not hung: stop the quit watchdog so
    // it cannot quit the app while the person is deciding.
    try { await window.go.main.App.HoldQuit() } catch { /* an older backend */ }
    const quit = await askConfirm({
      title: 'Unsaved changes',
      message: `${res.failed[0].label} couldn't be saved: ${res.failed[0].error}. Quit anyway?`,
      variant: 'destructive',
      confirmLabel: 'Quit anyway',
    })
    if (!quit) {
      try { await window.go.main.App.CancelQuit() } catch { /* nothing to undo */ }
      return
    }
  }
  try {
    await window.go.main.App.Quit()
  } catch {
    // Nothing left to do — the window is on its way down either way.
  }
}

// Workspace shortcuts work on the launcher too. F5 is intercepted, or the
// WebView reloads the whole app (and every unsaved edit with it).
function onKeydown(e: KeyboardEvent) {
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

interface OpenRequest { path?: string; message?: string; bundlePath?: string }

// handleOpenRequest acts on something the OS or command line asked Vibe to open:
// a folder opens as a workspace, a .vibe file opens the bundle Import dialog,
// anything else shows the backend's plain-language note.
async function handleOpenRequest(req: OpenRequest | null | undefined) {
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

let unsubBeforeClose: (() => void) | null = null
let unsubOpenPath: (() => void) | null = null

// A different workspace is a different place: land on Home, not on whatever
// route (and notebook id) the old one left behind. A reload that merely picks
// up the open workspace, or a take-over of the same one, keeps its route.
const router = useRouter()
watch(() => workspaceInfo.value?.root, (now, before) => {
  // Different workspace, or closed (so the next open starts at Home too). A
  // switch never nulls the info in between, so it is the first case.
  if (before && now !== before) void router.push('/')
})

onMounted(async () => {
  startWorkspaceTracking()
  await syncWorkspaceState()
  window.addEventListener('keydown', onKeydown)
  // Listen before asking for the launch state, so a request during a slow
  // first open is not lost (macOS Finder, or a second launch).
  unsubOpenPath = EventsOn('app:open-path', (req: OpenRequest) => { void handleOpenRequest(req) })
  if (!workspaceOpen.value) {
    // A folder or .vibe bundle passed on the command line (`vibe studio --gui <dir>`,
    // or the OS opening one with Vibe before the window was ready).
    try {
      await handleOpenRequest(await window.go.main.App.GetInitialWorkspace())
    } catch {
      // ignore
    }
  } else {
    // Still tells the backend it can send open requests as events.
    try { await window.go.main.App.GetInitialWorkspace() } catch { /* ignore */ }
  }
  // EventsOn returns an unsubscribe function scoped to THIS listener; the global
  // EventsOff(name) would remove every listener for the event name.
  unsubBeforeClose = EventsOn('app:before-close', handleBeforeClose)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  unsubBeforeClose?.()
  unsubOpenPath?.()
})
</script>

<template>
  <div class="sh" style="height: 100vh; width: 100vw; background: var(--sh-paper); color: var(--sh-ink);">
    <AppShell v-if="workspaceOpen" />
    <LauncherView v-else />

    <div v-if="appNotice" class="banner warn app-notice" role="status" data-testid="app-notice">
      <div class="body"><div class="sub" style="margin: 0;">{{ appNotice }}</div></div>
      <button class="btn" type="button" @click="appNotice = ''">Dismiss</button>
    </div>

    <NewWorkspaceDialog />
    <TakeOverDialog />
    <BundleExportDialog />
    <BundleImportDialog />

    <!-- Shared promise-based confirm (composables/useConfirm.ts) — the ONE
         ConfirmDialog every askConfirm() caller in the app resolves through,
         including non-component call sites like lib/markdown.ts's link
         click handler. -->
    <ConfirmDialog
      :visible="confirmVisible"
      :title="confirmTitle"
      :message="confirmMessage"
      :variant="confirmVariant"
      :confirm-label="confirmLabel"
      :alt-label="confirmAltLabel || undefined"
      :alt-destructive="confirmAltDestructive"
      @confirm="resolveConfirm(true)"
      @cancel="resolveConfirm(false)"
      @alt="resolveConfirmChoice('alt')"
    />
  </div>
</template>

<style>
.app-notice {
  position: fixed;
  left: 50%;
  bottom: 24px;
  transform: translateX(-50%);
  max-width: min(640px, calc(100vw - 32px));
  z-index: 60;
  display: flex;
  align-items: center;
  gap: 12px;
}
</style>
