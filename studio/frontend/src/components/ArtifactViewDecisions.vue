<script setup lang="ts">
// ArtifactViewDecisions — Settings > Workspace: the allow and deny decisions you
// made about custom artifact views, each with Revoke (the view asks again next
// time). They are kept in your own config directory, never in the workspace or a
// shared bundle. Text interpolation only.
import { onMounted, onUnmounted } from 'vue'
import { useArtifactViewDecisions } from '../composables/useArtifactViewDecisions'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

const { decisions, busy, error, refresh, revoke, setAsk } = useArtifactViewDecisions()

const label: Record<string, string> = {
  allowed: 'Allowed',
  changed: 'Allowed, but edited since: asks again',
  denied: 'Denied',
  gone: 'The artifact is no longer here',
  undecided: 'Not allowed yet',
}

const offs: Array<() => void> = []
onMounted(() => {
  void refresh()
  if (hasWailsRuntime()) {
    offs.push(EventsOn('workspace:changed', () => { void refresh() }))
    offs.push(EventsOn('workspace:changed-on-disk', () => { void refresh() }))
  }
})
onUnmounted(() => { for (const off of offs) off() })
</script>

<template>
  <div data-testid="view-decisions-section">
    <div class="sec-title"><h2 style="font-size: 22px;">Custom views and pages</h2><span class="label">Only yours, never shared</span></div>
    <div class="card card-pad col" style="gap: var(--sh-space-3);">
      <p v-if="!decisions.length" class="help" style="margin: 0;" data-testid="view-decisions-empty">
        No custom artifact views have been allowed or denied. A view that draws itself with code asks you the first time you open it.
      </p>
      <ul v-else class="view-decisions" data-testid="view-decisions">
        <li v-for="d in decisions" :key="d.name" class="row view-decisions__row" :data-testid="`view-decision-${d.name}`">
          <code>{{ d.name }}</code>
          <span class="help" style="flex: 1; margin: 0;">{{ label[d.state] ?? d.state }}</span>
          <label v-if="d.declaresAsk" class="row view-decisions__ask" :title="'This page can send text, which may include query results, to your model provider.'">
            <input type="checkbox" :checked="d.askOn" :disabled="busy === d.name" :data-testid="`view-decision-ask-${d.name}`" @change="setAsk(d.name, ($event.target as HTMLInputElement).checked)">
            <span class="help" style="margin: 0;">Let it ask the model</span>
          </label>
          <button type="button" class="btn btn-sm" :disabled="busy === d.name" :data-testid="`view-decision-revoke-${d.name}`" @click="revoke(d.name)">Revoke</button>
        </li>
      </ul>
      <div v-if="error" class="help" role="alert" style="color: var(--sh-error);">{{ error }}</div>
    </div>
  </div>
</template>

<style scoped>
.view-decisions { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sh-space-2); }
.view-decisions__row { align-items: center; gap: var(--sh-space-3); }
.view-decisions__ask { align-items: center; gap: var(--sh-space-2); }
</style>
