<script setup lang="ts">
import { ref, nextTick, watch, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from './Icon.vue'
import { useAgentChat } from '../composables/useAgentChat'
import AgentTranscript from './AgentTranscript.vue'
import AgentSandboxIntro from './AgentSandboxIntro.vue'

// AgentPanel is the slide-in right-docked wizard chat (W0/W0.5). It drives the
// streaming AgentChatStream binding via the shared useAgentChat composable: the
// agent's text streams in, tool use shows as chips that go running -> done, the
// conversation is multi-turn, and answers render as sanitized markdown.
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const route = useRoute()
const router = useRouter()
const { messages, sending, stoppable, pinnedContext, send, pin, unpin, reset, cancel, setScrollHandler, resolveApproval } = useAgentChat()

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
// The chat resets itself on workspace open/close (useAgentChat).
setScrollHandler(scrollToBottom)

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
  <div id="agent-panel" class="agent-drawer" :class="{ open }" data-testid="agent-panel" role="complementary" aria-label="Agent" :aria-hidden="!open">
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
          <AgentSandboxIntro />
        </div>

        <AgentTranscript :messages="messages" compact @resolve="resolveApproval" @open-notebook="openNotebook" />
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
          <div class="agent-actions">
            <button v-if="stoppable" class="btn btn-inverted" data-testid="agent-cancel" @click="cancel">
              <Icon name="x" :size="13" /> Stop
            </button>
            <button class="btn btn-primary" :disabled="sending || !draft.trim()" data-testid="agent-send" @click="submit">
              <Icon name="send" :size="13" /> {{ sending ? 'Sending…' : 'Send' }}
            </button>
          </div>
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
  color: var(--vibe-chrome-fg);
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
  background: var(--vibe-accent);
  color: var(--vibe-accent-fg);
  border-color: var(--vibe-accent);
}
.agent-hint {
  font-family: var(--sh-font-mono);
  font-size: 10px;
  color: var(--sh-graphite);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.agent-actions { display: flex; gap: 8px; }
</style>
