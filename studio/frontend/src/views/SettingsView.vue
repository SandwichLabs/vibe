<script setup lang="ts">
import { ref, onMounted } from 'vue'

const meta = ref<Record<string, string>>({})
const title = ref('')
const description = ref('')
const loading = ref(true)
const saving = ref(false)
const message = ref('')

async function loadMeta() {
  loading.value = true
  try {
    meta.value = await window.go.main.App.GetAllMeta()
    title.value = meta.value.title || ''
    description.value = meta.value.description || ''
  } catch { /* ignore */ }
  loading.value = false
}

async function save() {
  saving.value = true
  message.value = ''
  try {
    await window.go.main.App.SetMeta('title', title.value)
    await window.go.main.App.SetMeta('description', description.value)
    await window.go.main.App.SaveFile()
    message.value = 'Settings saved'
    setTimeout(() => { message.value = '' }, 2000)
  } catch (e: any) {
    message.value = 'Error: ' + (e.message || e)
  }
  saving.value = false
}

onMounted(loadMeta)
</script>

<template>
  <div class="p-6 max-w-xl">
    <h1 class="text-2xl font-bold mb-6">Settings</h1>

    <div v-if="loading" class="text-gray-500">Loading...</div>

    <div v-else class="space-y-4">
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Title</label>
        <input v-model="title" class="w-full px-3 py-2 border rounded-lg" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Description</label>
        <textarea v-model="description" rows="3" class="w-full px-3 py-2 border rounded-lg resize-none"></textarea>
      </div>

      <button @click="save" :disabled="saving" class="py-2 px-6 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50">
        {{ saving ? 'Saving...' : 'Save Settings' }}
      </button>

      <div v-if="message" class="text-sm" :class="message.startsWith('Error') ? 'text-red-600' : 'text-green-600'">
        {{ message }}
      </div>

      <!-- Raw metadata -->
      <div class="mt-8">
        <h2 class="text-lg font-semibold mb-3">All Metadata</h2>
        <div class="bg-gray-50 rounded-lg p-4 text-sm">
          <div v-for="(v, k) in meta" :key="k" class="flex gap-4 py-1 border-b border-gray-100 last:border-0">
            <span class="font-medium text-gray-600 w-40 shrink-0">{{ k }}</span>
            <span class="text-gray-800 break-all">{{ v }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
