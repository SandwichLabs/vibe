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

  it('leaves Tab alone while focus is in a different modal, including one nested in the root', async () => {
    const open = ref(true)
    const root = ref<HTMLElement | null>(null)
    const Comp = defineComponent({
      setup() {
        useModalKeys(open, () => {}, root)
        return () => h('div', { 'aria-modal': 'true' }, [h('div', { ref: root }, [
          h('button', { id: 'a' }, 'a'),
          h('div', { 'aria-modal': 'true', id: 'nested' }, [h('button', { id: 'n1' }, 'n1')]),
          h('button', { id: 'z' }, 'z'),
        ])])
      },
    })
    const w = mount(Comp, { attachTo: document.body })
    await nextTick(); await nextTick()
    document.getElementById('n1')!.focus()
    const e = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true })
    document.dispatchEvent(e)
    expect(e.defaultPrevented).toBe(false)
    // Lost focus (body) is still pulled back in.
    ;(document.activeElement as HTMLElement).blur()
    const lost = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true })
    document.dispatchEvent(lost)
    expect(lost.defaultPrevented).toBe(true)
    expect(document.activeElement?.id).toBe('a')
    w.unmount()
  })

  it('treats only the checked radio of a native group as a tab stop', async () => {
    const open = ref(true)
    const root = ref<HTMLElement | null>(null)
    const Comp = defineComponent({
      setup() {
        useModalKeys(open, () => {}, root)
        const radio = (id: string, checked = false) => h('input', { id, type: 'radio', name: 'g', checked })
        return () => h('div', { ref: root }, [h('button', { id: 'first' }, 'f'), radio('r1'), radio('r2', true), radio('r3'), h('button', { id: 'last' }, 'l')])
      },
    })
    const w = mount(Comp, { attachTo: document.body })
    await nextTick(); await nextTick()
    // Tab from the last button wraps to the first; Shift+Tab from first wraps to last (r1..r3 are not the ends).
    document.getElementById('last')!.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', cancelable: true }))
    expect(document.activeElement?.id).toBe('first')
    w.unmount()
    // A group whose checked radio is the last stop: Tab from it wraps.
    const Comp2 = defineComponent({
      setup() {
        useModalKeys(open, () => {}, root)
        return () => h('div', { ref: root }, [h('button', { id: 'b1' }, 'b'), h('input', { id: 'q1', type: 'radio', name: 'q' }), h('input', { id: 'q2', type: 'radio', name: 'q', checked: true })])
      },
    })
    const w2 = mount(Comp2, { attachTo: document.body })
    await nextTick(); await nextTick()
    document.getElementById('q2')!.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', cancelable: true }))
    expect(document.activeElement?.id).toBe('b1')
    // q1 (unchecked sibling) is not a stop: Shift+Tab from b1 lands on the checked q2.
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, cancelable: true }))
    expect(document.activeElement?.id).toBe('q2')
    w2.unmount()
  })

  it('uses fallbackFocus when focus was on the body (no opener) at open', async () => {
    const open = ref(false)
    const root = ref<HTMLElement | null>(null)
    const fb = ref<HTMLElement | null>(null)
    const Comp = defineComponent({
      setup() {
        useModalKeys(open, () => {}, root, undefined, () => fb.value)
        return () => h('div', [h('button', { id: 'fb', ref: fb }, 'fb'), open.value ? h('div', { ref: root }, [h('button', { id: 'in' }, 'in')]) : null])
      },
    })
    const w = mount(Comp, { attachTo: document.body })
    ;(document.activeElement as HTMLElement | null)?.blur()
    expect(document.activeElement).toBe(document.body)
    open.value = true
    await nextTick(); await nextTick()
    open.value = false
    await nextTick(); await nextTick()
    expect(document.activeElement?.id).toBe('fb')
    w.unmount()
  })
})
