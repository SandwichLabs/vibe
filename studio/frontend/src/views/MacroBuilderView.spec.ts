// Component spec for MacroBuilderView (Phase 5 / unit P5.9) — the Macro builder.
//
// Per the parity-plan §5 and the agent-as-Wizard/builder mockups, the
// MacroBuilderView is the Studio-only surface that translates Access's macro
// builder into the SchoolHouse Vue system. It must:
//   - list the container's stored macros (ListMacro) and let the builder pick one
//   - add / reorder / remove ordered actions, each with a kind picker and a
//     target picker whose options are catalog-backed
//     (ListNavTargets / ListForms / ListReports / ListMacro)
//   - persist the edited macro via SaveMacro(spec) with a well-formed MacroSpec
//   - test-run the macro via RunMacro(id) (the "Run" button)
//
// It is BUILDER-ONLY: it must be unreachable in App Mode (route guarded) and
// absent from the App-Mode chrome.
//
// This is the RED step for P5.9: MacroBuilderView.vue does not exist yet, nor is
// there a /macros route. The import below + the assertions therefore fail
// because the production symbol is missing — the minimal stub added to make this
// compile must still fail on BEHAVIOUR, not implement the real builder.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import MacroBuilderView from './MacroBuilderView.vue'

interface MacroAction {
  kind: string
  args?: Record<string, string>
}

interface MacroSpec {
  id: string
  title: string
  version: number
  actions: MacroAction[]
}

interface NavTarget {
  kind: string
  id: string
  label: string
}

function seedMacros(): MacroSpec[] {
  return [
    {
      id: 'm_open_orders',
      title: 'Open Orders',
      version: 1,
      actions: [],
    },
  ]
}

function seedNavTargets(): NavTarget[] {
  return [
    { kind: 'form', id: 'frm_orders', label: 'Orders' },
    { kind: 'report', id: 'rpt_sales', label: 'Sales Report' },
  ]
}

