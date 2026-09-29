<script setup lang="ts">
// A2UIBarChart — zero-baselined horizontal bars, inline "bars" or a live
// "sql" binding. Ported from renderer.js's renderBarChart/appendBars.
//
// Zero-baselined on purpose: bar length encodes magnitude, and starting the
// axis at the smallest value would make a 233-vs-208 gap look like 10x. Bar
// widths are computed here as plain numbers (a clamped percentage) and bound
// only through that numeric :style — never a raw model string.
import { ref, onMounted, computed, inject } from 'vue'
import type { A2UIComponentT } from './types'
import { runLiveQuery, toPoints, colorClass, type Point } from './query'
import { formatNumber } from '../lib/formatCell'

const props = defineProps<{ component: A2UIComponentT }>()
const abortSignal = inject<AbortSignal | undefined>('a2uiAbortSignal', undefined)

const loading = ref(false)
const error = ref('')
const livePoints = ref<Point[]>([])

onMounted(async () => {
  if (!props.component.sql) return
  loading.value = true
  const { columns, rows, error: err, aborted } = await runLiveQuery(props.component.sql, abortSignal)
  if (aborted) return // caller stopped caring (unmounted/reloaded) — never apply a stale result
  loading.value = false
  if (err) {
    error.value = err
    return
  }
  livePoints.value = toPoints(columns, rows)
})

const points = computed<Point[]>(() => (props.component.sql ? livePoints.value : (props.component.bars ?? [])))
const values = computed(() => points.value.map((p) => p.value))
const maxV = computed(() => Math.max(...values.value))
const minV = computed(() => Math.min(...values.value, 0))
const span = computed(() => maxV.value - minV.value || 1)

function widthPct(v: number): number {
  return Math.max(((v - minV.value) / span.value) * 100, 1)
}
</script>

<template>
  <div class="a2ui-barchart">
    <div v-if="component.label" class="a2ui-component__title">{{ component.label }}</div>
    <div v-if="component.sql && loading" class="a2ui-loading">RUNNING QUERY…</div>
    <div v-else-if="component.sql && error" class="a2ui-error">QUERY ERROR: {{ error }}</div>
    <div v-else-if="points.length === 0" class="a2ui-empty">NO ROWS</div>
    <template v-else>
      <div v-for="(bar, i) in points" :key="i" class="a2ui-bar">
        <span class="a2ui-bar__label">{{ bar.label }}</span>
        <div class="a2ui-bar__track">
          <div class="a2ui-bar__fill" :class="colorClass(component.color)" :style="{ width: widthPct(bar.value) + '%' }"></div>
        </div>
        <span class="a2ui-bar__value">{{ formatNumber(bar.value) }}</span>
      </div>
    </template>
  </div>
</template>
