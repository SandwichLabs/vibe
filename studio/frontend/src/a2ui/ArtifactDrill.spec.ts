// Drill-down and pivot inside a data artifact view (#162, step 5): the real
// useArtifactParams behind a fake query runtime, so a drill goes through the
// same coercion as a control.
import { describe, it, expect } from 'vitest'
import { nextTick } from 'vue'
import { effectScope, shallowReactive } from 'vue'
import { mount } from '@vue/test-utils'
import A2UIPage from './A2UIPage.vue'
import { ARTIFACT_RUNTIME, type ArtifactRuntime, type ArtifactQueryState } from './artifactRuntime'
import { useArtifactParams } from '../composables/useArtifactParams'
import type { A2UIPageT } from './types'

const detail: ArtifactDetail = {
  name: 'sales', title: 'Sales', description: '', rev: 'r', updated_at: '', queries: [],
  params: [
    { name: 'region', type: 'enum', default: '', options: ['East', 'West'] },
    { name: 'top', type: 'integer', default: '10', min: 1, max: 100 },
    { name: 'any', type: 'enum', default: '', options_query: 'regions' },
  ],
  defaults: { region: '', top: 10, any: '' },
}

const byRegion = { columns: ['region', 'revenue'], types: ['VARCHAR', 'BIGINT'], rows: [['East', 10], ['North', 7], ['West', 5], [null, 1]] }
const long = {
  columns: ['region', 'month', 'revenue'], types: ['VARCHAR', 'VARCHAR', 'BIGINT'],
  rows: [['East', 'Jan', 1], ['East', 'Feb', 2], ['West', 'Jan', 3]],
}

function setup(components: A2UIPageT['components'], results: Record<string, any>, truncated: string[] = [], attach = false) {
  const scope = effectScope()
  const params = scope.run(() => useArtifactParams(() => detail))!
  params.load(detail)
  const states = new Map<string, ArtifactQueryState>()
  const rt: ArtifactRuntime = {
    result: (q) => {
      if (!states.has(q)) states.set(q, shallowReactive({ loading: false, error: '', data: results[q] ?? null, fetched: true, truncated: truncated.includes(q) }))
      return states.get(q)!
    },
    params: { param: params.param, display: params.display, error: params.error, isDefault: params.isDefault, set: params.set, reset: params.reset },
  }
  const wrapper = mount(A2UIPage, { props: { page: { title: 't', components } }, global: { provide: { [ARTIFACT_RUNTIME as symbol]: rt } }, attachTo: attach ? document.body : undefined })
  return { wrapper, params, states }
}

describe('drill on a table', () => {
  const comps = [{ id: 't', type: 'table', query: 'q', drill: { param: 'region', column: 'region' } }]

  it('a click sets the coerced parameter and Enter on the row button does the same', async () => {
    const { wrapper, params } = setup(comps, { q: byRegion })
    const rows = wrapper.findAll('tbody tr')
    await rows[0].trigger('click')
    expect(params.values.region).toBe('East')
    await wrapper.find('tbody tr:nth-child(3) button').trigger('click') // what Enter on a button does
    expect(params.values.region).toBe('West')
    expect(wrapper.find('[data-testid=a2ui-drill-error]').exists()).toBe(false)
  })

  it('a value outside a static enum shows the inline error, commits nothing', async () => {
    const { wrapper, params } = setup(comps, { q: byRegion })
    await wrapper.findAll('tbody tr')[1].trigger('click') // North
    expect(params.values.region).toBe('')
    expect(params.error('region')).toMatch(/one of the options/)
    expect(wrapper.find('[data-testid=a2ui-drill-error]').text()).toMatch(/one of the options/)
  })

  it('a NULL cell sets nothing', async () => {
    const { wrapper, params } = setup(comps, { q: byRegion })
    await wrapper.findAll('tbody tr')[3].trigger('click')
    expect(params.values.region).toBe('')
    expect(wrapper.find('[data-testid=a2ui-drill-error]').text()).toMatch(/no value/)
  })

  it('a drill column missing from the result disables drill and says so', async () => {
    const { wrapper, params } = setup([{ id: 't', type: 'table', query: 'q', drill: { param: 'region', column: 'area' } }], { q: byRegion })
    expect(wrapper.text()).toContain('Drill column "area" is not in this query')
    expect(wrapper.find('tbody button').exists()).toBe(false)
    await wrapper.findAll('tbody tr')[0].trigger('click')
    expect(params.values.region).toBe('')
  })

  it('a table without drill, or outside an artifact, has no buttons', () => {
    const { wrapper } = setup([{ id: 't', type: 'table', query: 'q' }], { q: byRegion })
    expect(wrapper.find('tbody button').exists()).toBe(false)
    const plain = mount(A2UIPage, { props: { page: { title: 't', components: [{ id: 't', type: 'table', columns: ['a'], rows: [['x']], drill: { param: 'p', column: 'a' } }] } } })
    expect(plain.find('tbody button').exists()).toBe(false)
  })

  it('a numeric cell drills a text or integer parameter; one past the range is refused', async () => {
    const t = (param: string) => [{ id: 't', type: 'table', query: 'q', drill: { param, column: 'revenue' } }]
    const a = setup(t('top'), { q: byRegion })
    await a.wrapper.findAll('tbody tr')[2].trigger('click')
    expect(a.params.values.top).toBe(5)
    const b = setup(t('top'), { q: { ...byRegion, rows: [['East', 500]] } })
    await b.wrapper.findAll('tbody tr')[0].trigger('click')
    expect(b.params.values.top).toBe(10)
    expect(b.params.error('top')).toMatch(/at most 100/)
  })
})

