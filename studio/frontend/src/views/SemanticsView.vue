<script setup lang="ts">
// SemanticsView (E4): the workspace's semantic layer. Entities, metrics, joins
// and a glossary live as files under semantics/; this view reads them, runs what
// they declare ("Try it", assertions, a join profile) and is the inbox where the
// person decides on definitions an agent proposed. It edits no file itself, so
// there is no conflict banner: a definition changes on disk (an editor, git, an
// accepted proposal) and the card re-reads. State and actions live in
// useSemantics, useProposals and useDefinitionRuns.
import { computed, reactive, watch } from 'vue'
import Icon from '../components/Icon.vue'
import MetricCard from '../components/MetricCard.vue'
import EntityCard from '../components/EntityCard.vue'
import JoinCard from '../components/JoinCard.vue'
import GlossaryPanel from '../components/GlossaryPanel.vue'
import ProposalInbox from '../components/ProposalInbox.vue'
import { useSemantics, type SemanticsKind } from '../composables/useSemantics'
import { useProposals } from '../composables/useProposals'
import { useDefinitionRuns } from '../composables/useDefinitionRuns'
import { workspaceInfo } from '../composables/useWorkspace'
import '../semantics.css'

const { list, loading, error, detail, detailError, selected, isEmpty, select, reload } = useSemantics()
const proposals = useProposals()
const runs = reactive(useDefinitionRuns())
const readOnly = computed(() => !!workspaceInfo.value?.readOnly)

const groups = computed(() => {
  const l = list.value
  if (!l) return []
  return [
    { kind: 'metric' as const, label: 'Metrics', rows: l.metrics.map((d) => ({ name: d.name, text: d.title || d.name, error: d.error })) },
    { kind: 'entity' as const, label: 'Entities', rows: l.entities.map((d) => ({ name: d.name, text: d.name, error: d.error })) },
    { kind: 'join' as const, label: 'Joins', rows: l.joins.map((d) => ({ name: d.name, text: d.name, error: d.error })) },
  ]
})

