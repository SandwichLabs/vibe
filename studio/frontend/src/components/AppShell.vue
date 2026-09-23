<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { flushAll } from '../composables/usePendingSaves'
import { askConfirm } from '../composables/useConfirm'
import Icon from './Icon.vue'
import AgentPanel from './AgentPanel.vue'

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
  saving.value = true
  try {
    await window.go.main.App.SaveFile()
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

let unsubFileOpened: (() => void) | null = null

onMounted(() => {
  loadFileInfo()
  window.addEventListener('vibe:open-agent', openAgentFromEvent)
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
          @click="agentOpen = !agentOpen"
        >
          <Icon name="bot" :size="16" /> Agent
        </button>
        <span class="avatar-sq">VS</span>
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
        <button @click="save" :disabled="saving" class="btn btn-primary" style="width: 100%; justify-content: center;">
          <Icon name="save" :size="16" />
          {{ saving ? 'Saving…' : 'Save' }}
        </button>
        <button @click="closeFile" data-testid="close-file" class="btn btn-inverted" style="width: 100%; justify-content: center;">
          Close File
        </button>
      </div>
    </aside>

    <!-- Main content -->
    <main style="overflow: hidden; background: var(--sh-paper); min-width: 0;">
      <router-view />
    </main>

    <AgentPanel :open="agentOpen" @close="agentOpen = false" />

    <div v-if="errorMessage" class="error-toast">{{ errorMessage }}</div>
  </div>
</template>
