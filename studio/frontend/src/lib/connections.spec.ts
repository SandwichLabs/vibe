import { describe, it, expect } from 'vitest'
import { detailLine, statusKey, statusLabel, oneLine, selectFrom, scopeLabel, typeCode, NAME_RE, STATUS_META } from './connections'

const base = { name: 'prod_pg', type: 'postgres', scope: 'local', readOnly: true, default: false, remote: true } as const

describe('connection display helpers (#117)', () => {
  it('maps the session statuses to six distinct shapes', () => {
    const keys = ['ok', 'not-connected', 'error', 'needs-credentials', 'extension-missing', 'locked-by'].map(statusKey)
    expect(new Set(keys).size).toBe(6)
    expect(new Set(keys.map((k) => STATUS_META[k].icon)).size).toBe(6)
  })

  it('says Team, This computer and All my workspaces, never the file-level words', () => {
    expect(scopeLabel('shared')).toBe('Team')
    expect(scopeLabel('local')).toBe('This computer')
    expect(scopeLabel('user')).toBe('All my workspaces')
  })

  it('shows a detail line only when the person can act (SPEC 05 copy)', () => {
    expect(detailLine({ ...base, status: 'ok' })).toBeNull()
    expect(detailLine({ ...base, status: 'not-connected' })).toBeNull()
    expect(detailLine({ ...base, status: 'error', detail: "failed to attach: password was rejected. extra" })!)
      .toMatchObject({ action: 'fix', actionLabel: 'Fix', text: "Can’t connect: failed to attach: password was rejected." })
    expect(detailLine({ ...base, type: 'motherduck', status: 'needs-credentials' })!)
      .toMatchObject({ text: 'Needs a MotherDuck token.', actionLabel: 'Add it' })
    expect(detailLine({ ...base, status: 'needs-credentials', serverChanged: { old: 'db.internal', new: 'db-new.internal' } })!)
      .toMatchObject({ text: 'The server changed since you signed in (db.internal to db-new.internal).', actionLabel: 'Sign in again', action: 'signin' })
    expect(detailLine({ ...base, type: 's3', status: 'extension-missing', extension: 'httpfs' })!)
      .toMatchObject({ text: 'Needs the httpfs extension.', action: 'install' })
    expect(detailLine({ ...base, status: 'locked-by', lockedByPid: 4182 })!)
      .toMatchObject({ text: 'In use by another program (pid 4182).', action: 'takeover', actionLabel: 'Take over' })
  })

  it('keeps an error to one short line', () => {
    expect(oneLine('a'.repeat(300)).length).toBeLessThanOrEqual(90)
    expect(oneLine('')).toBe('something went wrong')
    expect(oneLine('first. second')).toBe('first')
  })

  it('names a status for screen readers', () => {
    expect(statusLabel({ ...base, status: 'needs-credentials' })).toBe('Needs credentials')
  })

  it('builds the starter query with every part quoted', () => {
    expect(selectFrom('prod_pg.public.orders')).toBe('SELECT * FROM "prod_pg"."public"."orders" LIMIT 100')
    expect(selectFrom('a.b."c d"')).toContain('"')
  })

  it('knows the type tiles and the name rule', () => {
    expect(typeCode('postgres')).toBe('PG')
    for (const [t, c] of [['mysql', 'MY'], ['azure', 'AZ'], ['ducklake', 'DL'], ['iceberg', 'IC'], ['delta', 'DT']]) expect(typeCode(t)).toBe(c)
    expect(NAME_RE.test('prod_pg')).toBe(true)
    expect(NAME_RE.test('Prod-PG')).toBe(false)
  })
})
