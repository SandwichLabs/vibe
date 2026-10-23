// useLibrary reloads: on external changes and when the in-app agent changed data.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'

const handlers: Record<string, (p?: any) => void> = {}
vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: (n: string, cb: any) => { handlers[n] = cb; return () => { delete handlers[n] } } }))
import { useLibrary } from './useLibrary'

describe('useLibrary reload events', () => {
  beforeEach(() => { vi.useFakeTimers(); ;(window as any).runtime = {} })
  afterEach(() => { vi.useRealTimers(); delete (window as any).runtime })

  it('reloads once, coalesced, on data:changed and on artifact/notebook disk changes only', async () => {
    const list = vi.fn(async () => [])
    ;(window as any).go = { main: { App: { ListNotebooks: list, ListArtifacts: list } } }
    const w = mount(defineComponent({ setup() { useLibrary(ref(null)); return () => h('div') } }))
    await flushPromises()
    list.mockClear()
    handlers['data:changed']()
    handlers['data:changed']()
    await vi.advanceTimersByTimeAsync(300)
    expect(list).toHaveBeenCalledTimes(2) // one load = ListNotebooks + ListArtifacts
    list.mockClear()
    handlers['workspace:changed-on-disk']({ kind: 'skill' })
    await vi.advanceTimersByTimeAsync(300)
    expect(list).not.toHaveBeenCalled()
    handlers['workspace:changed-on-disk']({ kind: 'artifact' })
    await vi.advanceTimersByTimeAsync(300)
    expect(list).toHaveBeenCalledTimes(2)
    w.unmount()
  })
})
