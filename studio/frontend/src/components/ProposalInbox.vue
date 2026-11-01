<script setup lang="ts">
// ProposalInbox — the definitions an agent proposed, for the person to decide.
// Nothing in semantics/ changes until Accept. A proposal opens beside the file
// it would replace as a line diff (changed lines carry + / - as well as colour);
// the rationale, the evidence the agent gave and the warnings Go found are shown
// as text. Accept writes exactly the version on screen: if the file or the
// proposal changed after you looked, Go refuses, this re-reads and shows the new
// diff ("changed again"). A file that changed since the agent proposed shows the
// reason and offers "Accept anyway" instead, which still needs this version.
import { computed } from 'vue'
import { diffLines, diffStats } from '../lib/lineDiff'

const props = defineProps<{
  proposals: Proposal[]
  preview: ProposalPreview | null
  busy: boolean
  error: string
  changedAgain: string
  updated?: boolean
  readOnly?: boolean
}>()
const emit = defineEmits<{ (e: 'select', id: string): void; (e: 'accept', overwrite: boolean): void; (e: 'reject'): void }>()

const diff = computed(() => (props.preview ? diffLines(props.preview.current_text, props.preview.proposed_text) : []))
const stats = computed(() => diffStats(diff.value))
// stale_reason is a fragment from Go: "changed", "was created", "was deleted" or
// "the term was deleted"; it reads as the sentence it completes.
const staleText = computed(() => {
  const r = props.preview?.stale_reason ?? 'changed'
  return r.startsWith('the term') ? `The glossary term ${r.slice('the term '.length)} since this was proposed.` : `The file ${r} since this was proposed.`
})
const mark = (op: string) => (op === '+' ? '+' : op === '-' ? '-' : ' ')
</script>

<template>
  <div class="col proposal-inbox" data-testid="proposal-inbox">
    <h2 class="card-title" style="margin: 0; font-size: 28px;">Proposals</h2>
    <div v-if="error" class="note attn" role="alert" data-testid="proposal-error"><span>{{ error }}</span></div>

    <template v-if="preview">
      <div class="row" style="gap: var(--sh-space-3); align-items: baseline; flex-wrap: wrap;">
        <span class="badge badge--info">{{ preview.proposal.kind }}</span>
        <strong data-testid="proposal-name">{{ preview.proposal.name }}</strong>
        <span class="help" style="margin: 0;">proposed by {{ preview.proposal.proposed_by }}, {{ preview.proposal.proposed_at }}</span>
      </div>
      <p style="margin: 0;" data-testid="proposal-rationale">{{ preview.proposal.rationale }}</p>
      <div v-if="preview.proposal.evidence?.length" class="col" style="gap: var(--sh-space-1);">
        <span class="sh-label" style="margin: 0;">Evidence given</span>
        <ul class="proposal-inbox__list" data-testid="proposal-evidence">
          <li v-for="(e, i) in preview.proposal.evidence" :key="i">{{ e }}</li>
        </ul>
      </div>
      <div v-if="changedAgain" class="note warn" role="alert" data-testid="proposal-changed-again">
        <span><strong>This changed again since you looked.</strong> Nothing was written. The diff below is the new version; read it, then decide. ({{ changedAgain }})</span>
      </div>
      <div v-else-if="updated" class="note" role="status" data-testid="proposal-updated">
        <span>Updated while you were reading: the proposal or the file it would replace changed. The diff below is the current one.</span>
      </div>
      <div v-if="preview.stale" class="note warn" role="status" data-testid="proposal-stale">
        <span><strong>{{ staleText }}</strong> Accepting anyway writes the proposal over what is there now, not a merge.</span>
      </div>
      <div v-for="w in preview.warnings" :key="w" class="note warn" role="status" data-testid="proposal-warning"><span>{{ w }}</span></div>

      <p class="help" style="margin: 0;" data-testid="proposal-diff-head">
        <code>{{ preview.target }}</code> <span class="proposal-inbox__sep">·</span> {{ preview.exists ? 'replaces the current file' : 'a new file' }} <span class="proposal-inbox__sep">·</span> {{ stats.added }} added, {{ stats.removed }} removed
      </p>
      <div class="sh-mono proposal-inbox__diff" role="region" tabindex="0" aria-label="Changes the proposal makes" data-testid="proposal-diff">
        <span v-for="(l, i) in diff" :key="i" class="proposal-inbox__line" :class="{ add: l.op === '+', del: l.op === '-' }" :data-op="l.op"><span class="proposal-inbox__mark" aria-hidden="true">{{ mark(l.op) }}</span>{{ l.text }}</span>
      </div>

      <div class="row" style="gap: var(--sh-space-3);">
        <button type="button" class="btn btn-primary" :disabled="busy || readOnly" :title="readOnly ? 'This workspace is read-only.' : ''" data-testid="proposal-accept" @click="emit('accept', preview.stale)">
          {{ preview.stale ? 'Accept anyway' : 'Accept' }}
        </button>
        <button type="button" class="btn" :disabled="busy" data-testid="proposal-reject" @click="emit('reject')">Reject</button>
      </div>
    </template>

    <template v-else>
      <p v-if="!proposals.length" class="help" style="margin: 0;" data-testid="proposal-empty">
        No proposals are waiting. When the assistant suggests a definition (it can, in the chat, once you ask it to model your data), it lands here for you to read and accept. Nothing changes until you do.
      </p>
      <ul v-else class="proposal-inbox__cards">
        <li v-for="p in proposals" :key="p.id">
          <button type="button" class="card card-pad proposal-inbox__card" :data-testid="`proposal-card-${p.id}`" @click="emit('select', p.id)">
            <span class="row" style="gap: var(--sh-space-2); align-items: baseline;"><span class="badge badge--info">{{ p.kind }}</span><strong>{{ p.name }}</strong></span>
            <span class="help" style="margin: 0;">{{ p.error ? p.error : p.rationale }}</span>
          </button>
        </li>
      </ul>
    </template>
  </div>
</template>

<style scoped>
.proposal-inbox { gap: var(--sh-space-3); }
.proposal-inbox__sep { margin: 0 var(--sh-space-1); }
.proposal-inbox__list { margin: 0; padding-left: var(--sh-space-4); list-style: disc; }
.proposal-inbox__cards { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sh-space-2); }
.proposal-inbox__card { width: 100%; text-align: left; display: flex; flex-direction: column; gap: var(--sh-space-1); cursor: pointer; }
.proposal-inbox__diff { margin: 0; padding: var(--sh-space-2) 0; background: var(--sh-paper-2); border: var(--sh-border-thin); border-radius: var(--sh-radius-sm); max-height: 420px; overflow: auto; font-size: var(--sh-fs-xs); }
.proposal-inbox__line { display: block; min-height: 1.2em; padding: 0 var(--sh-space-3); white-space: pre-wrap; overflow-wrap: anywhere; }
.proposal-inbox__line.add { background: color-mix(in srgb, var(--sh-success) 16%, transparent); }
.proposal-inbox__line.del { background: color-mix(in srgb, var(--sh-error) 16%, transparent); }
.proposal-inbox__mark { display: inline-block; width: 2ch; font-weight: 700; }
.proposal-inbox__line.add .proposal-inbox__mark { color: var(--sh-success); }
.proposal-inbox__line.del .proposal-inbox__mark { color: var(--sh-error); }
</style>
