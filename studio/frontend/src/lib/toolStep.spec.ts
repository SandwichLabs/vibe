import { describe, it, expect } from 'vitest'
import { formatToolInput } from './toolStep'

describe('formatToolInput', () => {
  it('returns empty fields for no input', () => {
    expect(formatToolInput(undefined)).toEqual({ pretty: '', preview: '' })
    expect(formatToolInput('')).toEqual({ pretty: '', preview: '' })
  })

  it('pretty-prints JSON input and extracts a preview', () => {
    const { pretty, preview, sql } = formatToolInput('{"table":"orders"}')
    expect(pretty).toBe(JSON.stringify({ table: 'orders' }, null, 2))
    expect(preview).toBe('{"table":"orders"}')
    expect(sql).toBeUndefined()
  })

  it('extracts the sql field for the preview and Copy SQL button', () => {
    const { sql, preview, pretty } = formatToolInput('{"sql":"SELECT  1\\n  FROM t"}')
    expect(sql).toBe('SELECT  1\n  FROM t')
    expect(preview).toBe('SELECT 1 FROM t')
    expect(pretty).toContain('"sql"')
  })

  it('falls back to the raw string when input is not valid JSON', () => {
    const { pretty, preview, sql } = formatToolInput('not json at all')
    expect(pretty).toBe('not json at all')
    expect(preview).toBe('not json at all')
    expect(sql).toBeUndefined()
  })
})
