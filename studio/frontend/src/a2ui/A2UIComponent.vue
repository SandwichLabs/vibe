<script setup lang="ts">
// A2UIComponent — the recursive per-node dispatcher. Ported from renderer.js's
// RENDERERS table + the build()/childMap tree walk in renderPage: children
// attach to their parent by id (never by DOM nesting authored by the model),
// resolved once in A2UIPage.vue's childMap and looked up here via inject.
//
// container/statRow render their children *inside* themselves (the only two
// types meant to hold content); every other type renders its own content
// followed by any children as siblings, matching renderer.js's build(), which
// appends children onto whatever node a type produced regardless of type.
import { computed, inject, type ComputedRef } from 'vue'
import type { A2UIComponentT } from './types'
import { colorClass, clampGap } from './query'
import A2UIStat from './A2UIStat.vue'
import A2UITable from './A2UITable.vue'
import A2UIBarChart from './A2UIBarChart.vue'
import A2UILineChart from './A2UILineChart.vue'

const props = defineProps<{ component: A2UIComponentT }>()

const childMap = inject<ComputedRef<Map<string, A2UIComponentT[]>>>('a2uiChildMap')!
const children = computed<A2UIComponentT[]>(() => childMap.value.get(props.component.id) ?? [])

const holdsChildrenItself = computed(() => props.component.type === 'container' || props.component.type === 'statRow')

const headingTag = computed(() => `h${props.component.level ?? 2}`)
</script>

<template>
  <component :is="headingTag" v-if="component.type === 'heading'" class="a2ui-heading">{{ component.text }}</component>

  <p v-else-if="component.type === 'text'" class="a2ui-text" :class="{ 'a2ui-text--muted': component.muted }">{{ component.text }}</p>

  <hr v-else-if="component.type === 'divider'" class="a2ui-divider" />

  <div v-else-if="component.type === 'callout'" class="a2ui-callout" :class="colorClass(component.color)">
    <span v-if="component.icon" class="a2ui-callout__icon">{{ component.icon }}</span>
    <span class="a2ui-callout__text">{{ component.text }}</span>
  </div>

  <span v-else-if="component.type === 'badge'" class="a2ui-badge" :class="colorClass(component.color)">{{ component.text }}</span>

  <div v-else-if="component.type === 'list'" class="a2ui-list">
    <div v-if="component.label" class="a2ui-component__title">{{ component.label }}</div>
    <ul class="a2ui-list__items" :class="colorClass(component.color)">
      <li v-for="(item, i) in component.items ?? []" :key="i">{{ item }}</li>
    </ul>
  </div>

  <pre v-else-if="component.type === 'code'" class="a2ui-code"><code>{{ component.text }}</code></pre>

  <div
    v-else-if="component.type === 'container'"
    class="a2ui-container"
    :class="`a2ui-container--${component.layout ?? 'column'}`"
    :style="{ gap: clampGap(component.gap) + 'px' }"
  >
    <A2UIComponent v-for="child in children" :key="child.id" :component="child" />
  </div>

  <div v-else-if="component.type === 'statRow'" class="a2ui-statrow">
    <A2UIComponent v-for="child in children" :key="child.id" :component="child" />
  </div>

  <A2UIStat v-else-if="component.type === 'stat'" :component="component" />
  <A2UITable v-else-if="component.type === 'table'" :component="component" />
  <A2UIBarChart v-else-if="component.type === 'barChart'" :component="component" />
  <A2UILineChart v-else-if="component.type === 'lineChart'" :component="component" />

  <template v-if="!holdsChildrenItself">
    <A2UIComponent v-for="child in children" :key="'sib-' + child.id" :component="child" />
  </template>
</template>
