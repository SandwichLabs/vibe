<script setup lang="ts">
// AgentTranscript — the agent conversation's messages (F4), shared by the
// slide-in AgentPanel and the full-page AgentView: who said it, the user bubble,
// the markdown answer (sanitized), "Thinking…" / "Verifying…" notes, and each
// tool step as a chip with its inline A2UI result or a link to the notebook it
// changed. Renders the message rows only; the scrolling stream, the empty state
// and the input are the host's. `compact` is the narrow drawer's sizing.
import Icon from './Icon.vue'
import ToolStepChip from './ToolStepChip.vue'
import A2UIPage from '../a2ui/A2UIPage.vue'
import { renderMarkdown, type ChatMessage, type ToolStep } from '../composables/useAgentChat'

defineProps<{ messages: ChatMessage[]; compact?: boolean }>()
const emit = defineEmits<{ resolve: [step: ToolStep, ok: boolean]; openNotebook: [id: string] }>()
</script>

<template>
  <div v-for="(m, i) in messages" :key="i" class="amsg" :class="{ 'amsg--compact': compact }">
    <div class="av" :class="m.role === 'user' ? 'user' : 'bot'">
      {{ m.role === 'user' ? 'You' : 'AI' }}
    </div>
    <div class="body">
      <div class="who">
        {{ m.role === 'user' ? 'You' : m.role === 'error' ? 'Agent · error' : m.cancelled ? 'Agent · cancelled' : (m.verifying ? 'Agent · verifying' : m.streaming ? 'Agent · streaming' : 'Agent') }}
      </div>
      <div v-if="m.role === 'user'" class="user-bubble">{{ m.text }}</div>
      <p v-else-if="m.role === 'error'" class="agent-text agent-error">{{ m.text }}</p>
      <div v-else-if="m.text" class="agent-text agent-md vibe-content vibe-md-content" v-html="renderMarkdown(m.text)"></div>
      <p v-else-if="m.streaming && !m.cancelled" class="agent-text agent-dim">Thinking…</p>
      <p v-if="m.cancelled" class="agent-text agent-dim" data-testid="agent-cancelled">Cancelled</p>
      <p v-if="m.verifying" class="agent-text agent-dim" data-testid="agent-verifying">Verifying that answer against the data…</p>

      <template v-for="(s, si) in m.steps" :key="si">
        <ToolStepChip :step="s" :preview-len="compact ? 48 : 80" @resolve="(ok) => emit('resolve', s, ok)" />
        <A2UIPage v-if="s.page" :page="s.page" class="agent-a2ui-embed" data-testid="agent-a2ui" />
        <button
          v-if="s.notebookLink"
          class="agent-pinbtn"
          style="margin: 4px 0;"
          data-testid="agent-open-notebook"
          @click="emit('openNotebook', s.notebookLink.id)"
        ><Icon name="fileText" :size="13" /> Open notebook — {{ s.notebookLink.title }}</button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.agent-text { white-space: pre-wrap; }
.agent-md { white-space: normal; }
.agent-md :deep(p) { margin: 0 0 6px; }
.agent-md :deep(pre) {
  background: var(--vibe-code-bg);
  color: var(--vibe-code-fg);
  padding: 10px 12px;
  border-radius: var(--sh-radius-sm);
  overflow-x: auto;
  font-family: var(--sh-font-mono);
  font-size: 12.5px;
}
.amsg--compact .agent-md :deep(pre) { padding: 8px 10px; font-size: 12px; }
.agent-md :deep(code) { font-family: var(--sh-font-mono); font-size: 12.5px; }
.amsg--compact .agent-md :deep(code) { font-size: 12px; }
/* The global `.sh code` rule (schoolhouse/colors_and_type.css) gives inline code
   its own light --sh-paper-2 background + ink border. Inside a fenced block that
   collides with the dark <pre> above it: the <code> painted its own near-white
   box under near-white inherited text. Reset it like the Pages renderer's
   `.vibe-content pre code` already does, so fenced code reads light on dark. */
.agent-md :deep(pre code) {
  background: transparent;
  border: none;
  padding: 0;
  color: inherit;
}
/* Each host keeps the markdown extras it always had: tables in the page, lists in the drawer. */
.amsg:not(.amsg--compact) .agent-md :deep(table) { border-collapse: collapse; }
.amsg:not(.amsg--compact) .agent-md :deep(th), .amsg:not(.amsg--compact) .agent-md :deep(td) { border: 1px solid var(--sh-line); padding: 4px 8px; }
.amsg--compact .agent-md :deep(ul), .amsg--compact .agent-md :deep(ol) { margin: 4px 0; padding-left: 20px; }
.agent-error { color: var(--sh-error); }
.agent-dim { color: var(--sh-graphite); font-style: italic; }
/* present_data's inline A2UI result (#67) — a compact card in the transcript,
   distinct from the plain-text agent bubble above it. */
.agent-a2ui-embed {
  border: var(--sh-border-thin);
  border-radius: var(--sh-radius);
  background: var(--sh-surface);
  padding: 10px 12px;
  margin: 4px 0 8px;
}
.agent-pinbtn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  border: 1.5px solid var(--sh-ink);
  border-radius: var(--sh-radius-sm);
  background: var(--sh-surface);
  cursor: pointer;
  font: inherit;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
</style>
