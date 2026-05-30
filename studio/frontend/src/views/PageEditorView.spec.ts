// Component test for the P3.6 interactive input-filter re-render wiring in
// PageEditorView. It mounts the real component against a mocked
// window.go.main.App.RenderPageContent, simulates a change on a rendered filter
// control, and asserts:
//   - RenderPageContent is re-invoked with the collected params (Option A full
//     re-render), not the hardcoded empty map
//   - the selected value round-trips: the next render receives the value and the
//     server-marked selection is reflected back into the DOM
//   - clearing the control (empty value) removes the param => unfiltered
//
// CodeMirror (useVibeEditor) and vue-router are mocked so the test runs in
// jsdom without the editor's heavy DOM/measurement requirements.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

// Mock the CodeMirror composable — the editor is irrelevant to filter wiring.
vi.mock('../composables/useVibeEditor', () => ({
  createVibeEditor: () => ({
    view: {},
    setValue: vi.fn(),
    destroy: vi.fn(),
  }),
}))

// Mock vue-router so useRoute/useRouter resolve without a router instance.
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'p1' } }),
  useRouter: () => ({ push: vi.fn() }),
}))

import PageEditorView from './PageEditorView.vue'

// Render the dropdown control + a sentinel reflecting the current region param,
// mirroring the backend renderInput contract: the wrapper carries
// data-vibe-input, the <select> options carry their value, and the threaded
// value is marked selected.
function renderHtml(region: string): string {
  const opts =
    `<option value=""${region === '' ? ' selected' : ''}>(all)</option>` +
    ['West', 'East', 'North']
      .map((r) => `<option value="${r}"${r === region ? ' selected' : ''}>${r}</option>`)
      .join('')
  const tag = region ? `ORDER_${region}` : 'ALL_ROWS'
  return (
    `<div class="vibe-input" data-vibe-input="region">` +
    `<select class="vibe-dropdown">${opts}</select></div>` +
    `<div class="sentinel">${tag}</div>`
  )
}

function makeApp() {
  const RenderPageContent = vi.fn(async (_id: string, params: Record<string, string>) => {
    return renderHtml(params.region ?? '')
  })
  const app = {
    GetPage: vi.fn(async () => ({
      id: 'p1',
      title: 'Filters',
      content: '{dropdown: region, source=regions_q, column=region}',
      page_type: 'notebook',
      sort_order: 0,
    })),
    RenderPageContent,
    CancelRender: vi.fn(async () => {}),
    UpdatePage: vi.fn(async () => {}),
    SaveFile: vi.fn(async () => {}),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app, RenderPageContent }
}

function mountView() {
  return mount(PageEditorView, {
    global: { stubs: { Icon: true } },
    attachTo: document.body,
  })
}

describe('PageEditorView interactive input filters (P3.6)', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })

  it('collects a filter change and re-calls RenderPageContent with the populated params', async () => {
    const { RenderPageContent } = makeApp()
    const wrapper = mountView()
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0)) // let the post-render nextTick wire listeners

    // Initial render used an empty params map (no filter selected yet).
    expect(RenderPageContent).toHaveBeenCalled()
    expect(RenderPageContent.mock.calls[0][1]).toEqual({})
    expect(wrapper.find('.sentinel').text()).toBe('ALL_ROWS')

    // Simulate the user picking "West" in the rendered dropdown.
    const select = wrapper.find('select.vibe-dropdown')
    expect(select.exists()).toBe(true)
    await select.setValue('West')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))

    // Re-render must have been invoked with the collected param (Option A).
    const lastCall = RenderPageContent.mock.calls.at(-1)!
    expect(lastCall[1]).toEqual({ region: 'West' })

    // The selected value round-trips: the new render reflects the filter, and the
    // West option is marked selected in the DOM.
    expect(wrapper.find('.sentinel').text()).toBe('ORDER_West')
    const selectedOption = wrapper.find('select.vibe-dropdown option[value="West"]')
    expect((selectedOption.element as HTMLOptionElement).selected).toBe(true)
  })

  it('clearing the control removes the param so the page renders unfiltered', async () => {
    const { RenderPageContent } = makeApp()
    const wrapper = mountView()
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))

    // Select then clear back to the empty value.
    await wrapper.find('select.vibe-dropdown').setValue('West')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))
    expect(RenderPageContent.mock.calls.at(-1)![1]).toEqual({ region: 'West' })

    await wrapper.find('select.vibe-dropdown').setValue('')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))

    // Empty value removes the key => unfiltered (absent param, not region:'').
    expect(RenderPageContent.mock.calls.at(-1)![1]).toEqual({})
    expect(wrapper.find('.sentinel').text()).toBe('ALL_ROWS')
  })
})
