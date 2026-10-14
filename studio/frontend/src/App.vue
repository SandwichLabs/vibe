<script setup lang="ts">
import { App } from './lib/bindings'
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
import UpdatePrompt from './components/UpdatePrompt.vue'
import { startupUpdateCheck, refreshUpdateState, INSTALLING_NOTICE } from './composables/useUpdates'
import { confirmVisible, confirmTitle, confirmMessage, confirmVariant, confirmLabel, confirmAltLabel, confirmAltDestructive, resolveConfirm, resolveConfirmChoice } from './composables/useConfirm'
import { handleBeforeClose, handleOpenRequest, onKeydown, type OpenRequest } from './composables/useAppEvents'
import { workspaceOpen, workspaceInfo, startWorkspaceTracking, syncWorkspaceState, launching, appNotice } from './composables/useWorkspace'

let unsubBeforeClose: (() => void) | null = null
let unsubOpenPath: (() => void) | null = null
let unsubInstalling: (() => void) | null = null

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
  try {
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
        await handleOpenRequest(await App.GetInitialWorkspace())
      } catch {
        // ignore
      } finally {
        // Handled, whatever happened: a later reload must not reopen it (#33).
        try { await App.MarkLaunchHandled() } catch { /* ignore */ }
      }
    } else {
      launching.value = false
      // Still tells the backend it can send open requests as events, and that
      // the launch target (already satisfied by the open workspace) is spent.
      try { await App.GetInitialWorkspace(); await App.MarkLaunchHandled() } catch { /* ignore */ }
    }
    // EventsOn returns an unsubscribe function scoped to THIS listener; the global
    // EventsOff(name) would remove every listener for the event name.
    unsubBeforeClose = EventsOn('app:before-close', handleBeforeClose)
    // A quit is held while an update installs (the installer must not be orphaned).
    unsubInstalling = EventsOn('app:update-installing', () => { appNotice.value = INSTALLING_NOTICE })
  } catch (e) {
    // No Wails runtime (or a failed first sync): the Launcher still has to show.
    console.warn('startup', e)
  } finally {
    // Whatever failed above, never leave the page on the blank placeholder.
    launching.value = false
  }
  // After the launcher or a workspace is up: the one-time question (needsConsent) and, if the
  // person chose weekly, the quiet startup check. Never blocks, never throws.
  void refreshUpdateState().then(startupUpdateCheck)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  unsubBeforeClose?.()
  unsubOpenPath?.()
  unsubInstalling?.()
})
</script>

<template>
  <div class="sh" style="height: 100vh; width: 100vw; background: var(--sh-paper); color: var(--sh-ink);">
    <AppShell v-if="workspaceOpen" />
    <LauncherView v-else-if="!launching" />
    <div v-else data-testid="launching" aria-busy="true" />

    <div v-if="appNotice" class="banner warn app-notice" role="status" data-testid="app-notice">
      <div class="body"><div class="sub" style="margin: 0;">{{ appNotice }}</div></div>
      <button class="btn" type="button" @click="appNotice = ''">Dismiss</button>
    </div>

    <NewWorkspaceDialog />
    <TakeOverDialog />
    <BundleExportDialog />
    <BundleImportDialog />
    <UpdatePrompt v-if="!launching" />

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
/* It floats over content: lay the translucent warn tint over an opaque paper base. */
.app-notice.banner.warn {
  background: linear-gradient(var(--sh-marigold-8), var(--sh-marigold-8)), var(--sh-paper);
}
</style>
