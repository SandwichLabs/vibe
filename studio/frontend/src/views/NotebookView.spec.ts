// NotebookView interaction tests (#66 part A): run a sql cell and see it
// auto-fit a view, edit-without-running shows the stale badge, and a write
// attempt surfaces the read-only message RunA2UIQuery already returns.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../composables/useVibeEditor', () => ({
  createVibeEditor: (el: HTMLElement, opts: any) => ({
    view: {},
    setValue: (v: string) => { opts.onChange?.(v) },
    destroy: vi.fn(),
    getValue: () => opts.value ?? '',
  }),
}))

// The mocked route must be genuinely Vue-reactive — NotebookView's
// `watch(() => route.params.id, ...)` (the fix for #66 part A review item
// 3, the notebook-to-notebook overwrite bug) only fires when Vue can track
// a dependency on route.params.id, which a plain mutated object can't
// provide. Built inside the (async-capable) mock factory so it can import
// 'vue' itself without fighting vi.mock's hoisting-above-imports rule.
vi.mock('vue-router', async () => {
  const { reactive } = await import('vue')
  const mockRoute = reactive({ params: { id: 'nb1' } })
  ;(globalThis as any).__mockRoute = mockRoute
  return {
    useRoute: () => mockRoute,
    useRouter: () => ({ push: vi.fn() }),
    onBeforeRouteLeave: () => {},
  }
})

import NotebookView from './NotebookView.vue'

const BAR_NOTEBOOK = {
  schemaVersion: 1,
  id: 'nb1',
  title: 'My Notebook',
  updatedAt: '2026-01-01T00:00:00Z',
  cells: [
    { id: 'c1', type: 'sql', collapsed: false, source: 'SELECT region, sum(revenue) FROM sales GROUP BY ALL', view: 'table', viewPinned: false, label: 'by region' },
  ],
}

