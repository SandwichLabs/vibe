// useProposals (E4): the inbox flow. Accepting a proposal whose file changed
// since is a conflict that waits for "accept anyway"; nothing else writes.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, vi } from 'vitest'
import { useProposals } from './useProposals'

const prop = (id: string, status: Proposal['status'] = 'open'): Proposal => ({
  id, kind: 'metric', name: 'net_revenue', status, proposed_at: '2026-10-29T15:30:12Z', proposed_by: 'agent',
  rationale: 'Refunds are excluded.', evidence: [], replaces_rev: '', decided_at: '',
})
const pv = (id: string, stale = false): ProposalPreview => ({
  proposal: prop(id), target: 'semantics/metrics/net_revenue.yaml', exists: stale, current_text: stale ? 'old\n' : '',
  current_rev: stale ? 'r2' : '', proposed_text: 'new\n', stale, stale_reason: stale ? 'changed' : undefined,
  shown_rev: 'pin-' + id, warnings: [],
})

function install(app: MockApp) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
}

describe('useProposals', () => {
  it('loads the open proposals and accepts the selected one', async () => {
    const app: MockApp = {
      ListProposals: vi.fn().mockResolvedValueOnce([prop('a'), prop('b', 'accepted')]).mockResolvedValueOnce([prop('b', 'accepted')]),
      GetProposal: vi.fn(async (id: string) => pv(id)),
      AcceptProposal: vi.fn(async () => {}),
    }
    install(app)
    const p = useProposals()
    await p.load()
    expect(p.open.value.map((x) => x.id)).toEqual(['a'])
    await p.select('a')
    expect(p.preview.value?.proposed_text).toBe('new\n')
    expect(await p.accept()).toBe(true)
    expect(app.AcceptProposal).toHaveBeenCalledWith('a', 'pin-a', false)
    expect(p.preview.value).toBeNull()
    expect(p.open.value).toEqual([])
  })

  it('a changed file is a conflict until the person accepts anyway', async () => {
    const accept = vi.fn()
      .mockRejectedValueOnce(new Error('conflict: semantics/metrics/net_revenue.yaml changed since this was proposed'))
      .mockResolvedValueOnce(undefined)
    install({ ListProposals: vi.fn(async () => []), GetProposal: vi.fn(async (id: string) => pv(id, true)), AcceptProposal: accept })
    const p = useProposals()
    await p.select('a')
    expect(await p.accept()).toBe(false)
    expect(p.stale.value).toContain('changed since this was proposed')
    expect(p.error.value).toBe('')
    expect(p.preview.value?.stale).toBe(true)
    expect(await p.accept(true)).toBe(true)
    expect(accept).toHaveBeenLastCalledWith('a', 'pin-a', true)
    expect(p.stale.value).toBe('')
  })

  it('reject and failures report without writing', async () => {
    const app: MockApp = {
      ListProposals: vi.fn(async () => []),
      GetProposal: vi.fn(async (id: string) => pv(id)),
      RejectProposal: vi.fn().mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce(undefined),
    }
    install(app)
    const p = useProposals()
    await p.select('a')
    expect(await p.reject()).toBe(false)
    expect(p.error.value).toBe('boom')
    expect(await p.reject()).toBe(true)
    expect(p.preview.value).toBeNull()
  })
})
