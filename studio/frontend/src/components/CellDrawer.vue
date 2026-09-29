<script setup lang="ts">
// CellDrawer — a controlled disclosure for a notebook cell's SQL/card
// source (#95): the parent owns `open` and it is never persisted (view
// state only, reset closed on every load). The only close control is the
// summary button itself, so focus is already on it the moment the drawer
// closes — no focus-return code is needed here. Reduced motion is handled
// globally by theme/bridge.css (it zeroes transition-duration), so nothing
// special is done for that pref in this component either.
import { useId } from 'vue'
import Icon from './Icon.vue'

const props = defineProps<{ open: boolean; testid?: string }>()
const emit = defineEmits<{ toggle: [] }>()

const bodyId = useId()
</script>

<template>
  <button
    type="button"
    class="cell-drawer__summary"
    :class="{ 'is-open': props.open }"
    :aria-expanded="props.open"
    :aria-controls="bodyId"
    :data-testid="props.testid"
    @click="emit('toggle')"
  >
    <Icon name="chevronRight" :size="12" class="cell-drawer__caret" />
    <span class="cell-drawer__label"><slot name="summary" /></span>
  </button>
  <div
    :id="bodyId"
    class="cell-drawer"
    :class="{ 'is-open': props.open }"
    :inert="props.open ? undefined : true"
    :data-testid="props.testid ? `${props.testid}-body` : undefined"
  >
    <div class="cell-drawer__inner"><slot /></div>
  </div>
</template>

<style scoped>
.cell-drawer__summary {
  display: flex;
  align-items: center;
  gap: var(--sh-space-2);
  width: 100%;
  border: none;
  background: transparent;
  text-align: left;
  cursor: pointer;
  font-size: var(--sh-fs-xs);
  font-family: var(--sh-font-mono);
  color: var(--sh-graphite);
  padding: var(--sh-space-1) var(--sh-space-3);
}
.cell-drawer__summary:hover {
  background: var(--sh-paper-2);
}
.cell-drawer__label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cell-drawer__caret {
  flex: none;
  transition: transform 0.12s ease;
}
.cell-drawer__summary.is-open .cell-drawer__caret {
  transform: rotate(90deg);
}
.cell-drawer {
  display: grid;
  grid-template-rows: 0fr;
  transition: grid-template-rows 160ms ease;
}
.cell-drawer.is-open {
  grid-template-rows: 1fr;
}
.cell-drawer__inner {
  overflow: hidden;
  min-height: 0;
}
</style>
