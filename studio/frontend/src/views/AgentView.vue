<script setup lang="ts">
import { ref, nextTick, onMounted } from 'vue'
import Icon from '../components/Icon.vue'
import { useAgentChat } from '../composables/useAgentChat'
import A2UIPage from '../a2ui/A2UIPage.vue'

// AgentView is the full-page wizard chat (W0.5) — the same conversation engine
// as the slide-in AgentPanel (shared useAgentChat composable), given room for
// longer sessions, schema work, and report/query building.
const { messages, sending, send, reset, setScrollHandler, renderMarkdown } = useAgentChat()

const draft = ref('')
const streamEl = ref<HTMLElement | null>(null)

async function scrollToBottom() {
  await nextTick()
  if (streamEl.value) streamEl.value.scrollTop = streamEl.value.scrollHeight
}
onMounted(() => setScrollHandler(scrollToBottom))

async function submit() {
  const text = draft.value
  if (!text.trim() || sending.value) return
  draft.value = ''
  await send(text)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    submit()
  }
}
</script>

<template>
  <div class="agent-view" data-testid="agent-view">
    <div class="agent-view__head">
      <div>
        <div class="crumb"><span>Studio</span><span class="sep">/</span><span>Agent</span></div>
        <h1 class="agent-view__title"><Icon name="bot" :size="22" /> Agent wizard</h1>
      </div>
      <button class="btn btn-inverted" :disabled="messages.length === 0 || sending" @click="reset">
        <Icon name="refresh" :size="14" /> New chat
      </button>
    </div>

    <div ref="streamEl" class="agent-view__stream">
      <div v-if="messages.length === 0" class="agent-view__empty">
        <Icon name="sparkle" :size="28" />
        <p>Describe what you want to build or ask about your data. The wizard reads your schema, runs read-only SQL, and can create tables — every step shown below as it works.</p>
      </div>

      <div v-for="(m, i) in messages" :key="i" class="amsg">
        <div class="av" :class="m.role === 'user' ? 'user' : 'bot'">
          {{ m.role === 'user' ? 'You' : 'AI' }}
        </div>
        <div class="body">
          <div class="who">
            {{ m.role === 'user' ? 'You' : m.role === 'error' ? 'Agent · error' : (m.streaming ? 'Agent · streaming' : 'Agent') }}
          </div>
          <div v-if="m.role === 'user'" class="user-bubble">{{ m.text }}</div>
          <p v-else-if="m.role === 'error'" class="agent-text agent-error">{{ m.text }}</p>
          <div v-else-if="m.text" class="agent-text agent-md vibe-content vibe-md-content" v-html="renderMarkdown(m.text)"></div>
          <p v-else-if="m.streaming" class="agent-text agent-dim">Thinking…</p>

          <template v-for="(s, si) in m.steps" :key="si">
            <div
              class="tool-chip"
              :class="{ 'tool-chip-error': s.status === 'error' }"
              data-testid="agent-tool-chip"
            >
              <span class="lab"><Icon name="terminal" :size="12" /> {{ s.tool }}</span>
              <span v-if="s.input" class="time">{{ s.input.slice(0, 80) }}</span>
              <span v-if="s.status === 'running'" class="spin" data-testid="chip-running">●</span>
              <span v-else-if="s.status === 'error'" class="chip-error-mark" data-testid="chip-error" :title="s.output">✕</span>
              <span v-else class="check" data-testid="chip-done">✓</span>
            </div>
            <A2UIPage v-if="s.page" :page="s.page" class="agent-a2ui-embed" data-testid="agent-a2ui" />
          </template>
        </div>
      </div>
    </div>

    <div class="agent-view__input">
      <textarea
        v-model="draft"
        class="box agent-textarea"
        data-testid="agent-input"
        placeholder="Ask the agent to inspect data or build something…"
        @keydown="onKeydown"
      ></textarea>
      <div class="agent-view__inputrow">
        <span class="agent-hint">⏎ Send · ⇧⏎ Newline</span>
        <button class="btn btn-primary" :disabled="sending || !draft.trim()" data-testid="agent-send" @click="submit">
          <Icon name="send" :size="14" /> {{ sending ? 'Sending…' : 'Send' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.agent-view {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--sh-paper);
}
.agent-view__head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  padding: 20px 28px 16px;
  border-bottom: 2px solid var(--sh-ink);
  background: var(--sh-surface);
}
.crumb {
  font-family: var(--sh-font-mono);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--sh-graphite);
  display: flex;
  gap: 8px;
}
.agent-view__title {
  font-family: var(--sh-font-display);
  font-weight: 600;
  font-size: 26px;
  letter-spacing: -0.015em;
  margin: 6px 0 0;
  display: flex;
  align-items: center;
  gap: 10px;
}
.agent-view__stream {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 24px 28px;
  display: flex;
  flex-direction: column;
  gap: 18px;
  max-width: 920px;
  width: 100%;
  margin: 0 auto;
}
.agent-view__empty {
  color: var(--sh-graphite);
  font-size: 15px;
  line-height: 1.55;
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-width: 560px;
}
.agent-view__input {
  border-top: 2px solid var(--sh-ink);
  background: var(--sh-paper-2);
  padding: 14px 28px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: 920px;
  width: 100%;
  margin: 0 auto;
}
.agent-view__inputrow {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.agent-textarea {
  width: 100%;
  min-height: 68px;
  resize: vertical;
  color: var(--sh-ink);
  font-family: inherit;
  background: var(--sh-surface);
  border: 2px solid var(--sh-ink);
  border-radius: var(--sh-radius-sm);
  padding: 12px 14px;
  font-size: 14px;
}
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
.agent-md :deep(code) { font-family: var(--sh-font-mono); font-size: 12.5px; }
/* Same fix as AgentPanel.vue: the global `.sh code` rule paints inline code
   with its own light --sh-paper-2 background, which collides with the dark
   --sh-ink/--sh-paper <pre> above it — the code span floats as a near-white
   box under near-white inherited text. Reset it like the Pages renderer's
   `.vibe-content pre code` already does. */
.agent-md :deep(pre code) {
  background: transparent;
  border: none;
  padding: 0;
  color: inherit;
}
.agent-md :deep(table) { border-collapse: collapse; }
.agent-md :deep(th), .agent-md :deep(td) { border: 1px solid var(--sh-line); padding: 4px 8px; }
.agent-error { color: var(--sh-error); }
.agent-dim { color: var(--sh-graphite); font-style: italic; }
.agent-hint {
  font-family: var(--sh-font-mono);
  font-size: 10px;
  color: var(--sh-graphite);
  text-transform: uppercase;
  letter-spacing: 0.06em;
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
.agent-a2ui-embed {
  border: var(--sh-border-thin);
  border-radius: var(--sh-radius);
  background: var(--sh-surface);
  padding: 10px 12px;
  margin: 4px 0 8px;
}
</style>
