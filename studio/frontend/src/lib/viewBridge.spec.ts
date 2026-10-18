import { describe, it, expect } from 'vitest'
import { checkMessage, checkParams, RateWindow, viewTheme, RUN_ERROR_TEXT } from './viewBridge'

describe('checkMessage', () => {
  it('accepts exactly the message types a Worker may send', () => {
    expect(checkMessage({ t: 'hb' })).toEqual({ ok: true, msg: { t: 'hb' } })
    expect(checkMessage({ t: 'ready' }).ok).toBe(true)
    expect(checkMessage({ t: 'loading', file: 'vega' }).ok).toBe(true)
    expect(checkMessage({ t: 'loading' }).ok).toBe(false)
    expect(checkMessage({ t: 'loading', file: 'nope' }).ok).toBe(false)
    expect(checkMessage({ t: 'run', id: 1, query: 'orders', params: { region: 'EU', n: 3, on: true, none: null } }).ok).toBe(true)
    expect(checkMessage({ t: 'drawn', id: 2, svg: '<svg/>' }).ok).toBe(true)
    expect(checkMessage({ t: 'drawn', id: 2, a2ui: '{"title":"t","components":[]}' }).ok).toBe(true)
    expect(checkMessage({ t: 'drawn', id: 2, a2ui: { title: 't' } }).ok).toBe(false)
    expect(checkMessage({ t: 'drawFailed', id: 2, text: 'boom' })).toEqual({ ok: true, msg: { t: 'drawFailed', id: 2, text: 'boom' } })
  })

  it('ignores an unknown type, a non-object and a heartbeat with extras', () => {
    for (const bad of [null, 'run', 7, [], { t: 'eval', code: '1' }, { t: 5 }, {}, { t: 'hb', x: 1 }, { t: 'drawn', id: 1 }, { t: 'drawn', id: 1, svg: 5 }, { t: 'drawn', id: 1, svg: 'a', a2ui: {} }, { t: 'drawn', id: 0, svg: 'a' }]) {
      const v = checkMessage(bad)
      expect(v.ok, JSON.stringify(bad)).toBe(false)
    }
  })

  it('refuses a SQL-shaped run: SQL as the query name, or an extra sql / name / connection field', () => {
    const base = { t: 'run', id: 3, query: 'orders', params: {} }
    for (const bad of [
      { ...base, query: 'SELECT * FROM orders' },
      { ...base, query: 'a; DROP TABLE x' },
      { ...base, query: '1orders' },
      { ...base, query: 'x'.repeat(65) },
      { ...base, sql: 'SELECT 1' },
      { ...base, name: 'another-artifact' },
      { ...base, connection: 'prod' },
    ]) {
      const v = checkMessage(bad)
      expect(v, JSON.stringify(bad)).toEqual({ ok: false, code: 'bad', id: 3 })
    }
  })

  it('refuses oversized or non-flat params', () => {
    const run = (params: unknown) => checkMessage({ t: 'run', id: 1, query: 'q', params })
    expect(run({ a: 'x'.repeat(1001) }).ok).toBe(false)
    expect(run(Object.fromEntries(Array.from({ length: 33 }, (_, i) => [`p${i}`, 1]))).ok).toBe(false)
    expect(run(Object.fromEntries(Array.from({ length: 20 }, (_, i) => [`p${i}`, 'x'.repeat(900)]))).ok).toBe(false)
    expect(run({ a: { nested: 1 } }).ok).toBe(false)
    expect(run({ a: [1] }).ok).toBe(false)
    expect(run({ a: NaN }).ok).toBe(false)
    expect(run({ a: Infinity }).ok).toBe(false)
    expect(run({ 'bad name': 1 }).ok).toBe(false)
    expect(run({ a: () => 1 }).ok).toBe(false)
    expect(run([1, 2]).ok).toBe(false)
    expect(run('SELECT 1').ok).toBe(false)
    expect(run(undefined).ok).toBe(false)
  })

  it('a run without a usable id is ignored, not answered', () => {
    expect(checkMessage({ t: 'run', query: 'q', params: {} })).toEqual({ ok: false, code: 'ignore', id: undefined })
    expect(checkMessage({ t: 'run', id: -1, query: 'q', params: {} }).ok).toBe(false)
  })

  it('caps a failure text a view hands back', () => {
    const v = checkMessage({ t: 'loadFailed', text: 'x'.repeat(5000) })
    expect(v.ok && v.msg.t === 'loadFailed' && v.msg.text.length).toBe(300)
    const w = checkMessage({ t: 'loadFailed', text: { evil: 1 } })
    expect(w.ok && w.msg.t === 'loadFailed' && w.msg.text).toBe('The view failed.')
  })

  it('has fixed error texts that carry no detail', () => {
    for (const t of Object.values(RUN_ERROR_TEXT)) expect(t).toMatch(/^[A-Z][a-z A-Z.]+$/)
  })
})

describe('checkParams', () => {
  it('copies a plain object and refuses a prototype-carrying one', () => {
    expect(checkParams({ a: 1 })).toEqual({ a: 1 })
    class P { a = 1 }
    expect(checkParams(new P())).toBeNull()
    expect(checkParams(Object.create(null))).toEqual({})
  })
})

describe('RateWindow', () => {
  it('allows `limit` in the window, then refuses until the oldest ages out', () => {
    const w = new RateWindow(3, 1000)
    expect([0, 1, 2].map((t) => w.take(t))).toEqual([true, true, true])
    expect(w.take(500)).toBe(false)
    expect(w.take(1000)).toBe(true) // the call at t=0 has aged out
    expect(w.take(1001)).toBe(true)
    expect(w.take(1002)).toBe(true)
    expect(w.take(1003)).toBe(false)
  })
})

describe('viewTheme', () => {
  it('hands over hex colours only, as plain strings', () => {
    const t = viewTheme({ 'chart.1': '#1F7268', 'chart.2': 'url(javascript:1)', fg: '#111', bg: '#ffffff', accent: 'red' }, 'dark')
    expect(t.type).toBe('dark')
    expect(t['chart.1']).toBe('#1F7268')
    expect(t['chart.2']).toBeUndefined()
    expect(t.accent).toBeUndefined()
    expect(t.chart).toEqual(['#1F7268'])
    expect(t.fg).toBe('#111')
    expect(viewTheme({}, 'weird').type).toBe('light')
  })
})
