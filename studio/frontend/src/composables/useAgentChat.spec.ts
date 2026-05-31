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
