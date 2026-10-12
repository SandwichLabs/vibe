// Component tests for the Agent panel wiring in AppShell.
//
// #68 removed App Mode (Studio is the only mode now), so the Agent panel is
// always reachable via an "Agent" toggle in the top bar. These mount the
// real AppShell against a mocked window.go.main.App and assert the panel
// mounts closed and opens on toggle.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'
import AppShell from './AppShell.vue'
import { useAgentChat } from '../composables/useAgentChat'

function makeApp() {
  const app = {}
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app }
}

function mountShell() {
  return mount(AppShell, { global: { plugins: [router], stubs: { Icon: true } } })
}

describe('AppShell agent panel wiring', () => {
  beforeEach(async () => {
    vi.useRealTimers()
    if (router.currentRoute.value.name !== 'dashboard') {
      await router.push('/')
      await router.isReady()
    }
  })

  it('shows the Agent toggle and the panel mounts (closed)', async () => {
    makeApp()
    const wrapper = mountShell()
    await flushPromises()

    const toggle = wrapper.find('[data-testid="agent-toggle"]')
    expect(toggle.exists(), 'expected the Agent toggle').toBe(true)

    // Panel is mounted but closed (no `open` class) until toggled.
    const drawer = wrapper.find('[data-testid="agent-panel"]')
    expect(drawer.exists(), 'agent panel should be mounted').toBe(true)
    expect(drawer.classes()).not.toContain('open')
  })

  it('clicking the toggle opens the panel', async () => {
    makeApp()
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="agent-toggle"]').trigger('click')
    await flushPromises()

    const drawer = wrapper.find('[data-testid="agent-panel"]')
    expect(drawer.classes(), 'panel should be open after toggle').toContain('open')
  })

  it('marks the Agent nav entry and toggle while a turn is in flight (#2)', async () => {
    makeApp()
    useAgentChat().resetAll()
    const wrapper = mountShell()
    await flushPromises()
    expect(wrapper.find('[data-testid="agent-busy-nav"]').exists()).toBe(false)

    useAgentChat().sending.value = true
    await flushPromises()
    expect(wrapper.find('[data-testid="agent-busy-nav"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agent-busy-toggle"]').exists()).toBe(true)
    expect(wrapper.find('a[aria-label^="Agent,"]').attributes('aria-label')).toBe('Agent, a turn is running')

    useAgentChat().resetAll()
    await flushPromises()
    expect(wrapper.find('[data-testid="agent-busy-nav"]').exists()).toBe(false)
  })
})
