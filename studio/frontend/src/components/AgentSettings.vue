<script setup lang="ts">
// Settings → Agent (#79). Per-file: the sandbox mode radio group and the
// "Ask before changes" switch, both persisted in _vibe_meta via
// SetAgentConfig; provider/model/endpoint are app-global and shown
// read-only (see AppearanceSettings.vue for the same "app-wide vs. this
// file" framing pattern this section follows).
import { ref, onMounted } from 'vue'
import { SANDBOX_MODES } from '../lib/agentSandbox'
import { errorText } from '../lib/errors'

const cfg = ref<AgentConfig | null>(null)
const loading = ref(true)
const saving = ref(false)
const status = ref('')

async function load() {
  loading.value = true
  try {
    cfg.value = await window.go.main.App.GetAgentConfig()
  } catch { /* no file open */ }
  loading.value = false
}

async function persist() {
  if (!cfg.value) return
  saving.value = true
  status.value = ''
  try {
    await window.go.main.App.SetAgentConfig(cfg.value)
    status.value = 'Saved'
    setTimeout(() => { if (status.value === 'Saved') status.value = '' }, 2000)
  } catch (e: any) {
    status.value = 'Error: ' + errorText(e)
    // The radio/checkbox already flipped optimistically (v-model-style,
    // via setMode/setAsk mutating cfg directly) before this call ran —
    // reload the file's actual stored value so the control doesn't keep
    // showing a choice that was rejected and never written.
    await load()
  }
  saving.value = false
}

function setMode(value: string) {
  if (!cfg.value) return
  cfg.value.sandbox = value
  persist()
}

function setAsk(on: boolean) {
  if (!cfg.value) return
  cfg.value.askBeforeWrites = on
  persist()
}

onMounted(load)
</script>

<template>
  <div v-if="cfg">
    <div class="sec-title">
      <h2 style="font-size: 22px;">Agent</h2>
      <span class="label">Stored in the .vibe</span>
    </div>

    <div class="card card-pad col" style="gap: var(--sh-space-5);">
      <fieldset class="col" style="gap: var(--sh-space-3); border: none; padding: 0; margin: 0;">
        <legend class="field-label" style="padding: 0; margin-bottom: 4px;">What the agent can do</legend>
        <label
          v-for="m in SANDBOX_MODES"
          :key="m.value"
          class="row"
          style="align-items: flex-start; gap: 10px; cursor: pointer;"
        >
          <input
            type="radio"
            name="agent-sandbox-mode"
            :value="m.value"
            :checked="cfg.sandbox === m.value"
            :data-testid="`agent-sandbox-${m.value}`"
            @change="setMode(m.value)"
          />
          <span>
            <span style="font-weight: 600; display: block;">{{ m.label }}</span>
            <span class="sh-meta">{{ m.help }}</span>
          </span>
        </label>
      </fieldset>

      <label class="row" style="gap: 10px; cursor: pointer; padding-top: var(--sh-space-3); border-top: 1.5px dashed var(--vibe-border-subtle);">
        <input
          type="checkbox"
          :checked="cfg.askBeforeWrites"
          data-testid="agent-ask-before-writes"
          @change="setAsk(($event.target as HTMLInputElement).checked)"
        />
        <span>
          <span style="font-weight: 600; display: block;">Ask before changes</span>
          <span class="sh-meta">The agent shows you each change and waits for your OK. MCP clients can't be asked, so while this is on they can only read.</span>
        </span>
      </label>

      <div class="col" style="gap: var(--sh-space-2); padding-top: var(--sh-space-3); border-top: 1.5px dashed var(--vibe-border-subtle);">
        <div class="kv"><span class="k">Provider</span><span class="v">{{ cfg.provider }}</span></div>
        <div class="kv"><span class="k">Model</span><span class="v">{{ cfg.model }}</span></div>
        <div class="kv"><span class="k">Endpoint</span><span class="v break-all">{{ cfg.endpoint }}</span></div>
        <p class="sh-micro sh-mono" style="color: var(--sh-graphite); margin: 0;">
          Set in ~/.vibe.yaml or VIBE_LLM_* (see docs/llm-config.md)
        </p>
      </div>

      <p v-if="status" role="status" class="sh-body" :style="{ fontWeight: 600, color: status.startsWith('Error') ? 'var(--sh-error)' : 'var(--sh-teal-lo)' }">
        {{ status }}
      </p>
    </div>
  </div>
</template>
