// Component spec for AppShellView — the Switchboard (P4.7 runtime + W4 builder).
//
// Per parity-plan §W4 and mockup #5 (sw-main):
//
//   (a) Studio Mode — the spatial builder: a source palette (forms / reports /
//       pages), a .sw-canvas of .sw-node tiles hanging off a distinct home
//       node, a selected-item inspector (display name / icon / sort order /
//       access rules), worker preview, and "Publish shell" via SaveNav.
//
//   (b) App Mode — the runtime switchboard: large 44px-hit-target marigold
//       buttons that navigate to the bound form/report/page, honoring
//       startup_object and the W4 visible_to access rules.
//
// These are supplementary to the Rodney gate (the acceptance criterion per
// docs/parity-remediation-plan.md §1).
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import AppShellView from './AppShellView.vue'
import { useAppMode } from '../composables/useAppMode'

interface NavItem {
  id: string
  parent_id: string
  label: string
  action_kind: string
  target_id: string
  sort_order: number
  icon?: string
  visible_to?: string
}

interface NavTarget {
  kind: string
  id: string
  label: string
}

interface AppMocks {
  ListNav: ReturnType<typeof vi.fn>
  ListNavTargets: ReturnType<typeof vi.fn>
  SaveNav: ReturnType<typeof vi.fn>
  GetAppConfig: ReturnType<typeof vi.fn>
  OpenForm?: ReturnType<typeof vi.fn>
  OpenReport?: ReturnType<typeof vi.fn>
  RunMacro?: ReturnType<typeof vi.fn>
  RunAutoExec?: ReturnType<typeof vi.fn>
}

function seedNav(): NavItem[] {
  // A single root switchboard page ('') with two items.
  return [
    {
      id: 'nav_orders',
      parent_id: '',
      label: 'Orders',
      action_kind: 'open_form',
      target_id: 'frm_orders',
      sort_order: 0,
    },
    {
      id: 'nav_sales',
      parent_id: '',
      label: 'Sales Report',
      action_kind: 'open_report',
      target_id: 'rpt_sales',
      sort_order: 1,
    },
  ]
}

function seedTargets(): NavTarget[] {
  return [
    { kind: 'open_form', id: 'frm_orders', label: 'Orders' },
    { kind: 'open_form', id: 'frm_customers', label: 'Customers' },
    { kind: 'open_report', id: 'rpt_sales', label: 'Sales Report' },
    { kind: 'open_page', id: 'pg_home', label: 'Home' },
  ]
}

