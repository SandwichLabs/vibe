<script setup lang="ts">
// A2UILineChart — an SVG line chart, inline "points" or a live "sql" binding.
// Ported from renderer.js's renderLineChart/buildLine: role="img" +
// aria-label (a line with no scale is decoration, and a chart with no text
// alternative is inaccessible), and a labeled top/bottom gridline.
//
// Review fix (#67 step 2 round 2): axis labels render as HTML, absolutely
// positioned over/below the SVG by percentage of the viewBox, NOT as SVG
// <text> at a fixed 9 "user units" font-size. The agent panel is ~410px
// wide; the 640-unit viewBox scales down to roughly half that, so a 9-unit
// SVG text element rendered there is closer to 5 actual screen pixels —
// illegible. HTML text is sized in real CSS px regardless of how much the
// SVG itself is scaled, so it stays >=11px on screen at any panel width.
// Y-axis label POSITION still uses percentage of the viewBox (the SVG's
// rendered box always keeps the viewBox's aspect ratio, so a percentage
// lands in the same place at any container width) — but which X labels to
// SHOW is decided from a real measured pixel width (see labelIndices):
// picking by a fixed "≤8" index count assumed a wide desktop context, and
// in this ~300px-wide card even 6 monthly labels ("2026-01".."2026-06")
// overlapped each other (caught live in QA screenshot, not by a unit
// test — jsdom's measured widths are unreliable, so the fixed-width JS
// unit tests assert the algorithm directly rather than pixel overlap).
import { ref, onMounted, nextTick, watch, computed } from 'vue'
import type { A2UIComponentT } from './types'
import { toPoints, colorClass, pickLabelIndices, type Point } from './query'
import { useLiveQuery } from './useLiveQuery'
import { formatNumber } from '../lib/formatCell'
import { useMotion } from './useMotion'

const props = defineProps<{ component: A2UIComponentT }>()
const { loading, error, data } = useLiveQuery(() => props.component)
const livePoints = computed<Point[]>(() => (data.value ? toPoints(data.value.columns, data.value.rows) : []))

const points = computed<Point[]>(() => (props.component.sql ? livePoints.value : (props.component.points ?? [])))

const W = 640
const H = 200
const PAD = { top: 12, right: 12, bottom: 12, left: 12 }
const innerW = W - PAD.left - PAD.right
const innerH = H - PAD.top - PAD.bottom

const values = computed(() => points.value.map((p) => p.value))
const maxV = computed(() => Math.max(...values.value))
const minV = computed(() => Math.min(...values.value))
const span = computed(() => maxV.value - minV.value || 1)
// The line rises from the bottom gridline with the component's "motion"
// preset; axis labels and tooltips always show the real values.
const { values: anim } = useMotion(() => values.value.map((v) => v - minV.value), () => props.component.motion)
function animY(i: number): number {
  return y((anim.value[i] ?? values.value[i] - minV.value) + minV.value)
}

function x(i: number): number {
  const n = points.value.length
  return PAD.left + (n === 1 ? innerW / 2 : (i / (n - 1)) * innerW)
}
function y(v: number): number {
  return PAD.top + innerH - ((v - minV.value) / span.value) * innerH
}
function xPct(i: number): number {
  return (x(i) / W) * 100
}
function yPct(v: number): number {
  return (y(v) / H) * 100
}

// axisRowEl/containerWidthPx: the real measured width of the X-axis label
// row, re-measured whenever `points` changes (a sql-bound chart's row
// doesn't exist in the DOM at all until the query resolves). Falls back to
// treating the viewBox's own W (640) as the pixel width before the first
// measurement, which is only ever a brief, one-frame default.
const axisRowEl = ref<HTMLElement | null>(null)
const containerWidthPx = ref(0)
async function measureWidth() {
  await nextTick()
  containerWidthPx.value = axisRowEl.value?.clientWidth || 0
}
onMounted(measureWidth)
watch(points, measureWidth)

// Which point indices get an X-axis label — see pickLabelIndices (query.ts)
// for the algorithm and why it replaced a fixed "at most 8, evenly by
// index" thinning.
const labelIndices = computed<number[]>(() =>
  pickLabelIndices(
    points.value.map((p) => p.label),
    containerWidthPx.value,
    W,
  ),
)
function showLabel(i: number): boolean {
  return labelIndices.value.includes(i)
}

// labelStyle anchors each X-axis label so none of them can clip past the
// card's edge (review fix): every label used to be centered
// (translateX(-50%)) on its point, including the first and last — whose
// points sit at the very left/right edge of the plot, so half of THEIR
// label text rendered outside the card (e.g. "2026-06" clipped to
// "2026-0"). The first label anchors its left edge to its point (grows
// rightward, into the card); the last anchors its right edge (grows
// leftward); everything in between still centers on its point.
function labelStyle(i: number): Record<string, string> {
  const left = `${xPct(i)}%`
  if (i === 0) return { left, transform: 'translateX(0)' }
  if (i === points.value.length - 1) return { left, transform: 'translateX(-100%)' }
  return { left, transform: 'translateX(-50%)' }
}

const ariaLabel = computed(
  () => `Line chart of ${points.value.length} points, ranging ${formatNumber(minV.value)} to ${formatNumber(maxV.value)}`,
)
const polylinePoints = computed(() => points.value.map((_, i) => `${x(i)},${animY(i)}`).join(' '))
</script>

<template>
  <div class="a2ui-linechart">
    <div v-if="component.label" class="a2ui-component__title">{{ component.label }}</div>
    <div v-if="component.sql && loading" class="a2ui-loading">RUNNING QUERY…</div>
    <div v-else-if="component.sql && error" class="a2ui-error">QUERY ERROR: {{ error }}</div>
    <div v-else-if="points.length === 0" class="a2ui-empty">NO ROWS</div>
    <div v-else class="a2ui-linechart__wrap">
      <div class="a2ui-linechart__plot">
        <svg :viewBox="`0 0 ${W} ${H}`" :class="['a2ui-linechart__svg', colorClass(component.color)]" role="img" :aria-label="ariaLabel">
          <line :x1="PAD.left" :x2="W - PAD.right" :y1="y(maxV)" :y2="y(maxV)" class="a2ui-linechart__grid a2ui-linechart__grid--top" />
          <line :x1="PAD.left" :x2="W - PAD.right" :y1="y(minV)" :y2="y(minV)" class="a2ui-linechart__grid a2ui-linechart__grid--bottom" />
          <polyline class="a2ui-linechart__line" :points="polylinePoints" />
          <circle v-for="(p, i) in points" :key="'dot' + i" :cx="x(i)" :cy="animY(i)" r="4" class="a2ui-linechart__dot">
            <title>{{ p.label }}: {{ formatNumber(p.value) }}</title>
          </circle>
        </svg>
        <div class="a2ui-linechart__axis-y" :style="{ top: yPct(maxV) + '%' }">{{ formatNumber(maxV) }}</div>
        <div class="a2ui-linechart__axis-y" :style="{ top: yPct(minV) + '%' }">{{ formatNumber(minV) }}</div>
      </div>
      <div ref="axisRowEl" class="a2ui-linechart__axis-x-row">
        <template v-for="(p, i) in points" :key="'lbl' + i">
          <div v-if="showLabel(i)" class="a2ui-linechart__axis-x" :style="labelStyle(i)">{{ p.label }}</div>
        </template>
      </div>
    </div>
  </div>
</template>
