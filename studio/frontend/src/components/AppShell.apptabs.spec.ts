// Component tests for the W1 App-Mode top tab strip (locked-shell navigation).
//
// In App Mode the 240px sidebar is removed and the switchboard's top-level nav
// items become tabs in the white `.topbar.app` chrome. A leading "Home" tab
// routes to the switchboard; each nav item routes to the SAME destination its
// switchboard button would (shared via lib/nav.ts). These mount the real
// AppShell against a mocked window.go.main.App + the real router.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router, setCachedMode } from '../router/index'
import AppShell from './AppShell.vue'

const NAV = [
  { id: 'n1', parent_id: '', label: 'New service call', action_kind: 'open_form', target_id: 'form_calls', sort_order: 1 },
  { id: 'n0', parent_id: '', label: 'Today', action_kind: 'open_report', target_id: 'rep_today', sort_order: 0 },
  // a child item (non-root) must NOT become a top-level tab
  { id: 'n2', parent_id: 'n1', label: 'Buried', action_kind: 'open_form', target_id: 'form_x', sort_order: 0 },
]

interface AppConfig {
  mode: string
  default_mode: string
  startup_object: string
  app_title: string
}

function makeApp(mode: 'studio' | 'app') {
  const cfg: AppConfig = { mode, default_mode: mode, startup_object: '', app_title: 'HVAC Ops' }
  const app = {
    GetFileInfo: vi.fn(async () => ({ path: '/tmp/hvac.vibe' })),
    GetAppConfig: vi.fn(async () => ({ ...cfg })),
    SetMode: vi.fn(async (m: string) => { cfg.mode = m }),
    SaveFile: vi.fn(async () => {}),
    CloseFile: vi.fn(async () => {}),
    ListNav: vi.fn(async () => NAV.map((n) => ({ ...n }))),
    ListNavTargets: vi.fn(async () => []),
    RunAutoExec: vi.fn(async () => ({ intents: [] })),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app, cfg }
}

function mountShell() {
  return mount(AppShell, { global: { plugins: [router], stubs: { Icon: true } } })
}

describe('AppShell App-Mode tab strip (W1)', () => {
  beforeEach(async () => {
    vi.useRealTimers()
    setCachedMode('studio')
    if (router.currentRoute.value.name !== 'dashboard') {
      await router.push('/')
      await router.isReady()
    }
  })

  it('renders a Home tab plus one tab per root nav item, sorted by sort_order', async () => {
    makeApp('app')
    const wrapper = mountShell()
    await flushPromises()

    const tabs = wrapper.findAll('[data-testid="app-tab"]')
    // Home + 2 root items (the child item is excluded).
    expect(tabs).toHaveLength(3)
    expect(tabs[0].text()).toContain('Home')
    expect(tabs[1].text()).toContain('Today') // sort_order 0
    expect(tabs[2].text()).toContain('New service call') // sort_order 1
    expect(wrapper.text()).not.toContain('Buried')
  })

  it('clicking a form tab routes to /forms with the bound target id', async () => {
    makeApp('app')
    const wrapper = mountShell()
    await flushPromises()

    const tabs = wrapper.findAll('[data-testid="app-tab"]')
    await tabs[2].trigger('click') // "New service call" → open_form form_calls
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/forms')
    expect(router.currentRoute.value.query.id).toBe('form_calls')
  })

  it('Studio Mode renders no tab strip', async () => {
    makeApp('studio')
    const wrapper = mountShell()
    await flushPromises()

    expect(wrapper.find('[data-testid="app-tabstrip"]').exists()).toBe(false)
  })
})

describe('AppShell App-Mode tab strip access rules (W4)', () => {
  beforeEach(async () => {
    vi.useRealTimers()
    setCachedMode('studio')
    if (router.currentRoute.value.name !== 'dashboard') {
      await router.push('/')
      await router.isReady()
    }
  })

  it('owner-only nav items never surface as tabs in the locked shell', async () => {
    const { app } = makeApp('app')
    app.ListNav = vi.fn(async () => [
      { id: 'n0', parent_id: '', label: 'Today', action_kind: 'open_report', target_id: 'rep_today', sort_order: 0 },
      { id: 'n1', parent_id: '', label: 'Owner Console', action_kind: 'open_page', target_id: 'pg_admin', sort_order: 1, visible_to: 'owner' },
    ])
    const wrapper = mountShell()
    await flushPromises()

    const tabs = wrapper.findAll('[data-testid="app-tab"]')
    // Home + the one visible item; the owner-only item is filtered out.
    expect(tabs).toHaveLength(2)
    expect(wrapper.text()).toContain('Today')
    expect(wrapper.text()).not.toContain('Owner Console')
  })
})
