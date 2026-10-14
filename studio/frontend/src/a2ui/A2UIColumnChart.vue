<script setup lang="ts">
// A2UIColumnChart — the KINETIC "stagger cascade": vertical columns, one per
// label, each split into its numeric series either stacked or grouped
// ("mode"), optionally sorted by total ("sorted"). Every segment rises with
// the motion preset, staggered column by column. Heights are computed
// percentages bound through a numeric :style, never a model string.
import { computed } from 'vue'
import type { A2UIComponentT } from './types'
import A2UIChartFrame from './A2UIChartFrame.vue'
import { useChartData } from './useChartData'
import { useMotion } from './useMotion'
import { seriesClass, toSeries } from './series'
import { formatNumber } from '../lib/formatCell'

const props = defineProps<{ component: A2UIComponentT }>()
const { loading, error, table } = useChartData(() => props.component)
const data = computed(() => toSeries(table.value))
const grouped = computed(() => props.component.mode === 'grouped')

interface Col { label: string; segs: number[]; total: number }
const cols = computed<Col[]>(() => {
  const out = data.value.labels.map((label, i) => {
    const segs = data.value.series.map((s) => Math.max(0, s.values[i]))
    return { label, segs, total: segs.reduce((a, b) => a + b, 0) }
  })
  return props.component.sorted ? out.slice().sort((a, b) => b.total - a.total) : out
})
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
    kind="columns" :label="component.label" :sql="!!component.sql" :loading="loading" :error="error"
    :empty="cols.length === 0 || k === 0" :animated="enabled" :legend="data.series.map((s) => s.name)"
    @replay="replay"
  >
    <div class="a2ui-columns" role="img" :aria-label="ariaLabel">
      <div v-for="(c, ci) in cols" :key="c.label + ci" class="a2ui-columns__col">
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
  </A2UIChartFrame>
</template>
