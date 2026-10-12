import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {}, EventsOff: () => {} }))

import ConnectionSelect from './ConnectionSelect.vue'
import { _resetConnectionsForTests, connectionList } from '../composables/useConnections'

const c = (o: Partial<ConnectionInfo>): ConnectionInfo => ({ name: 'x', type: 'postgres', scope: 'local', readOnly: true, default: false, status: 'ok', remote: true, ...o })

function setup() {
  const list = [
    c({ name: 'local', type: 'duckdb', scope: 'shared', readOnly: false, default: true, remote: false }),
    c({ name: 'pg', status: 'not-connected' }),
    c({ name: 'ops', type: 'duckdb', remote: false, status: 'locked-by' }),
    c({ name: 'lake', type: 's3', catalogless: true }),
  ]
  const app = {
    ListConnections: vi.fn(async () => ({ connections: list, relaunchRequired: false })),
    ConnectConnection: vi.fn(async () => null),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  connectionList.value = list
  return app
}

const mountSel = (props: { modelValue: string; variant?: "cell" | "toolbar"; disabled?: boolean }) => mount(ConnectionSelect, { props, attachTo: document.body, global: { stubs: { Icon: true } } })

describe('ConnectionSelect', () => {
  beforeEach(() => { _resetConnectionsForTests(); document.body.innerHTML = '' })

  it('the toolbar variant names the connection a run uses; empty means the workspace default', async () => {
    setup()
    const w = mountSel({ modelValue: '' })
    await flushPromises()
    expect(w.find('[data-testid="conn-select-value"]').text()).toBe('local')
    expect(w.find('[data-testid="conn-select"]').attributes('aria-label')).toBe('Run on: local')
  })

  it('the cell chip reads "Default · local" until a connection is chosen, then names it', async () => {
    setup()
    const w = mountSel({ modelValue: '', variant: 'cell' })
    await flushPromises()
    expect(w.find('[data-testid="conn-select"]').text()).toContain('Default · local')
    await w.setProps({ modelValue: 'pg' })
    expect(w.find('[data-testid="conn-select"]').text()).toContain('pg')
  })

  it('is a listbox: Enter opens, arrows move, Enter picks, Escape closes; storage scopes and locked files are not choosable', async () => {
    const app = setup()
    const w = mountSel({ modelValue: '' })
    await flushPromises()
    const btn = w.find('[data-testid="conn-select"]')
    expect(btn.attributes('aria-haspopup')).toBe('listbox')
    await btn.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const menu = w.find('[role="listbox"]')
    expect(menu.exists()).toBe(true)
    const opts = menu.findAll('[role="option"]')
    expect(opts.map((o) => o.attributes('data-testid'))).toEqual(['conn-opt-local', 'conn-opt-pg', 'conn-opt-ops'])
    expect(opts[2].attributes('aria-disabled')).toBe('true')
    await menu.trigger('keydown', { key: 'ArrowDown' })
    await menu.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['pg'])
    expect(app.ConnectConnection).toHaveBeenCalledWith({ name: 'pg', scope: '' }) // picking a not-connected one connects it
    expect(w.find('[role="listbox"]').exists()).toBe(false)

    await btn.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    await w.find('[role="listbox"]').trigger('keydown', { key: 'Escape' })
    expect(w.find('[role="listbox"]').exists()).toBe(false)
  })

  it('the cell menu offers "Workspace default" first and the saved-with-the-cell line', async () => {
    setup()
    const w = mountSel({ modelValue: 'pg', variant: 'cell' })
    await flushPromises()
    await w.find('[data-testid="conn-select"]').trigger('click')
    await flushPromises()
    const opts = w.findAll('[role="option"]')
    expect(opts[0].text()).toContain('Workspace default')
    expect(opts[0].attributes('data-testid')).toBe('conn-opt-default')
    expect(w.find('[role="listbox"]').text()).toContain('Saved with the cell, so a teammate runs it on the same connection.')
    await opts[0].trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([''])
  })

  it('it is per instance: two selectors hold two different choices', async () => {
    setup()
    const a = mountSel({ modelValue: 'pg' })
    const b = mountSel({ modelValue: '' })
    await flushPromises()
    expect(a.find('[data-testid="conn-select-value"]').text()).toBe('pg')
    expect(b.find('[data-testid="conn-select-value"]').text()).toBe('local')
  })
})
