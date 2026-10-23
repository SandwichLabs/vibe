<script setup lang="ts">
// TypePicker: step 1 of the New dialog (docs/plans/library.md L6). The cards
// are one radiogroup with a roving tabindex: arrows move the selection, Space
// selects, Enter asks for the next step. "Starts with a script" is its own
// control after the group (shown for Page only) and Advanced adds Chart script
// as a fourth radio in the same group.
import { computed, ref } from 'vue'
import Icon from './Icon.vue'
import { TYPE_ICON, TYPE_LABEL } from '../lib/libraryItems'
import type { LibraryType } from '../lib/libraryRoutes'
import { CARD_LINE, RUNS_AFTER_ALLOW } from '../lib/newItem'

const type = defineModel<LibraryType>({ required: true })
const withScript = defineModel<boolean>('withScript', { required: true })
const advanced = defineModel<boolean>('advanced', { required: true })
const emit = defineEmits<{ next: [] }>()

const group = ref<HTMLElement | null>(null)
const order = computed<LibraryType[]>(() => (advanced.value ? ['notebook', 'live', 'page', 'script'] : ['notebook', 'live', 'page']))

function focusCard(t: LibraryType) { group.value?.querySelector<HTMLElement>(`[data-type="${t}"]`)?.focus() }
defineExpose({ focusSelected: () => focusCard(type.value) })

function onKey(e: KeyboardEvent, t: LibraryType) {
  const i = order.value.indexOf(t)
  const step = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 0
  if (step) {
    e.preventDefault()
    const to = order.value[(i + step + order.value.length) % order.value.length]
    type.value = to
    queueMicrotask(() => focusCard(to))
  } else if (e.key === 'Home' || e.key === 'End') {
    e.preventDefault()
    const to = e.key === 'Home' ? order.value[0] : order.value[order.value.length - 1]
    type.value = to
    queueMicrotask(() => focusCard(to))
  } else if (e.key === 'Enter') {
    e.preventDefault()
    type.value = t
    emit('next')
  }
}
function toggleAdvanced() {
  advanced.value = !advanced.value
  if (!advanced.value && type.value === 'script') type.value = 'page'
}
</script>

<template>
  <div>
    <div ref="group" class="tp-group" :class="{ 'tp-group--4': advanced }" role="radiogroup" aria-label="What are you making?">
      <button v-for="t in order" :key="t" type="button" role="radio" class="tp-card" :class="{ 'tp-card--on': type === t }" :data-type="t" :data-testid="`type-${t}`"
        :aria-checked="type === t" :aria-labelledby="`tp-h-${t}`" :aria-describedby="`tp-d-${t}`" :tabindex="type === t ? 0 : -1" @click="type = t" @keydown="onKey($event, t)">
        <span :id="`tp-h-${t}`" class="tp-head"><Icon :name="TYPE_ICON[t]" :size="16" /> {{ TYPE_LABEL[t] }}</span>
        <span :id="`tp-d-${t}`" class="tp-line">{{ CARD_LINE[t] }}</span>
        <span v-if="t === 'script'" class="badge badge--warn tp-badge" data-testid="badge-script">{{ RUNS_AFTER_ALLOW }}</span>
      </button>
    </div>
    <div v-if="type === 'page'" class="tp-script">
      <label class="tp-check"><input v-model="withScript" type="checkbox" data-testid="with-script" /> Starts with a script</label>
      <span v-if="withScript" class="badge badge--warn" data-testid="badge-page-script">{{ RUNS_AFTER_ALLOW }}</span>
    </div>
    <button type="button" class="tp-adv" :aria-expanded="advanced" data-testid="advanced-toggle" @click="toggleAdvanced">
      <Icon :name="advanced ? 'chevronDown' : 'chevronRight'" :size="14" /> Advanced
    </button>
  </div>
</template>

<style scoped>
.tp-group { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--sh-space-3); }
.tp-card { display: flex; flex-direction: column; align-items: flex-start; gap: var(--sh-space-2); text-align: left; font: inherit; color: var(--sh-ink); background: var(--sh-paper); border: 2px solid var(--vibe-border); border-radius: var(--sh-radius); padding: var(--sh-space-3); cursor: pointer; }
.tp-card:focus-visible { box-shadow: var(--sh-focus-ring), var(--sh-shadow-teal); }
.tp-card--on { border-color: var(--sh-teal); box-shadow: var(--sh-shadow-teal); background: var(--sh-paper-2); }
.tp-head { display: flex; align-items: center; gap: var(--sh-space-2); font-weight: 600; }
.tp-line { font-size: var(--sh-fs-xs); color: var(--sh-graphite); }
.tp-group--4 { grid-template-columns: repeat(4, minmax(0, 1fr)); }
.tp-script { display: flex; align-items: center; gap: var(--sh-space-3); margin-top: var(--sh-space-3); flex-wrap: wrap; }
.tp-check { display: inline-flex; align-items: center; gap: var(--sh-space-2); cursor: pointer; }
.tp-adv { margin-top: var(--sh-space-3); display: inline-flex; align-items: center; gap: var(--sh-space-1); font: inherit; font-size: var(--sh-fs-sm); background: none; border: 0; color: var(--sh-ink); cursor: pointer; padding: 2px 4px; }
@media (max-width: 640px) { .tp-group, .tp-group--4 { grid-template-columns: 1fr; } }
</style>