describe('drill on bars and columns', () => {
  it('a bar is a focusable button that drills on click, Enter and Space', async () => {
    const { wrapper, params } = setup([{ id: 'b', type: 'barChart', query: 'q', drill: { param: 'region', column: 'region' } }], { q: byRegion })
    const bars = wrapper.findAll('.a2ui-bar')
    expect(bars[0].attributes('role')).toBe('button')
    expect(bars.filter((b) => b.attributes('tabindex') === '0')).toHaveLength(1)
    await bars[0].trigger('click')
    expect(params.values.region).toBe('East')
    await bars[2].trigger('keydown', { key: 'Enter' })
    expect(params.values.region).toBe('West')
    params.reset('region')
    await bars[0].trigger('keydown', { key: ' ' })
    expect(params.values.region).toBe('East')
  })

  it('arrow keys move focus and the single tab stop between bars; Enter then drills the focused one', async () => {
    const { wrapper, params } = setup([{ id: 'b', type: 'barChart', query: 'q', drill: { param: 'region', column: 'region' } }], { q: byRegion }, [], true)
    const bars = wrapper.findAll('.a2ui-bar')
    expect(bars[1].attributes('tabindex')).toBe('-1')
    ;(bars[0].element as HTMLElement).focus()
    await bars[0].trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(bars[1].element)
    expect(bars[1].attributes('tabindex')).toBe('0')
    expect(bars[0].attributes('tabindex')).toBe('-1')
    await bars[1].trigger('keydown', { key: 'ArrowDown' })
    await bars[2].trigger('keydown', { key: 'Enter' })
    expect(params.values.region).toBe('West')
    wrapper.unmount()
  })

  it('a pivot grid moves with the arrows, up and down by column', async () => {
    const g = { ...long, rows: [['East', 'Jan', 1], ['East', 'Feb', 2], ['West', 'Feb', 3]] }
    const { wrapper } = setup([{ id: 'p', type: 'pivot', query: 'q', rows: ['region'], columns: 'month', value: 'revenue', drill: { param: 'region', column: 'region' } }], { q: g }, [], true)
    const btns = wrapper.findAll('tbody button') // East/Jan, East/Feb, West/Feb
    ;(btns[1].element as HTMLElement).focus()
    await btns[1].trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(btns[2].element) // same column, next row
    await btns[2].trigger('keydown', { key: 'ArrowLeft' })
    expect(document.activeElement).toBe(btns[1].element)
    wrapper.unmount()
  })

  it('a sorted column chart drills the row it was drawn from', async () => {
    const q = { columns: ['region', 'revenue', 'id'], types: ['VARCHAR', 'BIGINT', 'BIGINT'], rows: [['East', 1, 'x'], ['West', 9, 'y']] }
    const { wrapper, params } = setup([{ id: 'c', type: 'columnChart', query: 'q', sorted: true, drill: { param: 'region', column: 'region' } }], { q })
    const cols = wrapper.findAll('.a2ui-columns__col')
    expect(cols[0].attributes('aria-label')).toContain('West') // sorted first
    await cols[0].trigger('click')
    expect(params.values.region).toBe('West')
  })

  it('a bar for a row the parameter refuses shows the error and runs nothing', async () => {
    const { wrapper, params } = setup([{ id: 'b', type: 'barChart', query: 'q', drill: { param: 'region', column: 'region' } }], { q: byRegion })
    await wrapper.findAll('.a2ui-bar')[1].trigger('click')
    expect(params.values.region).toBe('')
    expect(wrapper.find('[data-testid=a2ui-drill-error]').exists()).toBe(true)
  })
})

