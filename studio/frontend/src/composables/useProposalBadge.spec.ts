// useProposalBadge (E4): the Semantics nav badge counts open proposals only, and
// reads 0 when the workspace cannot answer.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, vi } from 'vitest'
import { openProposals, startProposalBadge } from './useProposalBadge'

const prop = (id: string, status: Proposal['status']): Proposal => ({
  id, kind: 'metric', name: 'm', status, proposed_at: '', proposed_by: 'agent', rationale: 'r', evidence: [], replaces_rev: '', decided_at: '',
})

function install(app: MockApp) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
}

describe('useProposalBadge', () => {
  it('counts only the open proposals', async () => {
    install({ ListProposals: vi.fn(async () => [prop('a', 'open'), prop('b', 'accepted'), prop('c', 'open')]) })
    const stop = startProposalBadge()
    await vi.waitFor(() => expect(openProposals.value).toBe(2))
    stop()
  })

  it('reads 0 when the workspace cannot answer', async () => {
    install({ ListProposals: vi.fn(async () => { throw new Error('no workspace') }) })
    openProposals.value = 5
    const stop = startProposalBadge()
    await vi.waitFor(() => expect(openProposals.value).toBe(0))
    stop()
  })
})
