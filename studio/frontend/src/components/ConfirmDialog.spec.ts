// ConfirmDialog.vue — the project's one shared confirm dialog (native
// window.confirm/alert are forbidden by convention). Covers the
// destructive/primary button variant and Escape-to-cancel.
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ConfirmDialog from './ConfirmDialog.vue'

describe('ConfirmDialog', () => {
  it('defaults to the destructive (red) confirm button', () => {
    const wrapper = mount(ConfirmDialog, { props: { visible: true, message: 'Delete this?' } })
    const confirmBtn = wrapper.findAll('button').find((b) => b.text() === 'Confirm')!
    expect(confirmBtn.classes()).toContain('btn-destructive')
    expect(confirmBtn.classes()).not.toContain('btn-primary')
  })

  it('uses the primary (teal) confirm button when variant="primary"', () => {
    const wrapper = mount(ConfirmDialog, { props: { visible: true, message: 'Open this link?', variant: 'primary' } })
    const confirmBtn = wrapper.findAll('button').find((b) => b.text() === 'Confirm')!
    expect(confirmBtn.classes()).toContain('btn-primary')
    expect(confirmBtn.classes()).not.toContain('btn-destructive')
  })

  it('emits cancel when Escape is pressed while visible', async () => {
    const wrapper = mount(ConfirmDialog, { props: { visible: true, message: 'x' } })
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('cancel')).toBeTruthy()
    expect(wrapper.emitted('confirm')).toBeFalsy()
  })

  it('does not emit cancel on Escape when not visible', async () => {
    const wrapper = mount(ConfirmDialog, { props: { visible: false, message: 'x' } })
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('cancel')).toBeFalsy()
  })

  it('emits confirm/cancel from the buttons', async () => {
    const wrapper = mount(ConfirmDialog, { props: { visible: true, message: 'x' } })
    await wrapper.findAll('button').find((b) => b.text() === 'Cancel')!.trigger('click')
    expect(wrapper.emitted('cancel')).toBeTruthy()
  })
})
