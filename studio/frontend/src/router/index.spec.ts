// Router + Studio-Mode sidebar regression guard (P2.8 fix).
//
// The P2.8 RelationshipsView shipped orphaned: it existed but was never wired
// into the router or the sidebar, so a Studio-Mode user had no way to reach it
// and the vite build tree-shook it out. These tests assert the wiring exists:
//   - the /relationships route resolves to RelationshipsView
//   - the AppShell (Studio-Mode shell) sidebar exposes a link to /relationships
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { router, setCachedMode } from './index'
import RelationshipsView from '../views/RelationshipsView.vue'
import SchemaDesignerView from '../views/SchemaDesignerView.vue'
import AppShellView from '../views/AppShellView.vue'
import PageRuntimeView from '../views/PageRuntimeView.vue'
import AppShell from '../components/AppShell.vue'

describe('router wiring (P2.8 regression guard)', () => {
  it('the /relationships route resolves to RelationshipsView', () => {
    const resolved = router.resolve('/relationships')
    expect(resolved.name).toBe('relationships')
    expect(resolved.matched[0]?.components?.default).toBe(RelationshipsView)
  })

  it('the /schema route resolves to SchemaDesignerView', () => {
    const resolved = router.resolve('/schema')
    expect(resolved.name).toBe('schema')
    expect(resolved.matched[0]?.components?.default).toBe(SchemaDesignerView)
  })

  // P4.7 regression guard: the Switchboard (AppShellView) must be a registered
  // route, not dead code reachable only by its own spec. It is the App-Mode home.
  it('the /switchboard route resolves to AppShellView and is NOT builderOnly', () => {
    const resolved = router.resolve('/switchboard')
    expect(resolved.name).toBe('switchboard')
    expect(resolved.matched[0]?.components?.default).toBe(AppShellView)
    // Must stay reachable in App Mode — the guard redirects blocked routes here.
    expect(resolved.meta?.builderOnly).toBeFalsy()
  })

  // P4.7: the runtime page viewer (open_page target) must be a real route and
  // must NOT be builderOnly (so App Mode can open a bound page).
  it('the /page/:id runtime route resolves to PageRuntimeView and is NOT builderOnly', () => {
    const resolved = router.resolve('/page/pg_home')
    expect(resolved.name).toBe('page-runtime')
    expect(resolved.matched[0]?.components?.default).toBe(PageRuntimeView)
    expect(resolved.meta?.builderOnly).toBeFalsy()
  })

  // P4.7: in App Mode the switchboard is reachable; navigating to it is not
  // redirected away.
  it('App Mode: the /switchboard route is reachable (not redirected)', async () => {
    setCachedMode('app')
    await router.push('/switchboard')
    await router.isReady()
    expect(router.currentRoute.value.name).toBe('switchboard')
    setCachedMode('studio')
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

    // …and a router-link to /schema (the SchemaDesignerView, P2.9).
    const schemaLink = wrapper
      .findAll('a')
      .find((a) => a.attributes('href')?.includes('/schema'))
    expect(schemaLink, 'expected a sidebar link to /schema').toBeTruthy()
    expect(schemaLink!.text()).toContain('Schema')
  })
})
