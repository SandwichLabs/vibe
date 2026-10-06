<script setup lang="ts">
// Settings > App > The agent's model. App-global (every workspace): provider,
// model and endpoint come from ~/.vibe.yaml or VIBE_LLM_* (docs/llm-config.md).
// The API key's keychain field arrives with #114.
import { ref, onMounted } from 'vue'

const cfg = ref<AgentConfig | null>(null)
onMounted(async () => {
  try { cfg.value = await window.go.main.App.GetAgentConfig() } catch { /* no workspace open */ }
})
</script>

<template>
  <div data-testid="agent-model-settings">
    <div class="sec-title"><h2 style="font-size: 22px;">The agent's model</h2><span class="label">Every workspace</span></div>
    <div class="card card-pad col" style="gap: var(--sh-space-2);">
      <template v-if="cfg">
        <div class="kv"><span class="k">Provider</span><span class="v">{{ cfg.provider }}</span></div>
        <div class="kv"><span class="k">Model</span><span class="v">{{ cfg.model }}</span></div>
        <div class="kv"><span class="k">Endpoint</span><span class="v break-all">{{ cfg.endpoint }}</span></div>
      </template>
      <p class="sh-micro sh-mono" style="color: var(--sh-graphite); margin: 0;">
        Set in ~/.vibe.yaml or VIBE_LLM_* (see docs/llm-config.md)
      </p>
    </div>
  </div>
</template>
