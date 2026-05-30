// Router + chrome guard for the Macro builder (Phase 5 / unit P5.9).
//
// MacroBuilderView is Studio-only. These assert:
//   - the /macros route resolves to MacroBuilderView and is builderOnly
//   - the Studio-Mode sidebar surfaces a link to /macros
//   - in App Mode the /macros route is redirected to the switchboard (the
//     route-locking guard) — i.e. it is ABSENT from App-Mode chrome
//
// RED step for P5.9: there is no /macros route nor MacroBuilderView wiring yet,
// so these fail (the import/resolve is missing).
import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { router, setCachedMode } from './index'
import MacroBuilderView from '../views/MacroBuilderView.vue'
import AppShell from '../components/AppShell.vue'

describe('Macro builder route + chrome guard (P5.9)', () => {
  afterEach(() => {
    setCachedMode('studio')
  })

  it('the /macros route resolves to MacroBuilderView and is builderOnly', () => {
    const resolved = router.resolve('/macros')
    expect(resolved.name).toBe('macros')
    expect(resolved.matched[0]?.components?.default).toBe(MacroBuilderView)
    expect(resolved.meta?.builderOnly).toBe(true)
  })

  it('the Studio-Mode sidebar exposes a link to /macros', () => {
    ;(globalThis as any).window = (globalThis as any).window || {}
    ;(window as any).go = { main: { App: { GetFileInfo: async () => null } } }

    const wrapper = mount(AppShell, {
      global: { plugins: [router], stubs: { Icon: true } },
    })

    expect(wrapper.text()).toContain('Studio Mode')
    const macroLink = wrapper
      .findAll('a')
      .find((a) => a.attributes('href')?.includes('/macros'))
    expect(macroLink, 'expected a sidebar link to /macros').toBeTruthy()
    expect(macroLink!.text()).toContain('Macro')
  })

  it('App Mode: navigating to /macros is redirected to the switchboard', async () => {
    setCachedMode('app')
    await router.push('/macros')
    await router.isReady()
    expect(router.currentRoute.value.name).toBe('switchboard')
  })
})
