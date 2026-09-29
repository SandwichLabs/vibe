// useAgentChat — shared conversational-agent state used by both the slide-in
// AgentPanel and the dedicated Agent page (W0.5).
//
// It drives the streaming AgentChatStream binding: each send emits a turn-scoped
// "agent:stream" Wails event sequence (delta | tool | tool_done | final | error)
// that we render live (typing text + tool-use chips that go running -> done),
// while the binding's resolved result is treated as the authoritative final.
// Conversation history is threaded back to the backend for multi-turn memory.
import { ref } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import type { A2UIPageT } from '../a2ui/types'
import { renderSafeMarkdown } from '../lib/markdown'
import { errorText } from '../lib/errors'

// PendingApproval mirrors internal/agents.ApprovalRequest (#79), plus a
// client-only `state` this composable drives as the user answers the card
// (or it times out/gets cancelled) — see resolveApproval.
export interface PendingApproval {
  id: string
  summary: string
  sql?: string
  where?: string
  destructive: boolean
  // 'gone' is local-only: ResolveAgentApproval itself failed (e.g. the
  // request already resolved/expired server-side before the click landed)
  // — see resolveApproval. Every other state mirrors agent:approval_done's
  // outcome.
  state: 'pending' | 'sending' | 'approved' | 'declined' | 'expired' | 'cancelled' | 'gone'
  // error holds "Save, then do it"'s own failure (#98): SaveFile rejected,
  // so the card stays 'pending' — Resolve is never even attempted — and
  // shows why instead of silently doing nothing.
  error?: string
}

export interface ToolStep {
  id?: string
  tool: string
  input?: string
  output?: string
  status: 'running' | 'done' | 'error'
  // page holds the validated A2UI page a successful present_data call
  // returned (#67), parsed from its JSON output, so the panel can render it
  // inline instead of the raw JSON tool-result text. Never set for a denied
  // or failed call (isError), and never set for any other tool.
  page?: A2UIPageT
  // notebookLink holds the {id, title} a successful add_query_cell/
  // create_dashboard call returned (#67 deferred part), so the panel can
  // offer an "Open notebook" link instead of leaving the user to find the
  // new/updated notebook themselves. Never set for a denied/failed call or
  // any other tool.
  notebookLink?: { id: string; title: string }
  // approval holds the pending/resolved write-approval card for this step
  // (#79), correlated to it by tool-call id (ApprovalRequest.callId ==
  // ToolStep.id). Undefined for every step that never needed approval.
  approval?: PendingApproval
}

export interface ChatMessage {
  role: 'user' | 'agent' | 'error'
  text: string
  steps: ToolStep[]
  streaming?: boolean
  // verifying is set true between a "verifying" stream event (#70's
  // grounding check re-sending an ungrounded answer for one recheck pass)
  // and the next delta/final/error event, so the panel can show a
  // "Verifying…" state instead of leaving the first (about-to-be-replaced)
  // answer sitting there looking finished.
  verifying?: boolean
}

// ApprovalRequestPayload is what "agent:approval" carries under its
// `request` key. Go's internal/agents.ApprovalRequest has a few more fields
// (Table, Mode, Rows) that exist for the Go-side summary text and MCP/debug
// logging but that nothing here renders, so they're left out of this type —
// the wire payload still carries them; they're just not worth naming twice.
interface ApprovalRequestPayload {
  id: string
  callId: string
  tool: string
  summary: string
  sql?: string
  where?: string
  destructive: boolean
}

interface ApprovalPayload {
  turnId: string
  request: ApprovalRequestPayload
}

interface ApprovalDonePayload {
  turnId: string
  id: string
  outcome: 'approved' | 'declined' | 'expired' | 'cancelled'
}

interface StreamPayload {
  turnId: string
  kind: 'delta' | 'tool' | 'tool_done' | 'final' | 'error' | 'verifying'
  text?: string
  id?: string
  tool?: string
  input?: string
  output?: string
  // tool_done only: the tool call was denied or failed (Output carries the
  // error message). Without this the panel had no way to distinguish a
  // denied call from a successful one — both rendered the same "done" chip.
  isError?: boolean
}

