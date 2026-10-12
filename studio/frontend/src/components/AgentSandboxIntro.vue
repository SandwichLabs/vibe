<script setup lang="ts">
import { App } from '../lib/bindings'
// AgentSandboxIntro — the agent empty-state paragraph, reflecting the
// file's current sandbox mode AND ask-before-writes setting (#91 M-F6/P-F2,
// #79). Shared by AgentView and AgentPanel's empty states, replacing each
// view's own hard-coded copy.
//
// #79 fixes the carry-over from #91: this used to fetch GetAgentConfig only
// once onMounted, so the docked panel (mounted once, kept alive across a
// Settings visit) went stale after a mode/ask change made elsewhere. It now
// also listens for "agent:config" (emitted when the agent settings change — updates in
// place with no re-fetch) and "workspace:opened" (a different workspace entirely, so
// a fresh fetch is needed).
import { ref, onMounted, onUnmounted } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { sandboxIntro } from '../lib/agentSandbox'
import { hasWailsRuntime } from '../lib/wails'

const text = ref(sandboxIntro(''))

async function refresh() {
  try {
    const cfg = await App.GetAgentConfig()
    text.value = sandboxIntro(cfg.sandbox, cfg.askBeforeWrites)
  } catch {
    // Leave whatever copy is already showing (e.g. the sandboxIntro('')
    // fallback) in place.
  }
}

let unsubConfig: (() => void) | null = null
let unsubOpened: (() => void) | null = null

onMounted(() => {
  refresh()
  if (hasWailsRuntime()) {
    unsubConfig = EventsOn('agent:config', (cfg: { sandbox: string; askBeforeWrites: boolean }) => {
      text.value = sandboxIntro(cfg.sandbox, cfg.askBeforeWrites)
    })
    unsubOpened = EventsOn('workspace:opened', refresh)
  }
})

onUnmounted(() => {
  unsubConfig?.()
  unsubOpened?.()
})
</script>

<template>
  <p data-testid="agent-sandbox-intro">{{ text }}</p>
</template>
