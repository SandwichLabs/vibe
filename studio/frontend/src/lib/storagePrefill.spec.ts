import { describe, it, expect } from 'vitest'
import { parseStoragePrefill, prefixProblem } from './storagePrefill'

describe('parseStoragePrefill', () => {
  it('accepts a storage type with its own scheme, and defaults to the local scope', () => {
    expect(parseStoragePrefill({ type: 's3', prefix: 's3://lake/wh/', name: 'lake_s3', in: 'user' })).toEqual({ type: 's3', prefix: 's3://lake/wh/', name: 'lake_s3', scope: 'user' })
    expect(parseStoragePrefill({ type: 'gcs', prefix: 'gs://b/' })).toEqual({ type: 'gcs', prefix: 'gs://b/', name: '', scope: 'local' })
    expect(parseStoragePrefill({ type: 'azure', prefix: 'abfss://c@acct.dfs.core.windows.net/x/' })).not.toBeNull()
  })
  it('is null with nothing to prefill', () => {
    expect(parseStoragePrefill({})).toBeNull()
    expect(parseStoragePrefill({ resume: '1', return: 'lake-draft' })).toBeNull()
  })
  it.each([
    [{ type: 'postgres', prefix: 's3://b/' }],
    [{ type: 's3', prefix: 'gs://b/' }],
    [{ type: 'gcs', prefix: 'gcs://b/', in: 'shared' }],
    [{ type: 's3', prefix: 's3://b/', name: '1bad' }],
    [{ type: 's3', prefix: ['s3://b/', 's3://c/'] }],
    [{ type: 's3', prefix: 's3://b/', name: ['a', 'b'] }],
    [{ type: 's3' }],
  ])('drops %j whole', (q) => { expect(parseStoragePrefill(q as Record<string, unknown>)).toBeNull() })
})

describe('prefixProblem', () => {
  it.each(['s3://', 's3:///x', 's3://b/../x', 's3://b/%2E%2e/x', 's3://b/a b', 's3://b/a?x', 's3://b/*', 's3://b/[a]', 's3://b/\nx', 's3://b/#f', 's3://b/./x', 's3://user:pw@b/x'])('refuses %j', (p) => {
    expect(prefixProblem('s3', p)).not.toBe('')
  })
  it('accepts a bucket and a folder', () => {
    expect(prefixProblem('s3', 's3://b')).toBe('')
    expect(prefixProblem('s3', 's3a://b/a/b/')).toBe('')
  })
})
