// useStorageDetect: 300 ms debounce, only values with "://", stale answers dropped.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { reactive, ref, nextTick, effectScope, type EffectScope } from 'vue'
import { useStorageDetect, DETECT_DELAY_MS } from './useStorageDetect'

const need = (url: string, over: Partial<StorageNeedView> = {}): StorageNeedView => ({ url, ...over })

const scopes: EffectScope[] = []

function setup(typeId = 'ducklake') {
  const calls: string[][] = []
  const resolvers: Array<(v: StorageNeedsView) => void> = []
  ;(window as any).go = { main: { App: {
    FindStorageFor: vi.fn((req: FindStorageRequest) => { calls.push(req.urls); return new Promise<StorageNeedsView>((res) => resolvers.push(res)) }),
  } } }
  const form = reactive({ path: '', options: {} as Record<string, string> })
  const t = ref(typeId)
  const scope = effectScope()
  const d = scope.run(() => useStorageDetect(t, form))!
  scopes.push(scope)
  return { form, d, calls, resolvers, t }
}

describe('useStorageDetect', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers(); scopes.splice(0).forEach((s) => s.stop()) })

  it('waits 300 ms of quiet, and asks once for the last value', async () => {
    const { form, calls } = setup()
    form.path = 's3://l'
    await nextTick()
    vi.advanceTimersByTime(200)
    form.path = 's3://lake/c.sqlite'
    await nextTick()
    vi.advanceTimersByTime(DETECT_DELAY_MS - 1)
    expect(calls).toHaveLength(0)
    vi.advanceTimersByTime(1)
    expect(calls).toEqual([['s3://lake/c.sqlite']])
  })

  it('sends only values with ://, and nothing for other types', async () => {
    const { form, calls, t } = setup()
    form.path = 'data/wh.ducklake'
    form.options['data_path'] = 'data/files'
    await nextTick()
    vi.advanceTimersByTime(1000)
    expect(calls).toHaveLength(0)

    form.options['data_path'] = ' s3://lake/data/ '
    await nextTick()
    vi.advanceTimersByTime(DETECT_DELAY_MS)
    expect(calls).toEqual([['s3://lake/data/']])

    t.value = 'postgres'
    await nextTick()
    form.path = 's3://other/x'
    await nextTick()
    vi.advanceTimersByTime(1000)
    expect(calls).toHaveLength(1)
  })

  it('drops a response that arrives after the text changed', async () => {
    const { form, d, calls, resolvers } = setup()
    form.path = 's3://a/x'
    await nextTick()
    vi.advanceTimersByTime(DETECT_DELAY_MS)
    form.path = 's3://b/x'
    await nextTick()
    vi.advanceTimersByTime(DETECT_DELAY_MS)
    expect(calls).toEqual([['s3://a/x'], ['s3://b/x']])
    resolvers[1]({ needs: [need('s3://b/x', { match: { name: 'bee', type: 's3', scope: 's3://b/', in: 'local', status: 'ok' } })] })
    await vi.runAllTicks()
    await Promise.resolve()
    resolvers[0]({ needs: [need('s3://a/x', { problem: 'old' })] }) // late
    await Promise.resolve()
    await Promise.resolve()
    expect(d.needFor('path')?.match?.name).toBe('bee')
    expect(d.needs.value).toHaveLength(1)
  })

  it('shows nothing for a field whose text no longer matches the answer', async () => {
    const { form, d, resolvers } = setup()
    form.path = 's3://a/x'
    await nextTick()
    vi.advanceTimersByTime(DETECT_DELAY_MS)
    resolvers[0]({ needs: [need('s3://a/x', { problem: 'p' })] })
    await Promise.resolve()
    await Promise.resolve()
    expect(d.needFor('path')?.problem).toBe('p')
    form.path = 's3://a/xy'
    await nextTick()
    expect(d.needFor('path')).toBeNull()
  })

  it('clears at once when the text no longer has a URL', async () => {
    const { form, d, resolvers } = setup()
    form.path = 's3://a/x'
    await nextTick()
    vi.advanceTimersByTime(DETECT_DELAY_MS)
    resolvers[0]({ needs: [need('s3://a/x')] })
    await Promise.resolve()
    await Promise.resolve()
    form.path = ''
    await nextTick()
    expect(d.needs.value).toEqual([])
  })

  it('one line when both URLs are read by the same connection, two when by different ones', async () => {
    const { form, d, resolvers } = setup()
    const m = (name: string): StorageMatchView => ({ name, type: 's3', scope: 's3://a/', in: 'local', status: 'ok' })
    form.path = 's3://a/c.sqlite'
    form.options['data_path'] = 's3://a/d/'
    await nextTick()
    vi.advanceTimersByTime(DETECT_DELAY_MS)
    resolvers[0]({ needs: [need('s3://a/c.sqlite', { match: m('x') }), need('s3://a/d/', { match: m('x') })] })
    await Promise.resolve(); await Promise.resolve()
    expect(d.needFor('path')?.match?.name).toBe('x')
    expect(d.needFor('data_path')).toBeNull()
    d.needs.value = [need('s3://a/c.sqlite', { match: m('x') }), need('s3://a/d/', { match: m('y') })]
    expect(d.needFor('data_path')?.match?.name).toBe('y')
  })
})
