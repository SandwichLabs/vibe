import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import CellDrawer from './CellDrawer.vue'

function mountDrawer(open: boolean) {
  return mount(CellDrawer, {
    props: { open, testid: 'sql-drawer' },
    slots: { summary: 'Show SQL', default: '<div>body</div>' },
    global: { stubs: { Icon: true } },
  })
}

describe('CellDrawer', () => {
  it('renders closed: collapsed aria state, matching aria-controls, inert body, no is-open class', () => {
    const wrapper = mountDrawer(false)
    const button = wrapper.find('[data-testid="sql-drawer"]')
    const body = wrapper.find('[data-testid="sql-drawer-body"]')
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(button.attributes('aria-controls')).toBe(body.attributes('id'))
    expect(body.attributes('inert')).toBeDefined()
    expect(button.classes()).not.toContain('is-open')
  })

  it('renders open: expanded aria state, no inert, is-open class', () => {
    const wrapper = mountDrawer(true)
    const button = wrapper.find('[data-testid="sql-drawer"]')
    const body = wrapper.find('[data-testid="sql-drawer-body"]')
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(body.attributes('inert')).toBeUndefined()
    expect(button.classes()).toContain('is-open')
  })

  it('emits toggle once per click', async () => {
    const wrapper = mountDrawer(false)
    await wrapper.find('[data-testid="sql-drawer"]').trigger('click')
    expect(wrapper.emitted('toggle')).toHaveLength(1)
  })
})