function makeApp(overrides: Partial<Record<string, any>> = {}) {
  const app = {
    GetNotebook: vi.fn(async () => JSON.parse(JSON.stringify(BAR_NOTEBOOK))),
    SaveNotebook: vi.fn(async (nb: any) => ({ notebook: nb, warnings: [] })),
    RunA2UIQuery: vi.fn(async () => ({
      columns: ['region', 'sum(revenue)'],
      rows: [['North', 100], ['South', 200], ['East', 50]],
      truncated: false,
    })),
    ...overrides,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

let mountedWrappers: ReturnType<typeof mount>[] = []
function mountView() {
  const wrapper = mount(NotebookView, { global: { stubs: { Icon: true } }, attachTo: document.body })
  mountedWrappers.push(wrapper)
  return wrapper
}

describe('NotebookView', () => {
  beforeEach(() => {
    vi.useRealTimers()
    ;(globalThis as any).__mockRoute.params.id = 'nb1'
  })

  // Every mounted NotebookView keeps a live watch() on the shared mocked
  // route object — without unmounting between tests, a PREVIOUS test's
  // component instance is still around and reacts to a LATER test's route
  // mutations too, causing spurious/duplicate GetNotebook and SaveNotebook
  // calls that made the route-switch test flaky when run alongside others.
  afterEach(() => {
    for (const w of mountedWrappers) w.unmount()
    mountedWrappers = []
  })

  it('loads and runs sql cells on open, auto-fitting a chart view', async () => {
    makeApp()
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="notebook-title"]').element).toBeTruthy()
    // Auto-fit: 3 distinct regions with a numeric second column -> bar.
    expect(wrapper.find('[data-testid="view-bar"]').classes()).toContain('row-selected')
  })

  // #68 follow-up (reviewer-caught data corruption): NotebookView must
  // stamp itself with the file-open session id in effect at LOAD time and
  // send it back on every save, so the backend can reject a save whose
  // token no longer matches the currently open file (e.g. the file was
  // switched via OpenFile/CreateFile/CreateFromTemplate while this view was
  // still mounted for the previous one).
  it('captures the session id from GetFileInfo at load and sends it with every SaveNotebook call', async () => {
    const app = makeApp({ GetFileInfo: vi.fn(async () => ({ session_id: 'session-abc' })) })
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.getLoadedSessionId()).toBe('session-abc')

    await vm.flushSave()
    expect(app.SaveNotebook).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'nb1' }),
      'session-abc',
    )
  })

  it('falls back to an empty session id when GetFileInfo is unavailable, without crashing the load', async () => {
    const app = makeApp() // no GetFileInfo override — makeApp's base app doesn't define one
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.getLoadedNotebookId()).toBe('nb1') // the notebook still loaded fine
    expect(vm.getLoadedSessionId()).toBe('')

    await vm.flushSave()
    expect(app.SaveNotebook).toHaveBeenCalledWith(expect.objectContaining({ id: 'nb1' }), '')
  })

  it('marks a cell stale when the source is edited without re-running', async () => {
    makeApp()
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="stale-badge"]').exists()).toBe(false)

    // Simulate an edit through the (mocked) editor's onChange.
    const vm = wrapper.vm as any
    vm.cells[0].source = vm.cells[0].source + ' -- edited'
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="stale-badge"]').exists()).toBe(true)
  })

  it('a write query surfaces the read-only message from RunA2UIQuery', async () => {
    const app = makeApp({
      RunA2UIQuery: vi.fn(async () => {
        throw new Error('live bindings are read-only queries over this file\'s own tables (no writes, no external files)')
      }),
    })
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    const vm = wrapper.vm as any
    vm.cells[0].source = 'DELETE FROM sales'
    await vm.runCell(vm.cells[0])
    await flushPromises()

    expect(wrapper.find('[data-testid="cell-error"]').text()).toContain('read-only')
    expect(app.RunA2UIQuery).toHaveBeenCalled()
  })

  it('picking an unfit view shows a mismatch note instead of the chart', async () => {
    makeApp()
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    await wrapper.find('[data-testid="view-stat"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="mismatch-note"]').exists()).toBe(true)
  })

  it('adds, moves, and deletes cells', async () => {
    makeApp()
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    await wrapper.find('[data-testid="add-markdown-cell"]').trigger('click')
    await flushPromises()
    const vm = wrapper.vm as any
    expect(vm.cells).toHaveLength(2)
    expect(vm.cells[1].type).toBe('markdown')

    vm.moveCell(vm.cells[1], -1)
    await flushPromises()
    expect(vm.cells[0].type).toBe('markdown')

    vm.deleteCell(vm.cells[0])
    await flushPromises()
    expect(vm.cells).toHaveLength(1)
  })

  // Review fix, step 4a #3: vue-router reuses this component instance
  // across a /notebooks/A -> /notebooks/B navigation — only route.params.id
  // changes. Before the fix, the debounced save still in flight (or one
  // that fired after cells.value had already been replaced with B's cells)
  // built its payload from the LIVE route id, so A's edit could be written
  // under B's id, silently overwriting B's content with A's.
  it('switching notebooks flushes the old one under its own id and never overwrites the new one', async () => {
    const NB_A = { schemaVersion: 1, id: 'nbA', title: 'Notebook A', updatedAt: 't', cells: [{ id: 'a1', type: 'markdown', collapsed: false, source: 'original A' }] }
    const NB_B = { schemaVersion: 1, id: 'nbB', title: 'Notebook B', updatedAt: 't', cells: [{ id: 'b1', type: 'markdown', collapsed: false, source: 'original B' }] }
    ;(globalThis as any).__mockRoute.params.id = 'nbA'
    const saveCalls: any[] = []
    makeApp({
      GetNotebook: vi.fn(async (id: string) => JSON.parse(JSON.stringify(id === 'nbA' ? NB_A : NB_B))),
      SaveNotebook: vi.fn(async (nb: any) => {
        saveCalls.push(JSON.parse(JSON.stringify(nb)))
        return { notebook: nb, warnings: [] }
      }),
    })
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.getLoadedNotebookId()).toBe('nbA')
    // Simulate an in-progress edit to A that has not yet autosaved.
    vm.cells[0].source = 'EDITED A'

    // Navigate to B — the component instance is reused (Vitest mounts fresh,
    // but this exercises the same `watch(() => route.params.id, ...)` path
    // vue-router's real reuse would trigger).
    ;(globalThis as any).__mockRoute.params.id = 'nbB'
    await flushPromises()
    await flushPromises()
    await flushPromises()

    // A's edit must have been flushed under A's OWN id before B loaded.
    const aSave = saveCalls.find((c) => c.id === 'nbA')
    expect(aSave).toBeTruthy()
    expect(aSave.cells[0].source).toBe('EDITED A')

    // B must be loaded fresh, completely unaffected by A's content.
    expect(vm.getLoadedNotebookId()).toBe('nbB')
    expect(vm.title).toBe('Notebook B')
    expect(vm.cells[0].source).toBe('original B')

    // The bug this guards against: a save landing under B's id but carrying
    // A's edited content.
    const corrupted = saveCalls.find((c) => c.id === 'nbB' && c.cells[0]?.source === 'EDITED A')
    expect(corrupted).toBeFalsy()
  })

  // --- coordinator follow-up #3: a "notebooks:changed" append event that
  // arrives WHILE a notebook is loading must be queued and replayed once
  // loading finishes, not silently dropped. ---
  it('queues an append event received while loading and replays it once the load settles', async () => {
    const NB2_BASE = { schemaVersion: 1, id: 'nb2', title: 'NB2', updatedAt: 't', cells: [] as any[] }
    const NB2_WITH_APPEND = { ...NB2_BASE, cells: [{ id: 'new1', type: 'markdown', collapsed: false, source: 'appended while loading' }] }

    let resolveNb2Load: ((v: any) => void) | null = null
    let getNotebookCalls = 0
    makeApp({
      GetNotebook: vi.fn((id: string) => {
        getNotebookCalls++
        if (id === 'nb1') return Promise.resolve(JSON.parse(JSON.stringify(BAR_NOTEBOOK)))
        // id === 'nb2': the FIRST call is loadNotebook's own fetch — held
        // pending so the test can fire the event while loading.value is
        // still true. Every subsequent call (the merge fetch inside
        // applyNotebooksChanged's replay) returns the version WITH the
        // appended cell.
        if (getNotebookCalls === 2) {
          return new Promise((resolve) => { resolveNb2Load = resolve })
        }
        return Promise.resolve(JSON.parse(JSON.stringify(NB2_WITH_APPEND)))
      }),
    })
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.getLoadedNotebookId()).toBe('nb1')

    // Switch to nb2 — loadNotebook('nb2') starts and blocks on GetNotebook.
    ;(globalThis as any).__mockRoute.params.id = 'nb2'
    await flushPromises()
    await flushPromises()

    expect(vm.getLoadedNotebookId()).toBe('nb2')
    expect(vm.isLoading()).toBe(true)

    // The event arrives mid-load — must be queued, not dropped.
    await vm.handleNotebooksChanged({ notebookId: 'nb2', appendedCellIds: ['new1'] })
    expect(vm.cells.map((c: any) => c.id)).not.toContain('new1') // not applied yet — still loading

    // The load itself finishes now.
    resolveNb2Load!(JSON.parse(JSON.stringify(NB2_BASE)))
    await flushPromises()
    await flushPromises()
    await flushPromises()
    await flushPromises()

    expect(vm.isLoading()).toBe(false)
    expect(vm.cells.map((c: any) => c.id)).toContain('new1')
  })

  it('drops a queued append event for a notebook that is no longer loaded (superseded)', async () => {
    let resolveNb2Load: ((v: any) => void) | null = null
    const NB2 = { schemaVersion: 1, id: 'nb2', title: 'NB2', updatedAt: 't', cells: [] as any[] }
    makeApp({
      GetNotebook: vi.fn((id: string) => {
        if (id === 'nb1') return Promise.resolve(JSON.parse(JSON.stringify(BAR_NOTEBOOK)))
        return new Promise((resolve) => { resolveNb2Load = resolve })
      }),
    })
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    const vm = wrapper.vm as any

    ;(globalThis as any).__mockRoute.params.id = 'nb2'
    await flushPromises()
    await flushPromises()
    expect(vm.getLoadedNotebookId()).toBe('nb2')

    // An event for the notebook we already LEFT (nb1) must never queue or apply.
    await vm.handleNotebooksChanged({ notebookId: 'nb1', appendedCellIds: ['ghost'] })
    resolveNb2Load!(JSON.parse(JSON.stringify(NB2)))
    await flushPromises()
    await flushPromises()
    await flushPromises()

    expect(vm.cells.map((c: any) => c.id)).not.toContain('ghost')
  })
})
