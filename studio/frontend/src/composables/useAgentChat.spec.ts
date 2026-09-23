// Tests for the useAgentChat composable (W0.5): streaming event handling,
// running->done tool chips, multi-turn history, and markdown sanitization.
import { describe, it, expect, beforeEach, vi } from 'vitest'

// Capture the handler registered via EventsOn so tests can drive a stream.
let streamHandler: ((p: any) => void) | null = null
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (_name: string, cb: (p: any) => void) => {
    streamHandler = cb
    return () => { streamHandler = null }
  },
}))

import { useAgentChat, renderMarkdown } from './useAgentChat'

function setBinding(impl: (turnId: string, msg: string, pinned: string, history: any[]) => Promise<any>) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: { AgentChatStream: vi.fn(impl) } } }
  return (window as any).go.main.App.AgentChatStream
}

describe('useAgentChat (W0.5)', () => {
  beforeEach(() => { streamHandler = null })

  it('streams deltas + running->done tool chips and finalizes', async () => {
    const binding = setBinding(async (turnId) => {
      // Simulate the backend's event sequence for this turn.
      streamHandler!({ turnId, kind: 'delta', text: 'Hel' })
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'list_tables', input: '' })
      // Mid-flight the chip should be "running".
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'list_tables', output: 'customers' })
      streamHandler!({ turnId, kind: 'delta', text: 'lo' })
      streamHandler!({ turnId, kind: 'final', text: 'Hello. You have customers.' })
      return { text: 'Hello. You have customers.', steps: [] }
    })

    const chat = useAgentChat()
    await chat.send('hi')

    expect(binding).toHaveBeenCalledTimes(1)
    expect(chat.messages.value).toHaveLength(2)
    const agent = chat.messages.value[1]
    expect(agent.role).toBe('agent')
    expect(agent.text).toBe('Hello. You have customers.')
    expect(agent.streaming).toBe(false)
    expect(agent.steps).toHaveLength(1)
    expect(agent.steps[0].tool).toBe('list_tables')
    expect(agent.steps[0].status).toBe('done')
    expect(agent.steps[0].output).toBe('customers')
  })

  it('marks a denied/errored tool call as status "error", not "done"', async () => {
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'create_table', input: '{}' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'create_table', output: 'denied', isError: true })
      streamHandler!({ turnId, kind: 'final', text: 'Cannot do that.' })
      return { text: 'Cannot do that.', steps: [] }
    })

    const chat = useAgentChat()
    await chat.send('create a table')

    const agent = chat.messages.value[1]
    expect(agent.steps).toHaveLength(1)
    expect(agent.steps[0].status).toBe('error')
    expect(agent.steps[0].output).toBe('denied')
  })

  it('threads multi-turn history to the backend', async () => {
    const binding = setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'final', text: 'reply' })
      return { text: 'reply', steps: [] }
    })

    const chat = useAgentChat()
    await chat.send('first question')
    await chat.send('second question')

    expect(binding).toHaveBeenCalledTimes(2)
    const secondCallHistory = binding.mock.calls[1][3]
    // History sent on the 2nd turn includes the 1st user + agent messages.
    expect(secondCallHistory).toEqual([
      { role: 'user', text: 'first question' },
      { role: 'agent', text: 'reply' },
    ])
  })

  it('passes pinned context and renders it', async () => {
    const binding = setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'final', text: 'ok' })
      return { text: 'ok', steps: [] }
    })
    const chat = useAgentChat()
    chat.pin('the tables screen (/tables)')
    expect(chat.pinnedContext.value).toContain('tables')
    await chat.send('what is here?')
    expect(binding.mock.calls[0][2]).toContain('tables')
  })

  it('surfaces a graceful error when the binding rejects', async () => {
    setBinding(async () => { throw new Error('connection refused') })
    const chat = useAgentChat()
    await chat.send('hi')
    const agent = chat.messages.value[1]
    expect(agent.role).toBe('error')
    expect(agent.text).toContain('connection refused')
    expect(chat.sending.value).toBe(false)
  })

  it('does not send empty messages', async () => {
    const binding = setBinding(async () => ({ text: 'x', steps: [] }))
    const chat = useAgentChat()
    await chat.send('   ')
    expect(binding).not.toHaveBeenCalled()
  })

  // present_data (#67): a successful tool_done for present_data should parse
  // its JSON output into a page the panel renders inline; a denied/failed
  // call, or any other tool, must never populate `page`.
  it('parses a successful present_data result into step.page', async () => {
    const pageJSON = JSON.stringify({ title: 'Customers', components: [{ id: 'h', type: 'heading', text: 'Customers' }] })
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'present_data', input: '{}' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'present_data', output: pageJSON })
      streamHandler!({ turnId, kind: 'final', text: 'Here you go.' })
      return { text: 'Here you go.', steps: [] }
    })
    const chat = useAgentChat()
    await chat.send('show me customers')
    const step = chat.messages.value[1].steps[0]
    expect(step.page).toEqual({ title: 'Customers', components: [{ id: 'h', type: 'heading', text: 'Customers' }] })
  })

  it('does not set page for a denied present_data call', async () => {
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'present_data', input: '{}' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'present_data', output: 'components[0].type: unknown type "pieChart"', isError: true })
      streamHandler!({ turnId, kind: 'final', text: 'Sorry, that failed.' })
      return { text: 'Sorry, that failed.', steps: [] }
    })
    const chat = useAgentChat()
    await chat.send('show me a pie chart')
    const step = chat.messages.value[1].steps[0]
    expect(step.status).toBe('error')
    expect(step.page).toBeUndefined()
  })

  // #70 grounding check: a "verifying" event sets a verifying flag, cleared
  // once the turn's own "final" event arrives.
  it('sets verifying=true on a "verifying" event and clears it on "final"', async () => {
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'delta', text: 'roughly 40' })
      streamHandler!({ turnId, kind: 'verifying' })
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'run_query', input: '{}' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'run_query', output: '[{"n":3}]' })
      streamHandler!({ turnId, kind: 'final', text: 'exactly 3, verified' })
      return { text: 'exactly 3, verified', steps: [] }
    })
    const chat = useAgentChat()
    await chat.send('how many rows roughly')
    const agent = chat.messages.value[1]
    expect(agent.verifying).toBe(false)
    expect(agent.text).toBe('exactly 3, verified')
  })

  // Review fix (round 2): a "verifying" event with NO matching "final"/
  // "error" ever following it (e.g. the binding's own authoritative result
  // settles the turn without that follow-up event landing) must not leave
  // "Agent · verifying" stuck forever — the finally block clears it
  // unconditionally once the AgentChatStream promise itself settles.
  it('clears verifying once the turn settles even if no final/error event follows it', async () => {
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'verifying' })
      // No "final"/"error" event ever emitted — only the binding's own
      // resolved result settles the turn.
      return { text: 'settled without a final event', steps: [] }
    })
    const chat = useAgentChat()
    await chat.send('how many rows roughly')
    const agent = chat.messages.value[1]
    expect(agent.verifying).toBe(false)
    expect(agent.text).toBe('settled without a final event')
  })

  // propose_schema (#68 follow-up): there is no dedicated Schema Builder
  // diagram screen any more — a proposed schema renders inline the same way
  // present_data does.
  it('parses a successful propose_schema result into step.page', async () => {
    const pageJSON = JSON.stringify({
      title: 'Proposed schema',
      components: [
        { id: 'h1', type: 'heading', text: 'Proposed schema', level: 1 },
        { id: 'h2', type: 'heading', text: 'customers', level: 2 },
        { id: 't1', type: 'table', columns: ['Column', 'Type', 'Constraints'], rows: [['id', 'ID', 'PRIMARY KEY']] },
      ],
    })
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'propose_schema', input: '{}' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'propose_schema', output: pageJSON })
      streamHandler!({ turnId, kind: 'final', text: 'Want me to build this?' })
      return { text: 'Want me to build this?', steps: [] }
    })
    const chat = useAgentChat()
    await chat.send('design my database')
    const step = chat.messages.value[1].steps[0]
    expect(step.page?.title).toBe('Proposed schema')
    expect(step.page?.components).toHaveLength(3)
  })

  it('does not set page for a denied propose_schema call', async () => {
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'propose_schema', input: '{}' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'propose_schema', output: 'propose_schema needs at least one table', isError: true })
      streamHandler!({ turnId, kind: 'final', text: 'Sorry, that failed.' })
      return { text: 'Sorry, that failed.', steps: [] }
    })
    const chat = useAgentChat()
    await chat.send('design nothing')
    const step = chat.messages.value[1].steps[0]
    expect(step.status).toBe('error')
    expect(step.page).toBeUndefined()
  })

  it('does not set page for a non-present_data tool, even with JSON-shaped output', async () => {
    setBinding(async (turnId) => {
      streamHandler!({ turnId, kind: 'tool', id: 't1', tool: 'run_query', input: '{}' })
      streamHandler!({ turnId, kind: 'tool_done', id: 't1', tool: 'run_query', output: '{"title":"x","components":[]}' })
      streamHandler!({ turnId, kind: 'final', text: 'done' })
      return { text: 'done', steps: [] }
    })
    const chat = useAgentChat()
    await chat.send('run a query')
    const step = chat.messages.value[1].steps[0]
    expect(step.page).toBeUndefined()
  })
})

describe('renderMarkdown', () => {
  it('renders markdown to HTML', () => {
    const html = renderMarkdown('**bold** and `code`')
    expect(html).toContain('<strong>bold</strong>')
    expect(html).toContain('<code>code</code>')
  })

  it('sanitizes dangerous HTML (XSS guard for the v-html sink)', () => {
    const html = renderMarkdown('hi <img src=x onerror=alert(1)> <script>alert(2)</script>')
    expect(html).not.toContain('onerror')
    expect(html).not.toContain('<script>')
  })
})
