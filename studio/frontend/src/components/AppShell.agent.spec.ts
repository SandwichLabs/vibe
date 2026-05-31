// Component tests for the W0 Agent panel wiring in AppShell.
//
// Per the parity remediation plan W0, the slide-in Agent wizard panel must be:
//   - reachable from Studio Mode via an "Agent" toggle in the top bar, and
//   - entirely absent in App Mode (the locked-down end-user surface has no
//     agent affordance; the backend AgentChat is also guardStudioOnly).
// These mount the real AppShell against a mocked window.go.main.App and assert
// the gating at the component boundary.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router, setCachedMode } from '../router/index'
import AppShell from './AppShell.vue'

interface AppConfig {
  mode: string
  default_mode: string
  startup_object: string
  app_title: string
}

function makeApp(mode: 'studio' | 'app') {
  const cfg: AppConfig = { mode, default_mode: mode, startup_object: 'switchboard', app_title: 'Acme Orders' }
  const app = {
    GetFileInfo: vi.fn(async () => ({ path: '/tmp/acme.vibe' })),
    GetAppConfig: vi.fn(async () => ({ ...cfg })),
    SetMode: vi.fn(async (m: string) => { cfg.mode = m }),
    AgentChat: vi.fn(async () => ({ text: 'hi', steps: [] })),
    SaveFile: vi.fn(async () => {}),
    CloseFile: vi.fn(async () => {}),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app, cfg }
}

function mountShell() {
  return mount(AppShell, { global: { plugins: [router], stubs: { Icon: true } } })
}

describe('AppShell agent panel wiring (W0)', () => {
  beforeEach(async () => {
    vi.useRealTimers()
    setCachedMode('studio')
    if (router.currentRoute.value.name !== 'dashboard') {
      await router.push('/')
      await router.isReady()
    }
  })

  it('Studio Mode: shows the Agent toggle and the panel mounts (closed)', async () => {
    makeApp('studio')
    const wrapper = mountShell()
    await flushPromises()

    const toggle = wrapper.find('[data-testid="agent-toggle"]')
    expect(toggle.exists(), 'expected the Agent toggle in Studio Mode').toBe(true)

    // Panel is mounted but closed (no `open` class) until toggled.
    const drawer = wrapper.find('[data-testid="agent-panel"]')
    expect(drawer.exists(), 'agent panel should be mounted in Studio Mode').toBe(true)
    expect(drawer.classes()).not.toContain('open')
  })

  it('Studio Mode: clicking the toggle opens the panel', async () => {
    makeApp('studio')
    const wrapper = mountShell()
    await flushPromises()

    await wrapper.find('[data-testid="agent-toggle"]').trigger('click')
    await flushPromises()

    const drawer = wrapper.find('[data-testid="agent-panel"]')
    expect(drawer.classes(), 'panel should be open after toggle').toContain('open')
  })

  it('App Mode: the Agent toggle and panel are entirely absent', async () => {
    makeApp('app')
    const wrapper = mountShell()
    await flushPromises()

    expect(wrapper.find('[data-testid="agent-toggle"]').exists(), 'no Agent toggle in App Mode').toBe(false)
    expect(wrapper.find('[data-testid="agent-panel"]').exists(), 'no Agent panel in App Mode').toBe(false)
  })
})
