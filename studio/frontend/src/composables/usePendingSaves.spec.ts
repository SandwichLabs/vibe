import { describe, it, expect, vi, beforeEach } from 'vitest'
import { register, flushAll, _resetForTests } from './usePendingSaves'

describe('usePendingSaves', () => {
  beforeEach(() => _resetForTests())

  it('awaits every registered flush', async () => {
    const calls: string[] = []
    register(() => 'a', async () => { calls.push('a') })
    register(() => 'b', async () => { calls.push('b') })
    await flushAll()
    expect(calls.sort()).toEqual(['a', 'b'])
  })

  it('unregister removes a flush so flushAll no longer calls it', async () => {
    const fn = vi.fn(async () => {})
    const unregister = register(() => 'x', fn)
    unregister()
    await flushAll()
    expect(fn).not.toHaveBeenCalled()
  })

  it('one failing flush does not block the others from being attempted', async () => {
    const calls: string[] = []
    register(() => 'failing', async () => { throw new Error('boom') })
    register(() => 'ok', async () => { calls.push('ok') })
    await flushAll()
    expect(calls).toEqual(['ok'])
  })

  it('reports each failure by label instead of swallowing it (round 3 review fix)', async () => {
    register(() => 'Notebook A', async () => { throw new Error('disk full') })
    register(() => 'Notebook B', async () => {})
    const { failed } = await flushAll()
    expect(failed).toEqual([{ label: 'Notebook A', error: 'disk full' }])
  })

  it('evaluates the label lazily, at failure time, not at registration time', async () => {
    let title = 'Untitled notebook'
    register(() => title, async () => { throw new Error('nope') })
    title = 'Renamed before the flush failed'
    const { failed } = await flushAll()
    expect(failed[0].label).toBe('Renamed before the flush failed')
  })

  it('flushAll with nothing registered resolves with no failures', async () => {
    await expect(flushAll()).resolves.toEqual({ failed: [] })
  })

  it('flushAll with everything succeeding resolves with no failures', async () => {
    register(() => 'ok', async () => {})
    await expect(flushAll()).resolves.toEqual({ failed: [] })
  })

  it('calling flushAll waits for slow flushes to actually complete', async () => {
    let resolved = false
    register(() => 'slow', () => new Promise<void>((resolve) => setTimeout(() => { resolved = true; resolve() }, 10)))
    await flushAll()
    expect(resolved).toBe(true)
  })
})
