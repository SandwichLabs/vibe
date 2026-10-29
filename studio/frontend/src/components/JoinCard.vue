<script setup lang="ts">
// JoinCard — how two entities relate: the declared cardinality beside the one a
// profiling run measured, with a warning when they disagree. Profile is the
// person's action: it runs three read-only counts and writes the result into the
// join's file (measured:); if the file changed while it ran, nothing is written
// and the conflict is shown. Profile is off in a read-only workspace.
import { computed } from 'vue'
import RunStatus from './RunStatus.vue'
import SourceText from './SourceText.vue'
import type { DefinitionRuns } from '../composables/useDefinitionRuns'

const props = defineProps<{ detail: DefinitionDetail; runs: DefinitionRuns; readOnly?: boolean }>()
// A fresh profile (this session) wins over the file's measured block as loaded.
const fresh = computed(() => (props.runs.profile && props.runs.profile.join === props.detail.join?.name ? props.runs.profile : null))
const measured = computed(() => fresh.value?.measured ?? props.detail.join?.measured)
const warnings = computed(() => fresh.value?.warnings ?? [])
const disagrees = computed(() => !!measured.value && measured.value.verdict !== 'consistent')
</script>

<template>
  <div v-if="detail.join" class="col definition-card" data-testid="join-card">
    <div class="row" style="align-items: baseline; gap: var(--sh-space-3); flex-wrap: wrap;">
      <h2 class="card-title" style="margin: 0; font-size: 28px;">{{ detail.join.name }}</h2>
      <code>{{ detail.join.left }}</code> to <code>{{ detail.join.right }}</code>
    </div>
    <p v-if="detail.join.description" style="margin: 0;">{{ detail.join.description }}</p>
    <dl class="definition-card__facts">
      <dt>On</dt><dd><code v-for="(c, i) in detail.join.on" :key="i" class="definition-card__chip">{{ detail.join.left }}.{{ c.left }} = {{ detail.join.right }}.{{ c.right }}</code></dd>
      <dt>Declared</dt><dd><span class="badge badge--graphite" data-testid="join-declared">{{ detail.join.cardinality }}</span></dd>
      <dt>Measured</dt>
      <dd data-testid="join-measured">
        <template v-if="measured">
          <span class="badge" :class="measured.verdict === 'consistent' ? 'badge--success' : 'badge--warn'"><span class="dot"></span>{{ measured.verdict }}</span>
          <span class="help"> at {{ measured.at }}: {{ measured.left_rows.toLocaleString() }} left rows, {{ measured.right_rows.toLocaleString() }} right rows, at most {{ measured.max_fanout.toLocaleString() }} right per left key, {{ measured.left_orphans.toLocaleString() }} left and {{ measured.right_orphans.toLocaleString() }} right rows without a match</span>
        </template>
        <span v-else class="help">not measured yet</span>
      </dd>
    </dl>
    <div v-if="disagrees || warnings.length" class="note warn" role="status" data-testid="join-warning">
      <span>
        <template v-if="warnings.length"><span v-for="w in warnings" :key="w" style="display: block;">{{ w }}</span></template>
        <template v-else>The measured data does not match the declared {{ detail.join.cardinality }}. Check the declaration, or the data.</template>
      </span>
    </div>
    <section class="col definition-card__section">
      <div class="row" style="gap: var(--sh-space-3);">
        <button type="button" class="btn btn-sm" :disabled="runs.busy || readOnly" :title="readOnly ? 'This workspace is read-only: the result cannot be written to the join file.' : 'Count the rows of both entities and record the result in the join file'" data-testid="join-profile" @click="runs.profileJoin(detail.join!.name)">Profile</button>
      </div>
      <RunStatus :busy="runs.busy" :elapsed="runs.elapsed" :error="runs.error" :conflict="runs.conflict ? `The join file changed while the profile ran, so nothing was written: ${runs.conflict}. Profile again.` : ''" what="Profiling" />
    </section>
    <SourceText :text="detail.text" :path="`semantics/joins/${detail.join.name}.yaml`" />
  </div>
</template>
