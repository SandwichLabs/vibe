import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const handlers: Record<string, ((f: any) => void) | undefined> = {}
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (f: any) => void) => { handlers[name] = cb; return () => { delete handlers[name] } },
  EventsOff: () => {},
}))

const writes: string[] = []
let disposed = 0
vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80; rows = 24; element = null; options: any = {}; unicode = { activeVersion: '' }
    loadAddon() {}
    open() {}
    onData() { return { dispose() {} } }
    onResize() { return { dispose() {} } }
    attachCustomKeyEventHandler() {}
    write(d: string, cb?: () => void) { writes.push(d); cb?.() }
    hasSelection() { return false }
    dispose() { disposed++ }
  },
}))
vi.mock('@xterm/xterm/css/xterm.css', () => ({}))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: class { fit() {} } }))
vi.mock('@xterm/addon-unicode11', () => ({ Unicode11Addon: class {} }))
vi.mock('@xterm/addon-webgl', () => ({ WebglAddon: class { onContextLoss() {} dispose() {} } }))

import ReplTerminal from './ReplTerminal.vue'

function install() {
  const app = {
    ReplOpen: vi.fn(async (c: string) => ({ id: 'a'.repeat(32), cols: 80, rows: 24, connection: c || 'main' })),
    ReplInput: vi.fn(async () => {}),
    ReplResize: vi.fn(async () => {}),
    ReplAck: vi.fn(async () => {}),
    ReplClose: vi.fn(async () => {}),
    ReplUse: vi.fn(async (_id: string, name: string) => name || 'main'),
  }
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {}
  return app
}

beforeEach(() => {
  HTMLCanvasElement.prototype.getContext = (() => null) as any // no WebGL2 here: the DOM renderer
  ;(globalThis as any).ResizeObserver = class { observe() {} disconnect() {} }
  writes.length = 0; disposed = 0; for (const k of Object.keys(handlers)) delete handlers[k] })

