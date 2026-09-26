<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { flushAll } from '../composables/usePendingSaves'
import { askConfirm } from '../composables/useConfirm'
import Icon from './Icon.vue'
import AgentPanel from './AgentPanel.vue'
import { useTheme } from '../theme/useTheme'
import { shortcutLabel } from '../composables/useModal'

const theme = useTheme()
const savedFlash = ref(false)

// Agent wizard panel — a slide-in chat available across every screen.
const agentOpen = ref(false)

const emit = defineEmits<{ 'file-closed': [] }>()
const route = useRoute()

const fileInfo = ref<FileInfo | null>(null)
const saving = ref(false)
const errorMessage = ref('')

// #68: Studio is the only mode. The App Mode / Access-application chrome
// (locked switchboard shell, mode toggle, unlock entrypoint) is gone —
// this is now a single always-on builder shell.
const navItems = [
  { path: '/', label: 'Dashboard', icon: 'home' },
  { path: '/notebooks', label: 'Notebooks', icon: 'layers' },
  { path: '/tables', label: 'Tables', icon: 'database' },
  { path: '/queries', label: 'Queries', icon: 'terminal' },
  { path: '/skills', label: 'Skills', icon: 'bookOpen' },
  { path: '/agent', label: 'Agent', icon: 'bot' },
  { path: '/import', label: 'Import', icon: 'download' },
  { path: '/settings', label: 'Settings', icon: 'settings' },
]

const fileName = ref('Untitled')

function isActive(path: string) {
  return route.path === path || (path !== '/' && route.path.startsWith(path))
}

async function loadFileInfo() {
  try {
    fileInfo.value = await window.go.main.App.GetFileInfo()
    fileName.value = fileInfo.value?.path ? (fileInfo.value.path.split('/').pop() || 'Untitled') : 'Untitled'
  } catch { /* ignore */ }
}

async function save() {
  if (saving.value) return
  saving.value = true
  try {
    await window.go.main.App.SaveFile()
    savedFlash.value = true
    setTimeout(() => { savedFlash.value = false }, 1500)
  } catch (e: any) {
    errorMessage.value = 'Save failed: ' + (e.message || e)
  } finally {
    saving.value = false
  }
}

async function closeFile() {
  try {
    // Every registered pending-save flush must complete FIRST, while the
    // container is still open, so a real edit is never silently lost.
    const { failed } = await flushAll()
    if (failed.length > 0) {
      const first = failed[0]
      const proceed = await askConfirm({
        title: 'Unsaved notebook',
        message: `A notebook couldn't be saved: ${first.error}. Close anyway?`,
        variant: 'destructive',
      })
      if (!proceed) return
    }
    await window.go.main.App.CloseFile()
    emit('file-closed')
  } catch (e: any) {
    errorMessage.value = 'Close failed: ' + (e.message || e)
  }
}

// Views (e.g. the Query Explorer's "Ask agent" button) open the wizard panel
// via this window event — the shell owns the panel, so this is the one
// cross-component trigger.
function openAgentFromEvent() {
  agentOpen.value = true
}

const hasWailsRuntime = () => typeof window !== 'undefined' && !!(window as any).runtime

// Global shortcuts: Ctrl/Cmd+S saves the file from anywhere (the sidebar
// button was the only way); Ctrl/Cmd+J toggles the agent drawer; Escape
// closes it when nothing else (a modal, an editor) claims the key first.
function onGlobalKeydown(e: KeyboardEvent) {
  const mod = e.metaKey || e.ctrlKey
  if (mod && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 's') {
    e.preventDefault()
    save()
    return
  }
  if (mod && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'j') {
    e.preventDefault()
    agentOpen.value = !agentOpen.value
    return
  }
  if (e.key === 'Escape' && agentOpen.value && !e.defaultPrevented) {
    agentOpen.value = false
  }
}

let unsubFileOpened: (() => void) | null = null