// Up and Down move between the rows of the list.
function arrowKeys(e: KeyboardEvent) {
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
  const rows = [...(e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('.semantics-list__row')]
  const i = rows.indexOf(document.activeElement as HTMLElement)
  if (i < 0) return
  e.preventDefault()
  rows[Math.min(rows.length - 1, Math.max(0, i + (e.key === 'ArrowDown' ? 1 : -1)))]?.focus()
}

const isActive = (kind: SemanticsKind, name: string) => selected.value?.kind === kind && selected.value.name === name
const detailKind = computed(() => (detailError.value || detail.value?.error ? 'broken' : selected.value?.kind))

// A proposal is read when it is picked, and again whenever the list reloads.
async function openProposal() {
  const s = selected.value
  if (s?.kind === 'proposal' && s.name) await proposals.select(s.name)
  else proposals.preview.value = null
}
watch(selected, () => { runs.reset(); void openProposal() })
watch(list, async () => { await proposals.load(); if (selected.value?.kind === 'proposal' && !proposals.stale.value) await openProposal() })

async function accept(overwrite: boolean) {
  const p = proposals.preview.value?.proposal
  if (!p || !(await proposals.accept(overwrite))) return
  await reload()
  select(p.kind, p.kind === 'glossary' ? '' : p.name)
}
async function reject() {
  if (await proposals.reject()) { await reload(); select('proposal') }
}
</script>

<template>
  <div class="row" style="height: 100%; align-items: stretch; gap: 0;" data-testid="semantics-view">
    <nav class="col semantics-list" aria-label="Semantic definitions" @keydown="arrowKeys">
      <div class="row" style="justify-content: space-between; padding: var(--sh-space-3) var(--sh-space-4); border-bottom: var(--sh-border);">
        <h2 class="sh-label" style="margin: 0;">Semantics</h2>
      </div>
      <div style="flex: 1; overflow: auto;">
        <div v-if="loading" class="sh-mono semantics-list__note">Loading…</div>
        <div v-else-if="error" class="note attn" role="alert" style="margin: var(--sh-space-3);" data-testid="semantics-error"><span>{{ error }}</span></div>
        <template v-else>
          <section v-for="g in groups" :key="g.kind" :data-testid="`group-${g.kind}`">
            <h3 class="semantics-list__head">{{ g.label }}</h3>
            <button
              v-for="r in g.rows" :key="r.name" type="button" class="semantics-list__row" :class="{ on: isActive(g.kind, r.name) }"
              :aria-current="isActive(g.kind, r.name) ? 'true' : undefined" :data-testid="`def-${g.kind}-${r.name}`" @click="select(g.kind, r.name)"
            >
              <span class="semantics-list__name">{{ r.text }}</span>
              <span v-if="r.error" class="badge badge--error" title="This file could not be read"><span class="dot"></span>error</span>
            </button>
            <p v-if="!g.rows.length" class="semantics-list__note">None yet</p>
          </section>
          <section data-testid="group-glossary">
            <h3 class="semantics-list__head">Glossary</h3>
            <button type="button" class="semantics-list__row" :class="{ on: selected?.kind === 'glossary' }" :aria-current="selected?.kind === 'glossary' ? 'true' : undefined" data-testid="def-glossary" @click="select('glossary')">
              <span class="semantics-list__name">{{ list?.terms.length ? `${list.terms.length} ${list.terms.length === 1 ? 'term' : 'terms'}` : 'No terms yet' }}</span>
              <span v-if="list?.glossary_error" class="badge badge--error"><span class="dot"></span>error</span>
            </button>
          </section>
          <section data-testid="group-proposals">
            <h3 class="semantics-list__head">Proposals <span v-if="list?.open_proposals" class="badge badge--warn" data-testid="proposal-count">{{ list.open_proposals }}</span></h3>
            <button type="button" class="semantics-list__row" :class="{ on: selected?.kind === 'proposal' && !selected.name }" :aria-current="selected?.kind === 'proposal' && !selected.name ? 'true' : undefined" data-testid="def-proposals" @click="select('proposal')">
              <span class="semantics-list__name">{{ list?.open_proposals ? `${list.open_proposals} waiting` : 'Inbox is empty' }}</span>
            </button>
            <button
              v-for="p in proposals.open.value" :key="p.id" type="button" class="semantics-list__row" :class="{ on: isActive('proposal', p.id) }" :aria-current="isActive('proposal', p.id) ? 'true' : undefined"
              :data-testid="`def-proposal-${p.id}`" @click="select('proposal', p.id)"
            >
              <span class="semantics-list__name">{{ p.name }}</span><span class="badge badge--info">{{ p.kind }}</span>
            </button>
          </section>
        </template>
      </div>
    </nav>

    <div class="semantics-detail">
      <div v-if="isEmpty && !selected" class="col semantics-empty" data-testid="semantics-empty">
        <Icon name="network" :size="40" />
        <h2>No definitions yet</h2>
        <p>The semantic layer names what your data means: entities (what a row is), metrics (one agreed way to compute a number, over a saved query), joins and a glossary. Two ways to start:</p>
        <ul>
          <li>Write the files yourself: <code>semantics/metrics/net_revenue.yaml</code> naming a saved query and a measure like <code>sum(amount)</code>. They appear here as soon as they are saved. <code>docs/reference/semantics.md</code> has the formats.</li>
          <li>Ask the assistant to model your data. It proposes definitions into the Proposals inbox here, and nothing changes in your folder until you accept.</li>
        </ul>
      </div>
      <div v-else-if="!selected" class="sh-mono semantics-empty" data-testid="semantics-pick">Select a definition to read it.</div>
      <ProposalInbox
        v-else-if="selected.kind === 'proposal'" :proposals="proposals.open.value" :preview="proposals.preview.value" :busy="proposals.busy.value"
        :error="proposals.error.value" :changed-again="proposals.stale.value" :updated="proposals.updated.value" :read-only="readOnly"
        @select="(id: string) => select('proposal', id)" @accept="accept" @reject="reject"
      />
      <div v-else-if="detailError" class="note attn" role="alert" data-testid="definition-error"><span>{{ detailError }}</span></div>
      <template v-else-if="detail">
        <div v-if="detail.error && detail.kind !== 'glossary'" class="col" style="gap: var(--sh-space-3);" data-testid="definition-broken">
          <h2 class="card-title" style="margin: 0; font-size: 28px;">{{ detail.name }}</h2>
          <div class="note attn" role="alert"><span>This file cannot be read: {{ detail.error }}</span></div>
          <pre class="sh-mono semantics-detail__raw">{{ detail.text }}</pre>
        </div>
        <MetricCard v-else-if="detailKind === 'metric'" :detail="detail" :runs="runs" />
        <EntityCard v-else-if="detailKind === 'entity'" :detail="detail" :runs="runs" />
        <JoinCard v-else-if="detailKind === 'join'" :detail="detail" :runs="runs" :read-only="readOnly" />
        <GlossaryPanel v-else-if="detailKind === 'glossary'" :detail="detail" />
      </template>
    </div>
  </div>
</template>

<style scoped>
.semantics-list { width: 300px; border-right: var(--sh-border); background: var(--sh-paper-2); gap: 0; flex: none; }
.semantics-list__head { margin: 0; padding: var(--sh-space-3) var(--sh-space-4) var(--sh-space-1); font-size: var(--sh-fs-xs); text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-graphite); display: flex; gap: var(--sh-space-2); align-items: center; }
.semantics-list__note { padding: var(--sh-space-1) var(--sh-space-4); margin: 0; color: var(--sh-graphite); font-size: var(--sh-fs-sm); }
.semantics-list__row { display: flex; width: 100%; align-items: center; justify-content: space-between; gap: var(--sh-space-2); padding: var(--sh-space-2) var(--sh-space-4); background: transparent; border: 0; border-bottom: var(--sh-border-thin); color: inherit; cursor: pointer; text-align: left; font: inherit; font-size: var(--sh-fs-sm); }
.semantics-list__row:hover { background: var(--sh-teal-8); }
.semantics-list__row.on { background: var(--sh-teal-8); font-weight: 600; box-shadow: inset 3px 0 0 var(--sh-teal); }
.semantics-list__name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.semantics-detail { flex: 1; overflow: auto; padding: var(--sh-space-6); min-width: 0; }
.semantics-detail__raw { white-space: pre-wrap; padding: var(--sh-space-3); background: var(--sh-paper-2); border: var(--sh-border-thin); border-radius: var(--sh-radius-sm); }
.semantics-empty { max-width: 640px; margin: var(--sh-space-10) auto 0; color: var(--sh-graphite); gap: var(--sh-space-3); }
.semantics-empty h2 { color: var(--sh-ink); margin: 0; }
.semantics-empty ul { margin: 0; padding-left: var(--sh-space-5); list-style: disc; display: flex; flex-direction: column; gap: var(--sh-space-2); }
</style>
