import { describe, it, expect, beforeEach } from 'vitest'
import { runPooled, _resetQueryPoolForTests } from './pool'

function gate() {
  let open!: () => void
  const p = new Promise<void>((r) => { open = r })
  return { p, open }
}

describe('runPooled', () => {
  beforeEach(() => { _resetQueryPoolForTests() })

  it('a run aborted while still queued leaves the queue and never starts', async () => {
    const g = gate()
    const started: string[] = []
    const hold = [0, 1, 2].map((i) => runPooled(async () => { started.push(`hold${i}`); await g.p }, undefined, 'low'))
    await Promise.resolve()
    const ctrl = new AbortController()
    const queued = runPooled(async () => { started.push('queued'); return 1 }, ctrl.signal, 'low')
    ctrl.abort()
    expect(await queued).toEqual({ aborted: true })
    g.open()
    await Promise.all(hold)
    expect(started).not.toContain('queued')
    // The aborted entry took no slot: the whole low lane is free again.
    const g2 = gate()
    const later = [0, 1, 2].map((i) => runPooled(async () => { started.push(`later${i}`); await g2.p }, undefined, 'low'))
    await Promise.resolve()
    expect(started.filter((x) => x.startsWith('later'))).toHaveLength(3)
    g2.open()
    await Promise.all(later)
  })

  it('a run aborted in flight reports aborted and still frees its slot', async () => {
    const g = gate()
    const ctrl = new AbortController()
    const running = runPooled(async () => { await g.p; return 'late' }, ctrl.signal, 'low')
    await Promise.resolve()
    ctrl.abort()
    g.open()
    expect(await running).toEqual({ aborted: true })
    expect(await runPooled(async () => 'next', undefined, 'low')).toEqual({ aborted: false, value: 'next' })
  })

  it('the low lane leaves a slot for high work', async () => {
    const g = gate()
    const low = [0, 1, 2, 3, 4].map(() => runPooled(() => g.p, undefined, 'low'))
    await Promise.resolve()
    let ran = false
    const high = runPooled(async () => { ran = true }, undefined, 'high')
    await high
    expect(ran).toBe(true)
    g.open()
    await Promise.all(low)
  })

  it('an error propagates after the slot is released', async () => {
    await expect(runPooled(async () => { throw new Error('boom') })).rejects.toThrow('boom')
    expect(await runPooled(async () => 1)).toEqual({ aborted: false, value: 1 })
  })
})
