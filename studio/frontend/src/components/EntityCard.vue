<script setup lang="ts">
// EntityCard — one entity: what a row of its table is (the grain), the table and
// the connection it is on, its id column, the columns flagged as personal data
// (pii) and its assertions. "Run assertions" checks the table through the agent
// policy's read-only path in Go. All text from the file is shown as text.
import { computed } from 'vue'
import AssertionResults from './AssertionResults.vue'
import RunStatus from './RunStatus.vue'
import SourceText from './SourceText.vue'
import type { DefinitionRuns } from '../composables/useDefinitionRuns'

const props = defineProps<{ detail: DefinitionDetail; runs: DefinitionRuns }>()
const pii = computed(() => (props.detail.entity?.columns ?? []).filter((c) => c.pii))
function describeAssertion(a: SemanticAssertion) {
  return a.kind + (a.column ? ` on ${a.column}` : '') + (a.values?.length ? ` (${a.values.join(', ')})` : '')
}
</script>

<template>
  <div v-if="detail.entity" class="col definition-card" data-testid="entity-card">
    <div class="row" style="align-items: baseline; gap: var(--sh-space-3); flex-wrap: wrap;">
      <h2 class="card-title" style="margin: 0; font-size: 28px;">{{ detail.entity.name }}</h2>
      <span v-if="detail.entity.owner" class="help" style="margin: 0;">owner {{ detail.entity.owner }}</span>
    </div>
    <p v-if="detail.entity.description" style="margin: 0;">{{ detail.entity.description }}</p>
    <dl class="definition-card__facts">
      <dt>Grain</dt><dd data-testid="entity-grain">{{ detail.entity.grain }}</dd>
      <dt>Table</dt><dd><code data-testid="entity-table">{{ detail.entity.table }}</code><span v-if="detail.entity.connection" class="help"> on {{ detail.entity.connection }}</span></dd>
      <dt>Id column</dt><dd><code>{{ detail.entity.id_column }}</code></dd>
      <template v-if="pii.length">
        <dt>Personal data</dt>
        <dd data-testid="entity-pii"><span v-for="c in pii" :key="c.name" class="definition-card__chip"><span class="badge badge--warn"><span class="dot"></span>{{ c.name }}</span></span></dd>
      </template>
      <template v-if="detail.entity.assertions?.length">
        <dt>Assertions</dt><dd data-testid="entity-assertions"><code v-for="(a, i) in detail.entity.assertions" :key="i" class="definition-card__chip">{{ describeAssertion(a) }}</code></dd>
      </template>
    </dl>
    <section v-if="detail.entity.assertions?.length" class="col definition-card__section">
      <div class="row" style="gap: var(--sh-space-3);">
        <button type="button" class="btn btn-sm" :disabled="runs.busy" data-run-trigger data-testid="run-assertions" @click="runs.runAssertions('entity', detail.entity!.name)">Run assertions</button>
      </div>
      <RunStatus :busy="runs.busy" :elapsed="runs.elapsed" :cancelled="runs.cancelled" @stop="runs.stop" :error="runs.error" :conflict="runs.conflict" what="Checking" />
      <AssertionResults v-if="runs.results.length" :rows="runs.results" />
    </section>
    <SourceText :text="detail.text" :path="`semantics/entities/${detail.entity.name}.yaml`" />
  </div>
</template>
