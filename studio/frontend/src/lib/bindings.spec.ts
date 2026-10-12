import { afterEach, describe, expect, it, vi } from 'vitest'
import { App, hasBinding } from './bindings'

afterEach(() => {
  delete (window as any).go
})

describe('bindings', () => {
  it('resolves window.go.main.App at call time, so mocks installed after import are seen', async () => {
    const quit = vi.fn(async () => {})
    ;(window as any).go = { main: { App: { Quit: quit } } }
    await App.Quit()
    expect(quit).toHaveBeenCalledTimes(1)
  })

  it('reads a missing binding as undefined and probes it with hasBinding', () => {
    expect(hasBinding('Quit')).toBe(false)
    expect(App.Quit).toBeUndefined()
    ;(window as any).go = { main: { App: { Quit: vi.fn() } } }
    expect(hasBinding('Quit')).toBe(true)
    expect(hasBinding('HoldQuit')).toBe(false)
  })
})
