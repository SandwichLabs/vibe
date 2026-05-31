// Supplementary component tests for the Form Builder (parity W2).
//
// These are NOT the acceptance gate — that is Rodney driving :34115 against the
// real backend (see docs/parity-remediation-plan.md §1). These cover logic that
// is awkward to prove through the DOM: selection→properties binding, archetype
// switching re-rendering the preview, edit-then-Publish persisting via SaveForm,
// and the per-field Ask-agent round-trip rendering a real response.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'
import FormBuilderView from './FormBuilderView.vue'

const SPEC = {
  id: 'service_call',
  title: 'Service call',
  table: 'service_calls',
  archetype: 'master_detail',
  on_submit: 'refresh',
  fields: [
    { name: 'technician', caption: 'Technician', control_type: 'text', placeholder: 'Pick from team', required: true, visibility: 'Always show', validation: 'Must exist in Technicians' },
    { name: 'issue', caption: 'Issue reported', control_type: 'textarea' },
  ],
  subform: { child_table: 'parts_used', child_key: 'call_id', parent_key: 'id', fields: [{ name: 'part_no', caption: 'Part #', control_type: 'text' }] },
}

function makeApp() {
  const saved: any[] = []
  const app = {
    GetForm: vi.fn(async () => JSON.parse(JSON.stringify(SPEC))),
    SaveForm: vi.fn(async (spec: any) => { saved.push(JSON.parse(JSON.stringify(spec))) }),
    AgentChat: vi.fn(async (_m: string, _c: string) => ({ text: 'Set Visibility rule to: hide when status = "Closed".', steps: [] })),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return { app, saved }
}

async function mountBuilder() {
  await router.push('/forms/service_call/edit')
  await router.isReady()
  const wrapper = mount(FormBuilderView, { global: { plugins: [router], stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}

describe('FormBuilderView (W2)', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders the 3-pane builder with a field row per bound field', async () => {
    makeApp()
    const wrapper = await mountBuilder()
    expect(wrapper.find('[data-testid="form-builder"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="fb-field-row"]')).toHaveLength(2)
    expect(wrapper.find('[data-testid="fb-props"]').exists()).toBe(true)
  })

  it('reflects the selected field in the properties panel', async () => {
    makeApp()
    const wrapper = await mountBuilder()
    // First field auto-selected; its label populates the Label input.
    const label = wrapper.find('[data-testid="fb-prop-label"]').element as HTMLInputElement
    expect(label.value).toBe('Technician')
    const placeholder = wrapper.find('[data-testid="fb-prop-placeholder"]').element as HTMLInputElement
    expect(placeholder.value).toBe('Pick from team')

    // Select the second field → panel updates.
    await wrapper.findAll('[data-testid="fb-field-row"]')[1].trigger('click')
    expect((wrapper.find('[data-testid="fb-prop-label"]').element as HTMLInputElement).value).toBe('Issue reported')
  })

  it('re-renders the preview when the archetype changes', async () => {
    makeApp()
    const wrapper = await mountBuilder()
    const preview = () => wrapper.find('[data-testid="fb-preview"]')
    expect(preview().attributes('data-archetype')).toBe('master_detail')
    // master_detail renders the columnar grid of bound controls.
    expect(wrapper.findAll('[data-testid="fb-preview-field"]').length).toBe(2)

    // Switch to Datasheet → preview becomes a table (no columnar field cells).
    const datasheetBtn = wrapper.findAll('[data-testid="fb-arch"]').find((b) => b.attributes('data-arch') === 'datasheet')!
    await datasheetBtn.trigger('click')
    expect(preview().attributes('data-archetype')).toBe('datasheet')
    expect(wrapper.findAll('[data-testid="fb-preview-field"]').length).toBe(0)
    expect(preview().find('table').exists()).toBe(true)
  })

  it('shows Unsaved after an edit and persists via SaveForm on Publish', async () => {
    const { app, saved } = makeApp()
    const wrapper = await mountBuilder()
    expect(wrapper.find('[data-testid="fb-unsaved"]').exists()).toBe(false)

    const label = wrapper.find('[data-testid="fb-prop-label"]')
    await label.setValue('Assigned technician')
    expect(wrapper.find('[data-testid="fb-unsaved"]').exists()).toBe(true)

    await wrapper.find('[data-testid="fb-publish"]').trigger('click')
    await flushPromises()
    expect(app.SaveForm).toHaveBeenCalledTimes(1)
    expect(saved[0].fields[0].caption).toBe('Assigned technician')
    // Badge clears once the working copy matches the saved snapshot.
    expect(wrapper.find('[data-testid="fb-unsaved"]').exists()).toBe(false)
  })

  it('toggles Required and includes it in the published spec', async () => {
    const { saved } = makeApp()
    const wrapper = await mountBuilder()
    // Select the second field (not required) and toggle it on.
    await wrapper.findAll('[data-testid="fb-field-row"]')[1].trigger('click')
    await wrapper.find('[data-testid="fb-prop-required"]').trigger('click')
    await wrapper.find('[data-testid="fb-publish"]').trigger('click')
    await flushPromises()
    expect(saved[0].fields[1].required).toBe(true)
  })

  it('renders a real agent response from the per-field Ask agent box', async () => {
    const { app } = makeApp()
    const wrapper = await mountBuilder()
    await wrapper.find('[data-testid="fb-agent-prompt"]').setValue('Make this hide when status is Closed')
    await wrapper.find('[data-testid="fb-agent-apply"]').trigger('click')
    await flushPromises()
    expect(app.AgentChat).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="fb-agent-response"]').text()).toContain('hide when status')
  })
})
