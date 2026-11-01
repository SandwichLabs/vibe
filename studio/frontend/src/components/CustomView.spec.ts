// CustomView (#160): what an allowed custom view shows. The Worker and its bridge
// are useViewBridge's (spec'd there); here the state it reports is rendered:
// controls from the manifest, the SVG only as an <img>, a stopped view with
// Reload, and every string from the view as text.
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, shallowRef, nextTick } from 'vue'
import { ARTIFACT_RUNTIME } from '../a2ui/artifactRuntime'

const state = {
  phase: ref('ready'), failure: ref(''), drawError: ref(''), svgUrl: ref(''), page: shallowRef<any>(null), drawing: ref(false), reload: vi.fn(),
}
vi.mock('../composables/useCustomView', () => ({ useCustomView: () => state }))

import CustomView from './CustomView.vue'

const detail = {
  name: 'chart', title: 'Sales chart', description: '', queries: [], view_kind: 'script', defaults: { region: 'EU', n: 3 }, rev: 'r', updated_at: '',
  params: [
    { name: 'region', type: 'enum', default: 'EU', options: ['EU', 'US'] },
    { name: 'n', type: 'integer', default: '3' },
    { name: 'from', type: 'date', default: null },
    { name: 'to', type: 'date', default: null },
  ],
} as unknown as ArtifactDetail

function mountView(over: { reset?: any; isDefault?: boolean } = {}) {
  const reset = over.reset ?? vi.fn()
  const runtime = {
    result: () => ({ loading: false, error: '', data: null, fetched: true, truncated: false }),
    params: {
      param: (n: string) => detail.params.find((p) => p.name === n),
      display: (n: string) => (detail.defaults as any)[n] ?? '',
      error: () => '', isDefault: () => over.isDefault ?? true, set: vi.fn(), reset,
    },
  }
  const w = mount(CustomView, { props: { name: 'chart', detail, values: { region: 'EU', n: 3 } }, global: { provide: { [ARTIFACT_RUNTIME as symbol]: runtime } } })
  return { w, reset }
}

describe('CustomView', () => {
  it('builds one control per parameter (dates paired) and Reset resets every parameter', async () => {
    state.phase.value = 'ready'
    const { w, reset } = mountView({ isDefault: false })
    expect(w.findAll('[data-testid="a2ui-control-select"]').length).toBe(1)
    expect(w.findAll('[data-testid="a2ui-control-numberInput"]').length).toBe(1)
    expect(w.findAll('[data-testid="a2ui-control-dateRange"]').length).toBe(1)
    await w.get('[data-testid="custom-view-reset"]').trigger('click')
    expect(reset.mock.calls.map((c: any[]) => c[0]).sort()).toEqual(['from', 'n', 'region', 'to'])
  })

  it('disables Reset while every parameter is at its default', () => {
    const { w } = mountView({ isDefault: true })
    expect((w.get('[data-testid="custom-view-reset"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows an SVG only as an <img> with a blob URL', () => {
    state.svgUrl.value = 'blob:assay/1'
    const { w } = mountView()
    const img = w.get('[data-testid="custom-view-image"]')
    expect(img.element.tagName).toBe('IMG')
    expect(img.attributes('src')).toBe('blob:assay/1')
    expect(img.attributes('alt')).toBe('Sales chart')
    expect(w.html()).not.toContain('<svg')
    state.svgUrl.value = ''
  })

  it('a stopped view says so and Reload starts it again', async () => {
    state.phase.value = 'stopped'
    state.failure.value = 'This view stopped responding'
    const { w } = mountView()
    expect(w.get('[data-testid="custom-view-stopped"]').text()).toContain('This view stopped responding')
    await w.get('[data-testid="custom-view-reload"]').trigger('click')
    expect(state.reload).toHaveBeenCalled()
    state.phase.value = 'ready'
  })

  it('renders a view-supplied failure as text, never as markup', () => {
    state.phase.value = 'failed'
    state.failure.value = '<img src=x onerror=alert(1)>'
    state.drawError.value = ''
    const { w } = mountView()
    expect(w.get('[data-testid="custom-view-stopped"]').text()).toContain('<img src=x onerror=alert(1)>')
    expect(w.find('[data-testid="custom-view-stopped"] img').exists()).toBe(false)
    state.phase.value = 'ready'
    state.failure.value = ''
    state.drawError.value = '<b>boom</b>'
    const again = mountView()
    expect(again.w.get('[data-testid="custom-view-error"]').text()).toBe('<b>boom</b>')
    expect(again.w.find('[data-testid="custom-view-error"] b').exists()).toBe(false)
    state.drawError.value = ''
  })

  it('a failed image does not hide a later A2UI page, and clears when the drawing changes', async () => {
    state.svgUrl.value = 'blob:assay/bad'
    const { w } = mountView()
    await w.get('[data-testid="custom-view-image"]').trigger('error')
    expect(w.find('[data-testid="custom-view-image-error"]').exists()).toBe(true)
    state.svgUrl.value = ''
    state.page.value = { title: 'T', components: [{ id: 'a', type: 'text', text: 'page after image' }] }
    await nextTick()
    expect(w.find('[data-testid="custom-view-image-error"]').exists()).toBe(false)
    expect(w.get('[data-testid="custom-view-page"]').text()).toContain('page after image')
    state.page.value = null
  })

  it('renders a validated A2UI page with the level-1 renderer', () => {
    state.page.value = { title: 'T', components: [{ id: 'a', type: 'text', text: 'from the view' }] }
    const { w } = mountView()
    expect(w.get('[data-testid="custom-view-page"]').text()).toContain('from the view')
    state.page.value = null
  })
})
