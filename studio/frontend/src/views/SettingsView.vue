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

    <div v-if="loading" class="text-muted font-mono text-sm">Loading...</div>

    <div v-else class="space-y-4">
      <div>
        <label class="field-label">Title</label>
        <input v-model="title" class="field-input" />
      </div>

      <div>
        <label class="field-label">Description</label>
        <textarea v-model="description" rows="3" class="field-input resize-none"></textarea>
      </div>

      <button @click="save" :disabled="saving" class="btn btn-primary">
        {{ saving ? 'Saving...' : 'Save Settings' }}
      </button>

      <div v-if="message" class="text-sm font-semibold" :class="message.startsWith('Error') ? 'text-error' : 'text-black'">
        {{ message }}
      </div>

      <!-- MCP Server -->
      <div class="mt-8">
        <h2 class="text-lg font-bold mb-3">MCP Server</h2>
        <div class="card p-4">
          <div class="flex items-center gap-3 mb-3">
            <span class="inline-block w-3 h-3 border-2 border-black" :class="mcpRunning ? 'bg-yellow' : 'bg-muted'"></span>
            <span class="font-mono text-xs uppercase font-bold">{{ mcpRunning ? 'Running' : 'Stopped' }}</span>
          </div>

          <div v-if="mcpRunning && mcpUrl" class="mb-3">
            <label class="field-label">Server URL</label>
            <div class="flex items-center gap-2">
              <code class="flex-1 px-3 py-2 bg-cream border-3 border-black text-sm font-mono select-all break-all">{{ mcpUrl }}</code>
              <button @click="copyMCPUrl" class="btn btn-sm">
                {{ mcpCopied ? 'Copied' : 'Copy' }}
              </button>
            </div>
          </div>

          <button
            @click="toggleMCP"
            :disabled="mcpToggling"
            class="btn"
            :class="mcpRunning ? 'btn-danger' : 'btn-primary'"
          >
            {{ mcpToggling ? '...' : (mcpRunning ? 'Stop' : 'Start') }}
          </button>

          <p class="mt-3 font-mono text-xs text-muted">
            Exposes container tools via MCP (Model Context Protocol) for AI clients. Server stops when the file is closed.
          </p>
        </div>
      </div>

      <!-- Raw metadata -->
      <div class="mt-8">
        <h2 class="text-lg font-bold mb-3">All Metadata</h2>
        <div class="card p-4">
          <div v-for="(v, k) in meta" :key="k" class="flex gap-4 py-1.5 border-b-2 border-black/10 last:border-0">
            <span class="font-mono text-xs uppercase font-bold text-muted w-40 shrink-0">{{ k }}</span>
            <span class="text-sm break-all">{{ v }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
