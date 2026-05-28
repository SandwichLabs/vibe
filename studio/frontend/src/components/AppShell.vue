<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import Icon from './Icon.vue'

const emit = defineEmits<{ 'file-closed': [] }>()
const route = useRoute()

const fileInfo = ref<FileInfo | null>(null)
const saving = ref(false)
const errorMessage = ref('')

const navItems = [
  { path: '/', label: 'Dashboard', icon: 'home' },
  { path: '/pages', label: 'Pages', icon: 'fileText' },
  { path: '/tables', label: 'Tables', icon: 'database' },
  { path: '/queries', label: 'Queries', icon: 'terminal' },
  { path: '/import', label: 'Import', icon: 'download' },
  { path: '/settings', label: 'Settings', icon: 'settings' },
]

const fileName = computed(() => {
  if (!fileInfo.value?.path) return 'Untitled'
  return fileInfo.value.path.split('/').pop() || 'Untitled'
})

function isActive(path: string) {
  return route.path === path || (path !== '/' && route.path.startsWith(path))
}

async function loadFileInfo() {
  try {
    fileInfo.value = await window.go.main.App.GetFileInfo()
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
    await window.go.main.App.CloseFile()
    emit('file-closed')
  } catch (e: any) {
    errorMessage.value = 'Close failed: ' + (e.message || e)
  }
}

onMounted(loadFileInfo)

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
</script>

<template>
  <div class="app">
    <!-- Top bar -->
    <div class="topbar" style="height: 64px;">
      <div class="brand">
        <span class="mark">V</span>
        <span class="word">Vibe<span class="last">Studio</span></span>
      </div>
      <div class="topright" style="gap: 10px;">
        <span class="pill"><span class="dot studio"></span>Studio Mode</span>
        <span class="pill"><span class="dot ok"></span>Local · Encrypted</span>
        <span class="avatar-sq">VS</span>
      </div>
    </div>

    <!-- Sidebar -->
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
        <button @click="closeFile" class="btn btn-inverted" style="width: 100%; justify-content: center;">
          Close File
        </button>
      </div>
    </aside>

    <!-- Main content -->
    <main style="overflow: hidden; background: var(--sh-paper); min-width: 0;">
      <router-view />
    </main>

    <div v-if="errorMessage" class="error-toast">{{ errorMessage }}</div>
  </div>
</template>
