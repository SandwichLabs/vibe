<script setup lang="ts">
// ArtifactControls — the level-1 parameter controls of an artifact whose view has
// no A2UI page of its own to hold them (a custom script view, an HTML page): one
// control per parameter out of the A2UIControl components, the same four types,
// the same artifact runtime and validation, plus Reset. A change commits through
// the runtime like any other control; the host redraws from the values.
import { computed, inject } from 'vue'
import A2UIControl from '../a2ui/A2UIControl.vue'
import { ARTIFACT_RUNTIME } from '../a2ui/artifactRuntime'
import { controlsFor } from '../a2ui/viewControls'

const props = defineProps<{ detail: ArtifactDetail; testid: string }>()
const rt = inject(ARTIFACT_RUNTIME, undefined)
const controls = computed(() => controlsFor(props.detail.params))
const atDefaults = computed(() => !rt || props.detail.params.every((p) => rt.params.isDefault(p.name)))
function resetAll() { for (const p of props.detail.params) rt?.params.reset(p.name) }
</script>

<template>
  <div v-if="controls.length" class="artifact-controls a2ui-page" :data-testid="`${testid}-controls`">
    <A2UIControl v-for="c in controls" :key="c.id" :component="c" />
    <button type="button" class="btn btn-ghost btn-sm" :disabled="atDefaults" :data-testid="`${testid}-reset`" @click="resetAll">Reset</button>
  </div>
</template>

<style scoped>
.artifact-controls { display: flex; flex-direction: row; flex-wrap: wrap; gap: var(--sh-space-4); align-items: flex-end; }
</style>
