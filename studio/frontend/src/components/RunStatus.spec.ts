// RunStatus: Stop sits outside the live region, is labelled by what runs, and
// hands its focus to the card's run button when the run ends.
import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import RunStatus from './RunStatus.vue'

const base = { elapsed: 3, error: '', conflict: '', what: 'Checking' }

describe('RunStatus', () => {
  it('labels Stop by the run and keeps it out of the live region', () => {
    const w = mount(RunStatus, { props: { ...base, busy: true } })
    const stop = w.get('[data-testid="run-stop"]')
    expect(stop.attributes('aria-label')).toBe('Stop: Checking')
    expect(w.get('[role="status"]').find('button').exists()).toBe(false)
    stop.trigger('click')
    expect(w.emitted('stop')).toHaveLength(1)
  })

  it('focuses the card run button after a stopped run ends', async () => {
    const Host = defineComponent({
      props: { busy: Boolean },
      setup(p) {
        return () => h('section', [h('button', { 'data-run-trigger': '', 'data-testid': 'trigger' }, 'Run'), h(RunStatus, { ...base, busy: p.busy })])
      },
    })
    const w = mount(Host, { props: { busy: true }, attachTo: document.body })
    await w.get('[data-testid="run-stop"]').trigger('click')
    await w.setProps({ busy: false })
    await flushPromises()
    expect(document.activeElement).toBe(w.get('[data-testid="trigger"]').element)
    expect(w.find('[data-testid="run-cancelled"]').exists()).toBe(false)
    w.unmount()
  })
})
