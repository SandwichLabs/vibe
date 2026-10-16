import { describe, it, expect } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'
import TestResult from './TestResult.vue'

const failed = (extra: Partial<TestResultView> = {}): TestResultView => ({
  ok: false, status: 'needs-credentials', ms: 12, count: 0,
  error: { code: 'bad-signin', message: 'The service turned down these keys or this token.', details: 'denied' },
  ...extra,
})

function mountResult(result: TestResultView) {
  return mount(TestResult, { props: { result, running: false, readOnly: true }, global: { stubs: { RouterLink: RouterLinkStub } } })
}

describe('TestResult', () => {
  it('says which storage connection failed and links to it', () => {
    const w = mountResult(failed({ requirement: 'floci' }))
    expect(w.get('[data-testid="test-requirement"]').text()).toBe('Storage connection floci failed')
    expect(w.text()).toContain('was not tried')
    const link = w.getComponent(RouterLinkStub)
    expect(link.props('to')).toBe('/connections/floci')
    expect(link.text()).toBe('Open floci')
  })

  it('keeps the plain heading when the connection itself failed', () => {
    const w = mountResult(failed())
    expect(w.find('[data-testid="test-requirement"]').exists()).toBe(false)
    expect(w.text()).toContain("Couldn't connect")
    expect(w.findComponent(RouterLinkStub).exists()).toBe(false)
  })
})
