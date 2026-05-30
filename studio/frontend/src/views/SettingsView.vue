<script setup lang="ts">
import { ref, onMounted } from 'vue'
import Icon from '../components/Icon.vue'

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
  <div style="height: 100%; overflow-y: auto; padding: 32px;">
    <div class="headrow">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Settings</div>
        <h1>Settings</h1>
      </div>
    </div>

    <div v-if="loading" class="sh-mono" style="color: var(--sh-graphite);">Loading…</div>

    <div v-else class="col" style="max-width: 36rem; gap: var(--sh-space-6);">
      <div class="card card-pad col" style="gap: var(--sh-space-4);">
        <div>
          <label class="field-label">Title</label>
          <input v-model="title" class="field-input" />
        </div>

        <div>
          <label class="field-label">Description</label>
          <textarea v-model="description" rows="3" class="field-input" style="resize: none;"></textarea>
        </div>

        <div class="row">
          <button @click="save" :disabled="saving" class="btn btn-primary">
            <Icon name="save" :size="16" /> {{ saving ? 'Saving…' : 'Save settings' }}
          </button>
          <span v-if="message" class="sh-body" :style="{ fontWeight: 600, color: message.startsWith('Error') ? 'var(--sh-error)' : 'var(--sh-teal-lo)' }">
            {{ message }}
          </span>
        </div>
      </div>

      <!-- MCP Server -->
      <div>
        <div class="sec-title"><h2 style="font-size: 22px;">MCP Server</h2></div>
        <div class="card card-pad">
          <div class="row" style="gap: var(--sh-space-2); margin-bottom: var(--sh-space-3);">
            <span class="status-dot" :class="mcpRunning ? 'ok' : 'off'"></span>
            <span class="sh-label" style="margin: 0;">{{ mcpRunning ? 'Running' : 'Stopped' }}</span>
          </div>

          <div v-if="mcpRunning && mcpUrl" style="margin-bottom: var(--sh-space-3);">
            <label class="field-label">Server URL</label>
            <div class="row" style="gap: var(--sh-space-2);">
              <code class="break-all" style="flex: 1; user-select: all; padding: 8px 12px;">{{ mcpUrl }}</code>
              <button @click="copyMCPUrl" class="btn btn-sm"><Icon name="copy" :size="14" /> {{ mcpCopied ? 'Copied' : 'Copy' }}</button>
            </div>
          </div>

          <button
            @click="toggleMCP"
            :disabled="mcpToggling"
            class="btn"
            :class="mcpRunning ? 'btn-destructive' : 'btn-primary'"
          >
            {{ mcpToggling ? '…' : (mcpRunning ? 'Stop' : 'Start') }}
          </button>

          <p class="sh-micro sh-mono" style="margin-top: var(--sh-space-3); color: var(--sh-graphite);">
            Exposes container tools via MCP (Model Context Protocol) for AI clients. Server stops when the file is closed.
          </p>
        </div>
      </div>

      <!-- Raw metadata -->
      <div>
        <div class="sec-title"><h2 style="font-size: 22px;">All metadata</h2></div>
        <div class="card card-pad">
          <div v-for="(v, k) in meta" :key="k" class="kv">
            <span class="k" style="width: 160px; flex: none;">{{ k }}</span>
            <span class="v break-all" style="text-align: right;">{{ v }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
