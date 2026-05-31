// useAgentChat — shared conversational-agent state used by both the slide-in
// AgentPanel and the dedicated Agent page (W0.5).
//
// It drives the streaming AgentChatStream binding: each send emits a turn-scoped
// "agent:stream" Wails event sequence (delta | tool | tool_done | final | error)
// that we render live (typing text + tool-use chips that go running -> done),
// while the binding's resolved result is treated as the authoritative final.
// Conversation history is threaded back to the backend for multi-turn memory.
import { ref } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { EventsOn } from '../wailsjs/runtime/runtime'

export interface ToolStep {
  id?: string
  tool: string
  input?: string
  output?: string
  status: 'running' | 'done'
}

export interface ChatMessage {
  role: 'user' | 'agent' | 'error'
  text: string
  steps: ToolStep[]
  streaming?: boolean
}

interface StreamPayload {
  turnId: string
  kind: 'delta' | 'tool' | 'tool_done' | 'final' | 'error'
  text?: string
  id?: string
  tool?: string
  input?: string
  output?: string
}

// renderMarkdown turns agent text into sanitized HTML. The agent output is
// rendered as markdown; DOMPurify guards the v-html sink against any HTML the
// model might emit.
export function renderMarkdown(text: string): string {
  const raw = marked.parse(text ?? '', { async: false }) as string
  return DOMPurify.sanitize(raw)
}

let turnSeq = 0
function nextTurnId(): string {
  turnSeq += 1
  // Avoid Math.random/crypto dependence for determinism in tests; a per-session
  // monotonic id is enough to scope events to a turn.
  return `turn-${turnSeq}-${turnSeq * 2654435761 % 1000000}`
}

export function useAgentChat() {
  const messages = ref<ChatMessage[]>([])
  const sending = ref(false)
  const pinnedContext = ref('')

  // onScroll is invoked after each mutation so the host can keep the view
  // pinned to the latest message.
  let onScroll: (() => void) | null = null
  function setScrollHandler(fn: () => void) { onScroll = fn }
  function scroll() { if (onScroll) onScroll() }

  function pin(context: string) { pinnedContext.value = context }
  function unpin() { pinnedContext.value = '' }
  function reset() { messages.value = [] }

  async function send(text: string) {
    const msg = text.trim()
    if (!msg || sending.value) return
    sending.value = true

    // History = the transcript so far (before this turn), for multi-turn memory.
    const history = messages.value
      .filter((m) => m.role !== 'error')
      .map((m) => ({ role: m.role === 'agent' ? 'agent' : 'user', text: m.text }))

    messages.value.push({ role: 'user', text: msg, steps: [] })
    const agentMsg: ChatMessage = { role: 'agent', text: '', steps: [], streaming: true }
    messages.value.push(agentMsg)
    scroll()

    const turnId = nextTurnId()
    const unsub = EventsOn('agent:stream', (p: StreamPayload) => {
      if (!p || p.turnId !== turnId) return
      switch (p.kind) {
        case 'delta':
          agentMsg.text += p.text || ''
          break
        case 'tool':
          agentMsg.steps.push({ id: p.id, tool: p.tool || '?', input: p.input, status: 'running' })
          break
        case 'tool_done': {
          const step = agentMsg.steps.find((s) => s.id === p.id && s.status === 'running')
          if (step) { step.status = 'done'; step.output = p.output }
          else agentMsg.steps.push({ id: p.id, tool: p.tool || '?', output: p.output, status: 'done' })
          break
        }
        case 'final':
          if (p.text) agentMsg.text = p.text
          break
        case 'error':
          agentMsg.role = 'error'
          agentMsg.text = `Agent error: ${p.text}. Is the LLM server reachable?`
          break
      }
      scroll()
    })

    try {
      const res = await window.go.main.App.AgentChatStream(turnId, msg, pinnedContext.value, history)
      // Authoritative final result (covers non-streaming servers / missed events).
      if (agentMsg.role !== 'error') {
        agentMsg.text = res?.text || agentMsg.text || '(no response)'
        // Mark any still-"running" chips as done now that the turn finished.
        agentMsg.steps.forEach((s) => { if (s.status === 'running') s.status = 'done' })
      }
    } catch (e: any) {
      const m = typeof e === 'string' ? e : e?.message || String(e)
      agentMsg.role = 'error'
      agentMsg.text = `Agent error: ${m}. Is the LLM server reachable?`
    } finally {
      agentMsg.streaming = false
      unsub()
      sending.value = false
      scroll()
    }
  }

  return { messages, sending, pinnedContext, send, pin, unpin, reset, setScrollHandler, renderMarkdown }
}
