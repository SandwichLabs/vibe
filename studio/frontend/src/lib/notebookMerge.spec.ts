import { describe, it, expect } from 'vitest'
import { cellsToMerge, pruneConfirmedTombstones, capPromptList } from './notebookMerge'

describe('cellsToMerge (#66 part B review fix, finding 2)', () => {
  it('save-echo-then-merge: an event with no appendedCellIds merges nothing', () => {
    const local = [{ id: 'a' }]
    const remote = [{ id: 'a' }, { id: 'b' }] // 'b' exists on the backend but we don't know about it yet
    expect(cellsToMerge(local, remote, undefined, new Set())).toEqual([])
    expect(cellsToMerge(local, remote, [], new Set())).toEqual([])
  })

  it('delete-then-merge: a tombstoned id is never resurrected even if the backend still has it', () => {
    const local = [{ id: 'a' }] // 'b' was deleted locally (removed from cells.value)
    const remote = [{ id: 'a' }, { id: 'b' }] // backend hasn't seen the delete yet — race window
    const appendedCellIds = ['c'] // an unrelated agent append landed in the same event
    const remoteWithNew = [...remote, { id: 'c' }]
    const tombstones = new Set(['b'])
    const merged = cellsToMerge(local, remoteWithNew, appendedCellIds, tombstones)
    expect(merged.map((c) => c.id)).toEqual(['c'])
    expect(merged.find((c) => c.id === 'b')).toBeUndefined()
  })

  it('append-while-typing: only the specifically appended id merges, not incidental other remote cells', () => {
    const local = [{ id: 'a' }]
    // The backend's current cells include 'a' (known), and a NEW cell 'z'
    // an agent appended — plus, hypothetically, some other id 'q' that
    // exists remotely but was never announced as appended (must not merge).
    const remote = [{ id: 'a', source: 'SELECT 1' }, { id: 'z', source: 'SELECT 2' }, { id: 'q', source: 'SELECT 3' }]
    const merged = cellsToMerge(local, remote, ['z'], new Set())
    expect(merged.map((c) => c.id)).toEqual(['z'])
  })

  it('never re-adds a cell already known locally, even if listed in appendedCellIds', () => {
    const local = [{ id: 'a' }]
    const remote = [{ id: 'a' }]
    expect(cellsToMerge(local, remote, ['a'], new Set())).toEqual([])
  })
})

describe('pruneConfirmedTombstones', () => {
  it('clears a tombstone once a save excluding it succeeds', () => {
    const tombstones = new Set(['a', 'b'])
    const justSaved = new Set(['b']) // 'a' was NOT in the just-saved payload -> confirmed excluded
    const next = pruneConfirmedTombstones(tombstones, justSaved)
    expect(next.has('a')).toBe(false)
    expect(next.has('b')).toBe(true) // still present in the save — not confirmed excluded yet
  })

  it('is a no-op when nothing was confirmed excluded', () => {
    const tombstones = new Set(['a'])
    const justSaved = new Set(['a'])
    expect(pruneConfirmedTombstones(tombstones, justSaved)).toEqual(new Set(['a']))
  })
})

describe('capPromptList (finding 7)', () => {
  it('caps entry count and per-entry length, collapsing newlines', () => {
    const many = Array.from({ length: 40 }, (_, i) => `entry ${i}`)
    expect(capPromptList(many)).toHaveLength(30)

    const long = ['a'.repeat(500)]
    const capped = capPromptList(long)
    expect(capped[0].length).toBeLessThanOrEqual(121)

    const multiline = ['line one\nline two']
    expect(capPromptList(multiline)[0]).not.toContain('\n')
  })
})