// renderMarkdown turns agent text into sanitized HTML. This is a thin
// re-export of the ONE shared safe renderer (lib/markdown.ts) — it used to
// carry its own marked/DOMPurify call here, a second copy that had drifted
// from Skills' sanitization (weaker FORBID_TAGS, no image/link handling).
// Consumers (AgentPanel, AgentView) must render the result inside a
// `.vibe-md-content` container (see lib/markdown.ts's vibeMdContentClass)
// so installSafeLinkHandling's click interception actually applies to it.
export const renderMarkdown = renderSafeMarkdown

// parsePresentDataPage parses present_data's (and propose_schema's — #68
// follow-up: there is no dedicated Schema Builder diagram screen, so a
// proposed schema renders the same inline-A2UI way as present_data)
// tool_done output (a JSON-encoded internal/a2ui.Page) into an A2UIPageT for
// inline rendering, or returns undefined for anything else — a
// denied/errored call, an unrelated tool, or output that isn't the expected
// {title, components[]} shape. The page was already validated server side
// (internal/a2ui.Validate); this is just a parse, not a re-validation.
function parsePresentDataPage(tool: string | undefined, output: string | undefined, isError: boolean | undefined): A2UIPageT | undefined {
  if ((tool !== 'present_data' && tool !== 'propose_schema') || isError || !output) return undefined
  try {
    const parsed = JSON.parse(output)
    if (parsed && typeof parsed === 'object' && typeof parsed.title === 'string' && Array.isArray(parsed.components)) {
      return parsed as A2UIPageT
    }
  } catch {
    // Not JSON, or not a page — nothing to render inline.
  }
  return undefined
}

