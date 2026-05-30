// Router + Studio-Mode sidebar regression guard (P2.8 fix).
//
// The P2.8 RelationshipsView shipped orphaned: it existed but was never wired
// into the router or the sidebar, so a Studio-Mode user had no way to reach it
// and the vite build tree-shook it out. These tests assert the wiring exists:
//   - the /relationships route resolves to RelationshipsView
//   - the AppShell (Studio-Mode shell) sidebar exposes a link to /relationships
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { router } from './index'
import RelationshipsView from '../views/RelationshipsView.vue'
import AppShell from '../components/AppShell.vue'

describe('router wiring (P2.8 regression guard)', () => {
  it('the /relationships route resolves to RelationshipsView', () => {
    const resolved = router.resolve('/relationships')
    expect(resolved.name).toBe('relationships')
    expect(resolved.matched[0]?.components?.default).toBe(RelationshipsView)
  })

  it('the Studio-Mode sidebar exposes a link to /relationships', () => {
    ;(globalThis as any).window = (globalThis as any).window || {}
    ;(window as any).go = { main: { App: { GetFileInfo: async () => null } } }

    const wrapper = mount(AppShell, {
      global: {
        plugins: [router],
        stubs: { Icon: true },
      },
    })

    // The shell is the Studio-Mode shell (the "Studio Mode" pill is present)…
    expect(wrapper.text()).toContain('Studio Mode')

    // …and the sidebar must surface a router-link to /relationships.
    const relLink = wrapper
      .findAll('a')
      .find((a) => a.attributes('href')?.includes('/relationships'))
    expect(relLink, 'expected a sidebar link to /relationships').toBeTruthy()
    expect(relLink!.text()).toContain('Relationships')
  })
})