describe('pivot', () => {
  const pv = { id: 'p', type: 'pivot', query: 'q', rows: ['region'], columns: 'month', value: 'revenue', label: 'Revenue', drill: { param: 'region', column: 'region' } }

  it('is a real table with column and row headers', () => {
    const { wrapper } = setup([pv], { q: long })
    expect(wrapper.find('table').attributes('aria-labelledby')).toBeTruthy()
    const heads = wrapper.findAll('thead th[scope=col]').map((h) => h.text())
    expect(heads).toEqual(['region', 'Jan', 'Feb'])
    expect(wrapper.findAll('tbody th[scope=row]').map((h) => h.text())).toEqual(['East', 'West'])
    expect(wrapper.find('thead th[scope=colgroup]').text()).toBe('month')
    expect(wrapper.findAll('tbody tr')[1].findAll('td').map((c) => c.text())).toEqual(['3', ''])
  })

  it('renders header text as text only', () => {
    const evil = { ...long, rows: [['<img src=x onerror=alert(1)>', '<b>bold</b>' + 'y'.repeat(300), 1]] }
    const { wrapper } = setup([pv], { q: evil })
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('table b').exists()).toBe(false)
    expect(wrapper.find('tbody th').text()).toBe('<img src=x onerror=alert(1)>')
    expect(wrapper.element.querySelectorAll('img, b, script')).toHaveLength(0)
  })

  it('names the cell on a duplicate and shows no grid', () => {
    const { wrapper } = setup([pv], { q: { ...long, rows: [['East', 'Jan', 1], ['East', 'Jan', 2]] } })
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.text()).toContain('East / Jan')
  })

  it('says when a pivot column is missing from the result', () => {
    const { wrapper } = setup([{ ...pv, value: 'nope' }], { q: long })
    expect(wrapper.text()).toContain('"nope"')
  })

  it('labels a truncated result', () => {
    const { wrapper } = setup([pv], { q: long }, ['q'])
    expect(wrapper.find('[data-testid=a2ui-pivot-truncated]').text()).toMatch(/500 rows/)
  })

  it('a cell drills its row; one tab stop; refused values show the error', async () => {
    const { wrapper, params } = setup([pv], { q: long })
    const btns = wrapper.findAll('tbody button')
    expect(btns).toHaveLength(3)
    expect(btns.filter((b) => b.attributes('tabindex') === '0')).toHaveLength(1)
    await btns[2].trigger('click') // West / Jan
    expect(params.values.region).toBe('West')
    const odd = setup([pv], { q: { ...long, rows: [['North', 'Jan', 1]] } })
    await odd.wrapper.find('tbody button').trigger('click')
    expect(odd.params.values.region).toBe('')
    expect(odd.wrapper.find('[data-testid=a2ui-drill-error]').exists()).toBe(true)
  })

  it('a pivot without drill has no buttons', () => {
    const { drill: _d, ...nodrill } = pv
    const { wrapper } = setup([nodrill], { q: long })
    expect(wrapper.find('tbody button').exists()).toBe(false)
  })

  it('is refused outside a data artifact', () => {
    const w = mount(A2UIPage, { props: { page: { title: 't', components: [pv] } } })
    expect(w.text()).toContain('only available in a data artifact')
  })
})

