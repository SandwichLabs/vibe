<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'

const emit = defineEmits<{ 'file-closed': [] }>()
const router = useRouter()
const route = useRoute()

const fileInfo = ref<FileInfo | null>(null)
const saving = ref(false)
const errorMessage = ref('')

const navItems = [
  { path: '/', label: 'Dashboard', icon: '⊞' },
  { path: '/pages', label: 'Pages', icon: '⊟' },
  { path: '/tables', label: 'Tables', icon: '⊠' },
  { path: '/queries', label: 'Queries', icon: '⊡' },
  { path: '/import', label: 'Import', icon: '⊕' },
  { path: '/settings', label: 'Settings', icon: '⊗' },
]

const fileName = computed(() => {
  if (!fileInfo.value?.path) return 'Untitled'
  return fileInfo.value.path.split('/').pop() || 'Untitled'
})

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
  <div class="flex h-full">
    <!-- Sidebar -->
    <nav class="w-56 bg-black text-white flex flex-col border-r-3 border-black">
      <div class="p-4 border-b-3 border-yellow">
        <div class="font-bold text-lg text-yellow">Vibe Studio</div>
        <div class="text-xs text-muted truncate mt-1 font-mono">{{ fileName }}</div>
      </div>
      <div class="flex-1 py-1">
        <router-link
          v-for="item in navItems"
          :key="item.path"
          :to="item.path"
          class="flex items-center gap-3 px-4 py-2.5 text-sm font-medium transition-colors"
          :class="(route.path === item.path || (item.path !== '/' && route.path.startsWith(item.path)))
            ? 'bg-yellow text-black'
            : 'text-white/70 hover:bg-white/10 hover:text-white'"
        >
          <span class="text-base">{{ item.icon }}</span>
          <span>{{ item.label }}</span>
        </router-link>
      </div>
      <div class="p-3 border-t-3 border-yellow space-y-2">
        <button @click="save" :disabled="saving" class="btn btn-primary w-full text-center">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
        <button @click="closeFile" class="btn btn-inverted w-full text-center">
          Close File
        </button>
      </div>
    </nav>

    <!-- Main content -->
    <main class="flex-1 overflow-auto bg-cream">
      <router-view />
    </main>

    <div v-if="errorMessage" class="error-toast">
      {{ errorMessage }}
    </div>
  </div>
</template>
