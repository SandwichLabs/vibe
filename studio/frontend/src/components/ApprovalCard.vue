<script setup lang="ts">
// ApprovalCard — the inline "OK this change?" card for a pending agent
// write (#79). Sits under its matching ToolStepChip (see that component),
// correlated by tool-call id, so the transcript keeps explaining why the
// agent is asking right next to the ask itself — a modal dialog (useModal)
// would block that context instead.
import type { PendingApproval } from '../composables/useAgentChat'
import { fileDirty } from '../composables/useFileDirty'

// done: true once the matching ToolStep has left 'running' (set by
// ToolStepChip from step.status), so the state line can say "Approved."
// once the write has actually finished instead of sitting on "running…"
// forever.
const props = defineProps<{ approval: PendingApproval; done?: boolean }>()
const emit = defineEmits<{ resolve: [approved: boolean, saveFirst?: boolean] }>()
</script>

<template>
  <div class="approval-card" role="group" aria-label="Approval needed" data-testid="approval-card">
    <div class="approval-card__title">Allow this change?</div>
    <p class="approval-card__summary">{{ props.approval.summary }}</p>
    <!-- where is already folded into the summary sentence (see describeWrite
         in internal/agents/approval.go) — the <pre> only adds anything new
         for run_query, where sql is the full statement, not a paraphrase. -->
    <pre v-if="props.approval.sql" class="approval-card__detail">{{ props.approval.sql }}</pre>

    <p v-if="fileDirty && props.approval.state === 'pending'" class="approval-card__hint">
      You have unsaved table changes. Save them first to be able to undo just this change with Revert to last save.
    </p>

    <div class="approval-card__actions">
      <button
        type="button"
        class="btn btn-sm"
        :disabled="props.approval.state !== 'pending'"
        data-testid="approval-cancel"
        @click="emit('resolve', false)"
      >
        Cancel
      </button>
      <button
        v-if="fileDirty && props.approval.state === 'pending'"
        type="button"
        class="btn btn-sm"
        data-testid="approval-save-do-it"
        @click="emit('resolve', true, true)"
      >
        Save, then do it
      </button>
      <button
        type="button"
        class="btn btn-sm"
        :class="props.approval.destructive ? 'btn-destructive' : 'btn-primary'"
        :disabled="props.approval.state !== 'pending'"
        data-testid="approval-do-it"
        @click="emit('resolve', true)"
      >
        Do it
      </button>
    </div>

    <p class="approval-card__state" role="status" aria-live="polite">
      <template v-if="props.approval.state === 'approved'">{{ props.done ? 'Approved.' : 'Approved, running…' }}</template>
      <template v-else-if="props.approval.state === 'declined'">Cancelled, nothing changed.</template>
      <template v-else-if="props.approval.state === 'expired'">No answer in time, nothing changed.</template>
      <template v-else-if="props.approval.state === 'cancelled'">The file was closed or reverted, nothing changed.</template>
      <template v-else-if="props.approval.state === 'gone'">No longer waiting, nothing changed.</template>
      <template v-else-if="props.approval.error">{{ props.approval.error }}</template>
    </p>
  </div>
</template>

<style scoped>
.approval-card {
  margin: 6px 0;
  padding: 10px 12px;
  border: 1.5px solid var(--sh-coral);
  border-radius: var(--sh-radius-sm);
  background: var(--sh-coral-8);
}
.approval-card__title {
  font-weight: 700;
  margin-bottom: 4px;
}
.approval-card__summary {
  margin: 0 0 6px;
}
.approval-card__detail {
  margin: 0 0 8px;
  padding: 6px 8px;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: var(--sh-font-mono);
  font-size: 12px;
  background: var(--sh-surface);
  border: var(--sh-border-thin);
  border-radius: var(--sh-radius-sm);
}
.approval-card__hint {
  margin: 0 0 8px;
  font-size: 12px;
  color: var(--sh-graphite);
}
.approval-card__actions {
  display: flex;
  gap: 8px;
}
.approval-card__state {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--sh-graphite);
}
.approval-card__state:empty {
  display: none;
}
</style>
