<script setup lang="ts">
// A2UIPivot — a long-form query result as a crosstab (#162, D6). Layout only:
// buildPivot (pivot.ts) does the arrangement and the component never
// aggregates; totals are rows the query returned. A real <table>: a column
// group header, column headers (scope="col") and row headers (scope="row").
// Every header and cell is text interpolation; a long header is clipped by CSS
// and carries its full text as a title. With `drill`, each filled cell is a
// button (one tab stop, arrow keys between cells) that sets the drill parameter.
import { computed, inject, useId } from 'vue'
import type { A2UIComponentT } from './types'
import { ARTIFACT_RUNTIME } from './artifactRuntime'
import { useBoundQuery } from './useBoundQuery'
import { buildPivot } from './pivot'
import { useDrill } from './useDrill'
import { useRoving } from './useRoving'

const props = defineProps<{ component: A2UIComponentT }>()
const rt = inject(ARTIFACT_RUNTIME, undefined)
const bound = rt ? useBoundQuery(rt, () => props.component) : null
const uid = useId()

const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])
const pivot = computed(() => {
  const d = bound?.data.value
  if (!d) return null
  const c = props.component
  return buildPivot(d.columns, d.types, d.rows, { rows: strings(c.rows), columns: String(c.columns ?? ''), value: String(c.value ?? '') })
})
const grid = computed(() => (pivot.value?.ok ? pivot.value.grid : null))
const problem = computed(() => (pivot.value && !pivot.value.ok ? pivot.value.error : ''))

const drill = useDrill(() => props.component, () => bound?.data.value ?? null)
// A running index over the filled cells, in reading order.
const cellIndex = computed(() => {
  let n = 0
  return (grid.value?.rows ?? []).map((r) => r.cells.map((c) => (c ? n++ : -1)))
})
const cellCount = computed(() => cellIndex.value.reduce((n, r) => n + r.filter((i) => i >= 0).length, 0))
const roving = useRoving(() => cellCount.value)
const short = (s: string) => (s.length > 60 ? s.slice(0, 60) + '…' : s)
// The drilled row's header carries the mark when the drill column is a row key; otherwise its cells do.
const keyDrill = computed(() => !!grid.value && grid.value.rowColumns.includes(drill.column.value))
const rowCurrent = (cells: ({ src: number } | null)[]) => cells.some((c) => c && drill.isCurrent(c.src))
const cur = (b: boolean) => (b ? 'true' : undefined)
</script>

<template>
  <div class="a2ui-tablewrap a2ui-pivotwrap" data-testid="a2ui-pivot">
    <div v-if="component.label" :id="`${uid}-t`" class="a2ui-component__title">{{ component.label }}</div>
    <div v-if="!rt" class="a2ui-error">A pivot is only available in a data artifact.</div>
    <div v-else-if="bound?.loading.value && !bound.data.value" class="a2ui-loading">RUNNING QUERY…</div>
    <div v-else-if="bound?.error.value" class="a2ui-error">QUERY ERROR: {{ bound.error.value }}</div>
    <div v-else-if="problem" class="a2ui-error" role="alert">PIVOT ERROR: {{ problem }}</div>
    <div v-else-if="!grid || grid.rows.length === 0" class="a2ui-empty">NO ROWS</div>
    <template v-else>
      <div v-if="drill.problem.value" class="a2ui-error" role="alert">{{ drill.problem.value }}</div>
      <div v-if="bound?.truncated.value" class="a2ui-pivot__note" data-testid="a2ui-pivot-truncated">Showing the first 500 rows of the result; the grid is incomplete.</div>
      <table
        class="a2ui-table a2ui-pivot" :aria-labelledby="component.label ? `${uid}-t` : undefined"
        @keydown="drill.enabled.value && roving.onKeydown($event)" @focusin="roving.onFocusin"
      >
        <thead>
          <tr>
            <td v-if="grid.rowColumns.length" :colspan="grid.rowColumns.length" class="a2ui-pivot__corner"></td>
            <th :colspan="Math.max(grid.colKeys.length, 1)" scope="colgroup" class="a2ui-pivot__group" :title="String(component.columns)">{{ component.columns }}</th>
          </tr>
          <tr>
            <th v-for="(rc, i) in grid.rowColumns" :key="`rc${i}`" scope="col" class="a2ui-pivot__head" :title="rc">{{ rc }}</th>
            <th v-for="(ck, i) in grid.colKeys" :key="`ck${i}`" scope="col" class="a2ui-pivot__head cell-num" :title="ck">{{ ck }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, ri) in grid.rows" :key="ri">
            <th
              v-for="(k, ki) in row.keys" :key="`k${ki}`" scope="row" class="a2ui-pivot__rowhead" :title="k"
              :class="{ 'a2ui-pivot__rowhead--current': keyDrill && rowCurrent(row.cells) }" :aria-current="cur(keyDrill && rowCurrent(row.cells))"
            >{{ k }}</th>
            <td v-for="(cell, ci) in row.cells" :key="ci" :class="{ 'cell-num': cell?.numeric }" :title="cell?.title">
              <template v-if="cell">
                <button
                  v-if="drill.enabled.value" type="button" class="a2ui-drill a2ui-pivot__btn"
                  :class="{ 'a2ui-drill--current': !keyDrill && drill.isCurrent(cell.src) }" :aria-current="cur(!keyDrill && drill.isCurrent(cell.src))"
                  :aria-label="`${row.keys.map(short).join(', ')}, ${short(grid.colKeys[ci])}: ${cell.text}. ${drill.action(cell.src)}`"
                  data-roving :data-roving-i="cellIndex[ri][ci]" :data-col="ci" :tabindex="roving.tab(cellIndex[ri][ci])"
                  @click="drill.drill(cell.src)"
                >{{ cell.text }}</button>
                <template v-else>{{ cell.text }}</template>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-if="bound?.loading.value" class="a2ui-loading" role="status">RUNNING QUERY…</div>
      <div v-if="drill.error.value" class="a2ui-error" role="alert" data-testid="a2ui-drill-error">{{ drill.error.value }}</div>
    </template>
  </div>
</template>
