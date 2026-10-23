// LibraryView (docs/plans/library.md L5): the merged list, chips, filter, sort,
// partial failure, opening, and the New button.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))
import LibraryView from './LibraryView.vue'

const NB = [
  { id: 'n1', title: 'Churn questions', cellCount: 12, updatedAt: '2026-10-22T10:00:00Z' },
  { id: 'n2', title: 'Old', cellCount: 0, updatedAt: '2026-09-01T10:00:00Z', error: 'parse error' },
]
const AR = [
  { name: 'weekly-sales', title: 'Weekly sales', description: 'by region', view: 'a2ui', updated_at: '2026-10-23T10:00:00Z', rev: 'r' },
  { name: 'board', title: 'Board', description: '', view: 'html (scripted)', updated_at: '2026-10-21T10:00:00Z', rev: 'r' },
  { name: 'orders-pivot', title: '', description: '', view: '', updated_at: '', rev: '', error: 'artifact.yaml: unknown field "vew"' },
]

function setApp(over: Record<string, any> = {}) {
  ;(window as any).go = { main: { App: { ListNotebooks: vi.fn(async () => NB), ListArtifacts: vi.fn(async () => AR), ...over } } }
}
async function mountAt(path: string) {
  await router.push(path)
  await router.isReady()
  const w = mount(LibraryView, { global: { plugins: [router], stubs: { Icon: true } } })
  await flushPromises()
  return w
}
const rows = (w: any) => w.findAll('li').map((r: any) => r.find('.lib-title').text())

describe('LibraryView', () => {
  beforeEach(() => setApp())

  it('lists both kinds, newest first, with the chips and counts', async () => {
    const w = await mountAt('/library')
    expect(rows(w)).toEqual(['Weekly sales', 'Churn questions', 'Board', 'Old', 'orders-pivot'])
    expect(w.findAll('.lib-chip').map((c) => c.text())).toEqual(['All 5', 'Notebooks 2', 'Live views 1', 'Pages 1'])
  })

  it('shows a broken artifact with Needs fixing and its error as text', async () => {
    const w = await mountAt('/library')
    const row = w.find('[data-artifact-name="orders-pivot"]')
    expect(row.text()).toContain('Artifact')
    expect(row.text()).toContain('unknown field "vew"')
    expect(row.text()).toContain('Needs fixing')
  })

  it('the type in the URL picks the chip and the rows', async () => {
    const w = await mountAt('/library?type=notebook')
    expect(rows(w)).toEqual(['Churn questions', 'Old'])
    expect(w.get('[data-testid="lib-chip-notebook"]').attributes('aria-pressed')).toBe('true')
  })

  it('clicking a chip changes the URL', async () => {
    const w = await mountAt('/library')
    await w.get('[data-testid="lib-chip-page"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.type).toBe('page')
    expect(rows(w)).toEqual(['Board'])
  })

  it('filters by name and sorts A-Z on toggle', async () => {
    const w = await mountAt('/library')
    await w.get('[data-testid="lib-filter"]').setValue('SALES')
    expect(rows(w)).toEqual(['Weekly sales'])
    await w.get('[data-testid="lib-filter"]').setValue('')
    await w.get('[data-testid="lib-sort"]').trigger('click')
    expect(rows(w)).toEqual(['Board', 'Churn questions', 'Old', 'orders-pivot', 'Weekly sales'])
  })

  it('one list failing keeps the other and shows its error', async () => {
    setApp({ ListArtifacts: vi.fn(async () => { throw new Error('disk gone') }) })
    const w = await mountAt('/library')
    expect(w.get('[data-testid="library-error"]').text()).toContain('disk gone')
    expect(rows(w)).toEqual(['Churn questions', 'Old'])
  })

  it('opens each kind on its route', async () => {
    const w = await mountAt('/library')
    await w.get('[data-notebook-id="n1"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/library/notebook/n1')
    const w2 = await mountAt('/library')
    await w2.get('[data-artifact-name="board"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/library/artifact/board')
  })

  it('an empty workspace uses the picker wording', async () => {
    setApp({ ListNotebooks: vi.fn(async () => []), ListArtifacts: vi.fn(async () => []) })
    const w = await mountAt('/library')
    expect(w.get('[data-testid="library-empty"]').text()).toContain('Nothing in the library yet. New makes a notebook, a live view or a page')
  })

  it('an empty chip says so', async () => {
    const w = await mountAt('/library?type=live')
    await w.get('[data-testid="lib-filter"]').setValue('zzz')
    expect(w.get('[data-testid="library-empty-filter"]').text()).toContain('Nothing matches')
  })

  it('?new=1 opens the picker on Notebook once and leaves the URL clean', async () => {
    const w = await mountAt('/notebooks?new=1')
    expect(w.find('[data-testid="new-item-dialog"]').exists()).toBe(true)
    expect(w.get('[data-testid="type-notebook"]').attributes('aria-checked')).toBe('true')
    expect(router.currentRoute.value.query.new).toBeUndefined()
  })

  it('?new=page and ?new=script preselect those cards', async () => {
    let w = await mountAt('/library?new=page')
    expect(w.get('[data-testid="type-page"]').attributes('aria-checked')).toBe('true')
    w.unmount()
    w = await mountAt('/library?new=script')
    expect(w.get('[data-testid="type-script"]').attributes('aria-checked')).toBe('true')
  })

  it('the one New button opens the picker', async () => {
    const w = await mountAt('/library')
    expect(w.find('[data-testid="new-item-dialog"]').exists()).toBe(false)
    await w.get('[data-testid="new-item"]').trigger('click')
    expect(w.find('[data-testid="new-item-dialog"]').exists()).toBe(true)
  })

  it('an empty chip offers New for that type, preselected', async () => {
    setApp({ ListArtifacts: vi.fn(async () => []) })
    const w = await mountAt('/library?type=page')
    const b = w.get('[data-testid="empty-new"]')
    expect(b.text()).toContain('New page')
    await b.trigger('click')
    expect(w.get('[data-testid="type-page"]').attributes('aria-checked')).toBe('true')
  })
})
