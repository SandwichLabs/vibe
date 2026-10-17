// useArtifactRuns — the runs behind a data artifact (#162): shared, dependency
// aware, stale-safe, and polite to the one query pool.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { reactive, effectScope, nextTick } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { useArtifactRuns } from './useArtifactRuns'
import { runLiveQuery, _resetQueryPoolForTests } from '../a2ui/query'

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const ok = (n: unknown): A2UIQueryResult => ({ columns: ['n'], types: ['INTEGER'], rows: [[n]] })

const queries = [
  { name: 'q4', uses: ['top'] },
  { name: 'q5', uses: ['top'] },
  { name: 'by_region', uses: ['from', 'to'] },
  { name: 'customers', uses: ['region'] },
  { name: 'regions', uses: [] as string[] },
]

function setup(run: (n: string, q: string, v: Record<string, unknown>) => Promise<A2UIQueryResult>, extra: { sessionOk?: () => boolean } = {}) {
  const values = reactive<Record<string, any>>({ from: '2026-01-01', to: '2026-03-01', region: '', top: 1 })
  const scope = effectScope()
  const runs = scope.run(() => useArtifactRuns({ name: () => 'sales', queries: () => queries, values: () => values, run, ...extra }))!
  return { values, runs, scope }
}

describe('useArtifactRuns rebind', () => {
  beforeEach(() => { _resetQueryPoolForTests() })

  it('after a new revision, only what the page asks for again keeps running', async () => {
    const run = vi.fn(async () => ok(1))
    const values = reactive<Record<string, any>>({ region: '' })
    const rev = reactive({ v: 'r1' })
    const scope = effectScope()
    const runs = scope.run(() => useArtifactRuns({
      name: () => 'sales', queries: () => [{ name: 'a', uses: ['region'] }, { name: 'b', uses: ['region'] }],
      values: () => values, rev: () => rev.v, run,
    }))!
    runs.demand('a')
    await flushPromises()
    rev.v = 'r2' // the view now binds b instead of a
    await nextTick()
    runs.demand('b')
    await flushPromises()
    run.mockClear()
    values.region = 'West'
    await nextTick(); await flushPromises()
    expect(run.mock.calls.map((c: any[]) => c[1])).toEqual(['b'])
  })
})

