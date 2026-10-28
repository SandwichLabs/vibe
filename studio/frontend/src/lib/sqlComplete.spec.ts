import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { EditorState } from '@codemirror/state'
import { CompletionContext, completionKeymap } from '@codemirror/autocomplete'
import { kindType, sqlSource, toCompletions, keywordFallback } from './sqlComplete'

// "|" marks the cursor.
function ctxAt(src: string, explicit = false, aborted = false) {
  const pos = src.indexOf('|')
  const doc = src.replace('|', '')
  const state = EditorState.create({ doc })
  const ctx = new CompletionContext(state, pos, explicit)
  if (aborted) Object.defineProperty(ctx, 'aborted', { get: () => true })
  return { ctx, doc, pos }
}

function setApp(app: Record<string, unknown> | null) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = app ? { main: { App: app } } : undefined
}

describe('sqlSource', () => {
  let conn = ''
  const source = sqlSource({ connection: () => conn })
  beforeEach(() => { conn = '' })
  afterEach(() => setApp(null))

  it('sends the doc, the UTF-16 cursor and the connection getter\'s current value', async () => {
    const CompleteSQL = vi.fn(async () => ({ from: 14, items: [] }))
    setApp({ CompleteSQL })
    const { ctx, doc, pos } = ctxAt('SELECT * FROM o|')
    conn = 'shop'
    await source(ctx)
    expect(CompleteSQL).toHaveBeenCalledWith({ sql: doc, offset: pos, connection: 'shop' })
    conn = 'other'
    await source(ctxAt('SELECT * FROM o|').ctx)
    expect(CompleteSQL).toHaveBeenLastCalledWith(expect.objectContaining({ connection: 'other' }))
  })

  it('maps kind to type, insert to apply, and keeps detail and from', async () => {
    setApp({
      CompleteSQL: vi.fn(async () => ({
        from: 14,
        items: [
          { label: 'orders', kind: 'table', detail: 'table in shop.main (2 columns)' },
          { label: 'Order Lines', kind: 'view', insert: '"Order Lines"' },
          { label: 'total', kind: 'column', detail: 'DECIMAL(10,2)' },
        ],
      })),
    })
    const res = await source(ctxAt('SELECT * FROM o|').ctx)
    expect(res?.from).toBe(14)
    expect(res?.options.map((o) => [o.label, o.type, o.apply, o.detail])).toEqual([
      ['orders', 'class', 'orders', 'table in shop.main (2 columns)'],
      ['Order Lines', 'interface', '"Order Lines"', undefined],
      ['total', 'property', 'total', 'DECIMAL(10,2)'],
    ])
    expect(res?.validFor).toBeInstanceOf(RegExp)
    const [orders, , total] = res!.options
    expect(total.boost).toBeGreaterThan(orders.boost!)
  })

  it('asks Go again, instead of filtering locally, for a quoted word', async () => {
    setApp({ CompleteSQL: vi.fn(async () => ({ from: 14, items: [{ label: 'Order Lines', kind: 'view', insert: '"Order Lines"' }] })) })
    const res = await source(ctxAt('SELECT * FROM "Ord|').ctx)
    expect(res?.filter).toBe(false)
    expect(res?.validFor).toBeUndefined()
  })

  it('does not filter locally when Go cut the list at its cap (incomplete)', async () => {
    setApp({ CompleteSQL: vi.fn(async () => ({ from: 14, incomplete: true, items: [{ label: 'sa_1', kind: 'table' }] })) })
    const res = await source(ctxAt('SELECT * FROM s|').ctx)
    expect(res?.filter).toBe(false)
    expect(res?.validFor).toBeUndefined()

    setApp({ CompleteSQL: vi.fn(async () => ({ from: 14, items: [{ label: 'sa_1', kind: 'table' }] })) })
    const full = await source(ctxAt('SELECT * FROM s|').ctx)
    expect(full?.validFor).toBeInstanceOf(RegExp)
  })

  it('drops the answer when the request was superseded (aborted)', async () => {
    setApp({ CompleteSQL: vi.fn(async () => ({ from: 0, items: [{ label: 'x', kind: 'table' }] })) })
    expect(await source(ctxAt('SELECT * FROM o|', false, true).ctx)).toBeNull()
  })

  it('falls back to keywords with no binding and when the binding rejects', async () => {
    setApp(null)
    const none = await source(ctxAt('SEL|').ctx)
    expect(none?.options.map((o) => o.label)).toContain('SELECT')

    setApp({ CompleteSQL: vi.fn(async () => { throw new Error('no workspace open') }) })
    const failed = await source(ctxAt('SEL|').ctx)
    expect(failed?.options.map((o) => o.label)).toContain('SELECT')
  })

  it('stays quiet on a bare space unless explicit, and answers after a dot', async () => {
    const CompleteSQL = vi.fn(async () => ({ from: 11, items: [{ label: 'id', kind: 'column' }] }))
    setApp({ CompleteSQL })
    expect(await source(ctxAt('SELECT * FROM |').ctx)).toBeNull()
    expect(CompleteSQL).not.toHaveBeenCalled()
    expect(await source(ctxAt('SELECT * FROM |', true).ctx)).not.toBeNull()
    expect(await source(ctxAt('SELECT o.|').ctx)).not.toBeNull()
  })
})

describe('keymap and mapping', () => {
  it('the completion keymap does not claim Ctrl/Cmd+Enter, so running keeps priority', () => {
    const keys = completionKeymap.flatMap((k) => [k.key, k.mac, k.win, k.linux]).filter(Boolean) as string[]
    expect(keys.some((k) => /(Mod|Ctrl|Cmd|Meta)-Enter/i.test(k))).toBe(false)
    expect(keys).toContain('Enter')
  })

  it('maps every kind the Go completer emits', () => {
    for (const k of ['keyword', 'connection', 'schema', 'table', 'view', 'column', 'function', 'alias'] as const) {
      expect(kindType[k]).toBeTruthy()
      expect(toCompletions([{ label: 'a', kind: k }])[0].type).toBe(kindType[k])
    }
  })

  it('keywordFallback prefix-matches and is null on a bare space', () => {
    expect(keywordFallback(ctxAt('SELECT |').ctx)).toBeNull()
    expect(keywordFallback(ctxAt('wh|').ctx)?.options.map((o) => o.label)).toContain('WHERE')
  })
})
