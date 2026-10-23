// DashboardView (docs/plans/library.md L8): Library tiles by type, each linking
// to the Library on that type; New notebook opens the picker on Notebook.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { router } from '../router/index'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))
import DashboardView from './DashboardView.vue'

function setApp(counts: Record<string, number>) {
  ;(window as any).go = {
    main: {
      App: {
        GetDashboard: vi.fn(async () => ({
          title: 'Acme', description: '', notebook_count: 4, query_count: 2, skill_count: 0, table_count: 3,
          artifact_counts: { live: 2, page: 1, script: 0, broken: 0, ...counts },
          local_tables: [], default_connection: 'local', read_only: false,
        })),
      },
    },
  }
}
async function mountHome() {
  await router.push('/')
  await router.isReady()
  const w = mount(DashboardView, { global: { plugins: [router], stubs: { Icon: true } } })
  await flushPromises()
  return w
}

describe('DashboardView Library tiles', () => {
  beforeEach(() => setApp({}))

  it('shows notebooks, live views and pages with their counts, no chart scripts at zero', async () => {
    const w = await mountHome()
    expect(w.find('[data-testid="library-tile-notebook"]').text()).toContain('Notebooks')
    expect(w.find('[data-testid="library-tile-notebook"]').text()).toContain('4')
    expect(w.find('[data-testid="library-tile-live"]').text()).toContain('Live views')
    expect(w.find('[data-testid="library-tile-live"]').text()).toContain('2')
    expect(w.find('[data-testid="library-tile-page"]').text()).toContain('1')
    expect(w.find('[data-testid="library-tile-script"]').exists()).toBe(false)
    expect(w.find('[data-testid="library-broken"]').exists()).toBe(false)
  })

  it('adds Chart scripts once there is one and says how many artifacts need fixing', async () => {
    setApp({ script: 3, broken: 2 })
    const w = await mountHome()
    expect(w.find('[data-testid="library-tile-script"]').text()).toContain('3')
    expect(w.find('[data-testid="library-broken"]').text()).toContain('2 artifacts need fixing')
  })

  it('a tile opens the Library on its type', async () => {
    const w = await mountHome()
    await w.find('[data-testid="library-tile-live"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/library')
    expect(router.currentRoute.value.query).toMatchObject({ type: 'live' })
  })

  it('New notebook opens the picker preselected to Notebook', async () => {
    const w = await mountHome()
    await w.findAll('button').find((b) => b.text().includes('New notebook'))!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/library?new=notebook')
  })

  it('the Library card opens the unfiltered list', async () => {
    const w = await mountHome()
    await w.find('[data-testid="library-card"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/library')
  })
})
