<script setup lang="ts">
import { ref, onMounted } from 'vue'

const meta = ref<Record<string, string>>({})
const title = ref('')
const description = ref('')
const loading = ref(true)
const saving = ref(false)
const message = ref('')

const mcpRunning = ref(false)
const mcpUrl = ref('')
const mcpToggling = ref(false)
const mcpCopied = ref(false)

async function loadMeta() {
  loading.value = true
  try {
    meta.value = await window.go.main.App.GetAllMeta()
    title.value = meta.value.title || ''
    description.value = meta.value.description || ''
  } catch { /* ignore */ }
  loading.value = false
}

async function loadMCPStatus() {
  try {
    const status = await window.go.main.App.GetMCPStatus()
    mcpRunning.value = status.running
    mcpUrl.value = status.url || ''
  } catch { /* ignore */ }
}

async function toggleMCP() {
  mcpToggling.value = true
  try {
    if (mcpRunning.value) {
      await window.go.main.App.StopMCPServer()
      mcpRunning.value = false
      mcpUrl.value = ''
    } else {
      const url = await window.go.main.App.StartMCPServer()
      mcpRunning.value = true
      mcpUrl.value = url
    }
  } catch (e: any) {
    message.value = 'Error: ' + (e.message || e)
    setTimeout(() => { message.value = '' }, 3000)
  }
  mcpToggling.value = false
}

async function copyMCPUrl() {
  try {
    await navigator.clipboard.writeText(mcpUrl.value)
    mcpCopied.value = true
    setTimeout(() => { mcpCopied.value = false }, 2000)
  } catch { /* ignore */ }
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

onMounted(() => {
  loadMeta()
  loadMCPStatus()
})
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

      <!-- MCP Server -->
      <div class="mt-8">
        <h2 class="text-lg font-semibold mb-3">MCP Server</h2>
        <div class="bg-gray-50 rounded-lg p-4">
          <div class="flex items-center gap-3 mb-3">
            <span class="inline-block w-3 h-3 rounded-full" :class="mcpRunning ? 'bg-green-500' : 'bg-gray-400'"></span>
            <span class="text-sm font-medium text-gray-700">{{ mcpRunning ? 'Running' : 'Stopped' }}</span>
          </div>

          <div v-if="mcpRunning && mcpUrl" class="mb-3">
            <label class="block text-xs font-medium text-gray-500 mb-1">Server URL</label>
            <div class="flex items-center gap-2">
              <code class="flex-1 px-3 py-2 bg-white border rounded text-sm text-gray-800 select-all break-all">{{ mcpUrl }}</code>
              <button @click="copyMCPUrl" class="px-3 py-2 text-sm bg-gray-200 hover:bg-gray-300 rounded">
                {{ mcpCopied ? 'Copied' : 'Copy' }}
              </button>
            </div>
          </div>

          <button
            @click="toggleMCP"
            :disabled="mcpToggling"
            class="py-2 px-6 text-sm text-white rounded-lg disabled:opacity-50"
            :class="mcpRunning ? 'bg-red-600 hover:bg-red-700' : 'bg-green-600 hover:bg-green-700'"
          >
            {{ mcpToggling ? '...' : (mcpRunning ? 'Stop' : 'Start') }}
          </button>

          <p class="mt-2 text-xs text-gray-500">
            Exposes container tools via MCP (Model Context Protocol) for AI clients. Server stops when the file is closed.
          </p>
        </div>
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
