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

  // #81: the optional third button for the Save/Don't save/Cancel prompt.
  it('renders a third button and emits alt when altLabel is set', async () => {
    const wrapper = mount(ConfirmDialog, {
      props: { visible: true, message: 'Save changes?', altLabel: "Don't save" },
    })
    const buttons = wrapper.findAll('button')
    expect(buttons).toHaveLength(3)
    const altBtn = buttons.find((b) => b.text() === "Don't save")!
    expect(altBtn).toBeTruthy()
    await altBtn.trigger('click')
    expect(wrapper.emitted('alt')).toBeTruthy()
    expect(wrapper.emitted('confirm')).toBeFalsy()
    expect(wrapper.emitted('cancel')).toBeFalsy()
  })

  it('renders only two buttons when altLabel is not set (regression)', () => {
    const wrapper = mount(ConfirmDialog, { props: { visible: true, message: 'x' } })
    expect(wrapper.findAll('button')).toHaveLength(2)
  })

  it('gives the alt button the destructive tint when altDestructive is set', () => {
    const wrapper = mount(ConfirmDialog, {
      props: { visible: true, message: 'x', altLabel: "Don't save", altDestructive: true, variant: 'primary' },
    })
    const altBtn = wrapper.findAll('button').find((b) => b.text() === "Don't save")!
    expect(altBtn.classes()).toContain('btn-destructive')
    expect(altBtn.classes()).not.toContain('btn-ghost')
  })

  it('keeps the alt button ghost-styled by default', () => {
    const wrapper = mount(ConfirmDialog, { props: { visible: true, message: 'x', altLabel: 'Alt' } })
    const altBtn = wrapper.findAll('button').find((b) => b.text() === 'Alt')!
    expect(altBtn.classes()).toContain('btn-ghost')
    expect(altBtn.classes()).not.toContain('btn-destructive')
  })

  // #81's Save/Don't save/Cancel prompt passes variant: 'primary' (Save
  // isn't destructive) and keeps focusing the confirm button, third button
  // or not, so Enter still means "Save" there.
  it('focuses the confirm button on the closed->open transition when variant is primary, even with a third button present', async () => {
    const wrapper = mount(ConfirmDialog, {
      props: { visible: false, message: 'x', altLabel: 'Alt', variant: 'primary' },
      attachTo: document.body,
    })
    await wrapper.setProps({ visible: true })
    await new Promise((r) => setTimeout(r, 0))
    await wrapper.vm.$nextTick()
    const confirmBtn = wrapper.findAll('button').find((b) => b.text() === 'Confirm')!
    expect(document.activeElement).toBe(confirmBtn.element)
    wrapper.unmount()
  })

  // A destructive confirm (the default) must never let Enter discard data
  // by accident, so initial focus goes to Cancel instead of the button
  // that destroys something.
  it('focuses Cancel, not the destructive confirm button, on the closed->open transition by default', async () => {
    const wrapper = mount(ConfirmDialog, {
      props: { visible: false, message: 'Discard unsaved table changes?' },
      attachTo: document.body,
    })
    await wrapper.setProps({ visible: true })
    await new Promise((r) => setTimeout(r, 0))
    await wrapper.vm.$nextTick()
    const cancelBtn = wrapper.findAll('button').find((b) => b.text() === 'Cancel')!
    expect(document.activeElement).toBe(cancelBtn.element)
    wrapper.unmount()
  })
})
