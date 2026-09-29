<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { EventsOn } from './wailsjs/runtime/runtime'
import UnlockView from './views/UnlockView.vue'
import AppShell from './components/AppShell.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import { confirmVisible, confirmTitle, confirmMessage, confirmVariant, confirmLabel, confirmAltLabel, confirmAltDestructive, resolveConfirm, resolveConfirmChoice, askConfirm } from './composables/useConfirm'
import { startFileDirtyTracking, closeWithPrompt } from './composables/useFileDirty'
import { flushAll } from './composables/usePendingSaves'
import { errorText } from './lib/errors'

const isOpen = ref(false)
const initialFile = ref('')

async function checkFileOpen() {
  try {
    isOpen.value = await window.go.main.App.IsFileOpen()
  } catch {
    isOpen.value = false
  }
}

async function loadInitialFile() {
  try {
    const f = await window.go.main.App.GetInitialFile()
    if (f && !isOpen.value) {
      initialFile.value = f
    }
  } catch {
    // ignore
  }
}

function handleFileOpened() {
  isOpen.value = true
}

async function handleFileClosed() {
  isOpen.value = false
}

// beforeCloseTimeoutMs bounds how long the quit flow waits for
// flushAll() — a hung frontend (a stuck save, a broken promise) must never
// be able to block the app from quitting forever (review fix, step 4a
// round 2 #1).
const BEFORE_CLOSE_TIMEOUT_MS = 3000

// Wails' OnBeforeClose (studio/main.go's app.beforeClose) intercepts the
// FIRST window-close/quit attempt and emits this event instead of letting
// the process exit, so every view with a pending debounced save gets a
// chance to flush before the container (and the process) goes away. Once
// flushed (or the timeout fires):
//   - a clean file (payload.dirty false) calls Quit() straight away, as
//     before, which sets a server-side flag so the NEXT OnBeforeClose lets
//     the close through;
//   - a dirty file (#81) goes through the SAME closeWithPrompt AppShell's
//     Close button uses (useFileDirty.ts, required item 5): Cancel calls
//     CancelQuit() so the NEXT attempt prompts again from scratch; Save/
//     Don't save close the container (releasing it BEFORE Quit(), same as
//     AppShell.closeFile) and then Quit().
async function handleBeforeClose(payload?: { dirty?: boolean }) {
  await Promise.race([
    flushAll(),
    new Promise<void>((resolve) => setTimeout(resolve, BEFORE_CLOSE_TIMEOUT_MS)),
  ])

  if (payload?.dirty) {
    let fileName = 'this file'
    try {
      const info = await window.go.main.App.GetFileInfo()
      fileName = info?.path?.split('/').pop() || fileName
    } catch { /* fall back to the generic name */ }

    try {
      const closed = await closeWithPrompt(fileName)
      if (!closed) {
        try { await window.go.main.App.CancelQuit() } catch { /* nothing left to do */ }
        return
      }
    } catch (e: any) {
      // The close itself failed (e.g. a save error) — cancel the quit
      // rather than proceeding to Quit() with the container still open in
      // an unknown state.
      await askConfirm({ title: 'Could not close the file', message: errorText(e), confirmLabel: 'OK', variant: 'primary' })
      try { await window.go.main.App.CancelQuit() } catch { /* nothing left to do */ }
      return
    }
  }

  try {
    await window.go.main.App.Quit()
  } catch {
    // Nothing left to do — the window is on its way down either way.
  }
}

let unsubFileOpened: (() => void) | null = null
let unsubFileClosed: (() => void) | null = null
let unsubBeforeClose: (() => void) | null = null

onMounted(async () => {
  await checkFileOpen()
  await loadInitialFile()
  startFileDirtyTracking()
  // Review fix, step 4a round 2 #2: EventsOn returns an unsubscribe
  // function scoped to THIS listener — the global EventsOff(name) this
  // used to call removes EVERY listener registered for that event name,
  // including ones other views (e.g. NotebookView) register for the same
  // 'file:closed' event, silently breaking their own cleanup.
  unsubFileOpened = EventsOn('file:opened', handleFileOpened)
  unsubFileClosed = EventsOn('file:closed', handleFileClosed)
  unsubBeforeClose = EventsOn('app:before-close', handleBeforeClose)
})

onUnmounted(() => {
  unsubFileOpened?.()
  unsubFileClosed?.()
  unsubBeforeClose?.()
})
</script>

<template>
  <div class="sh" style="height: 100vh; width: 100vw; background: var(--sh-paper); color: var(--sh-ink);">
    <UnlockView v-if="!isOpen" :initial-file="initialFile" @file-opened="handleFileOpened" />
    <AppShell v-else @file-closed="handleFileClosed" />

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
