// useModalKeys — the shared Escape + focus behaviour of every modal, including
// ConfirmDialog (F13), which picks its own first focus.
import { describe, it, expect, vi } from 'vitest'
import { defineComponent, h, ref, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useModalKeys } from './useModal'

function harness(opts: { startOpen?: boolean; initialFocus?: boolean } = {}) {
  const open = ref(!!opts.startOpen)
  const close = vi.fn(() => { open.value = false })
  const root = ref<HTMLElement | null>(null)
  const second = ref<HTMLElement | null>(null)
  const Comp = defineComponent({
    setup() {
      useModalKeys(open, close, root, opts.initialFocus ? () => second.value : undefined)
      return () => h('div', [
        h('button', { id: 'opener' }, 'open'),
        open.value ? h('div', { ref: root }, [h('input', { id: 'first' }), h('button', { id: 'second', ref: second }, 'x')]) : null,
      ])
    },
  })
  const wrapper = mount(Comp, { attachTo: document.body })
  return { wrapper, open, close }
}

describe('useModalKeys', () => {
  it('moves focus to the first focusable element on open, Escape closes, and focus returns to the opener', async () => {
    const { wrapper, open, close } = harness()
    const opener = document.getElementById('opener')!
    opener.focus()
    open.value = true
    await nextTick(); await nextTick()
    expect(document.activeElement?.id).toBe('first')
    const e = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true })
    document.dispatchEvent(e)
    expect(close).toHaveBeenCalledTimes(1)
    expect(e.defaultPrevented).toBe(true)
    await nextTick()
    expect(document.activeElement?.id).toBe('opener')
    wrapper.unmount()
  })

  it('ignores Escape while closed', async () => {
    const { wrapper, close } = harness()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(close).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('a modal that is already open when it mounts still handles Escape and focus', async () => {
    const { wrapper, close } = harness({ startOpen: true })
    await nextTick(); await nextTick()
    expect(document.activeElement?.id).toBe('first')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(close).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('initialFocus picks the element to focus first', async () => {
    const { wrapper, open } = harness({ initialFocus: true })
    open.value = true
    await nextTick(); await nextTick()
    expect(document.activeElement?.id).toBe('second')
    wrapper.unmount()
  })

  it('stops listening once unmounted', async () => {
    const { wrapper, open, close } = harness()
    open.value = true
    await nextTick()
    wrapper.unmount()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(close).not.toHaveBeenCalled()
  })

  it('Tab and Shift+Tab wrap inside the root', async () => {
    const { wrapper, open } = harness()
    open.value = true
    await nextTick(); await nextTick()
    const second = document.getElementById('second')!
    second.focus()
    const tab = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true })
    document.dispatchEvent(tab)
    expect(tab.defaultPrevented).toBe(true)
    expect(document.activeElement?.id).toBe('first')
    const back = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, cancelable: true })
    document.dispatchEvent(back)
    expect(document.activeElement?.id).toBe('second')
    // In the middle the browser's own Tab is left alone.
    document.getElementById('first')!.focus()
    const mid = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true })
    document.dispatchEvent(mid)
    expect(mid.defaultPrevented).toBe(false)
    wrapper.unmount()
  })
})