describe('drill review round 1', () => {
  const tbl = { id: 't', type: 'table', query: 'q', drill: { param: 'region', column: 'region' } }
  const bar = { id: 'b', type: 'barChart', query: 'q', drill: { param: 'region', column: 'region' } }

  it('a re-run of the drilled component keeps the table, and focus, on screen', async () => {
    const { wrapper, states } = setup([tbl], { q: byRegion }, [], true)
    const btn = wrapper.findAll('tbody button')[0]
    ;(btn.element as HTMLElement).focus()
    states.get('q')!.loading = true // the drill re-ran this very query
    await nextTick()
    expect(wrapper.find('.a2ui-loading').text()).toContain('RUNNING')
    expect(wrapper.findAll('tbody button')[0].element).toBe(btn.element)
    expect(document.activeElement).toBe(btn.element)
    wrapper.unmount()
  })

  it('keeps bars, columns and the pivot on screen while a result is re-running, and swaps only a first load', async () => {
    const comps = [bar, { id: 'c', type: 'columnChart', query: 'q', drill: { param: 'region', column: 'region' } },
      { id: 'p', type: 'pivot', query: 'g', rows: ['region'], columns: 'month', value: 'revenue' }]
    const { wrapper, states } = setup(comps, { q: byRegion, g: long })
    states.get('q')!.loading = true
    states.get('g')!.loading = true
    await nextTick()
    expect(wrapper.findAll('.a2ui-bar')).toHaveLength(4)
    expect(wrapper.findAll('.a2ui-columns__col')).toHaveLength(4)
    expect(wrapper.find('[data-testid=a2ui-columns] .a2ui-loading').text()).toContain('RUNNING QUERY')
    expect(wrapper.find('table.a2ui-pivot').exists()).toBe(true)
    const first = setup([tbl], {})
    first.states.get('q')!.loading = true
    await nextTick()
    expect(first.wrapper.find('table').exists()).toBe(false)
  })

  it('the note names the refused value and goes away on Reset and on a valid pick', async () => {
    const { wrapper, params } = setup([bar], { q: byRegion })
    await wrapper.findAll('.a2ui-bar')[1].trigger('click') // North
    expect(wrapper.find('[data-testid=a2ui-drill-error]').text()).toMatch(/“North” was refused for region: Choose one of the options/)
    params.reset('region')
    await nextTick()
    expect(wrapper.find('[data-testid=a2ui-drill-error]').exists()).toBe(false)
    await wrapper.findAll('.a2ui-bar')[1].trigger('click')
    expect(wrapper.find('[data-testid=a2ui-drill-error]').exists()).toBe(true)
    await wrapper.findAll('.a2ui-bar')[0].trigger('click') // East, valid
    expect(wrapper.find('[data-testid=a2ui-drill-error]').exists()).toBe(false)
  })

  it('a NULL refusal clears when the parameter changes', async () => {
    const { wrapper, params } = setup([bar], { q: byRegion })
    await wrapper.findAll('.a2ui-bar')[3].trigger('click')
    expect(wrapper.find('[data-testid=a2ui-drill-error]').text()).toMatch(/no value/)
    params.set('region', 'West')
    await nextTick()
    expect(wrapper.find('[data-testid=a2ui-drill-error]').exists()).toBe(false)
  })

  it('names the value the row sets, even when the button sits in another column', () => {
    const t = setup([{ ...tbl, drill: { param: 'region', column: 'region' } }], { q: { columns: ['customer', 'region'], types: ['VARCHAR', 'VARCHAR'], rows: [['cust5', 'North'], ['cust1', 'East']] } })
    expect(t.wrapper.find('tbody button').attributes('aria-label')).toBe('cust5. Filter region by North')
    const b = setup([bar], { q: byRegion })
    expect(b.wrapper.findAll('.a2ui-bar')[0].attributes('aria-label')).toContain('Filter region by East')
    const c = setup([{ id: 'c', type: 'columnChart', query: 'q', drill: { param: 'region', column: 'region' } }], { q: byRegion })
    expect(c.wrapper.findAll('.a2ui-columns__col')[0].attributes('aria-label')).toContain('Filter region by East')
  })

  it('marks the current row with aria-current, and nothing when every row matches or the value was refused', async () => {
    const { wrapper } = setup([bar], { q: { ...byRegion, rows: [['East', 10], ['West', 5]] } })
    expect(wrapper.findAll('[aria-current]')).toHaveLength(0)
    await wrapper.findAll('.a2ui-bar')[0].trigger('click')
    expect(wrapper.findAll('.a2ui-bar')[0].attributes('aria-current')).toBe('true')
    expect(wrapper.findAll('.a2ui-bar')[1].attributes('aria-current')).toBeUndefined()
    expect(wrapper.find('[aria-pressed]').exists()).toBe(false)
    const all = setup([bar], { q: { ...byRegion, rows: [['East', 10], ['East', 5]] } })
    await all.wrapper.findAll('.a2ui-bar')[0].trigger('click')
    expect(all.wrapper.findAll('[aria-current]')).toHaveLength(0)
    const bad = setup([bar], { q: byRegion })
    await bad.wrapper.findAll('.a2ui-bar')[0].trigger('click') // East valid
    await bad.wrapper.findAll('.a2ui-bar')[1].trigger('click') // North refused
    expect(bad.wrapper.findAll('[aria-current]')).toHaveLength(0)
    expect(bad.params.values.region).toBe('East') // the refused one changed nothing
  })

  it('a timestamp drilled into a date parameter shows as current', async () => {
    const d: ArtifactDetail = { ...detail, params: [{ name: 'day', type: 'date', default: '2026-01-01' }], defaults: { day: '2026-01-01' } }
    const q = { columns: ['day', 'n'], types: ['TIMESTAMP', 'BIGINT'], rows: [['2026-03-01 00:00:00', 1], ['2026-03-02 00:00:00', 2]] }
    const scope = effectScope()
    const params = scope.run(() => useArtifactParams(() => d))!
    params.load(d)
    const rt: ArtifactRuntime = {
      result: () => shallowReactive({ loading: false, error: '', data: q, fetched: true, truncated: false }),
      params: { param: params.param, display: params.display, error: params.error, isDefault: params.isDefault, set: params.set, reset: params.reset },
    }
    const w = mount(A2UIPage, { props: { page: { title: 't', components: [{ id: 'b', type: 'barChart', query: 'q', drill: { param: 'day', column: 'day' } }] } }, global: { provide: { [ARTIFACT_RUNTIME as symbol]: rt } } })
    await w.findAll('.a2ui-bar')[0].trigger('click')
    expect(params.values.day).toBe('2026-03-01')
    expect(w.findAll('.a2ui-bar')[0].attributes('aria-current')).toBe('true')
  })

  it('a numeric cell reaches a text parameter as its text (the candidate, not the raw cell)', async () => {
    const d: ArtifactDetail = { ...detail, params: [{ name: 'code', type: 'text', default: '' }], defaults: { code: '' } }
    const q = { columns: ['code', 'n'], types: ['BIGINT', 'BIGINT'], rows: [[42, 1], [7, 2]] }
    const scope = effectScope()
    const params = scope.run(() => useArtifactParams(() => d))!
    params.load(d)
    const rt: ArtifactRuntime = {
      result: () => shallowReactive({ loading: false, error: '', data: q, fetched: true, truncated: false }),
      params: { param: params.param, display: params.display, error: params.error, isDefault: params.isDefault, set: params.set, reset: params.reset },
    }
    const w = mount(A2UIPage, { props: { page: { title: 't', components: [{ id: 't', type: 'table', query: 'q', drill: { param: 'code', column: 'code' } }] } }, global: { provide: { [ARTIFACT_RUNTIME as symbol]: rt } } })
    await w.findAll('tbody tr')[0].trigger('click')
    expect(params.values.code).toBe('42')
    expect(params.error('code')).toBe('')
  })

  it('a plain click on a table with no drill does nothing and shows nothing', async () => {
    const { wrapper } = setup([{ id: 't', type: 'table', query: 'q' }], { q: byRegion })
    await wrapper.findAll('tbody tr')[0].trigger('click')
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
  })

  it('the column chart is a group (not an image) while drill is on, an image when it is off', () => {
    const on = setup([{ id: 'c', type: 'columnChart', query: 'q', drill: { param: 'region', column: 'region' } }], { q: byRegion })
    expect(on.wrapper.find('.a2ui-columns').attributes('role')).toBe('group')
    const off = setup([{ id: 'c', type: 'columnChart', query: 'q' }], { q: byRegion })
    expect(off.wrapper.find('.a2ui-columns').attributes('role')).toBe('img')
  })

  it('a refused draft is not offered as a select option', async () => {
    const { wrapper } = setup([{ id: 's', type: 'select', param: 'region' }, bar], { q: byRegion })
    await wrapper.findAll('.a2ui-bar')[1].trigger('click') // North, refused
    const opts = wrapper.findAll('select option').map((o) => o.text())
    expect(opts).not.toContain('North')
  })

  it('the single tab stop survives the data shrinking', async () => {
    const { wrapper, states } = setup([bar], { q: byRegion })
    ;(wrapper.findAll('.a2ui-bar')[3].element as HTMLElement).focus()
    await wrapper.findAll('.a2ui-bar')[3].trigger('focusin')
    states.get('q')!.data = { ...byRegion, rows: [['East', 1]] }
    await nextTick()
    const bars = wrapper.findAll('.a2ui-bar')
    expect(bars).toHaveLength(1)
    expect(bars[0].attributes('tabindex')).toBe('0')
  })

  it('the pivot marks the row header when the drill column is a row key', async () => {
    const pv = { id: 'p', type: 'pivot', query: 'q', rows: ['region'], columns: 'month', value: 'revenue', drill: { param: 'region', column: 'region' } }
    const { wrapper } = setup([pv], { q: long })
    await wrapper.findAll('tbody button')[0].trigger('click') // East
    expect(wrapper.findAll('tbody th[aria-current=true]').map((h) => h.text())).toEqual(['East'])
    expect(wrapper.findAll('tbody button[aria-current]')).toHaveLength(0)
  })
})
