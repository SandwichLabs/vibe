// useDefinitionRuns (E4): the three runs, a failed run reporting its error, and a
// profile refused because the join file changed (a conflict, not an error).
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, expect, it, vi } from 'vitest'
import { useDefinitionRuns } from './useDefinitionRuns'

const row = (status: AssertionRow['status']): AssertionRow => ({
  subject_kind: 'entity', subject: 'customer', assertion: 'unique', column: 'customer_id', status, violations: status === 'fail' ? 2 : 0, duration_ms: 1,
})

function install(app: MockApp) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
}

describe('useDefinitionRuns', () => {
  it('runs assertions and counts failures and errors', async () => {
    const run = vi.fn(async () => [row('pass'), row('fail'), row('error')])
    install({ RunAssertions: run })
    const r = useDefinitionRuns()
    await r.runAssertions('entity', 'customer')
    expect(run).toHaveBeenCalledWith('entity', 'customer')
    expect(r.results.value).toHaveLength(3)
    expect(r.failed.value).toBe(1)
    expect(r.errored.value).toBe(1)
    expect(r.busy.value).toBe(false)
  })

  it('tries a metric and profiles a join', async () => {
    const tryIt = vi.fn(async () => ({ columns: [], rows: [[1]], duration: '1 ms', row_count: 1, total_rows: 1 }))
    const prof = vi.fn(async () => ({ join: 'j', declared: 'many_to_one', measured: { verdict: 'fan_out' }, left_max_dup: 1, warnings: ['w'], rev: 'r' }))
    install({ RunMetric: tryIt, ProfileJoin: prof })
    const r = useDefinitionRuns()
    await r.tryMetric('net_revenue', ['region'])
    expect(tryIt).toHaveBeenCalledWith('net_revenue', ['region'])
    expect(r.tried.value?.row_count).toBe(1)
    await r.profileJoin('j')
    expect(r.profile.value?.warnings).toEqual(['w'])
  })

  it('reports a failed run, and a changed join file as a conflict', async () => {
    install({ RunAssertions: vi.fn(async () => { throw new Error('connection prod is down') }), ProfileJoin: vi.fn(async () => { throw new Error('conflict: the file changed') }) })
    const r = useDefinitionRuns()
    await r.runAssertions()
    expect(r.error.value).toContain('prod is down')
    expect(r.results.value).toEqual([])
    await r.profileJoin('j')
    expect(r.profile.value).toBeNull()
    expect(r.error.value).toBe('')
    expect(r.conflict.value).toContain('the file changed')
  })
})
