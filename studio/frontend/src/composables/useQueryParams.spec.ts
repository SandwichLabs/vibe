import { describe, it, expect } from 'vitest'
import { useQueryParams } from './useQueryParams'

const decls: QueryParamT[] = [
  { name: 'min_n', type: 'integer', required: false, default: '5' },
  { name: 'label', type: 'text', required: false, default: 'x' },
  { name: 'since', type: 'date', required: false, default: 'today-30d' },
  { name: 'flag', type: 'boolean', required: false, default: 'true' },
  { name: 'rate', type: 'number', required: true },
]

describe('useQueryParams', () => {
  it('pre-fills the defaults; a relative date stays blank for Go to resolve', () => {
    const qp = useQueryParams()
    qp.load(decls)
    expect(qp.raw).toMatchObject({ min_n: '5', label: 'x', since: '', flag: true, rate: '' })
    expect(qp.isDefault('min_n')).toBe(true)
  })

  it('sends typed values and leaves a blank non-text value out', () => {
    const qp = useQueryParams()
    qp.load(decls.slice(0, 4))
    qp.set('min_n', '9')
    qp.set('label', "a'; DROP TABLE t")
    qp.set('flag', false)
    const p = qp.payload()
    expect(p).toEqual({ ok: true, params: { min_n: 9, label: "a'; DROP TABLE t", flag: false } })
    expect(qp.isDefault('min_n')).toBe(false)
    qp.reset('min_n')
    expect(qp.raw.min_n).toBe('5')
  })

  it('refuses a bad value beside its input and sends nothing', () => {
    const qp = useQueryParams()
    qp.load(decls)
    qp.set('min_n', 'nine')
    expect(qp.shownErrors.min_n).toBe('Enter a whole number.')
    expect(qp.payload()).toEqual({ ok: false, error: 'Parameter min_n: Enter a whole number.' })
    qp.set('min_n', '5')
    qp.set('rate', '')
    expect(qp.payload()).toEqual({ ok: false, error: 'Parameter rate: A value is required.' })
    qp.set('rate', '1.5')
    qp.set('since', '2026-02-30')
    expect(qp.payload().ok).toBe(false)
  })

  it('treats an empty text value with no default as missing, never a silent empty string', () => {
    const qp = useQueryParams()
    qp.load([{ name: 'who', type: 'text', required: true }])
    expect(qp.payload()).toEqual({ ok: false, error: 'Parameter who: A value is required.' })
    qp.set('who', 'ann')
    expect(qp.payload()).toEqual({ ok: true, params: { who: 'ann' } })
  })

  it('clear drops the declarations', () => {
    const qp = useQueryParams()
    qp.load(decls)
    qp.clear()
    expect(qp.hasParams.value).toBe(false)
    expect(qp.payload()).toEqual({ ok: true, params: {} })
  })
})
