<script setup lang="ts">
// ToolStepChip — one agent tool-call chip, shared by AgentView (the full-page
// wizard) and AgentPanel (the slide-in drawer), which used to duplicate this
// markup verbatim (#91 P-F12/M-F21). A <details> disclosure: the collapsed
// <summary> keeps the existing running/done/error look, and opening it shows
// the full pretty-printed input (or just the SQL, when the call had one)
// plus a Copy button, and for a failed call its full error text.
import { ref, computed } from 'vue'
import Icon from './Icon.vue'
import ApprovalCard from './ApprovalCard.vue'
import { formatToolInput } from '../lib/toolStep'
import type { ToolStep } from '../composables/useAgentChat'

const props = defineProps<{ step: ToolStep; previewLen: number }>()
const emit = defineEmits<{ resolve: [approved: boolean] }>()

const formatted = computed(() => formatToolInput(props.step.input))
const preview = computed(() => {
  const p = formatted.value.preview
  return p.length > props.previewLen ? p.slice(0, props.previewLen) + '…' : p
})

const copied = ref(false)
async function copy() {
  const text = formatted.value.sql ?? formatted.value.pretty
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    return
  }
  copied.value = true
  setTimeout(() => { copied.value = false }, 1500)
}
</script>

<template>
  <div class="tool-step-wrap">
    <details class="tool-step">
      <summary
        class="tool-chip"
        :class="{ 'tool-chip-error': step.status === 'error' }"
        data-testid="agent-tool-chip"
      >
        <Icon name="chevronRight" :size="12" class="tool-step__caret" />
        <span class="lab"><Icon name="terminal" :size="12" /> {{ step.tool }}</span>
        <span v-if="preview" class="time">{{ preview }}</span>
        <span v-if="step.status === 'running'" class="spin" data-testid="chip-running">●</span>
        <span v-else-if="step.status === 'error'" class="chip-error-mark" data-testid="chip-error" :title="step.output">✕</span>
        <span v-else class="check" data-testid="chip-done">✓</span>
      </summary>
      <div class="tool-step__body" data-testid="agent-tool-chip-body">
        <pre>{{ formatted.sql ?? formatted.pretty }}</pre>
        <button class="btn btn-ghost btn-sm tool-step__copy" @click="copy">
          <Icon name="copy" :size="12" /> {{ copied ? 'Copied' : (formatted.sql ? 'Copy SQL' : 'Copy input') }}
        </button>
        <pre v-if="step.status === 'error' && step.output" class="tool-step__error">{{ step.output }}</pre>
      </div>
    </details>
    <!-- Outside <details> so the card is always visible, even collapsed
         (#79) — the whole point is that the user sees the ask without
         having to open the chip first. -->
    <ApprovalCard
      v-if="step.approval"
      :approval="step.approval"
      :done="step.status !== 'running'"
      @resolve="(ok) => emit('resolve', ok)"
    />
  </div>
</template>

<style scoped>
.tool-step {
  display: block;
}
.tool-step > summary {
  list-style: none;
  cursor: pointer;
}
.tool-step > summary::-webkit-details-marker {
  display: none;
}
.tool-step__caret {
  transition: transform 0.12s ease;
  flex: none;
}
.tool-step[open] .tool-step__caret {
  transform: rotate(90deg);
}
.tool-step__body {
  padding: 8px 10px;
  margin: 2px 0 6px;
  border: var(--sh-border-thin);
  border-radius: var(--sh-radius-sm);
  background: var(--sh-surface);
}
.tool-step__body pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: var(--sh-font-mono);
  font-size: 12px;
}
.tool-step__copy {
  margin-top: 6px;
}
.tool-step__error {
  margin-top: 8px !important;
  /* Matches the chip's own coral (not --sh-error/red) — a denied/failed
     tool call is "needs attention", the same framing as the chip border
     and ✕ mark just above it (review round 1, non-blocking #7). */
  color: var(--sh-coral);
}
.spin { animation: agent-pulse 1s ease-in-out infinite; }
@keyframes agent-pulse { 0%, 100% { opacity: 1 } 50% { opacity: 0.3 } }
/* A denied/failed tool call — coral is SchoolHouse's "needs attention", not
   destructive/red. */
.tool-chip-error {
  border-color: var(--sh-coral);
  background: var(--sh-coral-8);
}
.chip-error-mark {
  color: var(--sh-coral);
  font-weight: 700;
  cursor: help;
  margin-left: auto;
}
</style>
