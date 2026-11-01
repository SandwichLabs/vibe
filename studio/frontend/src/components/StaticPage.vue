<script setup lang="ts">
// StaticPage — an allowed static HTML page (#162, H3): the artifact's parameter
// controls above a sandbox="" frame that loads the page from Go's /slaw-view/
// route. The frame has no allow-* token: no script, no forms, no popups, no
// navigation of Studio; Go renders the page, escaped by context, under a CSP that
// lets it load only its own assets. The person can drag the frame's bottom edge
// to resize it (a page with no script cannot report its own height). Nothing
// from the page or its data reaches this component as markup.
import ArtifactControls from './ArtifactControls.vue'
import { usePageFrame } from '../composables/usePageFrame'
import type { ParamValue } from '../a2ui/artifactParams'

const props = defineProps<{ name: string; detail: ArtifactDetail; values: Record<string, ParamValue>; refresh?: number }>()
const { src, failure } = usePageFrame(() => props.name, () => props.detail, () => props.values, () => props.refresh ?? 0)
</script>

<template>
  <div class="static-page" data-testid="static-page">
    <ArtifactControls :detail="detail" testid="static-page" />
    <div v-if="failure" class="static-page__error" role="alert" data-testid="static-page-error">{{ failure }}</div>
    <div v-else class="static-page__frame" :aria-busy="!src" data-testid="static-page-frame-box">
      <iframe
        v-if="src" :src="src" sandbox="" referrerpolicy="no-referrer"
        :title="detail.title || name" class="static-page__iframe" data-testid="static-page-frame"
      />
      <div v-else class="sh-mono static-page__wait" role="status">Opening…</div>
    </div>
  </div>
</template>

<style scoped>
.static-page { display: flex; flex-direction: column; gap: var(--sh-space-4); min-width: 0; }
.static-page__frame { height: 560px; min-height: 200px; max-height: 4000px; resize: vertical; overflow: hidden; border: var(--sh-border); border-radius: var(--sh-radius); background: var(--sh-paper-2); }
.static-page__iframe { display: block; width: 100%; height: 100%; border: 0; background: Canvas; }
.static-page__wait { color: var(--sh-graphite); font-size: var(--sh-fs-xs); padding: var(--sh-space-3); }
.static-page__error { border: 2px solid var(--sh-error); color: var(--sh-error); background: var(--sh-paper-2); padding: var(--sh-space-3); border-radius: var(--sh-radius); }
</style>
