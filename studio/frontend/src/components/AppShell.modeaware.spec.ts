// Component tests for the mode-aware AppShell (Phase 4 / unit P4.6).
//
// Per parity-plan §4.1 and mockup M (mode-flow) / screen #6, the AppShell must
// stop hard-coding "Studio Mode" and instead drive its chrome from the live app
// mode read via GetAppConfig:
//   - Studio Mode  → teal pill, full builder nav
//                    (Schema / Tables / Queries / Pages / Import / Settings)
//   - App Mode     → marigold pill, app_title shown, builder nav HIDDEN; only the
//                    switchboard + permitted forms/reports are reachable.
// A Studio↔App toggle calls SetMode; returning to Studio routes through the
// unlock entrypoint (P4.4).
//
// These mount the real AppShell against a mocked window.go.main.App and the real
// router, and assert the acceptance behaviour at the component boundary. They are
// the RED step for P4.6 — the current AppShell hard-codes "Studio Mode", always
// renders the full nav, and never calls GetAppConfig/SetMode, so the App-Mode
// assertions fail on behaviour (not on compile).
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'
import AppShell from './AppShell.vue'

const BUILDER_LINKS = ['Schema', 'Tables', 'Queries', 'Pages', 'Import', 'Settings']

interface AppConfig {
  mode: string
  default_mode: string
  startup_object: string
  app_title: string
}

function makeApp(opts: { config?: Partial<AppConfig>; setMode?: (m: string) => Promise<void> } = {}) {
  // live mode store so SetMode + a follow-up GetAppConfig reflect the flip.
  const cfg: AppConfig = {
    mode: 'studio',
    default_mode: 'studio',
    startup_object: 'switchboard',
    app_title: 'Acme Orders',
    ...opts.config,
  }
  const app = {
    GetFileInfo: vi.fn(async () => ({ path: '/tmp/acme.vibe' })),
    GetAppConfig: vi.fn(async () => ({ ...cfg })),
    SetMode: vi.fn(async (m: string) => {
      if (opts.setMode) await opts.setMode(m)
      cfg.mode = m
    }),
    SaveFile: vi.fn(async () => {}),
    CloseFile: vi.fn(async () => {}),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app, cfg }
}

function mountShell() {
  return mount(AppShell, {
    global: {
      plugins: [router],
      stubs: { Icon: true },
    },
  })
}

// findLink locates a sidebar router-link / nav element by its visible label.
function navLinks(wrapper: any) {
  return wrapper.findAll('a, .side-link')
}
function hasLink(wrapper: any, label: string) {
  return navLinks(wrapper).some((el: any) => el.text().trim() === label || el.text().includes(label))
}

describe('AppShell mode-aware chrome (P4.6)', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })

  it('reads the live mode from GetAppConfig on mount', async () => {
    const { app } = makeApp()
    mountShell()
    await flushPromises()
    expect(app.GetAppConfig).toHaveBeenCalled()
  })

  it('Studio Mode: shows the teal Studio pill and the full builder nav', async () => {
    makeApp({ config: { mode: 'studio', default_mode: 'studio' } })
    const wrapper = mountShell()
    await flushPromises()

    expect(wrapper.text()).toContain('Studio Mode')

    // The Studio pill must carry the teal meaning-color (token or class), not
    // marigold. We accept either a `studio` modifier class or the teal token.
    const studioPill = wrapper.find('.pill .dot.studio, .pill .dot--studio, .pill.studio, .badge--teal')
    expect(studioPill.exists(), 'expected a teal Studio-mode pill').toBe(true)

    for (const label of BUILDER_LINKS) {
      expect(hasLink(wrapper, label), `expected builder link "${label}" in Studio Mode`).toBe(true)
    }
  })

  it('App Mode: shows the marigold pill + app_title and hides the builder nav', async () => {
    makeApp({ config: { mode: 'app', default_mode: 'app', app_title: 'Acme Orders' } })
    const wrapper = mountShell()
    await flushPromises()

    // marigold App-Mode pill present, teal Studio pill gone.
    expect(wrapper.text()).toContain('App Mode')
    const appPill = wrapper.find('.pill .dot.app, .pill .dot--app, .pill.app, .badge--marigold')
    expect(appPill.exists(), 'expected a marigold App-mode pill').toBe(true)

    // app_title surfaced in App Mode chrome.
    expect(wrapper.text()).toContain('Acme Orders')

    // builder nav links must NOT be reachable in App Mode.
    for (const label of BUILDER_LINKS) {
      expect(hasLink(wrapper, label), `builder link "${label}" must be hidden in App Mode`).toBe(false)
    }
  })

  it('toggling to App Mode calls SetMode("app") and swaps to locked chrome', async () => {
    const { app } = makeApp({ config: { mode: 'studio', default_mode: 'studio' } })
    const wrapper = mountShell()
    await flushPromises()

    const toggle = wrapper.findAll('button').find((b: any) => /app mode|switch to app|enter app/i.test(b.text()))
    expect(toggle, 'expected a Studio→App mode toggle control').toBeTruthy()
    await toggle!.trigger('click')
    await flushPromises()

    expect(app.SetMode).toHaveBeenCalledWith('app')

    // After flipping, the builder nav is locked away.
    for (const label of BUILDER_LINKS) {
      expect(hasLink(wrapper, label), `builder link "${label}" must be hidden after flip to App Mode`).toBe(false)
    }
  })

  it('returning to Studio Mode goes through the unlock entrypoint (P4.4)', async () => {
    const { app } = makeApp({ config: { mode: 'app', default_mode: 'app' } })
    const wrapper = mountShell()
    await flushPromises()

    const toggle = wrapper.findAll('button').find((b: any) => /studio mode|switch to studio|unlock|exit app/i.test(b.text()))
    expect(toggle, 'expected an App→Studio (unlock) toggle control').toBeTruthy()
    await toggle!.trigger('click')
    await flushPromises()

    // Returning to Studio must route through the unlock prompt rather than
    // immediately calling SetMode('studio'): an unlock entrypoint must be shown.
    const unlock = wrapper.find('.unlock-prompt, [data-testid="unlock-prompt"], input[type="password"]')
    expect(unlock.exists(), 'expected the unlock entrypoint when returning to Studio').toBe(true)
  })
})
