// agentDraft (docs/plans/library.md L7): the unsent request per type, the
// prefill rule, and the pending ask for a new notebook.
import { describe, it, expect, vi } from 'vitest'
import { agentDraft, openAgent, prefillText, OPEN_AGENT_EVENT } from './agentDraft'
import { setPendingAsk, takePendingAsk, clearPendingAsk } from '../composables/usePendingAsk'

describe('agentDraft', () => {
  it('names the type, the view kind and write_artifact for each artifact type', () => {
    expect(agentDraft('live', ' weekly sales ')).toBe('Build a live view (a data artifact with an A2UI view, view: a2ui) with write_artifact (read the data-artifacts skill first): weekly sales')
    expect(agentDraft('page', 'x')).toContain('HTML page (a data artifact with view: html)')
    expect(agentDraft('page', 'x', true)).toContain('with a script')
    expect(agentDraft('script', 'x')).toContain('chart script (a data artifact with view: script)')
    for (const t of ['live', 'page', 'script'] as const) expect(agentDraft(t, 'x')).toContain('write_artifact')
  })
})

describe('openAgent', () => {
  it('dispatches assay:open-agent with the draft in detail, or without detail', () => {
    const seen = vi.fn()
    window.addEventListener(OPEN_AGENT_EVENT, seen)
    openAgent('hello')
    openAgent()
    window.removeEventListener(OPEN_AGENT_EVENT, seen)
    expect((seen.mock.calls[0][0] as CustomEvent).detail).toEqual({ draft: 'hello' })
    expect((seen.mock.calls[1][0] as CustomEvent).detail).toBeNull()
  })
})

describe('prefillText', () => {
  it('fills an empty box or one still holding the previous prefill', () => {
    expect(prefillText('', '', 'new', false)).toBe('new')
    expect(prefillText('  \n', '', 'new', false)).toBe('new')
    expect(prefillText('old prefill', 'old prefill', 'new', false)).toBe('new')
  })
  it('never overwrites typed text, never while sending, never an empty draft', () => {
    expect(prefillText('my question', '', 'new', false)).toBeNull()
    expect(prefillText('old prefill edited', 'old prefill', 'new', false)).toBeNull()
    expect(prefillText('', '', 'new', true)).toBeNull()
    expect(prefillText('', '', '', false)).toBeNull()
  })
})

describe('usePendingAsk', () => {
  it('is taken once, only by its notebook, and dropped on clear', () => {
    setPendingAsk('nb1', 'q')
    expect(takePendingAsk('other')).toBeNull()
    expect(takePendingAsk('nb1')).toBe('q')
    expect(takePendingAsk('nb1')).toBeNull()
    setPendingAsk('nb2', 'q2')
    clearPendingAsk()
    expect(takePendingAsk('nb2')).toBeNull()
  })
})