function makeApp(opts: { nav?: NavItem[]; startup?: string } = {}): AppMocks {
  // Back the mock with a stateful store that mirrors the Go SaveNav reconcile
  // contract: SaveNav REPLACES the persisted set with exactly the payload (rows
  // absent from the payload are deleted). ListNav reads back from that store, so
  // a remove+save genuinely round-trips and a non-reconciling save would be
  // caught here — not masked by a stateless vi.fn.
  let store: NavItem[] = (opts.nav ?? seedNav()).map((n) => ({ ...n }))
  const app: AppMocks = {
    ListNav: vi.fn(async () => store.map((n) => ({ ...n }))),
    ListNavTargets: vi.fn(async () => seedTargets()),
    SaveNav: vi.fn(async (items: NavItem[]) => {
      store = items.map((n) => ({ ...n }))
    }),
    GetAppConfig: vi.fn(async () => ({
      mode: useAppMode().mode.value,
      default_mode: 'studio',
      startup_object: opts.startup ?? '',
      app_title: 'Acme Orders',
    })),
    OpenForm: vi.fn(async () => {}),
    OpenReport: vi.fn(async () => {}),
    // AutoExec (P5.7) runs on mount; default to no startup macro.
    RunAutoExec: vi.fn(async () => ({ intents: [] })),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

// router mock: AppShellView navigates to bound objects via vue-router. Record
// pushes so we can assert runtime navigation.
const pushed: any[] = []
const routerMock = {
  push: vi.fn((to: any) => {
    pushed.push(to)
    return Promise.resolve()
  }),
}

function mountView() {
  return mount(AppShellView, {
    global: {
      stubs: { Icon: true },
      mocks: { $router: routerMock },
    },
  })
}

describe('AppShellView Switchboard builder (W4 — Studio Mode)', () => {
  beforeEach(() => {
    pushed.length = 0
    routerMock.push.mockClear()
    useAppMode().setMode('studio')
  })

  it('renders the 3-pane builder: grouped palette, canvas nodes, distinct home node, inspector', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    expect(app.ListNav).toHaveBeenCalled()
    expect(app.ListNavTargets).toHaveBeenCalled()

    // Palette groups objects by kind.
    const palette = wrapper.findAll('[data-testid="palette-target"]')
    expect(palette.length).toBe(4)
    expect(wrapper.text()).toContain('Forms')
    expect(wrapper.text()).toContain('Reports')

    // Canvas: one .sw-node per nav item + the distinct home node + arrows.
    expect(wrapper.findAll('[data-testid="sw-node"]')).toHaveLength(2)
    const home = wrapper.find('[data-testid="sw-home"]')
    expect(home.exists()).toBe(true)
    expect(home.classes()).toContain('home')
    expect(wrapper.findAll('[data-testid="sw-arrow"]')).toHaveLength(2)

    // Inspector idles until a node is selected.
    expect(wrapper.find('[data-testid="sw-inspector"]').text()).toContain('Select a tile')
  })

  it('adds an item from the palette: a sw-node appears, selected, inspector opens on it', async () => {
    makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Click the "Customers" form in the palette.
    const customers = wrapper.findAll('[data-testid="palette-target"]')
      .find((p) => p.text().includes('Customers'))!
    await customers.trigger('click')
    await flushPromises()

    const nodes = wrapper.findAll('[data-testid="sw-node"]')
    expect(nodes).toHaveLength(3)
    const added = nodes.find((n) => n.text().includes('Customers'))!
    expect(added.classes()).toContain('selected')
    expect(wrapper.find('[data-testid="sw-inspector"]').text()).toContain('Customers')
    expect(wrapper.find('[data-testid="shell-dirty"]').exists()).toBe(true)
  })

  it('inspector edits (label, icon, access rule) persist through Publish shell', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Select the Orders node.
    await wrapper.findAll('[data-testid="sw-node"]')[0].trigger('click')
    await flushPromises()

    await wrapper.find('[data-testid="insp-label"]').setValue('Order Entry')
    await wrapper.find('[data-testid="insp-icon"]').setValue('clipboard')
    await wrapper.find('[data-testid="insp-visible-to"]').setValue('owner')
    await flushPromises()

    // The canvas reflects the rule immediately: restricted node + arrow label.
    expect(wrapper.find('[data-testid="sw-canvas"]').text()).toContain('owner only')

    await wrapper.find('[data-testid="publish-shell"]').trigger('click')
    await flushPromises()

    const saved = app.SaveNav.mock.calls.at(-1)![0] as NavItem[]
    const orders = saved.find((n) => n.target_id === 'frm_orders')!
    expect(orders.label).toBe('Order Entry')
    expect(orders.icon).toBe('clipboard')
    expect(orders.visible_to).toBe('owner')

    // Round-trip through the stateful store: a remount reads the rule back.
    const reloaded = mountView()
    await flushPromises()
    expect(reloaded.find('[data-testid="sw-canvas"]').text()).toContain('owner only')
  })

  it('removes an item via the inspector and Publish reconciles the deletion', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('[data-testid="sw-node"]')[0].trigger('click')
    await wrapper.find('[data-testid="insp-remove"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="sw-node"]')).toHaveLength(1)

    await wrapper.find('[data-testid="publish-shell"]').trigger('click')
    await flushPromises()
    const saved = app.SaveNav.mock.calls.at(-1)![0] as NavItem[]
    expect(saved.find((n) => n.target_id === 'frm_orders')).toBeFalsy()
    expect(saved.find((n) => n.target_id === 'rpt_sales')!.sort_order).toBe(0)

    // Deletion survives a reload through the stateful store (reconcile guard).
    const reloaded = mountView()
    await flushPromises()
    const nodes = reloaded.findAll('[data-testid="sw-node"]')
    expect(nodes).toHaveLength(1)
    expect(nodes[0].text()).toContain('Sales Report')
  })

  it('sort order edits in the inspector reorder the canvas and persist', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Move "Orders" (sort 0) to position 1.
    await wrapper.findAll('[data-testid="sw-node"]')[0].trigger('click')
    const sortInput = wrapper.find('[data-testid="insp-sort"]')
    await sortInput.setValue('1')
    await sortInput.trigger('change')
    await flushPromises()

    const nodes = wrapper.findAll('[data-testid="sw-node"]')
    expect(nodes[0].text()).toContain('Sales Report')
    expect(nodes[1].text()).toContain('Orders')

    await wrapper.find('[data-testid="publish-shell"]').trigger('click')
    await flushPromises()
    const saved = app.SaveNav.mock.calls.at(-1)![0] as NavItem[]
    expect(saved.find((n) => n.target_id === 'rpt_sales')!.sort_order).toBe(0)
    expect(saved.find((n) => n.target_id === 'frm_orders')!.sort_order).toBe(1)
  })

  it('blocks adding a 9th item to one switchboard page (Access ≤8 constraint)', async () => {
    const full: NavItem[] = Array.from({ length: 8 }, (_, i) => ({
      id: `nav_${i}`,
      parent_id: '',
      label: `Item ${i}`,
      action_kind: 'open_form',
      target_id: 'frm_orders',
      sort_order: i,
    }))
    makeApp({ nav: full })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('[data-testid="sw-node"]')).toHaveLength(8)

    const customers = wrapper.findAll('[data-testid="palette-target"]')
      .find((p) => p.text().includes('Customers'))!
    await customers.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="sw-node"]')).toHaveLength(8)
    expect(wrapper.find('[data-testid="nav-limit"]').exists()).toBe(true)
  })

  it('worker preview hides owner-only items without flipping the mode', async () => {
    makeApp({
      nav: [
        { id: 'n1', parent_id: '', label: 'Orders', action_kind: 'open_form', target_id: 'frm_orders', sort_order: 0 },
        { id: 'n2', parent_id: '', label: 'Owner Report', action_kind: 'open_report', target_id: 'rpt_sales', sort_order: 1, visible_to: 'owner' },
      ],
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('[data-testid="preview-worker"]').trigger('click')
    await flushPromises()

    const preview = wrapper.find('[data-testid="worker-preview"]')
    expect(preview.exists()).toBe(true)
    const buttons = preview.findAll('[data-testid="preview-button"]')
    expect(buttons).toHaveLength(1)
    expect(buttons[0].text()).toContain('Orders')
    expect(preview.text()).not.toContain('Owner Report')

    // Exit returns to the canvas.
    await wrapper.find('[data-testid="preview-worker"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="sw-canvas"]').exists()).toBe(true)
  })
})

