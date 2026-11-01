import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AssertionResults from './AssertionResults.vue'

const row = (over: Partial<AssertionRow>): AssertionRow => ({
  subject_kind: 'metric', subject: 'revenue', assertion: 'non_negative', status: 'pass', violations: 0, duration_ms: 1, ...over,
})

describe('AssertionResults', () => {
  it('labels a compile row "Compiles" and shows no violation count', () => {
    const w = mount(AssertionResults, { props: { rows: [row({ assertion: 'compile' }), row({})] } })
    expect(w.get('[data-testid="assertion-compile-0"]').text()).toBe('Compiles')
    expect(w.get('[data-testid="assertion-row-0"]').text()).not.toContain('0')
    expect(w.get('[data-testid="assertion-row-1"]').text()).toContain('non_negative')
  })

  it('shows the binder message of a failed compile row as text', () => {
    const msg = 'Binder Error: column "amunt" not found <b>x</b>'
    const w = mount(AssertionResults, { props: { rows: [
      row({ assertion: 'compile', status: 'fail', error: msg }),
      row({ status: 'error', error: 'not run: the definition does not compile' }),
    ] } })
    const first = w.get('[data-testid="assertion-row-0"]')
    expect(first.text()).toContain('Does not compile')
    expect(first.text()).toContain(msg)
    expect(first.text().split('Does not compile').length - 1).toBe(1)
    expect(first.find('b').exists()).toBe(false)
    expect(w.get('[data-testid="assertion-summary"]').text()).toContain('1 failed, 1 could not run')
  })

  it('a connection that cannot be reached is an error row, not "does not compile"', () => {
    const w = mount(AssertionResults, { props: { rows: [row({ assertion: 'compile', status: 'error', error: 'token refused' })] } })
    expect(w.get('[data-testid="assertion-compile-0"]').text()).toBe('Compile check')
    expect(w.text()).not.toContain('Does not compile')
  })
})
