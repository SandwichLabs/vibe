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
  { path: '/agent', label: 'Agent', icon: '⊙' },
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
    <nav class="w-56 bg-gray-900 text-gray-300 flex flex-col">
      <div class="p-4 border-b border-gray-700">
        <div class="text-white font-bold text-lg">Vibe Studio</div>
        <div class="text-xs text-gray-500 truncate mt-1">{{ fileName }}</div>
      </div>
      <div class="flex-1 py-2">
        <router-link
          v-for="item in navItems"
          :key="item.path"
          :to="item.path"
          class="flex items-center gap-3 px-4 py-2.5 text-sm hover:bg-gray-800 hover:text-white transition-colors"
          :class="{ 'bg-gray-800 text-white': route.path === item.path || (item.path !== '/' && route.path.startsWith(item.path)) }"
        >
          <span class="text-base">{{ item.icon }}</span>
          <span>{{ item.label }}</span>
        </router-link>
      </div>
      <div class="p-3 border-t border-gray-700 space-y-2">
        <button @click="save" :disabled="saving" class="w-full py-1.5 px-3 bg-blue-600 text-white text-sm rounded hover:bg-blue-700 disabled:opacity-50">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
        <button @click="closeFile" class="w-full py-1.5 px-3 bg-gray-700 text-gray-300 text-sm rounded hover:bg-gray-600">
          Close File
        </button>
      </div>
    </nav>

    <!-- Main content -->
    <main class="flex-1 overflow-auto bg-gray-50">
      <router-view />
    </main>

    <div v-if="errorMessage" class="fixed bottom-4 right-4 bg-red-600 text-white px-4 py-2 rounded-lg shadow-lg text-sm z-50">
      {{ errorMessage }}
    </div>
  </div>
</template>
