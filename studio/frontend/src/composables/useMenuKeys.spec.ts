// useMenuKeys — the one keyboard handler behind the rail's context menu, the
// workspace switcher and the "Run on" select (F7).
import { describe, it, expect, vi } from 'vitest'
import { useMenuKeys } from './useMenuKeys'

function menu(enabled: boolean[], start: number, opts: { tab?: 'restore' | 'leave'; choose?: boolean } = {}) {
  let cur = start
  const moveTo = vi.fn((i: number) => { cur = i })
  const close = vi.fn()
  const choose = opts.choose ? vi.fn() : undefined
  const onKey = useMenuKeys({ enabled: () => enabled, current: () => cur, moveTo, close, tab: opts.tab ?? 'leave', choose })
  const press = (key: string) => {
    const e = new KeyboardEvent('keydown', { key, cancelable: true })
    const stop = vi.spyOn(e, 'stopPropagation')
    onKey(e)
    return { e, stop }
  }
  return { press, moveTo, close, choose, cur: () => cur }
}

describe('useMenuKeys', () => {
  it('ArrowDown / ArrowUp move to the next / previous ENABLED entry and wrap', () => {
    const m = menu([true, false, true, true], 0)
    m.press('ArrowDown'); expect(m.cur()).toBe(2) // skips the disabled one
    m.press('ArrowDown'); expect(m.cur()).toBe(3)
    m.press('ArrowDown'); expect(m.cur()).toBe(0) // wraps
    m.press('ArrowUp'); expect(m.cur()).toBe(3) // wraps back
    m.press('ArrowUp'); expect(m.cur()).toBe(2)
    m.press('ArrowUp'); expect(m.cur()).toBe(0) // over the disabled entry
  })

  it('moves on from a disabled current entry instead of getting stuck', () => {
    const m = menu([false, true, true], 0)
    m.press('ArrowDown'); expect(m.cur()).toBe(1)
  })

  it('Home / End go to the first / last enabled entry', () => {
    const m = menu([false, true, true, false], 2)
    m.press('Home'); expect(m.cur()).toBe(1)
    m.press('End'); expect(m.cur()).toBe(2)
  })

  it('an all-disabled menu moves nowhere and does not throw', () => {
    const m = menu([false, false], 0)
    m.press('ArrowDown'); m.press('Home'); m.press('End')
    expect(m.moveTo).not.toHaveBeenCalled()
  })

  it('the arrows prevent the page from scrolling', () => {
    const m = menu([true, true], 0)
    expect(m.press('ArrowDown').e.defaultPrevented).toBe(true)
    expect(m.press('Home').e.defaultPrevented).toBe(true)
  })

  it('Escape closes with focus restored, and does not reach other Escape handlers', () => {
    const m = menu([true], 0)
    const { e, stop } = m.press('Escape')
    expect(m.close).toHaveBeenCalledWith(true)
    expect(e.defaultPrevented).toBe(true)
    expect(stop).toHaveBeenCalled()
  })

  it('Tab: a menu that keeps focus inside restores it and stops the browser moving it; others let it go', () => {
    const inside = menu([true], 0, { tab: 'restore' })
    const a = inside.press('Tab')
    expect(inside.close).toHaveBeenCalledWith(true)
    expect(a.e.defaultPrevented).toBe(true)

    const leaving = menu([true], 0, { tab: 'leave' })
    const b = leaving.press('Tab')
    expect(leaving.close).toHaveBeenCalledWith(false)
    expect(b.e.defaultPrevented).toBe(false)
  })

  it('Enter and Space choose the current entry only for a listbox', () => {
    const list = menu([true, true], 1, { choose: true })
    const a = list.press('Enter')
    expect(list.choose).toHaveBeenCalledWith(1)
    expect(a.e.defaultPrevented).toBe(true)
    list.press(' ')
    expect(list.choose).toHaveBeenCalledTimes(2)

    const buttons = menu([true, true], 1) // a menu of buttons: the click does it
    const b = buttons.press('Enter')
    expect(b.e.defaultPrevented).toBe(false)
  })
})
