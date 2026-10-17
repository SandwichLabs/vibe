<script setup lang="ts">
// A2UIControl — the four parameter controls of a data artifact view (#162):
// select, dateRange, numberInput, textInput. Each names a manifest parameter;
// what a control shows and sets goes through the injected artifact runtime,
// which commits only a value that validates (a bad one shows the error here and
// runs nothing). Every text shown (labels, option rows from an options_query,
// errors) is text interpolation. The value sent is taken from this component's
// own option list or the validated parameter, never from an option's DOM string.
import { computed, inject, useId } from 'vue'
import type { A2UIComponentT } from './types'
import { ARTIFACT_RUNTIME } from './artifactRuntime'

const props = defineProps<{ component: A2UIComponentT }>()
const rt = inject(ARTIFACT_RUNTIME, undefined)
const uid = useId()

const paramNames = computed<string[]>(() => {
  const c = props.component
  return (c.type === 'dateRange' ? [c.start, c.end] : [c.param]).filter((n): n is string => typeof n === 'string')
})
const primary = computed(() => rt?.params.param(paramNames.value[0]))
const label = computed(() => props.component.label || (props.component.type === 'dateRange' ? 'Date range' : primary.value?.label || paramNames.value[0] || ''))
const nameOf = (n: string) => rt?.params.param(n)?.label || n

// select: options as typed values, from the manifest or from an options_query.
const optionsQuery = primary.value?.type === 'enum' ? primary.value.options_query : undefined
const optionRows = optionsQuery && rt ? rt.result(optionsQuery) : undefined

interface Opt { value: string | boolean; text: string }
const options = computed<Opt[]>(() => {
  const p = primary.value
  if (!p || !rt) return []
  if (p.type === 'boolean') return [{ value: true, text: 'Yes' }, { value: false, text: 'No' }]
  const seen = new Set<string>()
  const out: Opt[] = []
  const add = (v: unknown) => {
    const s = String(v ?? '')
    if (seen.has(s) || seen.size >= 500) return
    seen.add(s)
    out.push({ value: s, text: s === '' ? 'All' : s })
  }
  // The default first (it may be "", read as "all"), then the current value, then the rows.
  if (typeof p.default === 'string') add(p.default)
  const shown = rt.params.display(p.name)
  if (typeof shown === 'string') add(shown)
  for (const o of p.options ?? []) add(o)
  for (const row of optionRows?.data?.rows ?? []) add(row[0])
  return out
})
const selectedIndex = computed(() => {
  const shown = rt?.params.display(paramNames.value[0])
  return options.value.findIndex((o) => o.value === shown)
})

function pick(e: Event) {
  const i = (e.target as HTMLSelectElement).selectedIndex
  const opt = options.value[i]
  if (opt) rt?.params.set(paramNames.value[0], opt.value)
}
function typed(name: string, e: Event, debounce: boolean) {
  rt?.params.set(name, (e.target as HTMLInputElement).value, debounce)
}
const shown = (name: string) => String(rt?.params.display(name) ?? '')
const err = (name: string) => rt?.params.error(name) ?? ''
const dirty = computed(() => paramNames.value.some((n) => rt && !rt.params.isDefault(n)))
function reset() { for (const n of paramNames.value) rt?.params.reset(n) }
</script>

<template>
  <div v-if="rt && primary" class="a2ui-control" :class="{ 'a2ui-control--range': component.type === 'dateRange' }" :data-testid="`a2ui-control-${component.type}`">
    <div class="a2ui-control__head">
      <label class="a2ui-component__title" :for="`${uid}-0`">{{ label }}</label>
      <button v-if="dirty" type="button" class="a2ui-control__reset" :aria-label="`Reset ${label} to its default`" @click="reset">Reset</button>
    </div>

    <select
      v-if="component.type === 'select'" :id="`${uid}-0`" class="field-input a2ui-control__input"
      :aria-invalid="!!err(primary.name)" :aria-describedby="err(primary.name) ? `${uid}-e0` : undefined"
      @change="pick"
    >
      <option v-for="(o, i) in options" :key="i" :value="i" :selected="i === selectedIndex">{{ o.text }}</option>
    </select>

    <div v-else-if="component.type === 'dateRange'" class="a2ui-control__pair">
      <template v-for="(n, i) in paramNames" :key="n">
        <span v-if="i === 1" class="a2ui-control__to" aria-hidden="true">to</span>
        <input
          :id="`${uid}-${i}`" type="date" class="field-input a2ui-control__input" :value="shown(n)"
          :aria-label="nameOf(n)" :aria-invalid="!!err(n)"
          :aria-describedby="err(n) ? `${uid}-e${i}` : undefined" @change="typed(n, $event, false)"
        />
      </template>
    </div>

    <input
      v-else-if="component.type === 'numberInput'" :id="`${uid}-0`" type="text" inputmode="decimal"
      class="field-input a2ui-control__input" :value="shown(primary.name)" :aria-invalid="!!err(primary.name)"
      :aria-describedby="err(primary.name) ? `${uid}-e0` : undefined" @input="typed(primary.name, $event, true)"
    />

    <input
      v-else :id="`${uid}-0`" type="text" class="field-input a2ui-control__input" :value="shown(primary.name)"
      :maxlength="primary.max_length || 1000" :aria-invalid="!!err(primary.name)"
      :aria-describedby="err(primary.name) ? `${uid}-e0` : undefined" @input="typed(primary.name, $event, true)"
    />

    <div v-for="(n, i) in paramNames" :key="`e-${n}`">
      <div v-if="err(n)" :id="`${uid}-e${i}`" class="a2ui-error a2ui-control__error" role="alert">{{ err(n) }}</div>
    </div>
  </div>
  <div v-else class="a2ui-error">This control is only available in a data artifact.</div>
</template>
