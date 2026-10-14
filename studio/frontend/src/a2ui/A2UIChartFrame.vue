<script setup lang="ts">
// A2UIChartFrame — the chrome every KINETIC chart shares: caption, the
// loading / error / empty states, a replay button (only when animation is
// on), and an optional series legend whose swatches come from the closed
// palette classes, never from a model-supplied color.
import Icon from '../components/Icon.vue'
import { seriesClass } from './series'

defineProps<{
  label?: string
  sql?: boolean
  loading: boolean
  error: string
  empty: boolean
  animated: boolean
  legend?: string[]
  kind: string
}>()
const emit = defineEmits<{ replay: [] }>()
</script>

<template>
  <div class="a2ui-kchart" :class="`a2ui-kchart--${kind}`" :data-testid="`a2ui-${kind}`">
    <div v-if="label || animated" class="a2ui-kchart__head">
      <div class="a2ui-component__title">{{ label }}</div>
      <button
        v-if="animated && !empty && !loading && !error"
        type="button"
        class="a2ui-kchart__replay"
        title="Replay animation"
        aria-label="Replay animation"
        @click="emit('replay')"
      ><Icon name="undo" :size="12" /></button>
    </div>
    <div v-if="sql && loading" class="a2ui-loading">RUNNING QUERY…</div>
    <div v-else-if="sql && error" class="a2ui-error">QUERY ERROR: {{ error }}</div>
    <div v-else-if="empty" class="a2ui-empty">NO ROWS</div>
    <template v-else>
      <slot />
      <div v-if="legend && legend.length > 1" class="a2ui-kchart__legend">
        <span v-for="(name, i) in legend" :key="i" class="a2ui-kchart__key" :class="seriesClass(i)">
          <span class="a2ui-kchart__swatch"></span>{{ name }}
        </span>
      </div>
    </template>
  </div>
</template>
