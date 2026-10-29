<script setup lang="ts">
// MetricsRead — the metrics a custom view or page reads through its queries
// (#282), for the allow prompt. Their files and saved queries are part of the
// hash the person allows, so a change to one asks again; the sentence says so
// and comes from Go with the status it describes. Names are shown as text.
defineProps<{ metrics: ArtifactMetricUse[]; note?: string }>()
</script>

<template>
  <div v-if="metrics.length" class="col metrics-read" data-testid="metrics-read">
    <strong class="metrics-read__h">Metrics it reads</strong>
    <ul class="metrics-read__list">
      <li v-for="m in metrics" :key="m.name" :data-testid="`metrics-read-${m.name}`">
        <code>{{ m.name }}</code>
        <span v-if="m.missing" class="help"> (no such metric yet)</span>
        <template v-else-if="m.query">
          <span class="help"> measures </span><code :data-testid="`metrics-read-measure-${m.name}`">{{ m.measure }}</code>
          <span v-if="m.dimensions?.length" class="help"> by {{ m.dimensions.join(', ') }}</span>
          <span class="help">, through the saved query </span><code>{{ m.query }}</code>
          <router-link :to="{ path: '/semantics', query: { kind: 'metric', name: m.name } }" class="help" :data-testid="`metrics-read-link-${m.name}`"> (open its card)</router-link>
        </template>
        <span v-else class="help"> (its file cannot be read)</span>
      </li>
    </ul>
    <p v-if="note" class="help" style="margin: 0;" data-testid="metrics-read-note">{{ note }}</p>
  </div>
</template>

<style scoped>
.metrics-read { gap: var(--sh-space-1); }
.metrics-read__h { font-size: var(--sh-fs-sm); }
.metrics-read__list { margin: 0; padding-left: var(--sh-space-4); display: flex; flex-direction: column; gap: var(--sh-space-1); }
</style>
