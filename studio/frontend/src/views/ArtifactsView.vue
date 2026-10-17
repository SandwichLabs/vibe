<script setup lang="ts">
// ArtifactsView — the data artifacts of the workspace (#162): live, parameterised
// views stored as artifacts/<name>/. Each row opens ArtifactView. A folder that
// cannot be read lists with its error so it can be found and fixed. Every text
// below is interpolation; nothing from a file reaches markup.
import { useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'
import { useArtifacts } from '../composables/useArtifacts'

const router = useRouter()
const { artifacts, loading, error } = useArtifacts()

function open(name: string) {
  router.push(`/artifacts/${encodeURIComponent(name)}`)
}
function formatDate(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString()
}
</script>

<template>
  <div class="col" style="height: 100%; gap: 0;">
    <div class="row" style="justify-content: space-between; padding: var(--sh-space-4) var(--sh-space-6); border-bottom: var(--sh-border);">
      <h2 class="sh-label" style="margin: 0;">Artifacts</h2>
    </div>

    <div style="flex: 1; overflow: auto; padding: var(--sh-space-6);">
      <div v-if="error" class="a2ui-error" role="alert" data-testid="artifacts-error">{{ error }}</div>
      <div v-else-if="loading" class="sh-mono" style="color: var(--sh-graphite);">Loading…</div>
      <div v-else-if="artifacts.length === 0" class="col" style="align-items: center; gap: var(--sh-space-3); margin-top: var(--sh-space-10); text-align: center;" data-testid="artifacts-empty">
        <Icon name="barChart" :size="32" style="color: var(--sh-graphite);" />
        <div class="sh-mono" style="color: var(--sh-graphite);">No data artifacts yet</div>
        <div class="sh-mono" style="color: var(--sh-graphite); font-size: var(--sh-fs-xs); max-width: 420px;">
          Ask the agent to build one, or add <code>artifacts/&lt;name&gt;/artifact.yaml</code> and <code>view.a2ui.json</code> to the workspace folder.
        </div>
      </div>
      <ul v-else class="col" style="gap: var(--sh-space-2); list-style: none; margin: 0; padding: 0;">
        <li v-for="a in artifacts" :key="a.name">
          <button
            type="button" class="card card-pad row artifact-row" data-testid="artifact-row" :data-artifact-name="a.name"
            :class="{ 'artifact-row--broken': !!a.error }" @click="open(a.name)"
          >
            <Icon :name="a.error ? 'alert' : 'barChart'" :size="16" :style="{ color: a.error ? 'var(--sh-error)' : 'var(--sh-teal)', flex: 'none' }" />
            <span style="min-width: 0; flex: 1; text-align: left;">
              <span class="artifact-row__title">{{ a.error ? a.name : a.title || a.name }}</span>
              <span v-if="a.error" class="artifact-row__meta artifact-row__error">{{ a.error }}</span>
              <span v-else class="artifact-row__meta">
                {{ a.name }}<template v-if="a.description"> · {{ a.description }}</template>
              </span>
            </span>
            <span v-if="a.error" class="badge badge--warn">Needs fixing</span>
            <span v-else-if="a.updated_at" class="sh-mono artifact-row__meta">{{ formatDate(a.updated_at) }}</span>
          </button>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.artifact-row { width: 100%; align-items: center; gap: var(--sh-space-3); cursor: pointer; text-align: left; font: inherit; color: inherit; }
.artifact-row__title { display: block; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.artifact-row__meta { display: block; font-family: var(--sh-font-mono); font-size: var(--sh-fs-xs); color: var(--sh-graphite); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.artifact-row__error { color: var(--sh-error); }
.artifact-row--broken { border-color: var(--sh-error); }
</style>
