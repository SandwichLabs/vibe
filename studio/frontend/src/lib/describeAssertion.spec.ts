import { describe, expect, it } from 'vitest'
import { describeAssertion } from './describeAssertion'

describe('describeAssertion', () => {
  it('keeps the old shapes', () => {
    expect(describeAssertion({ kind: 'non_negative' })).toBe('non_negative')
    expect(describeAssertion({ kind: 'accepted_values', column: 'status', values: ['a', 'b'] })).toBe('accepted_values on status (a, b)')
  })
  it('shows range bounds, the freshness window and minimum rows', () => {
    expect(describeAssertion({ kind: 'range', column: 'score', min: 0, max: 100 })).toBe('range on score (min 0, max 100)')
    expect(describeAssertion({ kind: 'range', max: 99.5 })).toBe('range (max 99.5)')
    expect(describeAssertion({ kind: 'freshness', column: 'log_date', max_age: '7d' })).toBe('freshness on log_date (max age 7d)')
    expect(describeAssertion({ kind: 'row_count', min_rows: 1 })).toBe('row_count (min 1 rows)')
  })
})
