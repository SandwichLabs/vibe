<script setup lang="ts">
// ArtifactView — one data artifact, live (#162): parameter controls and the
// bound components of its view. The page rendered is the one Go validated and
// re-marshalled (GetArtifact), never the stored bytes parsed here. A broken
// artifact opens to its errors. The runs behind it share one pool lane that
// never starves a person's own Run (useArtifactRuns); a workspace switch drops
// them. Error text is text interpolation only.
import { computed, provide, watch, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'
import A2UIPage from '../a2ui/A2UIPage.vue'
import { ARTIFACT_RUNTIME, type ArtifactRuntime } from '../a2ui/artifactRuntime'
import { useArtifact } from '../composables/useArtifact'
import { useArtifactParams } from '../composables/useArtifactParams'
import { useArtifactRuns } from '../composables/useArtifactRuns'
import { workspaceInfo } from '../composables/useWorkspace'

const route = useRoute()
const router = useRouter()
const name = computed(() => String(route.params.name ?? ''))
const { detail, loading, failure, reload } = useArtifact(() => name.value)

const params = useArtifactParams(() => detail.value)
// A fresh artifact starts from its defaults; a reload of the same one keeps the values still valid.
watch(detail, (d, before) => params.load(d ?? null, !!d && d.name === before?.name), { immediate: true, flush: 'sync' })

// Runs started in one workspace must not land in another.
const sessionId = workspaceInfo.value?.sessionId
const runs = useArtifactRuns({
  name: () => detail.value?.name ?? '',
  queries: () => detail.value?.queries ?? [],
  values: () => params.values,
  rev: () => detail.value?.rev ?? '',
  sessionOk: () => workspaceInfo.value?.sessionId === sessionId,
})
onUnmounted(runs.dispose)

const runtime: ArtifactRuntime = {
  // Asking for a query's state is what starts its run, once, for every component bound to it.
  result: (query) => { runs.demand(query); return runs.result(query) },
  params: { param: params.param, display: params.display, error: params.error, isDefault: params.isDefault, set: params.set, reset: params.reset },
}
provide(ARTIFACT_RUNTIME, runtime)

const page = computed(() => (detail.value && !detail.value.error ? detail.value.view : undefined))
function refresh() { void reload(); runs.rerunAll() }
</script>

<template>
  <div class="col" style="height: 100%; gap: 0;">
    <div class="row" style="justify-content: space-between; align-items: center; padding: var(--sh-space-3) var(--sh-space-6); border-bottom: var(--sh-border);">
      <div class="row" style="gap: var(--sh-space-3); align-items: center; min-width: 0;">
        <button type="button" class="btn btn-ghost btn-sm" aria-label="Back to artifacts" title="Back to artifacts" @click="router.push('/artifacts')"><Icon name="arrowLeft" :size="14" /></button>
        <div style="min-width: 0;">
          <h2 class="card-title" style="margin: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" data-testid="artifact-title">{{ detail?.title || name }}</h2>
          <div v-if="detail?.description" class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite);">{{ detail.description }}</div>
        </div>
      </div>
      <button type="button" class="btn btn-ghost btn-sm" title="Re-run every query" data-testid="artifact-refresh" @click="refresh"><Icon name="refresh" :size="14" /> Refresh</button>
    </div>

    <div style="flex: 1; overflow: auto; padding: var(--sh-space-6);">
      <div v-if="failure" class="a2ui-error" role="alert" data-testid="artifact-failure">{{ failure }}</div>
      <div v-else-if="loading && !detail" class="sh-mono" style="color: var(--sh-graphite);">Loading…</div>
      <div v-else-if="detail?.error" class="card card-pad col" style="gap: var(--sh-space-2); border-color: var(--sh-error);" role="alert" data-testid="artifact-errors">
        <strong>This artifact can't be shown yet.</strong>
        <span class="sh-mono" style="font-size: var(--sh-fs-xs); color: var(--sh-graphite);">Fix <code>artifacts/{{ detail.name }}/artifact.yaml</code> or <code>view.a2ui.json</code>; this page updates when the file is saved.</span>
        <pre class="sh-mono artifact-errors">{{ detail.error }}</pre>
      </div>
      <template v-else-if="page">
        <div v-if="runs.truncated.value.length" class="sh-mono artifact-note" data-testid="artifact-truncated">
          Showing the first 500 rows of {{ runs.truncated.value.join(', ') }}.
        </div>
        <A2UIPage :key="detail?.rev" :page="page" class="artifact-page" />
      </template>
    </div>
  </div>
</template>

<style scoped>
.artifact-errors { white-space: pre-wrap; margin: 0; color: var(--sh-error); font-size: var(--sh-fs-xs); }
.artifact-page :deep(.a2ui-page__title) { display: none; }
.artifact-note { font-size: var(--sh-fs-xs); color: var(--sh-graphite); margin-bottom: var(--sh-space-3); }
</style>
