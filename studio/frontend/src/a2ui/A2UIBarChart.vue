<script setup lang="ts">
// A2UIBarChart — zero-baselined horizontal bars, inline "bars" or a live
// "sql" binding. Ported from renderer.js's renderBarChart/appendBars.
//
// Zero-baselined on purpose: bar length encodes magnitude, and starting the
// axis at the smallest value would make a 233-vs-208 gap look like 10x. Bar
// widths are computed here as plain numbers (a clamped percentage) and bound
// only through that numeric :style — never a raw model string. Bar lengths
// animate in with the component's "motion" preset (useMotion).
import { computed } from 'vue'
import type { A2UIComponentT } from './types'
import { toPoints, colorClass, type Point } from './query'
import { useLiveQuery } from './useLiveQuery'
import { formatNumber } from '../lib/formatCell'
import { useMotion } from './useMotion'

const props = defineProps<{ component: A2UIComponentT }>()
const { loading, error, data } = useLiveQuery(() => props.component)
const livePoints = computed<Point[]>(() => (data.value ? toPoints(data.value.columns, data.value.rows) : []))

const points = computed<Point[]>(() => (props.component.sql ? livePoints.value : (props.component.bars ?? [])))
const values = computed(() => points.value.map((p) => p.value))
const maxV = computed(() => Math.max(...values.value))
const minV = computed(() => Math.min(...values.value, 0))
const span = computed(() => maxV.value - minV.value || 1)
// Animate from the axis start (minV), so a bar grows out of the baseline.
const { values: anim } = useMotion(() => values.value.map((v) => v - minV.value), () => props.component.motion)

function widthPct(i: number): number {
  const v = anim.value[i] ?? values.value[i] - minV.value
  return Math.min(Math.max((v / span.value) * 100, 1), 100)
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
          <div class="a2ui-bar__fill" :class="colorClass(component.color)" :style="{ width: widthPct(i) + '%' }"></div>
        </div>
        <span class="a2ui-bar__value">{{ formatNumber(bar.value) }}</span>
      </div>
    </template>
  </div>
</template>
