<script setup lang="ts">
// A2UIStat — a single labeled number, optionally live via "sql" (the query's
// first column of its first row). Ported from renderer.js's renderStat.
import { ref, onMounted, computed, inject } from 'vue'
import type { A2UIComponentT } from './types'
import { runLiveQuery, colorClass } from './query'
import { formatCell } from '../lib/formatCell'

const props = defineProps<{ component: A2UIComponentT }>()
const abortSignal = inject<AbortSignal | undefined>('a2uiAbortSignal', undefined)

const loading = ref(false)
const error = ref('')
const liveValue = ref<any>(undefined)
const liveType = ref<string | undefined>(undefined)
const liveFetched = ref(false)

onMounted(async () => {
  if (!props.component.sql) return
  loading.value = true
  const { columns, types, rows, error: err, aborted } = await runLiveQuery(props.component.sql, abortSignal)
  if (aborted) return // caller stopped caring (unmounted/reloaded) — never apply a stale result
  loading.value = false
  liveFetched.value = true
  if (err) {
    error.value = err
    return
  }
  const first = rows[0]
  if (!first) {
    liveValue.value = null
    return
  }
  liveValue.value = first[0] ?? (columns.length ? first[0] : undefined)
  liveType.value = types[0]
})

// component.types[0] is the client-only type Studio's sql-cell
// synthesizer stamps on a synthesized stat component — never part of the
// A2UI schema, never saved (see a2ui/types.ts).
const displayValue = computed(() => {
  if (props.component.sql) {
    if (loading.value) return '…'
    if (error.value) return '!'
    if (!liveFetched.value) return ''
    if (liveValue.value === null || liveValue.value === undefined) return '—'
    return formatCell(liveValue.value, liveType.value, { context: 'report' }).text
  }
  return props.component.value !== undefined
    ? formatCell(props.component.value, props.component.types?.[0], { context: 'report' }).text
    : ''
})
</script>

<template>
  <div class="a2ui-stat" :class="colorClass(component.color)">
    <div class="a2ui-stat__label">{{ component.label }}</div>
    <div class="a2ui-stat__value">{{ displayValue }}</div>
    <div v-if="component.detail" class="a2ui-stat__detail">{{ component.detail }}</div>
    <div v-if="component.sql && error" class="a2ui-error">QUERY ERROR: {{ error }}</div>
  </div>
</template>
