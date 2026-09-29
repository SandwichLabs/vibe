<script setup lang="ts">
// A2UITable — inline rows/columns, or a live "sql" binding. Ported from
// renderer.js's renderTable/buildTable.
import { ref, onMounted, computed, inject } from 'vue'
import type { A2UIComponentT } from './types'
import { runLiveQuery } from './query'
import { formatCell, cellClass, isNumericType } from '../lib/formatCell'

const props = defineProps<{ component: A2UIComponentT }>()
const abortSignal = inject<AbortSignal | undefined>('a2uiAbortSignal', undefined)

const loading = ref(false)
const error = ref('')
const liveColumns = ref<string[]>([])
const liveTypes = ref<string[]>([])
const liveRows = ref<any[][]>([])

onMounted(async () => {
  if (!props.component.sql) return
  loading.value = true
  const { columns, types, rows, error: err, aborted } = await runLiveQuery(props.component.sql, abortSignal)
  if (aborted) return // caller stopped caring (unmounted/reloaded) — never apply a stale result
  loading.value = false
  if (err) {
    error.value = err
    return
  }
  liveColumns.value = columns
  liveTypes.value = types
  liveRows.value = rows
})

const columns = computed<string[]>(() => (props.component.sql ? liveColumns.value : (props.component.columns ?? [])))
const rows = computed<any[][]>(() => (props.component.sql ? liveRows.value : (props.component.rows ?? [])))
// types is client-only (§4b): live queries get it from RunA2UIQuery; inline
// (model-authored) data has no column type, and synthesized notebook pages
// stamp it onto the component before render (never saved, never validated —
// see a2ui/types.ts).
const types = computed<string[]>(() => (props.component.sql ? liveTypes.value : (props.component.types ?? [])))

// One formatCell call per cell: precomputed once here rather than called
// 3x per cell (class/title/text) from the template.
const formattedRows = computed(() =>
  rows.value.map((row) => row.map((cell, ci) => formatCell(cell, types.value[ci], { context: 'report' }))),
)
</script>

<template>
  <div class="a2ui-tablewrap">
    <div v-if="component.label" class="a2ui-component__title">{{ component.label }}</div>
    <div v-if="component.sql && loading" class="a2ui-loading">RUNNING QUERY…</div>
    <div v-else-if="component.sql && error" class="a2ui-error">QUERY ERROR: {{ error }}</div>
    <div v-else-if="rows.length === 0" class="a2ui-empty">NO ROWS</div>
    <table v-else class="a2ui-table">
      <thead>
        <tr>
          <th v-for="(c, i) in columns" :key="i" :class="{ 'cell-num': isNumericType(types[i]) }">{{ c }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, ri) in formattedRows" :key="ri">
          <td
            v-for="(fc, ci) in row"
            :key="ci"
            :class="cellClass(fc, types[ci])"
            :title="fc.title"
          >
            {{ fc.text }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
