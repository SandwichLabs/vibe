import { describe, it, expect, vi } from 'vitest'
import { effectScope, nextTick, reactive, ref } from 'vue'
import { pageQuery, usePageFrame } from './usePageFrame'
import { FRAME_POLICY_TEXT } from '../lib/framePolicy'

const detail = {
  name: 'pg', title: 'Pg', description: '', queries: [], view_kind: 'html', rev: 'r', updated_at: '',
  defaults: { region: 'EU', n: 3, live: true, from: '2026-01-01' },
  params: [
    { name: 'region', type: 'enum', default: 'EU', options: ['EU', 'US'] },
    { name: 'n', type: 'integer', default: '3' },
    { name: 'live', type: 'boolean', default: 'true' },
    { name: 'from', type: 'date', default: '2026-01-01' },
  ],
} as unknown as ArtifactDetail

describe('pageQuery', () => {
  it('sends only values that differ from their defaults, encoded', () => {
    expect(pageQuery(detail, { region: 'EU', n: 3, live: true, from: '2026-01-01' })).toBe('')
    expect(pageQuery(detail, { region: 'US', n: 5, live: false, from: '2026-02-03' })).toBe('?region=US&n=5&live=false&from=2026-02-03')
    expect(pageQuery(detail, { region: 'a&b=c d' })).toBe('?region=a%26b%3Dc%20d')
  })
  it('names only declared parameters', () => {
    expect(pageQuery(detail, { nope: 'x', region: 'US' })).toBe('?region=US')
  })
})

function setup(over: { frameOk?: boolean; open?: (n: string, o: string) => Promise<ArtifactPageGrant> } = {}) {
  const values = reactive<Record<string, string | number | boolean>>({ region: 'EU', n: 3, live: true, from: '2026-01-01' })
  const refresh = ref(0)
  const open = over.open ?? vi.fn(async () => ({ token: 't'.repeat(64), hash: 'h' }))
  const scope = effectScope()
  const frame = scope.run(() => usePageFrame(() => 'pg', () => detail, () => values, () => refresh.value, {
    openPage: open, origin: () => 'wails://wails', frameOk: () => over.frameOk ?? true,
  }))!
  return { frame, values, refresh, open, scope }
}

describe('usePageFrame', () => {
  it('opens a token for its artifact and builds the route from the values', async () => {
    const { frame, values, open } = setup()
    await frame.reload()
    expect(open).toHaveBeenCalledWith('pg', 'wails://wails')
    expect(frame.src.value).toBe(`/assay-view/${'t'.repeat(64)}/page`)
    values.region = 'US'
    await nextTick()
    expect(frame.src.value).toBe(`/assay-view/${'t'.repeat(64)}/page?region=US`)
    expect(open).toHaveBeenCalledTimes(1)
  })

  it('opens nothing when the window has no frame policy for its origin', async () => {
    const { frame, open } = setup({ frameOk: false })
    await frame.reload()
    expect(open).not.toHaveBeenCalled()
    expect(frame.failure.value).toBe(FRAME_POLICY_TEXT)
    expect(frame.src.value).toBe('')
  })

  it('shows the fixed refusal when Go will not open the page', async () => {
    const { frame } = setup({ open: async () => { throw new Error('this view is not allowed to run') } })
    await frame.reload()
    expect(frame.failure.value).toBe('this view is not allowed to run')
    expect(frame.src.value).toBe('')
  })

  it('a refresh asks for a new token so the frame loads again', async () => {
    let n = 0
    const { frame, refresh, open } = setup({ open: vi.fn(async () => ({ token: String(++n).repeat(64), hash: 'h' })) })
    await frame.reload()
    const first = frame.src.value
    refresh.value++
    await nextTick(); await nextTick()
    expect(open).toHaveBeenCalledTimes(2)
    expect(frame.src.value).not.toBe(first)
  })

  it('a late answer for an older open is dropped', async () => {
    const resolvers: Array<(g: ArtifactPageGrant) => void> = []
    const { frame } = setup({ open: () => new Promise((r) => { resolvers.push(r) }) })
    const a = frame.reload()
    const b = frame.reload()
    resolvers[1]({ token: 'b'.repeat(64), hash: 'h' })
    await b
    resolvers[0]({ token: 'a'.repeat(64), hash: 'h' })
    await a
    expect(frame.src.value).toContain('b'.repeat(64))
  })
})