onMounted(() => {
  loadFileInfo()
  window.addEventListener('vibe:open-agent', openAgentFromEvent)
  window.addEventListener('keydown', onGlobalKeydown)
  // The sidebar's "Open file" label was captured once at mount and never
  // refreshed — stale if the open file changes underneath this
  // already-mounted shell (e.g. a direct OpenFile/CreateFile/
  // CreateFromTemplate binding call bypassing UnlockView, which is the same
  // class of call the file-identity-token fix in NotebookView/SaveNotebook
  // guards the data path against). Re-reading on every "file:opened" event
  // keeps it honest regardless of how the switch happened.
  if (hasWailsRuntime()) unsubFileOpened = EventsOn('file:opened', loadFileInfo)
})

onUnmounted(() => {
  window.removeEventListener('vibe:open-agent', openAgentFromEvent)
  window.removeEventListener('keydown', onGlobalKeydown)
  unsubFileOpened?.()
})

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
</script>

<template>
  <div class="app">
    <div class="topbar" style="height: 64px;">
      <div class="brand">
        <span class="mark">V</span>
        <span class="word">Vibe<span class="last">Studio</span></span>
      </div>
      <div class="topright" style="gap: 10px;">
        <span class="pill"><span class="dot ok"></span>Local · Encrypted</span>
        <button
          class="btn btn-inverted agent-toggle"
          data-testid="agent-toggle"
          :aria-expanded="agentOpen"
          aria-controls="agent-panel"
          :title="`Toggle agent · ${shortcutLabel('J')}`"
          @click="agentOpen = !agentOpen"
        >
          <Icon name="bot" :size="16" /> Agent
        </button>
        <button
          class="theme-toggle"
          data-testid="theme-toggle"
          :aria-label="theme.activeTheme.value.type === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'"
          :title="`${theme.activeTheme.value.name} · click to switch`"
          @click="theme.toggleDark()"
        >
          <Icon :name="theme.activeTheme.value.type === 'dark' ? 'sun' : 'moon'" :size="16" />
        </button>
      </div>
    </div>

    <!-- Sidebar (builder nav). -->
    <aside class="sidebar" style="display: flex; flex-direction: column;">
      <div class="side-section">Workspace</div>
      <router-link
        v-for="item in navItems"
        :key="item.path"
        :to="item.path"
        class="side-link"
        :class="{ active: isActive(item.path) }"
      >
        <Icon :name="item.icon" :size="18" />
        <span>{{ item.label }}</span>
      </router-link>

      <div style="flex: 1;"></div>

      <div class="nav-pill">
        <span class="label"><span class="dot"></span>Open file</span>
        <span class="url">{{ fileName }}</span>
      </div>

      <div style="padding: 0 16px 16px; display: flex; flex-direction: column; gap: 8px;">
        <button @click="save" :disabled="saving" class="btn btn-primary" style="width: 100%; justify-content: center;" :title="`Save file · ${shortcutLabel('S')}`">
          <Icon :name="savedFlash ? 'check' : 'save'" :size="16" />
          {{ saving ? 'Saving…' : (savedFlash ? 'Saved' : 'Save') }}
        </button>
        <button @click="closeFile" data-testid="close-file" class="btn btn-ghost" style="width: 100%; justify-content: center;">
          Close file
        </button>
      </div>
    </aside>

    <!-- Main content -->
    <main style="overflow: hidden; background: var(--sh-paper); min-width: 0;">
      <router-view />
    </main>

    <AgentPanel :open="agentOpen" @close="agentOpen = false" />

    <div v-if="errorMessage" class="error-toast" role="alert">{{ errorMessage }}</div>
  </div>
</template>

<style scoped>
.theme-toggle {
  width: 32px; height: 32px;
  display: inline-flex; align-items: center; justify-content: center;
  background: transparent;
  color: var(--vibe-chrome-fg);
  border: 1.5px solid var(--vibe-chrome-fg);
  border-radius: var(--sh-radius-sm);
  cursor: pointer;
}
.theme-toggle:hover { background: color-mix(in srgb, var(--vibe-chrome-fg) 12%, transparent); }
</style>