describe('AppShellView Switchboard runtime (P4.7 — App Mode)', () => {
  beforeEach(() => {
    pushed.length = 0
    routerMock.push.mockClear()
    useAppMode().setMode('app')
  })

  it('renders one large 44px-target marigold button per nav item', async () => {
    makeApp()
    const wrapper = mountView()
    await flushPromises()

    const buttons = wrapper.findAll('[data-testid="switchboard-button"]')
    expect(buttons.length).toBe(2)
    // Action labels surfaced on the runtime buttons.
    expect(wrapper.text()).toContain('Orders')
    expect(wrapper.text()).toContain('Sales Report')

    // Each button is a large 44px hit target with the marigold accent.
    const first = buttons[0]
    const cls = first.classes().join(' ')
    const style = first.attributes('style') || ''
    expect(
      /switchboard-button/.test(cls) || /marigold/.test(cls) || /marigold/.test(style),
      'expected marigold accent on switchboard button',
    ).toBe(true)
    expect(
      /44px/.test(style) || /min-height/.test(style) || /switchboard-button/.test(cls),
      'expected a 44px min hit target',
    ).toBe(true)
  })

  it('clicking a button routes to the bound form/report', async () => {
    makeApp()
    const wrapper = mountView()
    await flushPromises()

    const buttons = wrapper.findAll('[data-testid="switchboard-button"]')
    // First item → open_form frm_orders.
    await buttons[0].trigger('click')
    await flushPromises()
    expect(routerMock.push).toHaveBeenCalled()
    const firstNav = pushed.at(-1)
    expect(JSON.stringify(firstNav)).toContain('forms')
    expect(JSON.stringify(firstNav)).toContain('frm_orders')

    // Second item → open_report rpt_sales.
    await buttons[1].trigger('click')
    await flushPromises()
    const secondNav = pushed.at(-1)
    expect(JSON.stringify(secondNav)).toContain('reports')
    expect(JSON.stringify(secondNav)).toContain('rpt_sales')
  })

  it('routes open_page to the runtime page viewer, NOT the builderOnly editor', async () => {
    makeApp({
      nav: [
        {
          id: 'nav_home',
          parent_id: '',
          label: 'Home',
          action_kind: 'open_page',
          target_id: 'pg_home',
          sort_order: 0,
        },
      ],
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('[data-testid="switchboard-button"]')[0].trigger('click')
    await flushPromises()

    const nav = pushed.at(-1)
    const path = (nav as any).path as string
    // Must land on the runtime viewer (/page/pg_home), never the builderOnly
    // editor (/pages/pg_home/edit) which is locked out in App Mode.
    expect(path).toBe('/page/pg_home')
    expect(path).not.toContain('/edit')
  })

  it('runs AutoExec on mount and applies its open_switchboard startup intent (P5.7)', async () => {
    // The AutoExec macro returns an open_switchboard intent; the shell must apply
    // it through the shared dispatcher on mount (the "AutoExec opens the
    // switchboard maximized on file open" acceptance).
    const app = makeApp()
    app.RunAutoExec = vi.fn(async () => ({
      intents: [{ kind: 'open_switchboard', target: 'main' }],
    }))
    const wrapper = mountView()
    await flushPromises()

    expect(app.RunAutoExec).toHaveBeenCalledTimes(1)
    const landed = pushed.find((p) => JSON.stringify(p).includes('switchboard'))
    expect(landed, 'AutoExec open_switchboard intent must drive navigation on open').toBeTruthy()
    expect((landed as any).query.switchboard).toBe('main')
    void wrapper
  })

  it('honors startup_object as the initial switchboard / landing screen', async () => {
    // startup_object points at the report nav target; the runtime should land
    // there (navigate to it) on open.
    makeApp({ startup: 'rpt_sales' })
    const wrapper = mountView()
    await flushPromises()

    const landed = pushed.find((p) => JSON.stringify(p).includes('rpt_sales'))
    expect(landed, 'expected startup_object to drive the initial landing screen').toBeTruthy()
  })
})

describe('AppShellView Switchboard run_macro dispatch (P5.4 — App Mode)', () => {
  beforeEach(() => {
    pushed.length = 0
    routerMock.push.mockClear()
    useAppMode().setMode('app')
  })

  // A run_macro nav item: clicking it must call App.RunMacro(target_id) and
  // apply the returned UI intents. The intent shapes mocked here mirror exactly
  // what the Go backend (studio/macro_bindings.go RunMacro) actually emits:
  // navigation intents (open_form/open_report) the Executor produces from
  // UI-bound actions, a msg_box dialog intent, and a set_filter intent the Wails
  // layer forwards from a server-side SetFilter (AppliedFilter -> {kind:
  // 'set_filter', target, args:{where}}). See TestMacroBindingsSetFilterForwarded
  // for the Go-side assertion of that same contract.
  function macroNav(): NavItem[] {
    return [
      {
        id: 'nav_macro',
        parent_id: '',
        label: 'Run Backup',
        action_kind: 'run_macro',
        target_id: 'm1',
        sort_order: 0,
      },
    ]
  }

  it('invokes App.RunMacro and routes on an open_form intent', async () => {
    const app = makeApp({ nav: macroNav() })
    app.RunMacro = vi.fn(async () => ({
      intents: [{ kind: 'open_form', target: 'frm_orders', args: {} }],
    }))

    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('[data-testid="switchboard-button"]')[0].trigger('click')
    await flushPromises()

    // RunMacro called with the item's target_id.
    expect(app.RunMacro).toHaveBeenCalledWith('m1')

    // The returned open_form intent is applied as a route push to /forms?id=...
    expect(routerMock.push).toHaveBeenCalled()
    const nav = pushed.at(-1)
    expect(JSON.stringify(nav)).toContain('forms')
    expect(JSON.stringify(nav)).toContain('frm_orders')
  })

  it('renders a dialog for a msg_box intent', async () => {
    const app = makeApp({ nav: macroNav() })
    app.RunMacro = vi.fn(async () => ({
      intents: [{ kind: 'msg_box', target: '', args: { message: 'Backup complete' } }],
    }))

    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('[data-testid="switchboard-button"]')[0].trigger('click')
    await flushPromises()

    expect(app.RunMacro).toHaveBeenCalledWith('m1')
    // A MsgBox dialog surfaces with the message text.
    const dialog = wrapper.find('[data-testid="macro-msgbox"]')
    expect(dialog.exists(), 'expected a MsgBox dialog to render for a msg_box intent').toBe(true)
    expect(dialog.text()).toContain('Backup complete')
  })

  it('applies a set_filter intent as a route query when opening a form', async () => {
    const app = makeApp({ nav: macroNav() })
    app.RunMacro = vi.fn(async () => ({
      intents: [
        { kind: 'open_form', target: 'frm_orders', args: {} },
        { kind: 'set_filter', target: 'frm_orders', args: { where: 'total > 50' } },
      ],
    }))

    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('[data-testid="switchboard-button"]')[0].trigger('click')
    await flushPromises()

    expect(app.RunMacro).toHaveBeenCalledWith('m1')
    // The set_filter intent must be reflected in the pushed route query.
    const matched = pushed.find((p) => JSON.stringify(p).includes('total > 50'))
    expect(matched, 'expected set_filter applied as a route query').toBeTruthy()
  })
})

describe('AppShellView access rules (W4 — App Mode)', () => {
  beforeEach(() => {
    pushed.length = 0
    routerMock.push.mockClear()
    useAppMode().setMode('app')
  })

  it('hides owner-only items from the runtime switchboard', async () => {
    makeApp({
      nav: [
        { id: 'n1', parent_id: '', label: 'Orders', action_kind: 'open_form', target_id: 'frm_orders', sort_order: 0 },
        { id: 'n2', parent_id: '', label: 'Owner Report', action_kind: 'open_report', target_id: 'rpt_sales', sort_order: 1, visible_to: 'owner' },
      ],
    })
    const wrapper = mountView()
    await flushPromises()

    const buttons = wrapper.findAll('[data-testid="switchboard-button"]')
    expect(buttons).toHaveLength(1)
    expect(buttons[0].text()).toContain('Orders')
    expect(wrapper.text()).not.toContain('Owner Report')
  })
})
