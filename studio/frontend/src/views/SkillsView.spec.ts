// SkillsView (#62) — lists built-in + file skills, shows a rendered/raw
// detail pane, and lets the user create/edit/delete file skills. Built-ins
// must render read-only (no edit/delete affordance).
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'

// SkillsView listens for data:changed (7a) to refresh after an import
// elsewhere writes/refreshes a workspace-<table> skill.
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (_n: string, _cb: (p: any) => void) => () => {},
}))

import SkillsView from './SkillsView.vue'
import { resolveConfirmChoice } from '../composables/useConfirm'
import { flushAll, _resetForTests as resetSaves } from '../composables/usePendingSaves'

const SKILLS = [
  { name: 'data-profiling', description: 'Profile a table before answering.', instructions: '# Data profiling\n\nDo stuff.', source: 'built-in' },
  { name: 'workspace-sales', description: 'Facts about sales.', instructions: '# sales\n\n## Columns as stored\n\n- `month` (VARCHAR)', source: 'import', table_name: 'sales' },
  { name: 'revenue-rules', description: 'Revenue is in USD thousands.', instructions: 'Revenue is reported in USD thousands.', source: 'user' },
]

function makeApp(overrides: MockApp = {}) {
  const app: MockApp = {
    ListSkills: vi.fn(async () => SKILLS.map((s) => ({ ...s }))),
    SaveSkill: vi.fn(async () => {}),
    DeleteSkill: vi.fn(async () => {}),
    ...overrides,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

async function mountView() {
  await router.push('/skills')
  await router.isReady()
  const wrapper = mount(SkillsView, { global: { plugins: [router], stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}

describe('SkillsView', () => {
  beforeEach(() => vi.clearAllMocks())

  it('lists built-in, import, and user skills with source badges', async () => {
    makeApp()
    const wrapper = await mountView()
    const text = wrapper.text()
    expect(text).toContain('data-profiling')
    expect(text).toContain('workspace-sales')
    expect(text).toContain('revenue-rules')
    expect(text).toContain('built-in')
    expect(text).toContain('Imported')
    expect(text).toContain('user')
  })

  it('shows table gating info for an import skill', async () => {
    makeApp()
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('sales')
  })

  it('renders the selected skill body as sanitized HTML by default', async () => {
    makeApp()
    const wrapper = await mountView()
    const rendered = wrapper.find('[data-testid="skill-rendered"]')
    expect(rendered.exists()).toBe(true)
    expect(rendered.html()).toContain('Data profiling')
  })

  it('toggles to raw markdown view', async () => {
    makeApp()
    const wrapper = await mountView()
    await wrapper.find('[data-testid="skill-view-raw"]').trigger('click')
    const raw = wrapper.find('[data-testid="skill-raw"]')
    expect(raw.exists()).toBe(true)
    expect(raw.text()).toContain('# Data profiling')
  })

  it('does not offer edit/delete controls for a built-in skill', async () => {
    makeApp()
    const wrapper = await mountView()
    const row = wrapper.find('[data-skill-name="data-profiling"]')
    expect(row.find('button[title="Edit skill"]').exists()).toBe(false)
    expect(row.find('button[title="Delete skill"]').exists()).toBe(false)
  })

  it('offers edit/delete controls for a user skill', async () => {
    makeApp()
    const wrapper = await mountView()
    const row = wrapper.find('[data-skill-name="revenue-rules"]')
    expect(row.find('button[title="Edit skill"]').exists()).toBe(true)
    expect(row.find('button[title="Delete skill"]').exists()).toBe(true)
  })

  it('creating a skill calls SaveSkill with the form fields', async () => {
    const app = makeApp()
    const wrapper = await mountView()
    await wrapper.find('[data-testid="skill-new"]').trigger('click')
    await wrapper.find('[data-testid="skill-name-input"]').setValue('new-skill')
    await wrapper.find('[data-testid="skill-description-input"]').setValue('A new skill')
    await wrapper.find('[data-testid="skill-instructions-input"]').setValue('Do the thing.')
    await wrapper.find('[data-testid="skill-save"]').trigger('click')
    await flushPromises()
    expect(app.SaveSkill).toHaveBeenCalledWith('new-skill', 'A new skill', 'Do the thing.', '', '')
  })

  it('rejects an invalid skill name client-side without calling SaveSkill', async () => {
    const app = makeApp()
    const wrapper = await mountView()
    await wrapper.find('[data-testid="skill-new"]').trigger('click')
    await wrapper.find('[data-testid="skill-name-input"]').setValue('Not A Valid Name')
    await wrapper.find('[data-testid="skill-description-input"]').setValue('d')
    await wrapper.find('[data-testid="skill-instructions-input"]').setValue('i')
    await wrapper.find('[data-testid="skill-save"]').trigger('click')
    await flushPromises()
    expect(app.SaveSkill).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Lowercase letters')
  })

  it('deleting a user skill confirms then calls DeleteSkill', async () => {
    const app = makeApp()
    const wrapper = await mountView()
    const row = wrapper.find('[data-skill-name="revenue-rules"]')
    await row.find('button[title="Delete skill"]').trigger('click')
    await flushPromises()
    // ConfirmDialog is rendered; simulate confirm via its Confirm button.
    const confirmBtn = wrapper.findAll('button').find((b) => b.text() === 'Delete')
    expect(confirmBtn, 'expected a ConfirmDialog Delete button').toBeTruthy()
    await confirmBtn!.trigger('click')
    await flushPromises()
    expect(app.DeleteSkill).toHaveBeenCalledWith('revenue-rules')
  })

  describe('pending saves (switch / close / quit flush the open skill editor)', () => {
    beforeEach(() => resetSaves())

    it('flushAll saves an edited, open skill; an untouched editor is a no-op', async () => {
      const app = makeApp()
      const wrapper = await mountView()
      expect((await flushAll()).failed).toEqual([])
      expect(app.SaveSkill).not.toHaveBeenCalled()

      const vm = wrapper.vm as any
      vm.openEdit({ name: 'revenue-rules', description: 'd', instructions: 'old', source: 'user', rev: 'r1' })
      await flushPromises()
      vm.formInstructions = 'new text'
      await flushPromises()
      expect((await flushAll()).failed).toEqual([])
      expect(app.SaveSkill).toHaveBeenCalledWith('revenue-rules', 'd', 'new text', '', 'r1')
    })

    it('a conflicting edit fails the flush so the caller asks', async () => {
      makeApp({ SaveSkill: vi.fn(async () => { throw new Error('conflict: skills/x changed on disk since it was loaded') }) })
      const wrapper = await mountView()
      const vm = wrapper.vm as any
      vm.openEdit({ name: 'revenue-rules', description: 'd', instructions: 'old', source: 'user', rev: 'r1' })
      await flushPromises()
      vm.formInstructions = 'new text'
      await flushPromises()
      const res = await flushAll()
      expect(res.failed).toHaveLength(1)
      expect(res.failed[0].error).toContain('changed on disk')
    })
  })

  it('Keep mine sends the revision on disk when the banner was raised (#57)', async () => {
    const listed = [{ ...SKILLS[2], rev: 'disk-1' }]
    const app = makeApp({
      ListSkills: vi.fn(async () => listed.map((x) => ({ ...x }))),
      SaveSkill: vi.fn(async () => { throw new Error('conflict: skills/revenue-rules changed on disk since it was loaded') }),
      ResolveConflict: vi.fn(async () => ({ id: 'revenue-rules', rev: 'disk-2', title: 'revenue-rules' })),
    })
    const wrapper = await mountView()
    const vm = wrapper.vm as any
    vm.openEdit({ ...listed[0], rev: 'stale-0' })
    await flushPromises()
    vm.formInstructions = 'mine'
    await vm.submitEditor()
    await flushPromises()
    expect(wrapper.find('[data-testid="conflict-banner"]').exists()).toBe(true)
    await wrapper.find('[data-testid="conflict-overwrite"]').trigger('click')
    await flushPromises()
    resolveConfirmChoice('confirm')
    await flushPromises()
    expect((app as any).ResolveConflict).toHaveBeenCalledWith(expect.objectContaining({ kind: 'skill', id: 'revenue-rules', action: 'overwrite', rev: 'disk-1' }))
  })

  it('a refused Keep mine re-fetches the rev; choosing again sends the new one and succeeds', async () => {
    let diskRev = 'disk-1'
    const base = SKILLS[2]
    const app = makeApp({
      ListSkills: vi.fn(async () => [{ ...base, rev: diskRev }]),
      SaveSkill: vi.fn(async () => { throw new Error('conflict: skills/revenue-rules changed on disk since it was loaded') }),
      ResolveConflict: vi.fn()
        .mockImplementationOnce(async () => { diskRev = 'disk-2'; throw new Error('conflict: skills/revenue-rules changed on disk since it was loaded') })
        .mockImplementationOnce(async () => ({ id: 'revenue-rules', rev: 'disk-3', title: 'revenue-rules' })),
    })
    const wrapper = await mountView()
    const vm = wrapper.vm as any
    vm.openEdit({ ...base, rev: 'stale-0' })
    await flushPromises()
    vm.formInstructions = 'mine'
    await vm.submitEditor()
    await flushPromises()
    const keepMine = async () => {
      await wrapper.find('[data-testid="conflict-overwrite"]').trigger('click')
      await flushPromises()
      resolveConfirmChoice('confirm')
      await flushPromises()
    }
    await keepMine()
    expect(app.ResolveConflict).toHaveBeenLastCalledWith(expect.objectContaining({ action: 'overwrite', rev: 'disk-1' }))
    expect(wrapper.find('[data-testid="conflict-banner"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('changed on disk again')
    await keepMine()
    expect(app.ResolveConflict).toHaveBeenLastCalledWith(expect.objectContaining({ action: 'overwrite', rev: 'disk-2' }))
    expect(wrapper.find('[data-testid="conflict-banner"]').exists()).toBe(false)
  })
})
