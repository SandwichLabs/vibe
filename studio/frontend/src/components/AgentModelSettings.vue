<script setup lang="ts">
// Settings > App > The agent's model. App-global (every workspace): provider,
// model and endpoint come from ~/.vibe.yaml or VIBE_LLM_* (docs/reference/llm-config.md).
// The API key is write-only (SPEC 09, #114): it is saved to the OS keychain and
// never shown again. "•••• In your keychain · Replace · Clear". Nothing here can
// read a key back; the bindings only set, clear and report where it comes from.
import { ref, computed, onMounted, nextTick } from 'vue'
import Icon from './Icon.vue'
import { askConfirm } from '../composables/useConfirm'

const cfg = ref<AgentConfig | null>(null)
const keyStatus = ref<LLMKeyStatus | null>(null)
const editing = ref(false)
const draft = ref('')
const busy = ref(false)
const message = ref('')
const keyInput = ref<HTMLInputElement | null>(null)

async function refresh() {
  try { keyStatus.value = await window.go.main.App.GetLLMKeyStatus() } catch { keyStatus.value = null }
}
onMounted(async () => {
  try { cfg.value = await window.go.main.App.GetAgentConfig() } catch { /* no workspace open */ }
  await refresh()
})

const source = computed(() => keyStatus.value?.source ?? 'none')
const showForm = computed(() => editing.value || (source.value === 'none' || source.value === 'file') && !!keyStatus.value?.keychainAvailable)
const provider = computed(() => keyStatus.value?.provider ?? cfg.value?.provider ?? '')
const optional = computed(() => provider.value === 'local')

async function startReplace() {
  editing.value = true
  draft.value = ''
  await nextTick()
  keyInput.value?.focus()
}

async function save() {
  const key = draft.value.trim()
  if (!key) return
  busy.value = true
  message.value = ''
  try {
    await window.go.main.App.SetLLMKey('', key)
    draft.value = ''
    editing.value = false
    message.value = 'Saved to your keychain.'
    await refresh()
  } catch (e: any) {
    message.value = 'Error: ' + (e?.message ?? e)
  } finally { busy.value = false }
}

async function clearKey() {
  const ok = await askConfirm({ title: 'Remove the API key?', message: 'Vibe will forget the key saved in your keychain. You can paste it again later.', confirmLabel: 'Remove key' })
  if (!ok) return
  busy.value = true
  message.value = ''
  try {
    await window.go.main.App.ClearLLMKey('')
    editing.value = false
    message.value = 'Removed from your keychain.'
    await refresh()
  } catch (e: any) {
    message.value = 'Error: ' + (e?.message ?? e)
  } finally { busy.value = false }
}

async function migrate() {
  let overwrite = false
  if (keyStatus.value?.keychainHasKey) {
    const ok = await askConfirm({ title: 'Replace the key in your keychain?', message: 'Your keychain already has an API key. Moving the one from ~/.vibe.yaml replaces it.', confirmLabel: 'Replace key' })
    if (!ok) return
    overwrite = true
  }
  busy.value = true
  message.value = ''
  try {
    await window.go.main.App.MigrateLLMKeyToKeychain(overwrite)
    message.value = 'Moved to your keychain, and blanked in ~/.vibe.yaml.'
    await refresh()
  } catch (e: any) {
    message.value = 'Error: ' + (e?.message ?? e)
  } finally { busy.value = false }
}
</script>

