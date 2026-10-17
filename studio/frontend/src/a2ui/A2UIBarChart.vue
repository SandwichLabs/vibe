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
import { toPoints, colorClass, type Point, isBound } from './query'
import { useLiveQuery } from './useLiveQuery'
import { formatNumber } from '../lib/formatCell'
import { useMotion } from './useMotion'
import { useDrill } from './useDrill'
import { useRoving } from './useRoving'

const props = defineProps<{ component: A2UIComponentT }>()
const { loading, error, data } = useLiveQuery(() => props.component)
const livePoints = computed<Point[]>(() => (data.value ? toPoints(data.value.columns, data.value.rows) : []))

const points = computed<Point[]>(() => (isBound(props.component) ? livePoints.value : (props.component.bars ?? [])))
const values = computed(() => points.value.map((p) => p.value))
const maxV = computed(() => Math.max(...values.value))
const minV = computed(() => Math.min(...values.value, 0))
const span = computed(() => maxV.value - minV.value || 1)
// Animate from the axis start (minV), so a bar grows out of the baseline.
const { values: anim } = useMotion(() => values.value.map((v) => v - minV.value), () => props.component.motion)

// Drill (data artifact view only): a bar is one result row; click, or Enter
// or Space on a focused bar, sets the drill parameter from that row's column.
const drill = useDrill(() => props.component, () => (isBound(props.component) && data.value ? data.value : null))
const roving = useRoving(() => points.value.length)

function widthPct(i: number): number {
  const v = anim.value[i] ?? values.value[i] - minV.value
  return Math.min(Math.max((v / span.value) * 100, 1), 100)
}
</script>

<template>
  <div class="a2ui-barchart" @keydown="drill.enabled.value && roving.onKeydown($event)" @focusin="roving.onFocusin">
    <div v-if="component.label" class="a2ui-component__title">{{ component.label }}</div>
    <div v-if="isBound(component) && loading && !data" class="a2ui-loading">RUNNING QUERY…</div>
    <div v-else-if="isBound(component) && error" class="a2ui-error">QUERY ERROR: {{ error }}</div>
    <div v-else-if="points.length === 0" class="a2ui-empty">NO ROWS</div>
    <template v-else>
      <div v-if="drill.problem.value" class="a2ui-error" role="alert">{{ drill.problem.value }}</div>
      <div
        v-for="(bar, i) in points" :key="i" class="a2ui-bar"
        :class="{ 'a2ui-drill-row': drill.enabled.value, 'a2ui-drill-row--current': drill.isCurrent(i) }"
        v-bind="drill.enabled.value ? { role: 'button', 'data-roving': '', 'data-roving-i': i, tabindex: roving.tab(i), 'aria-current': drill.isCurrent(i) ? 'true' : undefined, 'aria-label': `${bar.label}: ${formatNumber(bar.value)}. ${drill.action(i)}` } : {}"
        @click="drill.drill(i)"
        @keydown="drill.enabled.value && drill.onKey($event, i)"
      >
        <span class="a2ui-bar__label">{{ bar.label }}</span>
        <div class="a2ui-bar__track">
          <div class="a2ui-bar__fill" :class="colorClass(component.color)" :style="{ width: widthPct(i) + '%' }"></div>
        </div>
        <span class="a2ui-bar__value">{{ formatNumber(bar.value) }}</span>
      </div>
      <div v-if="isBound(component) && loading" class="a2ui-loading" role="status">RUNNING QUERY…</div>
      <div v-if="drill.error.value" class="a2ui-error" role="alert" data-testid="a2ui-drill-error">{{ drill.error.value }}</div>
    </template>
  </div>
</template>
