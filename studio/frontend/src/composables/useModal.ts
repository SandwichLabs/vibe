/**
 * useModalKeys — shared keyboard + focus behaviour for the app's ad-hoc
 * modals (New notebook, Create table, Add row, Save query, Skill editor).
 * ConfirmDialog and AiCellDialog both use it now; before it, every other
 * modal was Esc-proof and left focus wherever it was (audit 2.0).
 *
 * While `open` is true: Escape calls `close`, and focus moves to the first
 * focusable element inside the modal root (or the root itself). When it
 * closes, focus returns to whatever had it before.
 *
 * `initialFocus`, when given and it returns an element, picks that element
 * instead of the first focusable one (ConfirmDialog focuses Cancel, not the
 * button that discards data). `fallbackFocus` is where focus goes on close when
 * the opener is gone from the page (its button was removed meanwhile).
 */
import { nextTick, onUnmounted, watch, type Ref } from 'vue'

const FOCUSABLE = 'input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [tabindex]:not([tabindex="-1"])'

// The elements Tab can land on: focusable, visible, and for a native radio
// group only the checked radio (or the first, when none is checked).
function tabStops(el: HTMLElement): HTMLElement[] {
  const all = [...el.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((n) => n.tabIndex >= 0 && !n.closest('[hidden]'))
  return all.filter((n) => {
    if (!(n instanceof HTMLInputElement) || n.type !== 'radio' || !n.name) return true
    const group = [...el.querySelectorAll<HTMLInputElement>('input[type="radio"]')].filter((r) => r.name === n.name && r.form === n.form && !r.disabled)
    return n === (group.find((r) => r.checked) ?? group[0])
  })
}

export function useModalKeys(open: Ref<boolean>, close: () => void, root?: Ref<HTMLElement | null>, initialFocus?: () => HTMLElement | null | undefined, fallbackFocus?: () => HTMLElement | null | undefined) {
  let previouslyFocused: Element | null = null

  // Tab and Shift+Tab wrap inside the modal root so focus never reaches the page behind it.
  // A modal stacked above this one holds the focus itself, so only act when focus is in
  // this root or in no modal at all.
  function trapTab(e: KeyboardEvent) {
    const el = root?.value
    if (!el) return
    const items = tabStops(el)
    const active = document.activeElement as HTMLElement | null
    const inside = !!active && el.contains(active)
    // Focus in a different modal (stacked above this one, or nested inside this
    // root) is that modal's to trap. No modal at all (focus lost) is ours.
    const theirs = active?.closest('[aria-modal="true"]') ?? null
    if (theirs && theirs !== el.closest('[aria-modal="true"]')) return
    if (items.length === 0) { e.preventDefault(); return }
    const first = items[0]
    const last = items[items.length - 1]
    if (!inside) { e.preventDefault(); (e.shiftKey ? last : first).focus() }
    else if (e.shiftKey && (active === first || active === el)) { e.preventDefault(); last.focus() }
    else if (!e.shiftKey && active === last) { e.preventDefault(); first.focus() }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Tab' && open.value) { trapTab(e); return }
    if (e.key === 'Escape' && open.value) {
      e.preventDefault()
      e.stopPropagation()
      close()
    }
  }

  watch(open, async (isOpen) => {
    if (isOpen) {
      previouslyFocused = document.activeElement
      document.addEventListener('keydown', onKeydown)
      await nextTick()
      const el = root?.value
      const first = initialFocus?.() ?? el?.querySelector<HTMLElement>(FOCUSABLE) ?? el
      first?.focus?.()
    } else {
      document.removeEventListener('keydown', onKeydown)
      const prev = previouslyFocused as HTMLElement | null
      previouslyFocused = null
      if (prev && prev !== document.body && typeof prev.focus === 'function' && document.contains(prev)) prev.focus()
      else fallbackFocus?.()?.focus?.()
    }
  }, { immediate: true })

  onUnmounted(() => document.removeEventListener('keydown', onKeydown))
}

/** True on Apple platforms — for shortcut hints like ⌘S vs Ctrl+S. */
const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || (navigator as any).userAgentData?.platform || '')

/** "⌘S" on Mac, "Ctrl+S" elsewhere. */
export function shortcutLabel(key: string): string {
  return isMac ? `⌘${key}` : `Ctrl+${key}`
}
