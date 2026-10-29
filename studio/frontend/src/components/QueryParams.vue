<script setup lang="ts">
// QueryParams — one typed input per $name parameter a saved query declares
// (#235), pre-filled with the default. State lives in useQueryParams; a value
// is sent to Go as a bound parameter, never spliced into the SQL.
import type { useQueryParams } from '../composables/useQueryParams'

const props = defineProps<{ qp: ReturnType<typeof useQueryParams> }>()
const inputType = (t: QueryParamT['type']) => (t === 'date' ? 'date' : 'text')
const mode = (t: QueryParamT['type']) => (t === 'number' ? 'decimal' : t === 'integer' ? 'numeric' : undefined)
const typed = (name: string, e: Event) => props.qp.set(name, (e.target as HTMLInputElement).value)
const ticked = (name: string, e: Event) => props.qp.set(name, (e.target as HTMLInputElement).checked)
</script>

<template>
  <div v-if="qp.hasParams.value" class="query-params" data-testid="query-params" role="group" aria-label="Query parameters">
    <div v-for="p in qp.decls.value" :key="p.name" class="query-param" :data-testid="`query-param-${p.name}`">
      <label class="query-param__label" :for="`qp-${p.name}`">${{ p.name }}</label>
      <input
        v-if="p.type === 'boolean'" :id="`qp-${p.name}`" type="checkbox" :checked="qp.raw[p.name] === true"
        @change="ticked(p.name, $event)"
      />
      <input
        v-else :id="`qp-${p.name}`" :type="inputType(p.type)" :inputmode="mode(p.type)" class="field-input query-param__input"
        :value="qp.raw[p.name] as string" :placeholder="p.default || undefined" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false"
        :aria-invalid="!!qp.shownErrors[p.name]" :aria-describedby="qp.shownErrors[p.name] ? `qp-${p.name}-e` : undefined"
        @input="typed(p.name, $event)"
      />
      <button v-if="!qp.isDefault(p.name)" type="button" class="btn btn-ghost btn-sm" :aria-label="`Reset $${p.name} to its default`" @click="qp.reset(p.name)">Reset</button>
      <span v-if="qp.shownErrors[p.name]" :id="`qp-${p.name}-e`" class="query-param__error" role="alert">{{ qp.shownErrors[p.name] }}</span>
    </div>
  </div>
</template>

<style scoped>
.query-params { display: flex; flex-wrap: wrap; gap: 8px 16px; align-items: flex-end; padding: 8px 24px; border-bottom: var(--sh-border); background: var(--sh-paper-2); }
.query-param { display: flex; align-items: center; gap: 6px; }
.query-param__label { font-family: var(--sh-font-mono); font-size: 12px; font-weight: 600; color: var(--sh-graphite); }
.query-param__input { width: 160px; }
.query-param__error { font-size: 12px; font-weight: 600; color: var(--sh-error); }
</style>