describe('ReplTerminal', () => {
  it('does nothing until its tab is active', async () => {
    const app = install()
    mount(ReplTerminal, { props: { connection: '', active: false } })
    await flushPromises()
    expect(app.ReplOpen).not.toHaveBeenCalled()
  })

  it('opens when active, then attaches with ReplAck(id, 0)', async () => {
    const app = install()
    const w = mount(ReplTerminal, { props: { connection: 'main', active: true } })
    await flushPromises()
    expect(app.ReplOpen).toHaveBeenCalledWith('main')
    expect(app.ReplAck).toHaveBeenCalledWith('a'.repeat(32), 0)
    expect(w.get('[data-testid="repl-terminal"]').attributes('data-repl-id')).toBe('a'.repeat(32))
    expect(app.ReplOpen.mock.invocationCallOrder[0]).toBeLessThan(app.ReplAck.mock.invocationCallOrder[0])
  })

  it('renders frames and acknowledges them once written', async () => {
    const app = install()
    mount(ReplTerminal, { props: { connection: '', active: true } })
    await flushPromises()
    handlers['repl:out:' + 'a'.repeat(32)]?.({ seq: 1, data: 'hello' })
    await flushPromises()
    expect(writes).toContain('hello')
    expect(app.ReplAck).toHaveBeenCalledWith('a'.repeat(32), 1)
  })

  it('closes the session on unmount', async () => {
    const app = install()
    const w = mount(ReplTerminal, { props: { connection: '', active: true } })
    await flushPromises()
    w.unmount()
    expect(app.ReplClose).toHaveBeenCalledWith('a'.repeat(32))
    expect(disposed).toBe(1)
  })

  it('shows Reopen after a closed frame and opens a new session on click', async () => {
    const app = install()
    const w = mount(ReplTerminal, { props: { connection: '', active: true } })
    await flushPromises()
    handlers['repl:out:' + 'a'.repeat(32)]?.({ seq: 2, data: '', closed: true, reason: 'workspace closed' })
    await flushPromises()
    expect(w.get('[data-testid="repl-status"]').text()).toContain('workspace closed')
    await w.get('[data-testid="repl-reopen"]').trigger('click')
    await flushPromises()
    expect(app.ReplOpen).toHaveBeenCalledTimes(2)
    expect(app.ReplClose).not.toHaveBeenCalled() // the first session had already ended
  })

  it('asks Go to move the console when the picker changes', async () => {
    const app = install()
    const w = mount(ReplTerminal, { props: { connection: 'main', active: true } })
    await flushPromises()
    await w.setProps({ connection: 'other' })
    await flushPromises()
    expect(app.ReplUse).toHaveBeenCalledWith('a'.repeat(32), 'other')
    expect(w.find('[data-testid="repl-notice"]').exists()).toBe(false)
  })

  it('says the console stayed put when Go refuses, and clears it when the picker goes back', async () => {
    const app = install()
    app.ReplUse.mockRejectedValueOnce(new Error('finish the statement or press Ctrl+C first'))
    const w = mount(ReplTerminal, { props: { connection: 'main', active: true } })
    await flushPromises()
    await w.setProps({ connection: 'other' })
    await flushPromises()
    expect(w.get('[data-testid="repl-notice"]').text()).toBe('Console still on main: finish the statement or press Ctrl+C first')
    await w.setProps({ connection: 'main' })
    await flushPromises()
    expect(w.find('[data-testid="repl-notice"]').exists()).toBe(false)
    expect(app.ReplUse).toHaveBeenCalledTimes(1)
  })

  it('serialises picker changes: the latest wins after the one in flight', async () => {
    const app = install()
    let release: (v: string) => void = () => {}
    app.ReplUse.mockImplementationOnce(() => new Promise<string>((r) => { release = r }))
    const w = mount(ReplTerminal, { props: { connection: 'main', active: true } })
    await flushPromises()
    await w.setProps({ connection: 'a' })
    await w.setProps({ connection: 'b' })
    await w.setProps({ connection: 'c' })
    await flushPromises()
    expect(app.ReplUse).toHaveBeenCalledTimes(1) // only one in flight
    release('a')
    await flushPromises()
    expect(app.ReplUse.mock.calls.map((c) => c[1])).toEqual(['a', 'c'])
  })

  it('follows the console\'s own connection: a typed .conn updates it, and the notice goes', async () => {
    const app = install()
    app.ReplUse.mockRejectedValueOnce(new Error('finish the statement or press Ctrl+C first'))
    const w = mount(ReplTerminal, { props: { connection: 'main', active: true } })
    await flushPromises()
    await w.setProps({ connection: 'other' })
    await flushPromises()
    expect(w.find('[data-testid="repl-notice"]').exists()).toBe(true)
    handlers['repl:conn:' + 'a'.repeat(32)]?.('other') // the person typed .conn other
    await flushPromises()
    expect(w.find('[data-testid="repl-notice"]').exists()).toBe(false)
    // ...and the picker going back to main now has to ask Go again
    await w.setProps({ connection: 'main' })
    await flushPromises()
    expect(app.ReplUse).toHaveBeenLastCalledWith('a'.repeat(32), 'main')
  })

  it('passes the default choice ("") through to Go', async () => {
    const app = install()
    const w = mount(ReplTerminal, { props: { connection: 'other', active: true } })
    await flushPromises()
    await w.setProps({ connection: '' })
    await flushPromises()
    expect(app.ReplUse).toHaveBeenCalledWith('a'.repeat(32), '')
  })

  it('shows the notice when a move to the default ("") is refused', async () => {
    const app = install()
    app.ReplUse.mockRejectedValueOnce(new Error('finish the statement or press Ctrl+C first'))
    const w = mount(ReplTerminal, { props: { connection: 'other', active: true } })
    await flushPromises()
    await w.setProps({ connection: '' })
    await flushPromises()
    expect(w.get('[data-testid="repl-notice"]').text()).toContain('finish the statement')
  })

  it('a ReplUse in flight across a reopen does not touch the new console', async () => {
    const app = install()
    let release: (v: string) => void = () => {}
    app.ReplUse.mockImplementationOnce(() => new Promise<string>((r) => { release = r }))
    const w = mount(ReplTerminal, { props: { connection: 'main', active: true } })
    await flushPromises()
    await w.setProps({ connection: 'x' })
    await flushPromises()
    handlers['repl:out:' + 'a'.repeat(32)]?.({ seq: 1, data: '', closed: true, reason: 'exit' })
    await flushPromises()
    await w.get('[data-testid="repl-reopen"]').trigger('click')
    await flushPromises()
    release('x') // the old console answers late
    await flushPromises()
    expect(w.find('[data-testid="repl-notice"]').exists()).toBe(false)
    expect(app.ReplUse).toHaveBeenCalledTimes(1)
  })
})