// parseNotebookLink parses add_query_cell/create_dashboard's tool_done
// output ({notebook_id, cell_id, title, warnings}) into the {id, title} the
// panel's "Open notebook" link needs, or undefined for anything else (a
// denied/errored call, a non-matching tool, or unparseable output).
function parseNotebookLink(tool: string | undefined, output: string | undefined, isError: boolean | undefined): { id: string; title: string } | undefined {
  if ((tool !== 'add_query_cell' && tool !== 'create_dashboard') || isError || !output) return undefined
  try {
    const parsed = JSON.parse(output)
    if (parsed && typeof parsed.notebook_id === 'string' && parsed.notebook_id) {
      return { id: parsed.notebook_id, title: typeof parsed.title === 'string' ? parsed.title : 'notebook' }
    }
  } catch {
    // Not JSON, or not this shape — nothing to link.
  }
  return undefined
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
    messages.value.push({ role: 'agent', text: '', steps: [], streaming: true })
    // Grab the agent message back THROUGH the reactive array so the stream
    // handler's mutations (text deltas, tool steps) trigger reactivity. Holding
    // the raw pre-push object would update state silently — watchers (e.g. the
    // Schema Builder's propose_schema watcher) would only fire on the next
    // unrelated reactive write.
    const agentMsg = messages.value[messages.value.length - 1]
    scroll()

    const turnId = nextTurnId()

    // agent:approval / agent:approval_done (#79) run alongside agent:stream,
    // filtered on the same turnId. Merge-by-id (same reasoning as the 'tool'
    // case above, in reverse): the approval event can arrive BEFORE the
    // matching 'tool' stream event, so a step may not exist yet here either.
    const unsubApproval = EventsOn('agent:approval', (p: ApprovalPayload) => {
      if (!p || p.turnId !== turnId) return
      const req = p.request
      const approval: PendingApproval = {
        id: req.id, summary: req.summary, sql: req.sql, where: req.where,
        destructive: req.destructive, state: 'pending',
      }
      const step = agentMsg.steps.find((s) => s.id === req.callId)
      if (step) step.approval = approval
      else agentMsg.steps.push({ id: req.callId, tool: req.tool, status: 'running', approval })
      scroll()
    })
    const unsubApprovalDone = EventsOn('agent:approval_done', (p: ApprovalDonePayload) => {
      if (!p || p.turnId !== turnId) return
      const step = agentMsg.steps.find((s) => s.approval?.id === p.id)
      if (step?.approval) step.approval.state = p.outcome
      scroll()
    })

    const unsub = EventsOn('agent:stream', (p: StreamPayload) => {
      if (!p || p.turnId !== turnId) return
      switch (p.kind) {
        case 'delta':
          agentMsg.text += p.text || ''
          break
        case 'tool': {
          // Merge by id instead of always pushing (#79): an "agent:approval"
          // event for this same call can arrive first and already have
          // created a placeholder step (see the agent:approval handler
          // below) — a plain push here would then duplicate it.
          const existing = agentMsg.steps.find((s) => s.id === p.id)
          if (existing) { existing.tool = p.tool || existing.tool; existing.input = p.input; existing.status = 'running' }
          else agentMsg.steps.push({ id: p.id, tool: p.tool || '?', input: p.input, status: 'running' })
          break
        }
        case 'tool_done': {
          const status = p.isError ? 'error' : 'done'
          const page = parsePresentDataPage(p.tool, p.output, p.isError)
          const notebookLink = parseNotebookLink(p.tool, p.output, p.isError)
          const step = agentMsg.steps.find((s) => s.id === p.id && s.status === 'running')
          if (step) { step.status = status; step.output = p.output; step.page = page; step.notebookLink = notebookLink }
          else agentMsg.steps.push({ id: p.id, tool: p.tool || '?', output: p.output, status, page, notebookLink })
          break
        }
        case 'final':
          if (p.text) agentMsg.text = p.text
          agentMsg.verifying = false
          break
        case 'error':
          agentMsg.role = 'error'
          agentMsg.text = `Agent error: ${p.text}. Is the LLM server reachable?`
          agentMsg.verifying = false
          break
        case 'verifying':
          // The agent's first answer asserted a fact with no grounding tool
          // call this turn (#70) and is being sent back once for
          // verification — the text currently shown is about to be
          // replaced by the recheck's final answer.
          agentMsg.verifying = true
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
      const m = errorText(e)
      agentMsg.role = 'error'
      agentMsg.text = `Agent error: ${m}. Is the LLM server reachable?`
    } finally {
      agentMsg.streaming = false
      // Belt-and-suspenders (review fix, round 2): the "final"/"error" event
      // handlers above already clear verifying, but both are events that
      // can arrive (or be missed) independently of when the AgentChatStream
      // binding promise itself resolves — a "verifying" event with no
      // matching "final"/"error" ever following it (e.g. the binding
      // resolves from its OWN authoritative result before that event's
      // handler runs, or the event is dropped) left "Agent · verifying"
      // stuck on screen after the turn had actually finished. The binding
      // promise settling, one way or the other, is the true end of the
      // turn, so this is where verifying is unconditionally cleared.
      agentMsg.verifying = false
      unsub()
      unsubApproval()
      unsubApprovalDone()
      sending.value = false
      scroll()
    }
  }

  // resolveApproval answers a pending card: optimistically flips it to
  // 'sending' (disabling both buttons immediately — see ApprovalCard), then
  // calls ResolveAgentApproval. The eventual real state ('approved'/
  // 'declined'/...) arrives via the "agent:approval_done" handler above; a
  // call error here means the click itself came too late — the request had
  // already expired or been cancelled server-side, or the backend simply
  // isn't reachable — and 'gone' says exactly that ("no longer waiting")
  // rather than falsely claiming the specific reason was a timeout.
  // saveFirst is "Save, then do it" (#98): the file has unsaved table
  // changes, so Revert would otherwise undo those along with whatever the
  // agent is about to do. Save landing first makes the agent's change the
  // only thing left to revert. If SaveFile itself fails, the card stays
  // 'pending' with the error shown — ResolveAgentApproval is never called,
  // so the write is not silently approved on a save that didn't happen.
  async function resolveApproval(step: ToolStep, approved: boolean, saveFirst = false) {
    if (!step.approval) return
    step.approval.state = 'sending'
    step.approval.error = undefined
    if (saveFirst) {
      try {
        await window.go.main.App.SaveFile()
      } catch (e) {
        step.approval.state = 'pending'
        step.approval.error = "Couldn't save: " + errorText(e)
        return
      }
    }
    try {
      await window.go.main.App.ResolveAgentApproval(step.approval.id, approved)
    } catch {
      // Keep a specific 'cancelled'/'expired' that arrived first.
      if (step.approval.state === 'sending') step.approval.state = 'gone'
    }
  }

  return { messages, sending, pinnedContext, send, pin, unpin, reset, setScrollHandler, renderMarkdown, resolveApproval }
}
