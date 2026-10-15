import { describe, it, expect } from 'vitest'
import { presetFill, endpointProblem } from './storagePresets'

const P = (id: string, options: Record<string, string>, regionHint?: string): ConnPreset => ({ id, label: id, options, regionHint })
const MINIO = P('minio', { endpoint: 'localhost:9000', url_style: 'path', use_ssl: 'false' })
const B2 = P('b2', { endpoint: 's3.<region>.backblazeb2.com', url_style: 'vhost', use_ssl: 'true' }, 'us-west-004')

describe('presetFill', () => {
  it('fills the three options and nothing else', () => {
    const r = presetFill(MINIO, { sslmode: 'x' }, 'eu-west-1')
    expect(r.options).toEqual({ sslmode: 'x', endpoint: 'localhost:9000', url_style: 'path', use_ssl: 'false' })
    expect(r.region).toBe('eu-west-1')
  })
  it('Amazon S3 clears what another store set; Other clears nothing', () => {
    const had = { endpoint: 'localhost:9000', url_style: 'path', use_ssl: 'false' }
    expect(presetFill(P('aws', {}), had, 'us-east-1').options).toEqual({})
    expect(presetFill(P('other', {}), had, 'us-east-1').options).toEqual(had)
  })
  it('fills <region> from the Region field', () => {
    const r = presetFill(B2, {}, 'us-west-002')
    expect(r.options['endpoint']).toBe('s3.us-west-002.backblazeb2.com')
    expect(r.region).toBe('us-west-002')
  })
  it('takes the hint when the region was never chosen', () => {
    expect(presetFill(B2, {}, 'us-east-1').region).toBe('us-west-004')
    expect(presetFill(B2, {}, '').options['endpoint']).toBe('s3.us-west-004.backblazeb2.com')
  })
  it('never lets the region inject into the endpoint', () => {
    const r = presetFill(B2, {}, 'a/evil.example#')
    expect(r.options['endpoint']).toBe('s3.<region>.backblazeb2.com')
    expect(endpointProblem(r.options['endpoint'])).toContain('Replace <region>')
  })
})

describe('endpointProblem', () => {
  it('is empty for a host and for nothing', () => {
    expect(endpointProblem('')).toBe('')
    expect(endpointProblem('localhost:4566')).toBe('')
  })
})