<template>
  <div data-testid="agent-model-settings">
    <div class="sec-title"><h2 style="font-size: 22px;">The agent's model</h2><span class="label">Every workspace</span></div>
    <div class="card card-pad col" style="gap: var(--sh-space-4);">
      <div v-if="cfg" class="col" style="gap: var(--sh-space-2);">
        <div class="kv"><span class="k">Provider</span><span class="v">{{ cfg.provider }}</span></div>
        <div class="kv"><span class="k">Model</span><span class="v">{{ cfg.model }}</span></div>
        <div class="kv"><span class="k">Endpoint</span><span class="v break-all">{{ cfg.endpoint }}</span></div>
      </div>
      <p class="sh-micro sh-mono" style="color: var(--sh-graphite); margin: 0;">
        Provider, model and endpoint are set in ~/.vibe.yaml or VIBE_LLM_* (see docs/reference/llm-config.md)
      </p>

      <div v-if="keyStatus" class="field" data-testid="llm-key">
        <label id="llm-key-l" for="llm-key-input">API key<span v-if="optional"> (optional for a local server)</span></label>

        <!-- Saved in the keychain: write-only -->
        <div v-if="source === 'keychain' && !editing" class="cred" role="group" aria-labelledby="llm-key-l" data-testid="llm-key-saved">
          <span class="dots" aria-label="Saved, hidden">&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;</span>
          <span class="where"><Icon name="shieldCheck" :size="14" /> In your keychain</span>
          <span class="acts">
            <button class="linkbtn" type="button" :disabled="busy" data-testid="llm-key-replace" @click="startReplace">Replace</button>
            <span class="sepdot" aria-hidden="true">&middot;</span>
            <button class="linkbtn danger" type="button" :disabled="busy" data-testid="llm-key-clear" @click="clearKey">Clear</button>
          </span>
        </div>

        <!-- Set by the environment: nothing to store -->
        <div v-else-if="source === 'env' && !editing" class="cred" role="group" aria-labelledby="llm-key-l" data-testid="llm-key-env">
          <span class="dots" aria-label="Set, hidden">&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;</span>
          <span class="where"><Icon name="shieldCheck" :size="14" /> From an environment variable</span>
          <span class="acts" v-if="keyStatus.keychainAvailable">
            <button class="linkbtn" type="button" :disabled="busy" @click="startReplace">Save a key in the keychain</button>
          </span>
        </div>

        <!-- Not stored yet, or replacing -->
        <form v-if="showForm" class="row" style="gap: var(--sh-space-2); align-items: stretch;" @submit.prevent="save">
          <input
            id="llm-key-input" ref="keyInput" v-model="draft" type="password" class="field-input" style="flex: 1;"
            autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="Paste a key to save it in your keychain"
            aria-describedby="llm-key-h" data-testid="llm-key-input"
          />
          <button class="btn btn-sm" type="submit" :disabled="busy || !draft.trim()" data-testid="llm-key-save">Save to keychain</button>
          <button v-if="editing" class="btn btn-sm" type="button" :disabled="busy" @click="editing = false; draft = ''">Cancel</button>
        </form>

        <div v-if="source === 'file'" class="help" data-testid="llm-key-file">The key in use is the plaintext one in <code>~/.vibe.yaml</code>. It is used for whichever provider is selected.</div>
        <div id="llm-key-h" class="help">The <code>VIBE_LLM_API_KEY</code> environment variable, if set, is used instead. The key is saved to your computer's keychain and Vibe can't show it again, only replace or clear it.</div>
        <div v-if="!keyStatus.keychainAvailable" class="note warn" data-testid="llm-key-nokeychain">
          <Icon name="lock" :size="16" />
          <span>Can't reach your keychain. Set <code>VIBE_LLM_API_KEY</code> (or your provider's usual variable) in the environment Vibe starts from. Vibe never writes a key to a file.</span>
        </div>
        <span v-if="message" class="sh-body" role="status" :style="{ fontWeight: 600, color: message.startsWith('Error') ? 'var(--sh-error)' : 'var(--sh-teal-lo)' }">{{ message }}</span>
      </div>

      <div v-if="keyStatus && keyStatus.canMigrate && source !== 'keychain'" class="note info" data-testid="llm-key-migrate">
        <Icon name="info" :size="16" />
        <span>Found a key in <code>~/.vibe.yaml</code>. <button class="linkbtn" type="button" style="padding: 0 4px;" :disabled="busy" data-testid="llm-key-migrate-btn" @click="migrate">Move it to the keychain</button> and blank the file. The rest of the file stays as it is.</span>
      </div>
    </div>
  </div>
</template>
