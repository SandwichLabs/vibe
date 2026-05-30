// Integration spec for P5.6 — Command-button runtime wiring in the page/form
// Vue runtime.
//
// Per the parity plan §5.2/§5.4 and the P5.5 button markup
// (internal/renderer/html/controls.go RenderButton), the page runtime renders
// command buttons as
//   <button ... class="vibe-button" data-macro-id="<macro>" data-action="<macro>">…</button>
// with NO inline JS. P5.6 makes the Vue runtime DETECT those rendered buttons
// (the data-macro-id hook) and bind their click to App.RunMacro(target),
// applying the returned ordered UI intents through the SAME intent-applier the
// AppShell switchboard uses (P5.4) — extracted into a shared composable
// (useMacroIntents.ts) so there is exactly one dispatcher.
//
// This is the RED step: PageRuntimeView.vue currently only v-html's the rendered
// page and calls execScripts; it does NOT find data-macro-id buttons, never
// calls RunMacro, and there is no shared intent dispatcher yet. The assertions
// below therefore fail on BEHAVIOUR (not on compile).
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

// PageRuntimeView reads the page id via vue-router's useRoute(). Stub the module
// so the view mounts without standing up a full router; useRouter() returns the
// recording mock below so the shared intent applier's navigation is observable.
const routeMock = { params: { id: 'pg_reports' }, query: { id: 'pg_reports' } }
const pushed: any[] = []
const routerMock = {
  push: vi.fn((to: any) => {
    pushed.push(to)
    return Promise.resolve()
  }),
}
vi.mock('vue-router', () => ({
  useRoute: () => routeMock,
  useRouter: () => routerMock,
}))

import PageRuntimeView from './PageRuntimeView.vue'

interface AppMocks {
  RenderPageContent: ReturnType<typeof vi.fn>
  RunMacro: ReturnType<typeof vi.fn>
}

// The exact button markup the Go renderer emits for {button: …, action: …}.
function buttonHtml(macroId: string, label: string): string {
  return (
    `<p>Reports dashboard</p>` +
    `<button type="button" class="vibe-button" ` +
    `data-macro-id="${macroId}" data-action="${macroId}">${label}</button>`
  )
}

function makeApp(html: string, runMacro: AppMocks['RunMacro']): AppMocks {
  const app: AppMocks = {
    RenderPageContent: vi.fn(async () => html),
    RunMacro: runMacro,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

function mountView() {
  return mount(PageRuntimeView, {
    global: {
      stubs: { Icon: true },
    },
  })
}

describe('PageRuntimeView command-button runtime wiring (P5.6)', () => {
  beforeEach(() => {
    pushed.length = 0
    routerMock.push.mockClear()
  })

  it('clicking a rendered command button calls RunMacro and applies [SetFilter, OpenReport] intents through the shared applier', async () => {
    // Stub RunMacro to resolve an ordered intent list: a set_filter scoped to the
    // report target, then an open_report. The shared applier must merge the
    // filter as a `where` route query onto the report navigation (the same
    // contract AppShellView uses).
    const runMacro = vi.fn(async () => ({
      intents: [
        { kind: 'set_filter', target: 'rpt_sales', args: { where: 'total > 50' } },
        { kind: 'open_report', target: 'rpt_sales', args: {} },
      ],
    }))
    const app = makeApp(buttonHtml('m2', 'Open Filtered Report'), runMacro)

    const wrapper = mountView()
    await flushPromises()

    // The rendered command button (data-macro-id hook from P5.5) is present.
    const btn = wrapper.find('button[data-macro-id="m2"]')
    expect(btn.exists(), 'rendered command button must be present').toBe(true)

    // Click it: the runtime must call RunMacro with the button's macro target.
    await btn.trigger('click')
    await flushPromises()

    expect(app.RunMacro).toHaveBeenCalledWith('m2')

    // The shared intent applier (P5.4 composable) pushes to /reports with the
    // set_filter where-clause merged onto the navigation.
    expect(routerMock.push).toHaveBeenCalled()
    const nav = pushed.find((p) => JSON.stringify(p).includes('reports'))
    expect(nav, 'expected open_report navigation to /reports').toBeTruthy()
    expect(JSON.stringify(nav)).toContain('rpt_sales')
    expect(JSON.stringify(nav), 'expected set_filter merged as a route query').toContain('total > 50')
  })

  it('a MsgBox intent shows the dialog and the button is disabled while RunMacro is in-flight', async () => {
    // A deferred RunMacro lets us observe the in-flight guard before resolution.
    let resolveRun: (v: unknown) => void = () => {}
    const runMacro = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveRun = resolve
        }),
    )
    const app = makeApp(buttonHtml('m2', 'Notify'), runMacro as unknown as AppMocks['RunMacro'])

    const wrapper = mountView()
    await flushPromises()

    const btn = wrapper.find('button[data-macro-id="m2"]')
    expect(btn.exists()).toBe(true)

    await btn.trigger('click')
    await flushPromises()

    expect(app.RunMacro).toHaveBeenCalledWith('m2')

    // In-flight guard: the button is disabled while the macro is running so a
    // second click cannot double-fire.
    expect(
      (btn.element as HTMLButtonElement).disabled,
      'command button must be disabled while RunMacro is in-flight',
    ).toBe(true)

    // A second click while in-flight must NOT invoke RunMacro again.
    await btn.trigger('click')
    await flushPromises()
    expect(app.RunMacro).toHaveBeenCalledTimes(1)

    // Resolve with a msg_box intent: the shared applier raises the MsgBox dialog.
    resolveRun({
      intents: [{ kind: 'msg_box', target: '', args: { message: 'Done' } }],
    })
    await flushPromises()

    const dialog = wrapper.find('[data-testid="macro-msgbox"]')
    expect(dialog.exists(), 'expected a MsgBox dialog for a msg_box intent').toBe(true)
    expect(dialog.text()).toContain('Done')

    // Guard released after completion: the button is interactive again.
    expect((btn.element as HTMLButtonElement).disabled).toBe(false)
  })
})
