<script setup lang="ts">
// A2UIColumnChart — the KINETIC "stagger cascade": vertical columns, one per
// label, each split into its numeric series either stacked or grouped
// ("mode"), optionally sorted by total ("sorted"). Every segment rises with
// the motion preset, staggered column by column. Heights are computed
// percentages bound through a numeric :style, never a model string.
import { computed } from 'vue'
import type { A2UIComponentT } from './types'
import { isBound } from './query'
import A2UIChartFrame from './A2UIChartFrame.vue'
import { useChartData } from './useChartData'
import { useMotion } from './useMotion'
import { seriesClass, toSeries } from './series'
import { formatNumber } from '../lib/formatCell'
import { useDrill } from './useDrill'
import { useRoving } from './useRoving'

const props = defineProps<{ component: A2UIComponentT }>()
const { loading, error, table } = useChartData(() => props.component)
const data = computed(() => toSeries(table.value))
const grouped = computed(() => props.component.mode === 'grouped')

// src: the result row this column was drawn from (sorting reorders columns, not rows).
interface Col { label: string; segs: number[]; total: number; src: number }
const cols = computed<Col[]>(() => {
  const out = data.value.labels.map((label, i) => {
    const segs = data.value.series.map((s) => Math.max(0, s.values[i]))
    return { label, segs, total: segs.reduce((a, b) => a + b, 0), src: i }
  })
  return props.component.sorted ? out.slice().sort((a, b) => b.total - a.total) : out
})
// Drill (data artifact view only): a column is one result row; click, or Enter
// or Space on a focused column, sets the drill parameter from that row's column.
const drill = useDrill(() => props.component, () => (isBound(props.component) && table.value.rows.length ? table.value : null))
const roving = useRoving(() => cols.value.length)

const k = computed(() => data.value.series.length)
const { values, replay, enabled } = useMotion(() => cols.value.flatMap((c) => c.segs), () => props.component.motion)

// Stacked columns scale to the tallest total; grouped bars to the tallest segment.
const scale = computed(() => {
  const m = grouped.value ? Math.max(0, ...cols.value.flatMap((c) => c.segs)) : Math.max(0, ...cols.value.map((c) => c.total))
  return m || 1
})
function h(ci: number, si: number): number {
  const v = values.value[ci * k.value + si] ?? 0
  return Math.max(0, Math.min((v / scale.value) * 100, 104))
}
const ariaLabel = computed(() => `Column chart, ${grouped.value ? 'grouped' : 'stacked'}, ${cols.value.length} columns of ${data.value.series.map((s) => s.name).join(', ')}`)
</script>

<template>
  <A2UIChartFrame
    kind="columns" :label="component.label" :sql="isBound(component)" :loading="loading && cols.length === 0" :error="error"
    :empty="cols.length === 0 || k === 0" :animated="enabled" :legend="data.series.map((s) => s.name)"
    @replay="replay"
  >
    <div v-if="drill.problem.value" class="a2ui-error" role="alert">{{ drill.problem.value }}</div>
    <div
      class="a2ui-columns" :role="drill.enabled.value ? 'group' : 'img'" :aria-label="ariaLabel"
      @keydown="drill.enabled.value && roving.onKeydown($event)" @focusin="roving.onFocusin"
    >
      <div
        v-for="(c, ci) in cols" :key="c.label + ci" class="a2ui-columns__col"
        :class="{ 'a2ui-drill-row': drill.enabled.value, 'a2ui-drill-row--current': drill.isCurrent(c.src) }"
        v-bind="drill.enabled.value ? { role: 'button', 'data-roving': '', 'data-roving-i': ci, tabindex: roving.tab(ci), 'aria-current': drill.isCurrent(c.src) ? 'true' : undefined, 'aria-label': `${c.label}: ${formatNumber(c.total)}. ${drill.action(c.src)}` } : {}"
        @click="drill.drill(c.src)"
        @keydown="drill.enabled.value && drill.onKey($event, c.src)"
      >
        <span class="a2ui-columns__total">{{ formatNumber(c.total) }}</span>
        <div class="a2ui-columns__track" :class="grouped ? 'a2ui-columns__track--grouped' : 'a2ui-columns__track--stacked'">
          <div
            v-for="(seg, si) in c.segs" :key="si"
            class="a2ui-columns__seg" :class="seriesClass(si)"
            :style="{ height: h(ci, si) + '%' }"
            :title="`${c.label} · ${data.series[si]?.name}: ${formatNumber(seg)}`"
          ></div>
        </div>
        <span class="a2ui-columns__label" :title="c.label">{{ c.label }}</span>
      </div>
    </div>
    <div v-if="drill.error.value" class="a2ui-error" role="alert" data-testid="a2ui-drill-error">{{ drill.error.value }}</div>
  </A2UIChartFrame>
</template>
