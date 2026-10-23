import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import NewArtifactDialog from './NewArtifactDialog.vue'

function app(NewArtifact: (n: string) => Promise<unknown>) {
  ;(window as any).go = { main: { App: { NewArtifact } } }
}

describe('NewArtifactDialog', () => {
  it('refuses a bad name without calling Go, and creates a good one', async () => {
    const NewArtifact = vi.fn(async () => ({}))
    app(NewArtifact)
    const w = mount(NewArtifactDialog, { props: { visible: true } })
    await w.find('[data-testid=new-artifact-name]').setValue('Bad Name')
    await w.find('form').trigger('submit')
    expect(NewArtifact).not.toHaveBeenCalled()
    expect(w.find('[data-testid=new-artifact-error]').text()).toMatch(/lowercase/)
    await w.find('[data-testid=new-artifact-name]').setValue('weekly-sales')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(NewArtifact).toHaveBeenCalledWith('weekly-sales', 'a2ui')
    expect(w.emitted('created')?.[0]).toEqual(['weekly-sales'])
  })

  it('shows Go\'s refusal of a taken name and stays open', async () => {
    app(vi.fn(async () => { throw new Error('an artifact named "orders" already exists') }))
    const w = mount(NewArtifactDialog, { props: { visible: true } })
    await w.find('[data-testid=new-artifact-name]').setValue('orders')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[data-testid=new-artifact-error]').text()).toContain('already exists')
    expect(w.emitted('created')).toBeUndefined()
  })
})
