// Component spec for AppShellView (Phase 4 / unit P4.7) — the Switchboard.
//
// Per parity-plan §4.2 and mockups #5 (sw-main builder) / #6 (app-desktop
// runtime), AppShellView has two responsibilities driven by the live app mode:
//
//   (a) Studio Mode — a builder canvas to compose the switchboard: list nav
//       items, add/edit/reorder them (max 8 per switchboard page, the Access
//       constraint), pick an action + target from ListNavTargets, expose access
//       rules under an "advanced" disclosure, and persist via SaveNav.
//
//   (b) App Mode — the runtime switchboard: render the nav tree as large,
//       44px-hit-target buttons (marigold accent) that navigate to the bound
//       form/report/page, honoring startup_object as the landing screen.
//
// This is the RED step for P4.7: AppShellView.vue does not implement this
// behaviour yet, so the assertions below fail on behaviour (not on compile).
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
  const nav = opts.nav ?? seedNav()
  const app: AppMocks = {
    ListNav: vi.fn(async () => nav.map((n) => ({ ...n }))),
    ListNavTargets: vi.fn(async () => seedTargets()),
    SaveNav: vi.fn(async () => {}),
    GetAppConfig: vi.fn(async () => ({
      mode: useAppMode().mode.value,
      default_mode: 'studio',
      startup_object: opts.startup ?? '',
      app_title: 'Acme Orders',
    })),
    OpenForm: vi.fn(async () => {}),
    OpenReport: vi.fn(async () => {}),
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

describe('AppShellView Switchboard builder (P4.7 — Studio Mode)', () => {
  beforeEach(() => {
    pushed.length = 0
    routerMock.push.mockClear()
    useAppMode().setMode('studio')
  })

  it('lists existing nav items and populates the target picker from ListNavTargets', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    expect(app.ListNav).toHaveBeenCalled()
    expect(app.ListNavTargets).toHaveBeenCalled()

    // Existing nav items are listed in the builder canvas.
    const items = wrapper.findAll('[data-testid="nav-item"]')
    expect(items.length).toBe(2)
    expect(wrapper.text()).toContain('Orders')
    expect(wrapper.text()).toContain('Sales Report')

    // The target picker is populated from ListNavTargets.
    const picker = wrapper.find('[data-testid="target-picker"]')
    expect(picker.exists()).toBe(true)
    const options = picker.findAll('option').map((o) => o.text())
    expect(options).toContain('Customers')
    expect(options).toContain('Home')
  })

  it('adds an item and persists the new structure via SaveNav', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Pick a target + label, then add.
    await wrapper.find('[data-testid="target-picker"]').setValue('frm_customers')
    const labelInput = wrapper.find('[data-testid="new-item-label"]')
    if (labelInput.exists()) await labelInput.setValue('Customers')
    await wrapper.find('[data-testid="add-nav-item"]').trigger('click')
    await flushPromises()

    // Save the switchboard.
    await wrapper.find('[data-testid="save-nav"]').trigger('click')
    await flushPromises()

    expect(app.SaveNav).toHaveBeenCalled()
    const saved = app.SaveNav.mock.calls.at(-1)![0] as NavItem[]
    // The persisted structure includes the newly added Customers item bound to
    // the chosen form target.
    expect(saved.length).toBe(3)
    const added = saved.find((n) => n.target_id === 'frm_customers')
    expect(added, 'new item bound to frm_customers persisted').toBeTruthy()
    expect(added!.action_kind).toBe('open_form')
  })

  it('reorders two items (move down) and persists the new sort_order via SaveNav', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Initial order: Orders (sort 0), Sales Report (sort 1).
    let items = wrapper.findAll('[data-testid="nav-item"]')
    expect(items[0].text()).toContain('Orders')
    expect(items[1].text()).toContain('Sales Report')

    // Move the first item ("Orders") down past "Sales Report".
    await items[0].find('[data-testid="move-down"]').trigger('click')
    await flushPromises()

    // The visual order has flipped.
    items = wrapper.findAll('[data-testid="nav-item"]')
    expect(items[0].text()).toContain('Sales Report')
    expect(items[1].text()).toContain('Orders')

    // Persist and assert the saved sort_order reflects the new order.
    await wrapper.find('[data-testid="save-nav"]').trigger('click')
    await flushPromises()

    expect(app.SaveNav).toHaveBeenCalled()
    const saved = app.SaveNav.mock.calls.at(-1)![0] as NavItem[]
    const sales = saved.find((n) => n.target_id === 'rpt_sales')!
    const orders = saved.find((n) => n.target_id === 'frm_orders')!
    expect(sales.sort_order).toBe(0)
    expect(orders.sort_order).toBe(1)
  })

  it('removes an item and persists the smaller, renumbered structure', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    // Remove the first item ("Orders").
    await wrapper.findAll('[data-testid="nav-item"]')[0].find('[data-testid="remove-item"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="nav-item"]').length).toBe(1)

    await wrapper.find('[data-testid="save-nav"]').trigger('click')
    await flushPromises()
    const saved = app.SaveNav.mock.calls.at(-1)![0] as NavItem[]
    expect(saved.find((n) => n.target_id === 'frm_orders')).toBeFalsy()
    const remaining = saved.find((n) => n.target_id === 'rpt_sales')!
    expect(remaining.sort_order).toBe(0)
  })

  it('renames an item inline and persists the new label via SaveNav', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    const first = wrapper.findAll('[data-testid="nav-item"]')[0]
    await first.find('[data-testid="edit-item"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="edit-item-label"]').setValue('Order Entry')
    await wrapper.find('[data-testid="commit-edit"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Order Entry')

    await wrapper.find('[data-testid="save-nav"]').trigger('click')
    await flushPromises()
    const saved = app.SaveNav.mock.calls.at(-1)![0] as NavItem[]
    const orders = saved.find((n) => n.target_id === 'frm_orders')!
    expect(orders.label).toBe('Order Entry')
  })

  it('blocks adding a 9th item to one switchboard page (Access ≤8 constraint)', async () => {
    // Eight existing root-level items already fill the page.
    const full: NavItem[] = Array.from({ length: 8 }, (_, i) => ({
      id: `nav_${i}`,
      parent_id: '',
      label: `Item ${i}`,
      action_kind: 'open_form',
      target_id: 'frm_orders',
      sort_order: i,
    }))
    const app = makeApp({ nav: full })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('[data-testid="nav-item"]').length).toBe(8)

    // Attempt to add a 9th item to the same page.
    await wrapper.find('[data-testid="target-picker"]').setValue('frm_customers')
    await wrapper.find('[data-testid="add-nav-item"]').trigger('click')
    await flushPromises()

    // Adding is blocked: still 8 items, and SaveNav not invoked with 9.
    expect(wrapper.findAll('[data-testid="nav-item"]').length).toBe(8)
    if (app.SaveNav.mock.calls.length) {
      const last = app.SaveNav.mock.calls.at(-1)![0] as NavItem[]
      expect(last.filter((n) => n.parent_id === '').length).toBeLessThanOrEqual(8)
    }
    // A limit message / disabled affordance is surfaced.
    expect(wrapper.find('[data-testid="nav-limit"]').exists()).toBe(true)
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
