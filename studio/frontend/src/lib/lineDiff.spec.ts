import { describe, expect, it } from 'vitest'
import { diffLines, diffStats } from './lineDiff'

describe('diffLines', () => {
  it('marks added and removed lines and keeps the rest', () => {
    const d = diffLines('a\nb\nc\n', 'a\nB\nc\nd\n')
    expect(d).toEqual([
      { op: ' ', text: 'a' }, { op: '-', text: 'b' }, { op: '+', text: 'B' }, { op: ' ', text: 'c' }, { op: '+', text: 'd' },
    ])
    expect(diffStats(d)).toEqual({ added: 2, removed: 1 })
  })

  it('a new file is all additions, identical texts are all context', () => {
    expect(diffLines('', 'x\ny\n').map((l) => l.op)).toEqual(['+', '+'])
    expect(diffLines('x\n', 'x\n')).toEqual([{ op: ' ', text: 'x' }])
    expect(diffLines('', '')).toEqual([])
  })

  it('ignores a trailing newline and CRLF', () => {
    expect(diffLines('a\r\nb', 'a\nb\n').every((l) => l.op === ' ')).toBe(true)
  })

  it('falls back to a blunt diff for huge texts', () => {
    const big = Array.from({ length: 2500 }, (_, i) => `l${i}`).join('\n')
    const d = diffLines(big, big + '\nx')
    expect(d.filter((l) => l.op === '-')).toHaveLength(2500)
    expect(d.filter((l) => l.op === '+')).toHaveLength(2501)
  })
})
