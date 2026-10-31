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

  it('cannot be double-started, and reset forgets the output', async () => {
    let release!: (rows: AssertionRow[]) => void
    const run = vi.fn(() => new Promise<AssertionRow[]>((resolve) => { release = resolve }))
    install({ RunAssertions: run })
    const r = useDefinitionRuns()
    const first = r.runAssertions('metric', 'net_revenue')
    expect(r.busy.value).toBe(true)
    await r.runAssertions('metric', 'net_revenue')
    expect(run).toHaveBeenCalledTimes(1)
    release([row('pass')])
    await first
    expect(r.busy.value).toBe(false)
    expect(r.results.value).toHaveLength(1)
    r.reset()
    expect(r.results.value).toEqual([])
  })

  it('stops a run by its kind, and shows a stopped run as cancelled, not an error', async () => {
    let fail!: (e: Error) => void
    const run = vi.fn(() => new Promise<AssertionRow[]>((_, reject) => { fail = reject }))
    const cancel = vi.fn(async (_key: string) => { fail(new Error('cancelled')) })
    install({ RunAssertions: run, CancelRun: cancel })
    const r = useDefinitionRuns()
    r.stop() // nothing running: no call
    expect(cancel).not.toHaveBeenCalled()
    const p = r.runAssertions('entity', 'customer')
    r.stop()
    await p
    expect(cancel).toHaveBeenCalledWith('assertions')
    expect(r.cancelled.value).toBe(true)
    expect(r.error.value).toBe('')
    expect(r.busy.value).toBe(false)
    expect(r.results.value).toEqual([])
  })

  it('uses the metric and profile keys for Try it and Profile', async () => {
    const hold: Array<(e: Error) => void> = []
    const stuck = () => new Promise<never>((_, reject) => { hold.push(reject) })
    const cancel = vi.fn(async (_key: string) => { hold.pop()!(new Error('cancelled')) })
    install({ RunMetric: stuck, ProfileJoin: stuck, CancelRun: cancel })
    const r = useDefinitionRuns()
    const a = r.tryMetric('m', [])
    r.stop(); await a
    const b = r.profileJoin('j')
    r.stop(); await b
    expect(cancel.mock.calls.map((c) => c[0])).toEqual(['metric', 'profile'])
  })

  it('stops the run in flight when the view goes away', async () => {
    const { mount } = await import('@vue/test-utils')
    const { defineComponent, h } = await import('vue')
    let fail!: (e: Error) => void
    const cancel = vi.fn(async (_key: string) => { fail(new Error('cancelled')) })
    install({ RunMetric: () => new Promise((_, rej) => { fail = rej }), CancelRun: cancel })
    let runs!: ReturnType<typeof useDefinitionRuns>
    const w = mount(defineComponent({ setup() { runs = useDefinitionRuns(); return () => h('div') } }))
    const p = runs.tryMetric('m', [])
    w.unmount()
    await p
    expect(cancel).toHaveBeenCalledWith('metric')
  })
})
