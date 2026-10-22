// useArtifactViewTrust (#159): Allow acts on the bytes on screen; a reload that
// brings other bytes while Allow is armed waits behind showNew().
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))

import { useArtifactViewTrust } from './useArtifactViewTrust'

const st = (hash: string, state: ArtifactViewStatus['state'] = 'undecided'): ArtifactViewStatus =>
  ({ name: 'chart', kind: 'script', state, hash, manifest: 'm', source: 's-' + hash })

function setup(reads: ArtifactViewStatus[]) {
  let i = 0
  const app: MockApp = {
    ArtifactViewState: vi.fn(async () => reads[Math.min(i++, reads.length - 1)]),
    AllowArtifactView: vi.fn(async (_n: string, hash: string) => st(hash, 'allowed')),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  let api!: ReturnType<typeof useArtifactViewTrust>
  mount(defineComponent({ setup() { api = useArtifactViewTrust(() => 'chart'); return () => h('div') } }))
  return { app, api: () => api }
}

describe('useArtifactViewTrust', () => {
  it('a reload with other bytes while undecided does not replace what is shown, and Allow is off', async () => {
    const { app, api } = setup([st('h1'), st('h2')])
    await flushPromises()
    expect(api().status.value?.hash).toBe('h1')
    await api().reload()
    expect(api().status.value?.hash).toBe('h1')
    expect(api().changedWhileReading.value).toBe(true)
    await api().allow()
    expect(app.AllowArtifactView).not.toHaveBeenCalled()
    api().showNew()
    expect(api().status.value?.hash).toBe('h2')
    expect(api().changedWhileReading.value).toBe(false)
    await api().allow()
    expect(app.AllowArtifactView).toHaveBeenCalledWith('chart', 'h2')
  })

  it('the same bytes reload quietly, and an allowed view that changes is adopted at once', async () => {
    const { api } = setup([st('h1'), st('h1'), st('h1', 'allowed'), st('h2', 'changed')])
    await flushPromises()
    await api().reload()
    expect(api().changedWhileReading.value).toBe(false)
    await api().reload() // now allowed on screen
    expect(api().status.value?.state).toBe('allowed')
    await api().reload() // edited on disk: no Allow was armed, show it as changed
    expect(api().status.value?.state).toBe('changed')
    expect(api().changedWhileReading.value).toBe(false)
  })

  it('an edited file that is restored clears "changed" on the next read', async () => {
    const { api } = setup([st('h1', 'allowed'), st('h2', 'changed'), st('h1', 'allowed')])
    await flushPromises()
    await api().reload()
    expect(api().status.value?.state).toBe('changed')
    await api().reload() // the file is back to the bytes that were allowed
    expect(api().status.value?.state).toBe('allowed')
    expect(api().status.value?.hash).toBe('h1')
    expect(api().changedWhileReading.value).toBe(false)
  })
})
