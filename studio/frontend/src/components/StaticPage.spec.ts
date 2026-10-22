// StaticPage (#162, H3): the frame is a script-less sandbox, the controls sit around it,
// and nothing the page says is markup here.
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { ARTIFACT_RUNTIME } from '../a2ui/artifactRuntime'

const src = ref('')
const failure = ref('')
vi.mock('../composables/usePageFrame', () => ({ usePageFrame: () => ({ src, failure, reload: vi.fn() }) }))

import StaticPage from './StaticPage.vue'

const detail = {
  name: 'pg', title: 'Sales', description: '', queries: [], view_kind: 'html', defaults: { n: 3 }, rev: 'r', updated_at: '',
  params: [{ name: 'n', type: 'integer', default: '3' }],
} as unknown as ArtifactDetail

function mountPage(isDefault = true) {
  const reset = vi.fn()
  const runtime = {
    result: () => ({ loading: false, error: '', data: null, fetched: true, truncated: false }),
    params: { param: (n: string) => detail.params.find((p) => p.name === n), display: () => 3, error: () => '', isDefault: () => isDefault, set: vi.fn(), reset },
  }
  const w = mount(StaticPage, { props: { name: 'pg', detail, values: { n: 3 } }, global: { provide: { [ARTIFACT_RUNTIME as symbol]: runtime } } })
  return { w, reset }
}

describe('StaticPage', () => {
  it('hosts the page in an iframe with an empty sandbox: no allow-* token of any kind', async () => {
    src.value = '/vibe-view/' + 'a'.repeat(64) + '/page'
    failure.value = ''
    const { w } = mountPage()
    await flushPromises()
    const f = w.get('[data-testid="static-page-frame"]')
    expect(f.element.tagName).toBe('IFRAME')
    expect(f.attributes('sandbox')).toBe('')
    expect(f.element.hasAttribute('sandbox')).toBe(true)
    expect(f.attributes('src')).toBe(src.value)
    expect(f.attributes('referrerpolicy')).toBe('no-referrer')
    for (const a of ['allow', 'allowfullscreen', 'srcdoc', 'csp']) expect(f.element.hasAttribute(a), a).toBe(false)
    expect(w.find('script').exists()).toBe(false)
  })

  it('has the level-1 controls around the frame, with Reset', async () => {
    src.value = '/vibe-view/x/page'
    const { w, reset } = mountPage(false)
    expect(w.findAll('[data-testid="a2ui-control-numberInput"]').length).toBe(1)
    await w.get('[data-testid="static-page-reset"]').trigger('click')
    expect(reset).toHaveBeenCalledWith('n')
  })

  it('is resizable and shows no frame while opening', () => {
    src.value = ''
    const { w } = mountPage()
    expect(w.find('[data-testid="static-page-frame"]').exists()).toBe(false)
    expect(w.get('[data-testid="static-page-frame-box"]').text()).toContain('Opening')
    expect(w.get('[data-testid="static-page-frame-box"]').attributes('aria-busy')).toBe('true')
  })

  it('shows a failure as text and no frame', () => {
    src.value = '/vibe-view/x/page'
    failure.value = '<img src=x onerror=alert(1)>'
    const { w } = mountPage()
    expect(w.find('[data-testid="static-page-frame"]').exists()).toBe(false)
    expect(w.get('[data-testid="static-page-error"]').text()).toBe('<img src=x onerror=alert(1)>')
    expect(w.find('img').exists()).toBe(false)
  })
})
