import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {}, EventsOff: () => {} }))

import NewWorkspaceDialog from './NewWorkspaceDialog.vue'
import { newWorkspaceOpen, workspaceInfo, _resetForTests } from '../composables/useWorkspace'

function makeApp(over: Record<string, any> = {}) {
  const app = {
    CheckGitAvailable: vi.fn(async () => true),
    ListTemplates: vi.fn(async () => [{ name: 'contacts', description: 'Contacts' }, { name: 'order-entry', description: 'Order entry' }]),
    CheckWorkspaceDir: vi.fn(async (parent: string, name: string) => ({ dir: `${parent}/${name}`, exists: false, notEmpty: false })),
    ChooseFolder: vi.fn(async () => '/picked'),
    CreateWorkspace: vi.fn(async (req: any) => ({ status: 'opened', path: '/work/acme', info: { root: '/work/acme', name: req.name, sessionId: 's' } })),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

const mountDialog = async () => {
  const w = mount(NewWorkspaceDialog, { attachTo: document.body, global: { stubs: { Icon: true } } })
  newWorkspaceOpen.value = true
  await flushPromises()
  return w
}

describe('NewWorkspaceDialog', () => {
  beforeEach(() => { _resetForTests(); document.body.innerHTML = ''; try { localStorage.clear() } catch { /* */ } })

  it('opens as a dialog with focus in Name, git on, and Blank selected', async () => {
    makeApp()
    const w = await mountDialog()
    expect(w.find('[role="dialog"]').attributes('aria-labelledby')).toBe('nw-h')
    expect(document.activeElement).toBe(w.find('#nw-name').element)
    expect((w.find('#nw-git').element as HTMLInputElement).checked).toBe(true)
    expect((w.find('[data-testid="nw-template-blank"]').element as HTMLInputElement).checked).toBe(true)
    expect(w.text()).toContain('Order entry')
    expect(w.text()).toContain('Customers, orders and order lines, with a starter notebook.')
    expect((w.find('[data-testid="nw-create"]').element as HTMLButtonElement).disabled).toBe(true)
    w.unmount()
  })

  it('previews the folder it will create and creates with the chosen options', async () => {
    const app = makeApp()
    const w = await mountDialog()
    await w.find('#nw-name').setValue('Acme Analytics')
    await w.find('[data-testid="nw-location"]').setValue('/work')
    await w.find('[data-testid="nw-template-order-entry"]').setValue(true)
    await flushPromises()
    expect(w.find('.url-tile').text()).toContain('/work/acme-analytics')
    await w.find('[data-testid="nw-create"]').trigger('click')
    await flushPromises()
    expect(app.CreateWorkspace).toHaveBeenCalledWith({
      name: 'Acme Analytics', dir: '', parentDir: '/work', git: true, template: 'order-entry', inPlace: false,
    })
    expect(newWorkspaceOpen.value).toBe(false)
    expect(workspaceInfo.value?.name).toBe('Acme Analytics')
    w.unmount()
  })

  it('Enter in a text field submits when valid', async () => {
    const app = makeApp()
    const w = await mountDialog()
    await w.find('#nw-name').setValue('Quick')
    await w.find('[data-testid="nw-location"]').setValue('/work')
    await flushPromises()
    await w.find('#nw-name').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(app.CreateWorkspace).toHaveBeenCalled()
    w.unmount()
  })

  it('an existing non-empty folder shows the inline error, marks the field invalid and disables Create', async () => {
    makeApp({ CheckWorkspaceDir: vi.fn(async (p: string, n: string) => ({ dir: `${p}/${n}`, exists: true, notEmpty: true })) })
    const w = await mountDialog()
    await w.find('#nw-name').setValue('Acme Analytics')
    await w.find('[data-testid="nw-location"]').setValue('/work')
    await flushPromises()
    expect(w.find('[data-testid="nw-folder-exists"]').text()).toContain("There's already a folder called acme-analytics in /work.")
    expect(w.find('[data-testid="nw-location"]').attributes('aria-invalid')).toBe('true')
    expect((w.find('[data-testid="nw-create"]').element as HTMLButtonElement).disabled).toBe(true)
    w.unmount()
  })

  it('without git on the machine the box is disabled with the reason', async () => {
    makeApp({ CheckGitAvailable: vi.fn(async () => false) })
    const w = await mountDialog()
    expect((w.find('#nw-git').element as HTMLInputElement).disabled).toBe(true)
    expect((w.find('#nw-git').element as HTMLInputElement).checked).toBe(false)
    expect(w.text()).toContain("git wasn't found on this computer.")
    w.unmount()
  })

  it('Choose… fills the location; Escape cancels', async () => {
    makeApp()
    const w = await mountDialog()
    await w.findAll('button').find((b) => b.text() === 'Choose…')!.trigger('click')
    await flushPromises()
    expect((w.find('[data-testid="nw-location"]').element as HTMLInputElement).value).toBe('/picked')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(newWorkspaceOpen.value).toBe(false)
    w.unmount()
  })

  it('a backend error is shown in the dialog and the dialog stays open', async () => {
    makeApp({ CreateWorkspace: vi.fn(async () => { throw new Error('Couldn\'t create the workspace: permission denied') }) })
    const w = await mountDialog()
    await w.find('#nw-name').setValue('X')
    await w.find('[data-testid="nw-location"]').setValue('/root')
    await flushPromises()
    await w.find('[data-testid="nw-create"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="nw-error"]').text()).toContain('permission denied')
    expect(newWorkspaceOpen.value).toBe(true)
    w.unmount()
  })
})