describe('useArtifactRuns', () => {
  beforeEach(() => { _resetQueryPoolForTests() })

  it('runs a query once for two components bound to it, sending only the values it uses', async () => {
    const run = vi.fn(async () => ok(1))
    const { runs } = setup(run)
    runs.demand('by_region')
    runs.demand('by_region')
    const a = runs.result('by_region')
    const b = runs.result('by_region')
    await flushPromises()
    expect(run).toHaveBeenCalledTimes(1)
    expect(run).toHaveBeenCalledWith('sales', 'by_region', { from: '2026-01-01', to: '2026-03-01' })
    expect(a).toBe(b)
    expect(a.data?.rows).toEqual([[1]])
    expect(a.fetched).toBe(true)
  })

  it('re-runs only the queries that use a changed parameter', async () => {
    const run = vi.fn(async () => ok(1))
    const { runs, values } = setup(run)
    runs.demand('by_region'); runs.demand('customers'); runs.demand('regions')
    await flushPromises()
    run.mockClear()
    values.region = 'West'
    await nextTick(); await flushPromises()
    expect(run).toHaveBeenCalledTimes(1)
    expect(run).toHaveBeenCalledWith('sales', 'customers', { region: 'West' })
    run.mockClear()
    values.to = '2026-04-01'
    await nextTick(); await flushPromises()
    expect(run).toHaveBeenCalledTimes(1)
    expect(run).toHaveBeenCalledWith('sales', 'by_region', { from: '2026-01-01', to: '2026-04-01' })
  })

  it('does not run a query nothing is bound to', async () => {
    const run = vi.fn(async () => ok(1))
    const { values } = setup(run)
    values.region = 'West'
    await nextTick(); await flushPromises()
    expect(run).not.toHaveBeenCalled()
  })

  it('drops a stale result after a fast second change and runs the newest value last', async () => {
    const gates = [deferred<A2UIQueryResult>(), deferred<A2UIQueryResult>(), deferred<A2UIQueryResult>()]
    let i = 0
    const run = vi.fn(() => gates[i++].promise)
    const { runs, values } = setup(run)
    runs.demand('customers') // call 0 in flight
    await flushPromises()
    values.region = 'East'; await nextTick() // call 0 aborted; the new run waits for it
    values.region = 'West'; await nextTick() // replaces the waiting run
    await flushPromises()
    expect(run).toHaveBeenCalledTimes(1) // one call in flight per query, never a pile
    gates[0].resolve(ok('stale'))
    await flushPromises()
    expect(run).toHaveBeenCalledTimes(2)
    expect(run).toHaveBeenLastCalledWith('sales', 'customers', { region: 'West' })
    expect(runs.result('customers').data).toBeNull() // the stale result was never applied
    expect(runs.result('customers').loading).toBe(true)
    gates[1].resolve(ok('fresh'))
    await flushPromises()
    expect(runs.result('customers').data?.rows).toEqual([['fresh']])
    expect(runs.result('customers').loading).toBe(false)
  })

  it('shows an error as text state and recovers on the next value', async () => {
    const run = vi.fn()
      .mockRejectedValueOnce(new Error('failed to run: Binder Error: nope'))
      .mockResolvedValueOnce(ok(2))
    const { runs, values } = setup(run)
    runs.demand('customers')
    await flushPromises()
    expect(runs.result('customers').error).toBe('Binder Error: nope')
    values.region = 'West'
    await nextTick(); await flushPromises()
    expect(runs.result('customers').error).toBe('')
    expect(runs.result('customers').data?.rows).toEqual([[2]])
  })

  it('flags a truncated result and refuses an unknown query without calling Go', async () => {
    const run = vi.fn(async () => ({ ...ok(1), truncated: true }))
    const { runs } = setup(run)
    runs.demand('regions'); runs.demand('nope')
    await flushPromises()
    expect(runs.truncated.value).toEqual(['regions'])
    expect(runs.result('nope').error).toContain('no query named')
    expect(run).toHaveBeenCalledTimes(1)
  })

  it('drops results after dispose and after the workspace changed', async () => {
    const gate = deferred<A2UIQueryResult>()
    const { runs, scope } = setup(() => gate.promise)
    runs.demand('regions')
    await flushPromises()
    runs.dispose(); scope.stop()
    gate.resolve(ok(1))
    await flushPromises()
    expect(runs.result('regions').data).toBeNull()

    let alive = true
    const gate2 = deferred<A2UIQueryResult>()
    const s2 = setup(() => gate2.promise, { sessionOk: () => alive })
    s2.runs.demand('regions')
    await flushPromises()
    alive = false
    gate2.resolve(ok(1))
    await flushPromises()
    expect(s2.runs.result('regions').data).toBeNull()
  })

  it('keeps the high lane free: a burst of parameter changes never delays a person\'s own Run', async () => {
    const hold: Array<() => void> = []
    const run = vi.fn(() => new Promise<A2UIQueryResult>((res) => { hold.push(() => res(ok(1))) }))
    const { runs, values } = setup(run)
    runs.demand('by_region'); runs.demand('customers'); runs.demand('regions'); runs.demand('q4'); runs.demand('q5')
    for (let k = 0; k < 30; k++) { values.region = `r${k}`; values.top = k; values.to = `2026-02-${String(k % 28 + 1).padStart(2, '0')}`; await nextTick() }
    await flushPromises()
    // Artifact runs hold at most the low-lane share (3 of 4 slots), one per query.
    expect(run.mock.calls.length).toBeLessThanOrEqual(3)
    const user = vi.fn(async () => ({ columns: ['x'], types: ['INTEGER'], rows: [[1]] }))
    ;(window as any).go = { main: { App: { RunA2UIQuery: user } } }
    const mine = runLiveQuery('SELECT 1', undefined, 'high')
    await flushPromises()
    expect(user).toHaveBeenCalledTimes(1) // started at once, behind nothing
    await mine
    runs.dispose()
    hold.forEach((f) => f())
  })
})
