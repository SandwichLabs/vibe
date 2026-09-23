<script setup lang="ts">
import { ref, nextTick, watch, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from './Icon.vue'
import { useAgentChat } from '../composables/useAgentChat'
import A2UIPage from '../a2ui/A2UIPage.vue'

// AgentPanel is the slide-in right-docked wizard chat (W0/W0.5). It drives the
// streaming AgentChatStream binding via the shared useAgentChat composable: the
// agent's text streams in, tool use shows as chips that go running -> done, the
// conversation is multi-turn, and answers render as sanitized markdown.
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const route = useRoute()
const router = useRouter()
const { messages, sending, pinnedContext, send, pin, unpin, setScrollHandler, renderMarkdown } = useAgentChat()

function openNotebook(id: string) {
  router.push(`/notebooks/${id}`)
  emit('close')
}

const draft = ref('')
const streamEl = ref<HTMLElement | null>(null)

const currentScreen = computed(() => {
  const name = (route.name as string) || route.path
  return `the ${name} screen (${route.path})`
})

function pinContext() { pin(currentScreen.value) }

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

watch(() => props.open, (o) => { if (o) scrollToBottom() })
</script>

<template>
  <div class="agent-drawer" :class="{ open }" data-testid="agent-panel">
    <div class="agent-side">
      <div class="head">
        <div class="title"><Icon name="bot" :size="18" /> Agent</div>
        <span v-if="pinnedContext" class="meta" data-testid="agent-pin">
          <Icon name="pin" :size="11" /> {{ pinnedContext }}
        </span>
        <button class="agent-close" @click="emit('close')" aria-label="Close agent panel">
          <Icon name="x" :size="16" />
        </button>
      </div>

      <div ref="streamEl" class="stream">
        <div v-if="messages.length === 0" class="agent-empty">
          <Icon name="sparkle" :size="20" />
          <p>Ask the wizard to inspect your data or build something. It can read your schema, run read-only SQL, and create tables.</p>
        </div>

        <div v-for="(m, i) in messages" :key="i" class="amsg">
          <div class="av" :class="m.role === 'user' ? 'user' : 'bot'">
            {{ m.role === 'user' ? 'You' : 'AI' }}
          </div>
          <div class="body">
            <div class="who">
              {{ m.role === 'user' ? 'You' : m.role === 'error' ? 'Agent · error' : (m.verifying ? 'Agent · verifying' : m.streaming ? 'Agent · streaming' : 'Agent') }}
            </div>
            <div v-if="m.role === 'user'" class="user-bubble">{{ m.text }}</div>
            <p v-else-if="m.role === 'error'" class="agent-text agent-error">{{ m.text }}</p>
            <div v-else-if="m.text" class="agent-text agent-md vibe-content vibe-md-content" v-html="renderMarkdown(m.text)"></div>
            <p v-else-if="m.streaming" class="agent-text agent-dim">Thinking…</p>
            <p v-if="m.verifying" class="agent-text agent-dim" data-testid="agent-verifying">Verifying that answer against the data…</p>

            <template v-for="(s, si) in m.steps" :key="si">
              <div
                class="tool-chip"
                :class="{ 'tool-chip-error': s.status === 'error' }"
                data-testid="agent-tool-chip"
              >
                <span class="lab"><Icon name="terminal" :size="12" /> {{ s.tool }}</span>
                <span v-if="s.input" class="time">{{ s.input.slice(0, 48) }}</span>
                <span v-if="s.status === 'running'" class="spin" data-testid="chip-running">●</span>
                <span v-else-if="s.status === 'error'" class="chip-error-mark" data-testid="chip-error" :title="s.output">✕</span>
                <span v-else class="check" data-testid="chip-done">✓</span>
              </div>
              <A2UIPage v-if="s.page" :page="s.page" class="agent-a2ui-embed" data-testid="agent-a2ui" />
              <button
                v-if="s.notebookLink"
                class="agent-pinbtn"
                style="margin: 4px 0;"
                data-testid="agent-open-notebook"
                @click="openNotebook(s.notebookLink.id)"
              ><Icon name="fileText" :size="13" /> Open notebook — {{ s.notebookLink.title }}</button>
            </template>
          </div>
        </div>
      </div>

      <div class="input">
        <div class="tools">
          <span style="flex: 1"></span>
          <button v-if="!pinnedContext" class="agent-pinbtn" @click="pinContext">
            <Icon name="pin" :size="13" /> Pin context
          </button>
          <button v-else class="agent-pinbtn active" @click="unpin">
            <Icon name="pin" :size="13" /> Unpin
          </button>
        </div>
        <textarea
          v-model="draft"
          class="box agent-textarea"
          data-testid="agent-input"
          placeholder="Ask the agent to inspect data or build something…"
          @keydown="onKeydown"
        ></textarea>
        <div class="row" style="display: flex; justify-content: space-between; align-items: center;">
          <span class="agent-hint">⏎ Send · ⇧⏎ Newline</span>
          <button class="btn btn-primary" :disabled="sending || !draft.trim()" data-testid="agent-send" @click="submit">
            <Icon name="send" :size="13" /> {{ sending ? 'Sending…' : 'Send' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.agent-drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: 410px;
  max-width: 92vw;
  z-index: 900;
  transform: translateX(100%);
  transition: transform 0.18s ease;
  pointer-events: none;
}
.agent-drawer.open {
  transform: translateX(0);
  pointer-events: auto;
}
.agent-drawer .agent-side {
  height: 100%;
}
/* The shipped hifi.css sets `.agent-side .stream { overflow: hidden }`, which
   locks scrolling. Override it (higher specificity via scope attr) and add the
   flex min-height:0 a scroll container needs. */
.agent-side .stream {
  overflow-y: auto;
  min-height: 0;
}
.agent-close {
  margin-left: 8px;
  background: transparent;
  border: none;
  color: var(--sh-paper);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
}
.agent-empty {
  color: var(--sh-graphite);
  font-size: 13px;
  line-height: 1.5;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.agent-text { white-space: pre-wrap; }
.agent-md { white-space: normal; }
.agent-md :deep(p) { margin: 0 0 6px; }
.agent-md :deep(pre) {
  background: var(--sh-ink);
  color: var(--sh-paper);
  padding: 8px 10px;
  border-radius: var(--sh-radius-sm);
  overflow-x: auto;
  font-family: var(--sh-font-mono);
  font-size: 12px;
}
.agent-md :deep(code) { font-family: var(--sh-font-mono); font-size: 12px; }
/* The global `.sh code` rule (schoolhouse/colors_and_type.css) gives inline
   code its own light --sh-paper-2 background + ink border. Inside a fenced
   block that collides with the dark --sh-ink/--sh-paper <pre> above: the
   <code> painted its own near-white box, and the near-white text inherited
   from <pre> sat on top of it — a near-illegible light-on-light block.
   Reset it exactly like the Pages renderer already does (vibe-content pre
   code in style.css) so fenced code reads as light text on the dark pre. */
.agent-md :deep(pre code) {
  background: transparent;
  border: none;
  padding: 0;
  color: inherit;
}
.agent-md :deep(ul), .agent-md :deep(ol) { margin: 4px 0; padding-left: 20px; }
.agent-error { color: var(--sh-coral); }
.agent-dim { color: var(--sh-graphite); font-style: italic; }
.agent-textarea {
  width: 100%;
  resize: vertical;
  color: var(--sh-ink);
  font-family: inherit;
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
.agent-pinbtn.active {
  background: var(--sh-teal);
  color: var(--sh-paper);
  border-color: var(--sh-teal);
}
.agent-hint {
  font-family: var(--sh-font-mono);
  font-size: 10px;
  color: var(--sh-graphite);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.spin { animation: agent-pulse 1s ease-in-out infinite; }
@keyframes agent-pulse { 0%, 100% { opacity: 1 } 50% { opacity: 0.3 } }
/* A denied/failed tool call (e.g. sandbox permission denial) — SchoolHouse
   coral is "needs attention", not destructive/red, matching a call the user
   should notice but that isn't a crash. */
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
/* present_data's inline A2UI result (#67) — a compact card in the transcript,
   distinct from the plain-text agent bubble above it. */
.agent-a2ui-embed {
  border: var(--sh-border-thin);
  border-radius: var(--sh-radius);
  background: var(--sh-surface);
  padding: 10px 12px;
  margin: 4px 0 8px;
}
</style>
