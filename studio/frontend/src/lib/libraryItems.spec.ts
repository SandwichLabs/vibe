import { describe, it, expect } from 'vitest'
import { artifactItem, artifactType, chipsFor, filterItems, notebookItem, relativeTime, sortItems } from './libraryItems'

const art = (over: Partial<ArtifactSummary> = {}): ArtifactSummary => ({ name: 'sales', title: 'Sales', description: '', view: 'a2ui', updated_at: '2026-10-01T00:00:00Z', rev: 'r', ...over })
const nb = (over: Partial<WailsNotebookSummary> = {}): WailsNotebookSummary => ({ id: 'n1', title: 'Churn', cellCount: 2, updatedAt: '2026-10-02T00:00:00Z', ...over })

describe('type mapping', () => {
  it('maps ViewLabel strings', () => {
    expect(artifactType('a2ui')).toBe('live')
    expect(artifactType('script')).toBe('script')
    expect(artifactType('html (static)')).toBe('page')
    expect(artifactType('html (scripted)')).toBe('page')
    expect(artifactType('')).toBeNull()
  })
  it('a scripted page carries a script tag in its detail', () => {
    expect(artifactItem(art({ view: 'html (scripted)', name: 'board', description: 'Board' })).detail).toBe('board · script · Board')
  })
  it('a broken artifact is an Artifact, with its name as title', () => {
    const i = artifactItem(art({ view: '', title: '', error: 'bad', name: 'orders' }))
    expect(i).toMatchObject({ type: null, typeLabel: 'Artifact', title: 'orders', error: 'bad' })
  })
  it('a broken notebook is still a Notebook', () => {
    expect(notebookItem(nb({ error: 'parse', title: 'stem' }))).toMatchObject({ type: 'notebook', typeLabel: 'Notebook', title: 'stem', error: 'parse' })
  })
  it('counts cells', () => {
    expect(notebookItem(nb({ cellCount: 1 })).detail).toBe('1 cell')
    expect(notebookItem(nb({ cellCount: 0 })).detail).toBe('0 cells')
  })
})

describe('chips', () => {
  const base = [notebookItem(nb()), artifactItem(art()), artifactItem(art({ name: 'p', view: 'html (static)' })), artifactItem(art({ name: 'b', view: '', error: 'x' }))]
  it('counts per type, a broken artifact under All only, no Chart scripts chip at zero', () => {
    const chips = chipsFor(base, null)
    expect(chips.map((c) => [c.label, c.count])).toEqual([['All', 4], ['Notebooks', 1], ['Live views', 1], ['Pages', 1]])
  })
  it('Chart scripts appears at one', () => {
    const chips = chipsFor([...base, artifactItem(art({ name: 's', view: 'script' }))], null)
    expect(chips.at(-1)).toMatchObject({ label: 'Chart scripts', count: 1 })
  })
  it('the open Chart scripts chip stays even at zero', () => {
    expect(chipsFor(base, 'script').at(-1)).toMatchObject({ label: 'Chart scripts', count: 0 })
  })
})

describe('filter and sort', () => {
  const items = [notebookItem(nb({ id: 'a', title: 'Zeta', updatedAt: '2026-10-01T00:00:00Z' })), artifactItem(art({ name: 'weekly-sales', title: 'Alpha', updated_at: '2026-10-03T00:00:00Z' })), artifactItem(art({ name: 'broken', view: '', error: 'e', updated_at: '' }))]
  it('filters by type and case-insensitive name or id', () => {
    expect(filterItems(items, 'notebook', '').map((i) => i.id)).toEqual(['a'])
    expect(filterItems(items, null, 'ALPHA').map((i) => i.id)).toEqual(['weekly-sales'])
    expect(filterItems(items, null, 'weekly').map((i) => i.id)).toEqual(['weekly-sales'])
    expect(filterItems(items, 'live', 'zeta')).toEqual([])
  })
  it('recent sorts newest first with unreadable times last', () => {
    expect(sortItems(items, 'recent').map((i) => i.id)).toEqual(['weekly-sales', 'a', 'broken'])
  })
  it('ties break by title', () => {
    const t = [notebookItem(nb({ id: 'b', title: 'B' })), notebookItem(nb({ id: 'a', title: 'A' }))]
    expect(sortItems(t, 'recent').map((i) => i.id)).toEqual(['a', 'b'])
  })
  it('name sorts A-Z', () => {
    expect(sortItems(items, 'name').map((i) => i.title)).toEqual(['Alpha', 'broken', 'Zeta'])
  })
})

describe('relativeTime', () => {
  const now = Date.parse('2026-10-23T12:00:00Z')
  it('reads minutes, hours, days', () => {
    expect(relativeTime('2026-10-23T11:58:00Z', now)).toBe('2 minutes ago')
    expect(relativeTime('2026-10-23T11:00:00Z', now)).toBe('1 hour ago')
    expect(relativeTime('2026-10-20T12:00:00Z', now)).toBe('3 days ago')
  })
  it('is empty for an unreadable time', () => { expect(relativeTime('', now)).toBe('') })
})
