// The KINETIC chart components rendered through A2UIPage: inline columns +
// rows, a live sql binding (and streamChart's refresh), the motion option
// animating geometry frame by frame, and colors coming only from classes.
import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import A2UIPage from './A2UIPage.vue'
import type { A2UIComponentT } from './types'

function render(c: A2UIComponentT) {
  return mount(A2UIPage, { props: { page: { title: 't', components: [c] } } })
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('KINETIC charts — inline data', () => {
  it('areaChart draws one area per numeric column with a legend', () => {
    const w = render({ id: 'a', type: 'areaChart', label: 'Flux', columns: ['hour', 'primary', 'baseline'], rows: [['00', 28, 18], ['03', 44, 25], ['06', 39, 31]] })
    expect(w.find('[data-testid="a2ui-area"]').exists()).toBe(true)
    expect(w.findAll('.a2ui-area__fill')).toHaveLength(2)
    expect(w.findAll('.a2ui-kchart__key').map((k) => k.text())).toEqual(['primary', 'baseline'])
    expect(w.find('svg').attributes('aria-label')).toContain('primary, baseline')
    expect(w.find('.a2ui-kchart__readout').text()).toContain('06')
  })

  it('ringChart draws a ring per row and lists label, value and max', () => {
    const w = render({ id: 'r', type: 'ringChart', rows: [['Tensor', 92], ['VRAM', 78, 120]] })
    expect(w.findAll('.a2ui-rings__arc')).toHaveLength(2)
    const rows = w.findAll('.a2ui-rings__row')
    expect(rows[0].text()).toContain('Tensor')
    expect(rows[1].text()).toContain('/ 120')
    expect(w.find('.a2ui-rings__center').text()).toBe('92%')
  })

  it('columnChart stacks by default, groups on request, and sorts by total', () => {
    const rows = [['East', 1, 1], ['West', 5, 5]]
    const stacked = render({ id: 'c', type: 'columnChart', columns: ['region', 'a', 'b'], rows })
    expect(stacked.findAll('.a2ui-columns__track--stacked')).toHaveLength(2)
    expect(stacked.findAll('.a2ui-columns__seg')).toHaveLength(4)
    expect(stacked.findAll('.a2ui-columns__seg')[2].attributes('style')).toContain('height: 50%')
    const grouped = render({ id: 'c', type: 'columnChart', columns: ['region', 'a', 'b'], rows, mode: 'grouped', sorted: true })
    expect(grouped.findAll('.a2ui-columns__track--grouped')).toHaveLength(2)
    expect(grouped.findAll('.a2ui-columns__label').map((l) => l.text())).toEqual(['West', 'East'])
  })

  it('radarChart needs three axes and ghosts every series after the first', () => {
    const w = render({ id: 'd', type: 'radarChart', columns: ['axis', 'current', 'baseline'], rows: [['A', 90, 60], ['B', 80, 70], ['C', 70, 80]], max: 100 })
    expect(w.findAll('.a2ui-radar__poly')).toHaveLength(2)
    expect(w.findAll('.a2ui-radar__poly--ghost')).toHaveLength(1)
    expect(w.findAll('.a2ui-radar__axis').map((t) => t.text())).toEqual(['A', 'B', 'C'])
    const tooFew = render({ id: 'd', type: 'radarChart', rows: [['A', 1], ['B', 2]] })
    expect(tooFew.find('.a2ui-empty').exists()).toBe(true)
  })

  it('sankeyChart draws a ribbon per flow between named nodes', () => {
    const w = render({ id: 's', type: 'sankeyChart', rows: [['Ingest', 'H100', 32], ['Ingest', 'Edge', 14], ['Cache', 'H100', 12]] })
    expect(w.findAll('.a2ui-sankey__band')).toHaveLength(3)
    expect(w.findAll('.a2ui-sankey__node')).toHaveLength(4)
    expect(w.findAll('.a2ui-sankey__label').map((t) => t.text())).toContain('H100 44')
  })

  it('heatmap quantizes cells into heat classes and leaves gaps empty', () => {
    const w = render({ id: 'h', type: 'heatmap', rows: [['A', '00h', 10], ['A', '04h', 90], ['B', '04h', 50]] })
    const cells = w.findAll('.a2ui-heatmap__cell')
    expect(cells).toHaveLength(4)
    expect(cells[0].classes()).toContain('a2ui-heat-0')
    expect(cells[1].classes()).toContain('a2ui-heat-3')
    expect(cells[2].classes()).toContain('a2ui-heatmap__cell--empty')
  })

  it('streamChart flags values above the threshold and keeps the last `window` points', () => {
    const rows = Array.from({ length: 40 }, (_, i) => [`t${i}`, i === 39 ? 95 : 50])
    const w = render({ id: 't', type: 'streamChart', rows, threshold: 88, window: 10, color: 'error' })
    expect(w.findAll('.a2ui-stream__bar')).toHaveLength(10)
    expect(w.findAll('.a2ui-stream__bar--hot')).toHaveLength(1)
    expect(w.find('.a2ui-stream').classes()).toContain('a2ui--error')
    expect(w.find('.a2ui-stream__latest--hot').text()).toBe('95')
  })

  it('never puts a model string in a style attribute', () => {
    const w = render({ id: 'c', type: 'columnChart', columns: ['k', 'v'], rows: [['red; background:url(x)', 3]] })
    for (const el of w.findAll('[style]')) expect(el.attributes('style')).not.toContain('url(')
  })
})

describe('KINETIC charts — live sql', () => {
  it('reads the bound query result, re-running streamChart on its refresh interval', async () => {
    vi.useFakeTimers()
    let n = 0
    const run = vi.fn(async () => ({ columns: ['ts', 'v'], types: [], rows: [['a', ++n]] }))
    ;(window as any).go = { main: { App: { RunA2UIQuery: run } } }
    const w = render({ id: 't', type: 'streamChart', sql: 'SELECT ts, v FROM t', refresh: 5 })
    await w.vm.$nextTick()
    expect(w.find('.a2ui-loading').exists()).toBe(true)
    await flushPromises()
    expect(w.find('.a2ui-stream__latest').text()).toBe('1')
    expect(w.find('.a2ui-stream__live').text()).toContain('5s')
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(run).toHaveBeenCalledTimes(2)
    expect(w.find('.a2ui-stream__latest').text()).toBe('2')
    w.unmount()
    await vi.advanceTimersByTimeAsync(10000)
    expect(run).toHaveBeenCalledTimes(2)
  })

  it('shows a query error inline', async () => {
    ;(window as any).go = { main: { App: { RunA2UIQuery: vi.fn(async () => { throw new Error('nope') }) } } }
    const w = render({ id: 'h', type: 'heatmap', sql: 'SELECT 1' })
    await flushPromises()
    expect(w.find('.a2ui-error').text()).toContain('nope')
  })
})

describe('KINETIC charts — motion', () => {
  function stubAnimationFrames() {
    let now = 0
    const queue: FrameRequestCallback[] = []
    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => queue.push(cb))
    vi.stubGlobal('cancelAnimationFrame', () => { queue.length = 0 })
    vi.spyOn(performance, 'now').mockImplementation(() => now)
    return (ms: number) => {
      now += ms
      const due = queue.splice(0)
      due.forEach((cb) => cb(now))
    }
  }

  it('sweeps from zero to the targets and offers a replay', async () => {
    const step = stubAnimationFrames()
    const w = render({ id: 'c', type: 'columnChart', columns: ['k', 'v'], rows: [['a', 10], ['b', 5]], motion: { easing: 'expo', speed: 2 } })
    const seg = () => w.findAll('.a2ui-columns__seg')[0].attributes('style')
    expect(seg()).toContain('height: 0%')
    step(100)
    await w.vm.$nextTick()
    const mid = parseFloat(seg()!.match(/height: ([\d.]+)%/)![1])
    expect(mid).toBeGreaterThan(0)
    expect(mid).toBeLessThan(100)
    step(2000)
    await w.vm.$nextTick()
    expect(seg()).toContain('height: 100%')
    expect(w.find('.a2ui-kchart__replay').exists()).toBe(true)
    await w.find('.a2ui-kchart__replay').trigger('click')
    expect(seg()).toContain('height: 0%')
  })

  it('draws the final frame at once with easing "none" or reduced motion, with no replay button', () => {
    vi.stubGlobal('matchMedia', () => ({ matches: true }))
    vi.stubGlobal('requestAnimationFrame', () => 0)
    const w = render({ id: 'b', type: 'barChart', bars: [{ label: 'a', value: 4 }, { label: 'b', value: 2 }] })
    expect(w.findAll('.a2ui-bar__fill')[0].attributes('style')).toContain('width: 100%')
    const none = render({ id: 'r', type: 'ringChart', rows: [['a', 50]], motion: { easing: 'none' } })
    expect(none.find('.a2ui-kchart__replay').exists()).toBe(false)
  })
})
