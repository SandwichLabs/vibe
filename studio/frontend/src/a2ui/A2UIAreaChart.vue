<script setup lang="ts">
// A2UIAreaChart — the KINETIC "morphing spline": one smooth Catmull-Rom area
// per numeric column over a shared label axis, every point animated with the
// chart's motion preset. Hover (or focus) a column to read every series at
// that label.
import { computed, ref } from 'vue'
import type { A2UIComponentT } from './types'
import A2UIChartFrame from './A2UIChartFrame.vue'
import { useChartData } from './useChartData'
import { useMotion } from './useMotion'
import { areaPath, seriesClass, splinePath, toSeries, type XY } from './series'
import { formatNumber } from '../lib/formatCell'

const props = defineProps<{ component: A2UIComponentT }>()
const { loading, error, table } = useChartData(() => props.component)
const data = computed(() => toSeries(table.value))
const flat = computed(() => data.value.series.flatMap((s) => s.values))
const { values, replay, enabled } = useMotion(() => flat.value, () => props.component.motion)

const W = 560
const H = 200
const PAD = 16
const all = computed(() => flat.value.concat(0))
const maxV = computed(() => Math.max(...all.value))
const minV = computed(() => Math.min(...all.value))
const span = computed(() => maxV.value - minV.value || 1)
const n = computed(() => data.value.labels.length)

function x(i: number): number {
  return n.value <= 1 ? W / 2 : PAD + (i / (n.value - 1)) * (W - PAD * 2)
}
function y(v: number): number {
  return PAD + (H - PAD * 2) - ((v - minV.value) / span.value) * (H - PAD * 2)
}
const baseline = computed(() => y(Math.max(minV.value, Math.min(0, maxV.value))))

const curves = computed(() =>
  data.value.series.map((s, si) => {
    const pts: XY[] = s.values.map((_, i) => ({ x: x(i), y: y(values.value[si * n.value + i] ?? 0) }))
    return { name: s.name, line: splinePath(pts), area: areaPath(pts, baseline.value), pts }
  }),
)

const hover = ref<number | null>(null)
const active = computed(() => hover.value ?? n.value - 1)
const ariaLabel = computed(
  () => `Area chart of ${data.value.series.map((s) => s.name).join(', ')} over ${n.value} labels, ranging ${formatNumber(minV.value)} to ${formatNumber(maxV.value)}`,
)
</script>

<template>
  <A2UIChartFrame
    kind="area" :label="component.label" :sql="!!component.sql" :loading="loading" :error="error"
    :empty="n === 0 || data.series.length === 0" :animated="enabled" :legend="data.series.map((s) => s.name)"
    @replay="replay"
  >
    <div class="a2ui-kchart__readout" aria-live="polite">
      <span>{{ data.labels[active] }}</span>
      <span v-for="(s, si) in data.series" :key="si" class="a2ui-kchart__readval" :class="seriesClass(si)">{{ formatNumber(s.values[active]) }}</span>
    </div>
    <svg :viewBox="`0 0 ${W} ${H}`" class="a2ui-kchart__svg" role="img" :aria-label="ariaLabel" @mouseleave="hover = null">
      <line v-for="r in [0.2, 0.5, 0.8]" :key="r" :x1="PAD" :x2="W - PAD" :y1="PAD + r * (H - PAD * 2)" :y2="PAD + r * (H - PAD * 2)" class="a2ui-kchart__grid" />
      <line v-if="hover !== null" :x1="x(hover)" :x2="x(hover)" :y1="PAD" :y2="H - PAD" class="a2ui-kchart__cursor" />
      <g v-for="(c, si) in curves" :key="si" :class="seriesClass(si)">
        <path :d="c.area" class="a2ui-area__fill" />
        <path :d="c.line" class="a2ui-area__line" :class="{ 'a2ui-area__line--secondary': si > 0 }" />
        <circle v-for="(p, i) in c.pts" :key="i" :cx="p.x" :cy="p.y" :r="hover === i ? 5 : 3" class="a2ui-area__dot" />
      </g>
      <rect
        v-for="(_, i) in data.labels" :key="'hit' + i"
        :x="x(i) - (W / Math.max(n, 1)) / 2" y="0" :width="W / Math.max(n, 1)" :height="H"
        class="a2ui-kchart__hit" @mouseenter="hover = i"
      ><title>{{ data.labels[i] }}: {{ data.series.map((s) => `${s.name} ${formatNumber(s.values[i])}`).join(', ') }}</title></rect>
    </svg>
    <div class="a2ui-kchart__xaxis">
      <span>{{ data.labels[0] }}</span>
      <span v-if="n > 1">{{ data.labels[n - 1] }}</span>
    </div>
  </A2UIChartFrame>
</template>
