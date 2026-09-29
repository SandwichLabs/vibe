<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import Icon from '../components/Icon.vue'
import AppearanceSettings from '../components/AppearanceSettings.vue'
import AgentSettings from '../components/AgentSettings.vue'
import { errorText } from '../lib/errors'
import { fileDirty } from '../composables/useFileDirty'
import { shortcutLabel } from '../composables/useModal'

const meta = ref<Record<string, string>>({})
const title = ref('')
const description = ref('')
const loading = ref(true)
const saving = ref(false)
const message = ref('')

const mcpRunning = ref(false)
const mcpUrl = ref('')
const mcpToken = ref('')
const mcpToggling = ref(false)
const mcpCopied = ref(false)
const mcpTokenCopied = ref(false)

async function loadMeta() {
  loading.value = true
  try {
    meta.value = await window.go.main.App.GetAllMeta()
    title.value = meta.value.title || ''
    description.value = meta.value.description || ''
  } catch { /* ignore */ }
  loading.value = false
}

// refreshMeta re-reads the "All metadata" kv list (updated_at in
// particular) WITHOUT touching the Title/Description form fields or
// `loading` — unlike loadMeta, which is only right at mount and right after
// this view's own Save. A Save elsewhere (the sidebar, Ctrl+S, an agent
// write) must not overwrite text the person has typed into Title/
// Description but not yet saved through "Save settings".
async function refreshMeta() {
  try {
    meta.value = await window.go.main.App.GetAllMeta()
  } catch { /* ignore */ }
}

async function loadMCPStatus() {
  try {
    const status = await window.go.main.App.GetMCPStatus()
    mcpRunning.value = status.running
    mcpUrl.value = status.url || ''
    mcpToken.value = status.token || ''
  } catch { /* ignore */ }
}

async function toggleMCP() {
  mcpToggling.value = true
  try {
    if (mcpRunning.value) {
      await window.go.main.App.StopMCPServer()
      mcpRunning.value = false
      mcpUrl.value = ''
      mcpToken.value = ''
    } else {
      await window.go.main.App.StartMCPServer()
      // Re-fetch status rather than trusting StartMCPServer's return value
      // alone: it only returns the URL, and the bearer token (#64/#70) lives
      // on GetMCPStatus so a page reload / re-open of Settings always shows
      // the CURRENT session's token, not a stale one captured at start time.
      await loadMCPStatus()
    }
  } catch (e: any) {
    message.value = 'Error: ' + errorText(e)
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

async function copyMCPToken() {
  try {
    await navigator.clipboard.writeText(mcpToken.value)
    mcpTokenCopied.value = true
    setTimeout(() => { mcpTokenCopied.value = false }, 2000)
  } catch { /* ignore */ }
}

async function save() {
  saving.value = true
  message.value = ''
  try {
    await window.go.main.App.SetMeta('title', title.value)
    await window.go.main.App.SetMeta('description', description.value)
    await window.go.main.App.SaveFile()
    meta.value = await window.go.main.App.GetAllMeta()
    message.value = 'Settings saved'
    setTimeout(() => { message.value = '' }, 2000)
  } catch (e: any) {
    message.value = 'Error: ' + errorText(e)
  }
  saving.value = false
}

onMounted(() => {
  loadMeta()
  loadMCPStatus()
})

// #81: a Save elsewhere (the sidebar button, Ctrl+S, an agent write) moves
// updated_at without this view doing anything — refresh the metadata list
// on the dirty->clean edge so the timestamp shown here stays current
// without requiring a navigate-away-and-back. refreshMeta (not loadMeta)
// so an unsaved Title/Description edit is never wiped by a Save elsewhere.
watch(fileDirty, (dirty, was) => {
  if (was && !dirty) refreshMeta()
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

    <div v-else class="col" style="max-width: 52rem; gap: var(--sh-space-6);">
      <AppearanceSettings />

      <div>
        <div class="sec-title">
          <h2 style="font-size: 22px;">This file</h2>
          <span class="label">Stored in the .vibe</span>
        </div>
      <div class="card card-pad col" style="gap: var(--sh-space-4);">
        <div>
          <label class="field-label" for="settings-title">Title</label>
          <input id="settings-title" v-model="title" class="field-input" />
        </div>

        <div>
          <label class="field-label" for="settings-description">Description</label>
          <textarea id="settings-description" v-model="description" rows="3" class="field-input" style="resize: vertical;"></textarea>
        </div>

        <div class="row">
          <button @click="save" :disabled="saving" class="btn btn-primary">
            <Icon name="save" :size="16" /> {{ saving ? 'Saving…' : 'Save settings' }}
          </button>
          <span v-if="message" class="sh-body" role="status" :style="{ fontWeight: 600, color: message.startsWith('Error') ? 'var(--sh-error)' : 'var(--sh-teal-lo)' }">
            {{ message }}
          </span>
        </div>
        <p class="sh-micro" style="color: var(--sh-graphite);">
          Table data (edits, imports, schema changes, agent writes) is kept in this session until you Save ({{ shortcutLabel('S') }}).
          Notebooks, saved queries and skills save as you go; Title/Description save with "Save settings" above.
          updated_at is the time of the last Save.
        </p>
      </div>
      </div>

      <AgentSettings />

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

          <div v-if="mcpRunning && mcpToken" style="margin-bottom: var(--sh-space-3);">
            <label class="field-label">Auth token</label>
            <div class="row" style="gap: var(--sh-space-2);">
              <code class="break-all" style="flex: 1; user-select: all; padding: 8px 12px;">{{ mcpToken }}</code>
              <button @click="copyMCPToken" class="btn btn-sm"><Icon name="copy" :size="14" /> {{ mcpTokenCopied ? 'Copied' : 'Copy' }}</button>
            </div>
            <p class="sh-micro sh-mono" style="margin-top: var(--sh-space-2); color: var(--sh-graphite);">
              Every request must send this as <code>Authorization: Bearer &lt;token&gt;</code> — the server rejects unauthenticated requests and anything sent with a browser Origin header. A new token is generated each time the server starts.
            </p>
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