function makeApp(over: Record<string, any> = {}) {
  const app: Record<string, any> = {
    ListMacro: vi.fn(async () => seedMacros()),
    SaveMacro: vi.fn(async (_spec: MacroSpec) => {}),
    DeleteMacro: vi.fn(async (_id: string) => {}),
    RunMacro: vi.fn(async (_id: string) => ({ intents: [] })),
    ListNavTargets: vi.fn(async () => seedNavTargets()),
    ListForms: vi.fn(async () => [{ id: 'frm_orders', title: 'Orders' }]),
    ListReports: vi.fn(async () => [{ id: 'rpt_sales', title: 'Sales Report' }]),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

function mountView() {
  return mount(MacroBuilderView, {
    global: {
      stubs: { Icon: true },
    },
  })
}

describe('MacroBuilderView (P5.9)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists the stored macros on mount via ListMacro', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    expect(app.ListMacro).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Open Orders')
  })

  it('adds an OpenForm action + a SetFilter, then SaveMacro receives a well-formed MacroSpec', async () => {
    const saved: MacroSpec[] = []
    const app = makeApp({
      SaveMacro: vi.fn(async (spec: MacroSpec) => {
        saved.push(JSON.parse(JSON.stringify(spec)))
      }),
    })

    const wrapper = mountView()
    await flushPromises()

    // The target pickers must be populated from the catalog bindings.
    expect(app.ListNavTargets).toHaveBeenCalled()

    // Select the macro to edit.
    const macroSelect = wrapper.find('[data-testid="macro-select"]')
    expect(macroSelect.exists(), 'expected a macro picker').toBe(true)
    await (macroSelect as any).setValue('m_open_orders')
    await flushPromises()

    // Add an OpenForm action.
    const addBtn = wrapper.find('[data-testid="add-action"]')
    expect(addBtn.exists(), 'expected an "add action" control').toBe(true)
    await addBtn.trigger('click')
    await flushPromises()

    // Configure the new action: kind=OpenForm, target=frm_orders.
    const kind0 = wrapper.find('[data-testid="action-kind-0"]')
    expect(kind0.exists(), 'expected a kind picker for action 0').toBe(true)
    await (kind0 as any).setValue('OpenForm')

    const target0 = wrapper.find('[data-testid="action-target-0"]')
    expect(target0.exists(), 'expected a catalog-backed target picker for action 0').toBe(true)
    // The target options must come from the catalog (frm_orders).
    expect(target0.html()).toContain('frm_orders')
    await (target0 as any).setValue('frm_orders')

    // Add a SetFilter action.
    await addBtn.trigger('click')
    await flushPromises()
    const kind1 = wrapper.find('[data-testid="action-kind-1"]')
    expect(kind1.exists(), 'expected a kind picker for action 1').toBe(true)
    await (kind1 as any).setValue('SetFilter')
    const where1 = wrapper.find('[data-testid="action-where-1"]')
    expect(where1.exists(), 'expected a where/arg input for the SetFilter action').toBe(true)
    await (where1 as any).setValue('status = \'open\'')

    // Save.
    const saveBtn = wrapper.find('[data-testid="save-macro"]')
    expect(saveBtn.exists(), 'expected a save button').toBe(true)
    await saveBtn.trigger('click')
    await flushPromises()

    // SaveMacro must have received a well-formed MacroSpec.
    expect(app.SaveMacro).toHaveBeenCalledTimes(1)
    expect(saved.length).toBe(1)
    const spec = saved[0]
    expect(spec.id).toBe('m_open_orders')
    expect(Array.isArray(spec.actions)).toBe(true)
    expect(spec.actions.length).toBe(2)
    expect(spec.actions[0].kind).toBe('OpenForm')
    expect(spec.actions[0].args?.target ?? spec.actions[0].args?.form).toBe('frm_orders')
    expect(spec.actions[1].kind).toBe('SetFilter')
    expect(JSON.stringify(spec.actions[1].args)).toContain("status = 'open'")
  })

  it('the Run button test-runs the selected macro via RunMacro', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await flushPromises()

    const macroSelect = wrapper.find('[data-testid="macro-select"]')
    await (macroSelect as any).setValue('m_open_orders')
    await flushPromises()

    const runBtn = wrapper.find('[data-testid="run-macro"]')
    expect(runBtn.exists(), 'expected a Run-to-test button').toBe(true)
    await runBtn.trigger('click')
    await flushPromises()

    expect(app.RunMacro).toHaveBeenCalledWith('m_open_orders')
  })

  it('reorders an action (move up) so the persisted order changes', async () => {
    const saved: MacroSpec[] = []
    const app = makeApp({
      ListMacro: vi.fn(async () => [
        {
          id: 'm_open_orders',
          title: 'Open Orders',
          version: 1,
          actions: [
            { kind: 'SetFilter', args: { where: 'a' } },
            { kind: 'OpenForm', args: { target: 'frm_orders' } },
          ],
        },
      ]),
      SaveMacro: vi.fn(async (spec: MacroSpec) => {
        saved.push(JSON.parse(JSON.stringify(spec)))
      }),
    })

    const wrapper = mountView()
    await flushPromises()
    await (wrapper.find('[data-testid="macro-select"]') as any).setValue('m_open_orders')
    await flushPromises()

    // Move action 1 (OpenForm) up above action 0 (SetFilter).
    const moveUp1 = wrapper.find('[data-testid="action-up-1"]')
    expect(moveUp1.exists(), 'expected a move-up control on action 1').toBe(true)
    await moveUp1.trigger('click')
    await flushPromises()

    await wrapper.find('[data-testid="save-macro"]').trigger('click')
    await flushPromises()

    expect(saved.length).toBe(1)
    expect(saved[0].actions[0].kind).toBe('OpenForm')
    expect(saved[0].actions[1].kind).toBe('SetFilter')
  })

  it('removes an action so it is dropped from the persisted spec', async () => {
    const saved: MacroSpec[] = []
    const app = makeApp({
      ListMacro: vi.fn(async () => [
        {
          id: 'm_open_orders',
          title: 'Open Orders',
          version: 1,
          actions: [
            { kind: 'OpenForm', args: { target: 'frm_orders' } },
            { kind: 'MsgBox', args: { message: 'hi' } },
          ],
        },
      ]),
      SaveMacro: vi.fn(async (spec: MacroSpec) => {
        saved.push(JSON.parse(JSON.stringify(spec)))
      }),
    })

    const wrapper = mountView()
    await flushPromises()
    await (wrapper.find('[data-testid="macro-select"]') as any).setValue('m_open_orders')
    await flushPromises()

    const removeBtn = wrapper.find('[data-testid="action-remove-1"]')
    expect(removeBtn.exists(), 'expected a remove control on action 1').toBe(true)
    await removeBtn.trigger('click')
    await flushPromises()

    await wrapper.find('[data-testid="save-macro"]').trigger('click')
    await flushPromises()

    expect(saved.length).toBe(1)
    expect(saved[0].actions.length).toBe(1)
    expect(saved[0].actions[0].kind).toBe('OpenForm')
  })
})
