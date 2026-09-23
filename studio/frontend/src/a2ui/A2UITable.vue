<script setup lang="ts">
// A2UITable — inline rows/columns, or a live "sql" binding. Ported from
// renderer.js's renderTable/buildTable.
import { ref, onMounted, computed, inject } from 'vue'
import type { A2UIComponentT } from './types'
import { runLiveQuery, formatCell } from './query'

const props = defineProps<{ component: A2UIComponentT }>()
const abortSignal = inject<AbortSignal | undefined>('a2uiAbortSignal', undefined)

const loading = ref(false)
const error = ref('')
const liveColumns = ref<string[]>([])
const liveRows = ref<any[][]>([])

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
  liveColumns.value = columns
  liveRows.value = rows
})

const columns = computed<string[]>(() => (props.component.sql ? liveColumns.value : (props.component.columns ?? [])))
const rows = computed<any[][]>(() => (props.component.sql ? liveRows.value : (props.component.rows ?? [])))
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
          <th v-for="(c, i) in columns" :key="i">{{ c }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, ri) in rows" :key="ri">
          <td v-for="(cell, ci) in row" :key="ci" :class="{ 'a2ui-td--num': typeof cell === 'number' }">
            {{ formatCell(cell) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
