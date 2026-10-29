<script setup lang="ts">
// ArtifactViewDecisions — Settings > Workspace: the allow and deny decisions you
// made about custom artifact views, each with Revoke (the view asks again next
// time). They are kept in your own config directory, never in the workspace or a
// shared bundle. Text interpolation only.
import { onMounted, onUnmounted } from 'vue'
import { useArtifactViewDecisions } from '../composables/useArtifactViewDecisions'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

const { decisions, activity, busy, error, refresh, refreshActivity, revoke, setAsk } = useArtifactViewDecisions()

const label: Record<string, string> = {
  allowed: 'Allowed',
  changed: 'Allowed, but edited since: asks again',
  metricChanged: 'A metric it reads changed: asks again',
  denied: 'Denied',
  gone: 'The artifact is no longer here',
  undecided: 'Not allowed yet',
}

const kindText = (d: ArtifactViewDecision) =>
  d.kind === 'html' ? (d.pageKind === 'scripted' ? 'Scripted page' : 'Static page') : d.kind === 'script' ? 'Script view' : ''
const at = (iso: string) => { const t = new Date(iso); return isNaN(t.getTime()) ? iso : t.toLocaleString() }

const offs: Array<() => void> = []
onMounted(() => {
  void refresh()
  if (hasWailsRuntime()) {
    offs.push(EventsOn('workspace:changed', () => { void refresh() }))
    offs.push(EventsOn('workspace:changed-on-disk', () => { void refresh() }))
    offs.push(EventsOn('agent:activity', () => { void refreshActivity() }))
  }
})
onUnmounted(() => { for (const off of offs) off() })
</script>

<template>
  <div data-testid="view-decisions-section">
    <div class="sec-title"><h2 style="font-size: 22px;">Pages</h2><span class="label">Only yours, never shared</span></div>
    <div class="card card-pad col" style="gap: var(--sh-space-3);">
      <p v-if="!decisions.length" class="help" style="margin: 0;" data-testid="view-decisions-empty">
        No pages or custom views have been allowed or denied. An HTML page, or a view that draws itself with code, asks you the first time you open it.
      </p>
      <ul v-else class="view-decisions" data-testid="view-decisions">
        <li v-for="d in decisions" :key="d.name" class="row view-decisions__row" :data-testid="`view-decision-${d.name}`">
          <code>{{ d.name }}</code>
          <span v-if="kindText(d)" class="badge" :data-testid="`view-decision-kind-${d.name}`">{{ kindText(d) }}</span>
          <span v-if="d.capabilities?.length" class="help" style="margin: 0;" :data-testid="`view-decision-caps-${d.name}`">{{ d.capabilities.join(', ') }}</span>
          <span v-if="d.metricsUsed?.length" class="help" style="margin: 0;" :title="'Their files are part of what you allowed: a change to one asks again.'" :data-testid="`view-decision-metrics-${d.name}`">reads {{ d.metricsUsed.map((m) => m.name).join(', ') }}</span>
          <span class="help" style="flex: 1; margin: 0;">{{ (d.state === 'changed' && d.depsChanged ? label.metricChanged : label[d.state]) ?? d.state }}</span>
          <label v-if="d.declaresAsk" class="row view-decisions__ask" :title="'This page can send text, which may include query results, to your model provider.'">
            <input type="checkbox" :checked="d.askOn" :disabled="busy === d.name" :data-testid="`view-decision-ask-${d.name}`" @change="setAsk(d.name, ($event.target as HTMLInputElement).checked)">
            <span class="help" style="margin: 0;">Let it ask the model</span>
          </label>
          <button type="button" class="btn btn-sm" :disabled="busy === d.name" :data-testid="`view-decision-revoke-${d.name}`" @click="revoke(d.name)">Revoke</button>
        </li>
      </ul>
      <div class="col" style="gap: var(--sh-space-2);" data-testid="page-activity-section">
        <strong style="font-size: var(--sh-fs-sm);">Recent model use by pages</strong>
        <p v-if="!activity.length" class="help" style="margin: 0;" data-testid="page-activity-empty">No page has asked the model in this session.</p>
        <ul v-else class="view-decisions" data-testid="page-activity">
          <li v-for="(e, i) in activity" :key="i" class="row view-decisions__row" :data-testid="`page-activity-${i}`">
            <code>{{ e.name }}</code>
            <span class="help" style="margin: 0; flex: 1;">{{ at(e.at) }} · {{ e.kind }} · sent {{ e.promptBytes }} bytes, got {{ e.replyBytes }} bytes · {{ e.outcome }}</span>
          </li>
        </ul>
      </div>
      <div v-if="error" class="help" role="alert" style="color: var(--sh-error);">{{ error }}</div>
    </div>
  </div>
</template>

<style scoped>
.view-decisions { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sh-space-2); }
.view-decisions__row { align-items: center; gap: var(--sh-space-3); }
.view-decisions__ask { align-items: center; gap: var(--sh-space-2); }
</style>
