<script setup lang="ts">
import { ref, nextTick, watch, computed } from 'vue'
import { useRoute } from 'vue-router'
import Icon from './Icon.vue'

// AgentPanel is the slide-in right-docked wizard chat (W0). It drives the real
// AgentChat binding, which runs a tool-calling loop against the configured
// OpenAI-compatible LLM endpoint (default: local llama-server at :8080). Tool
// use the agent performs is surfaced inline as tool-chips, and the user can pin
// the current screen as context for the agent.
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const route = useRoute()

interface ToolStep { tool: string; input: string; output: string }
interface ChatMessage {
  role: 'user' | 'agent' | 'error'
  text: string
  steps?: ToolStep[]
}

const messages = ref<ChatMessage[]>([])
const draft = ref('')
const sending = ref(false)
const pinnedContext = ref('')
const streamEl = ref<HTMLElement | null>(null)

// A human label for what the user is currently looking at, used when pinning.
const currentScreen = computed(() => {
  const name = (route.name as string) || route.path
  return `the ${name} screen (${route.path})`
})

function pinContext() {
  pinnedContext.value = currentScreen.value
}
function clearPin() {
  pinnedContext.value = ''
}

async function scrollToBottom() {
  await nextTick()
  if (streamEl.value) streamEl.value.scrollTop = streamEl.value.scrollHeight
}

async function send() {
  const text = draft.value.trim()
  if (!text || sending.value) return
  messages.value.push({ role: 'user', text })
  draft.value = ''
  sending.value = true
  await scrollToBottom()
  try {
    const res = await window.go.main.App.AgentChat(text, pinnedContext.value)
    messages.value.push({
      role: 'agent',
      text: res?.text || '(no response)',
      steps: (res?.steps || []) as ToolStep[],
    })
  } catch (e: any) {
    const msg = typeof e === 'string' ? e : e?.message || String(e)
    messages.value.push({
      role: 'error',
      text: `Agent error: ${msg}. Is the LLM server reachable?`,
    })
  } finally {
    sending.value = false
    await scrollToBottom()
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
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
            <div class="who">{{ m.role === 'user' ? 'You' : m.role === 'error' ? 'Agent · error' : 'Agent' }}</div>
            <div v-if="m.role === 'user'" class="user-bubble">{{ m.text }}</div>
            <p v-else class="agent-text" :class="{ 'agent-error': m.role === 'error' }">{{ m.text }}</p>
            <div
              v-for="(s, si) in (m.steps || [])"
              :key="si"
              class="tool-chip"
              data-testid="agent-tool-chip"
            >
              <span class="lab"><Icon name="terminal" :size="12" /> {{ s.tool }}</span>
              <span v-if="s.input" class="time">{{ s.input.slice(0, 60) }}</span>
              <span class="check">✓</span>
            </div>
          </div>
        </div>

        <div v-if="sending" class="amsg" data-testid="agent-thinking">
          <div class="av bot">AI</div>
          <div class="body">
            <div class="who">Agent · thinking</div>
            <p class="agent-text">Working on it…</p>
          </div>
        </div>
      </div>

      <div class="input">
        <div class="tools">
          <span style="flex: 1"></span>
          <button v-if="!pinnedContext" class="agent-pinbtn" @click="pinContext">
            <Icon name="pin" :size="13" /> Pin context
          </button>
          <button v-else class="agent-pinbtn active" @click="clearPin">
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
          <button class="btn btn-primary" :disabled="sending || !draft.trim()" data-testid="agent-send" @click="send">
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
.stream { overflow-y: auto; }
.agent-text { white-space: pre-wrap; }
.agent-error { color: var(--sh-coral); }
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
</style>
