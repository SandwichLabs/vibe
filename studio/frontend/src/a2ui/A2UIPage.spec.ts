// Component tests for the A2UI renderer (#67): static (inline-data)
// rendering for every component type, live "sql" binding through
// RunA2UIQuery (loading -> data, and loading -> error), nested
// container/statRow children, the zero-baseline bar chart, the SVG line
// chart's accessibility attributes and X-label thinning, and the closed
// color enum being applied only via class, never a style attribute.
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import A2UIPage from './A2UIPage.vue'
import type { A2UIPageT } from './types'

// Read a2ui.css directly off disk (a `?raw` Vite import resolved empty in
// this project's vitest setup, likely intercepted by another CSS-handling
// plugin) for the two source-rule assertions below, which check declared
// CSS rules rather than jsdom's unreliable getComputedStyle() resolution of
// custom properties.
const a2uiCSS = readFileSync(resolve(process.cwd(), 'src/a2ui/a2ui.css'), 'utf-8')

function setRunA2UIQuery(impl: (sql: string) => Promise<{ columns: string[]; rows: any[][] }>) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: { RunA2UIQuery: vi.fn(impl) } } }
}

function page(components: A2UIPageT['components'], title = 'Test page'): A2UIPageT {
  return { title, components }
}

describe('A2UIPage — static (inline data) rendering', () => {
  it('renders the page title and a heading', () => {
    const wrapper = mount(A2UIPage, { props: { page: page([{ id: 'h', type: 'heading', text: 'Hello', level: 1 }], 'My Page') } })
    expect(wrapper.find('.a2ui-page__title').text()).toBe('My Page')
    expect(wrapper.find('h1.a2ui-heading').text()).toBe('Hello')
  })

  it('renders text with the muted modifier', () => {
    const wrapper = mount(A2UIPage, { props: { page: page([{ id: 't', type: 'text', text: 'hi', muted: true }]) } })
    expect(wrapper.find('.a2ui-text').classes()).toContain('a2ui-text--muted')
  })

  it('renders a divider', () => {
    const wrapper = mount(A2UIPage, { props: { page: page([{ id: 'd', type: 'divider' }]) } })
    expect(wrapper.find('hr.a2ui-divider').exists()).toBe(true)
  })

  it('renders inline stat value and detail', () => {
    const wrapper = mount(A2UIPage, {
      props: { page: page([{ id: 's', type: 'stat', label: 'Total', value: 42, detail: 'this month' }]) },
    })
    expect(wrapper.find('.a2ui-stat__label').text()).toBe('Total')
    expect(wrapper.find('.a2ui-stat__value').text()).toBe('42')
    expect(wrapper.find('.a2ui-stat__detail').text()).toBe('this month')
  })

  it('renders an inline table', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([{ id: 'tb', type: 'table', columns: ['a', 'b'], rows: [['x', 1], ['y', 2]] }]),
      },
    })
    expect(wrapper.findAll('th').map((n) => n.text())).toEqual(['a', 'b'])
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].findAll('td').map((n) => n.text())).toEqual(['x', '1'])
  })

  // types is the client-only field NotebookView's synthesizedPage
  // stamps on a table/stat component so it formats with the column's real
  // DuckDB type (never part of the saved/validated A2UI schema).
  it('renders a synthesized table with types: DECIMAL formatted to its scale, grouped, right-aligned', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([{
          id: 'tb',
          type: 'table',
          columns: ['id', 'amt'],
          rows: [['9007199254740993', 15675.3]],
          types: ['BIGINT', 'DECIMAL(12,2)'],
        }]),
      },
    })
    const cells = wrapper.findAll('tbody td')
    expect(cells[0].text()).toBe('9,007,199,254,740,993')
    expect(cells[1].text()).toBe('15,675.30')
    expect(cells[1].classes()).toContain('cell-num')

    // Both numeric headers also get .cell-num, right-aligning over the
    // right-aligned digits below them
    // (a2ui.css's `.a2ui-table th.cell-num` rule beats the plain left-align
    // rule — that CSS cascade isn't exercised by jsdom, so this proves the
    // class is applied).
    const headers = wrapper.findAll('thead th')
    expect(headers[0].classes()).toContain('cell-num')
    expect(headers[1].classes()).toContain('cell-num')
  })

  // The a2ui-table's numeric-header override lives in a2ui.css itself
  // (unlayered, so it always beats theme/bridge.css's layered rules
  // regardless of specificity — see that file's comment), and the old
  // one-off `.a2ui-td--num` class is gone.
  it('a2ui.css has a higher-specificity th.cell-num override and no dead .a2ui-td--num rule', () => {
    expect(a2uiCSS).toMatch(/\.a2ui-page \.a2ui-table th\.cell-num\s*{/)
    expect(a2uiCSS).not.toMatch(/\.a2ui-td--num\s*{/)
  })

  it('renders an inline bar chart, zero-baselined, with numeric-only widths', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([{ id: 'b', type: 'barChart', bars: [{ label: 'a', value: 200 }, { label: 'b', value: 100 }] }]),
      },
    })
    const bars = wrapper.findAll('.a2ui-bar')
    expect(bars).toHaveLength(2)
    // max=200, min=min(0,100,200)=0 -> span=200 -> first bar 100%, second 50%.
    const fills = wrapper.findAll('.a2ui-bar__fill')
    expect(fills[0].attributes('style')).toContain('width: 100%')
    expect(fills[1].attributes('style')).toContain('width: 50%')
  })

  it('renders an inline line chart as an accessible SVG', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([
          { id: 'l', type: 'lineChart', points: [{ label: 'fall', value: 201 }, { label: 'winter', value: 209 }] },
        ]),
      },
    })
    const svg = wrapper.find('svg.a2ui-linechart__svg')
    expect(svg.exists()).toBe(true)
    expect(svg.attributes('role')).toBe('img')
    expect(svg.attributes('aria-label')).toContain('Line chart of 2 points')
    expect(wrapper.find('polyline.a2ui-linechart__line').exists()).toBe(true)
    expect(wrapper.findAll('circle.a2ui-linechart__dot')).toHaveLength(2)
  })

  it('thins X labels for a long series, always showing the first and last', () => {
    const points = Array.from({ length: 40 }, (_, i) => ({ label: `p${i}`, value: i }))
    const wrapper = mount(A2UIPage, { props: { page: page([{ id: 'l', type: 'lineChart', points }]) } })
    // Axis labels render as HTML, not SVG <text> (review fix — legibility at
    // the agent panel's narrow width), so the Y value labels and thinned X
    // labels are found separately. The exact thinning algorithm (picking by
    // real measured pixel width, not a fixed index count) is unit-tested
    // directly against pickLabelIndices in query.spec.ts — jsdom's
    // clientWidth is always 0, so this only checks the structural
    // guarantees that hold regardless of measured width.
    const yLabels = wrapper.findAll('.a2ui-linechart__axis-y')
    const xLabels = wrapper.findAll('.a2ui-linechart__axis-x')
    expect(yLabels).toHaveLength(2)
    expect(xLabels.length).toBeLessThan(40) // thinned, not every point labeled
    expect(xLabels[0].text()).toBe('p0')
    expect(xLabels[xLabels.length - 1].text()).toBe('p39')
  })

  // Review fix (#67 step 2 round 3, item 3): the first/last X-axis label
  // used to always center on its point (translateX(-50%)), including the
  // first and last points which sit at the very left/right edge of the
  // plot — so half of THEIR label text rendered outside the card
  // ("2026-06" clipped to "2026-0"). The first label must now anchor its
  // left edge to its point (grow rightward, into the card) and the last
  // must anchor its right edge (grow leftward); everything in between
  // still centers.
  it('anchors the first line-chart X label left and the last right, so neither clips past the card edge', () => {
    const points = [
      { label: '2026-01', value: 1 },
      { label: '2026-02', value: 2 },
      { label: '2026-03', value: 3 },
      { label: '2026-04', value: 4 },
      { label: '2026-05', value: 5 },
      { label: '2026-06', value: 6 },
    ]
    const wrapper = mount(A2UIPage, { props: { page: page([{ id: 'l', type: 'lineChart', points }]) } })
    const xLabels = wrapper.findAll('.a2ui-linechart__axis-x')
    expect(xLabels.length).toBe(6) // 6 points, well under the 8-label thinning cap
    expect(xLabels[0].text()).toBe('2026-01')
    expect(xLabels[0].attributes('style')).toContain('transform: translateX(0')
    expect(xLabels[xLabels.length - 1].text()).toBe('2026-06')
    expect(xLabels[xLabels.length - 1].attributes('style')).toContain('translateX(-100%')
    // A middle label still centers on its point.
    const middle = xLabels.find((l) => l.text() === '2026-03')!
    expect(middle.attributes('style')).toContain('translateX(-50%')
  })

  it('renders line-chart axis labels as real HTML text outside the SVG, at a fixed 11px', () => {
    const points = [{ label: 'fall', value: 201 }, { label: 'winter', value: 209 }]
    const wrapper = mount(A2UIPage, { props: { page: page([{ id: 'l', type: 'lineChart', points }]) } })
    const yLabel = wrapper.find('.a2ui-linechart__axis-y')
    const xLabel = wrapper.find('.a2ui-linechart__axis-x')
    expect(yLabel.exists()).toBe(true)
    expect(xLabel.exists()).toBe(true)
    // Neither is inside the <svg> — an SVG <text> element's font-size is in
    // viewBox user units, which shrink with however much the SVG itself is
    // scaled down; HTML text is not. (jsdom does not resolve CSS custom
    // properties through getComputedStyle reliably enough to assert the
    // rendered pixel size here — the a2ui.css source-rule test below and
    // the live-QA screenshots are the source of truth for the actual 11px.)
    expect(wrapper.find('svg text').exists()).toBe(false)
    expect(yLabel.element.tagName).toBe('DIV')
    expect(xLabel.element.tagName).toBe('DIV')
  })

  // Review fix, live-QA catch: a plain `.a2ui-page__title { font-size:
  // var(--sh-fs-lg) }` (specificity 0,1,0) lost to the shipped `.sh h2 {
  // font-size: var(--sh-fs-3xl) }` (0,1,1) and rendered the page title at
  // 36px instead of 18px — caught by screenshot, not by any unit test,
  // because jsdom doesn't resolve CSS custom properties through
  // getComputedStyle reliably enough to catch a specificity loss this way.
  // Guard against a regression structurally instead: every a2ui.css rule
  // (other than the `.a2ui-page` root itself) must be scoped under
  // `.a2ui-page ` so its specificity reliably outranks any `.sh <tag>`
  // rule regardless of stylesheet load order.
  it('scopes every a2ui.css rule under .a2ui-page (except the root) so it outranks .sh <tag> rules', () => {
    const selectorLines = a2uiCSS
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l.includes('{') && !l.startsWith('/*') && !l.startsWith('*'))
    expect(selectorLines.length).toBeGreaterThan(20)
    for (const line of selectorLines) {
      const selector = line.split('{')[0].trim()
      if (selector === '.a2ui-page') continue
      // At-rules (@keyframes, @media) are not selectors; rules inside an
      // @media block are on their own lines and still checked.
      if (selector.startsWith('@')) continue
      expect(selector.startsWith('.a2ui-page ')).toBe(true)
    }
  })

  it('a2ui.css declares the line-chart axis labels at a fixed 11px (not viewBox-relative)', () => {
    const yRule = /\.a2ui-linechart__axis-y\s*{[^}]*}/.exec(a2uiCSS)?.[0] ?? ''
    const xRule = /\.a2ui-linechart__axis-x\s*{[^}]*}/.exec(a2uiCSS)?.[0] ?? ''
    expect(yRule).toContain('font-size: 11px')
    expect(xRule).toContain('font-size: 11px')
    // Must not be inside any svg-scoped selector.
    expect(a2uiCSS).not.toMatch(/a2ui-linechart__svg[^{]*\.a2ui-linechart__axis/)
  })

  it('renders list, badge, callout and code', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([
          { id: 'li', type: 'list', items: ['a', 'b', 'c'] },
          { id: 'bd', type: 'badge', text: 'New' },
          { id: 'co', type: 'callout', text: 'Heads up', icon: '!' },
          { id: 'cd', type: 'code', text: 'SELECT 1', language: 'sql' },
        ]),
      },
    })
    expect(wrapper.findAll('.a2ui-list__items li').map((n) => n.text())).toEqual(['a', 'b', 'c'])
    expect(wrapper.find('.a2ui-badge').text()).toBe('New')
    expect(wrapper.find('.a2ui-callout__text').text()).toBe('Heads up')
    expect(wrapper.find('.a2ui-code code').text()).toBe('SELECT 1')
  })

  it('applies the closed color enum only via class, never a style attribute', () => {
    const wrapper = mount(A2UIPage, {
      props: { page: page([{ id: 'bd', type: 'badge', text: 'x', color: 'yellow' }]) },
    })
    const badge = wrapper.find('.a2ui-badge')
    expect(badge.classes()).toContain('a2ui--yellow')
    expect(badge.attributes('style')).toBeUndefined()
  })

  // Review fix (#67 step 2 round 2, item 6): marigold ("yellow") text on
  // white is ~2:1 contrast. Every color token's TEXT stays ink; the accent
  // color only ever paints a background tint/border/fill.
  it('keeps badge/callout/list text one constant (ink) color for every color token, including yellow', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([
          { id: 'b1', type: 'badge', text: 'x', color: 'yellow' },
          { id: 'b2', type: 'badge', text: 'x', color: 'black' },
          { id: 'b3', type: 'badge', text: 'x', color: 'error' },
          { id: 'b4', type: 'badge', text: 'x', color: 'muted' },
        ]),
      },
    })
    const badges = wrapper.findAll('.a2ui-badge')
    const colors = badges.map((b) => getComputedStyle(b.element).color)
    const borderColors = badges.map((b) => getComputedStyle(b.element).borderColor)
    // Text color is the same across every token (ink, never the accent) —
    // only the border/background tint varies per token.
    expect(new Set(colors).size).toBe(1)
    // The yellow badge's text color must differ from its own border color:
    // if it didn't, the badge would still be using the low-contrast
    // marigold accent as its text color.
    expect(colors[0]).not.toBe(borderColors[0])
  })

  // Review fix (item 5): a bare <h1> must not render larger than the page
  // title (the shipped `.sh h1` rule sizes it at 52px, which is exactly
  // this bug — see a2ui.css's comment on h1.a2ui-heading). jsdom does not
  // resolve CSS custom properties (var(--sh-fs-*)) through getComputedStyle
  // reliably, so this asserts against the declared SchoolHouse token names
  // directly (the same tokens .a2ui-page__title uses, in descending order)
  // rather than a computed pixel value; the live-QA screenshots are the
  // source of truth for the actual rendered sizes.
  it('caps heading levels at explicit sizes at or below the page title token', () => {
    const titleRule = /\.a2ui-page__title\s*{[^}]*}/.exec(a2uiCSS)?.[0] ?? ''
    const h1Rule = /h1\.a2ui-heading\s*{[^}]*}/.exec(a2uiCSS)?.[0] ?? ''
    const h2Rule = /h2\.a2ui-heading\s*{[^}]*}/.exec(a2uiCSS)?.[0] ?? ''
    const h3Rule = /h3\.a2ui-heading\s*{[^}]*}/.exec(a2uiCSS)?.[0] ?? ''
    expect(titleRule).toContain('--sh-fs-lg')
    expect(h1Rule).toContain('--sh-fs-lg') // same as the title, never larger
    expect(h2Rule).toContain('--sh-fs-base') // strictly smaller than lg
    expect(h3Rule).toContain('--sh-fs-sm') // strictly smaller than base
  })

  it('renders h1/h2/h3 as the correct tag for heading.level', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([
          { id: 'h1', type: 'heading', text: 'one', level: 1 },
          { id: 'h2', type: 'heading', text: 'two', level: 2 },
          { id: 'h3', type: 'heading', text: 'three', level: 3 },
        ]),
      },
    })
    expect(wrapper.find('h1.a2ui-heading').exists()).toBe(true)
    expect(wrapper.find('h2.a2ui-heading').exists()).toBe(true)
    expect(wrapper.find('h3.a2ui-heading').exists()).toBe(true)
  })

  // Review fix (item 7): a bar row's value column must not resize the
  // track — every track should be the same rendered width regardless of how
  // many digits the value has.
  it('keeps every bar track the same width regardless of value digit count', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([
          { id: 'b', type: 'barChart', bars: [{ label: 'a', value: 1 }, { label: 'b', value: 123456 }] },
        ]),
      },
    })
    const values = wrapper.findAll('.a2ui-bar__value')
    const widths = values.map((v) => getComputedStyle(v.element).minWidth)
    expect(widths[0]).toBe(widths[1])
    expect(widths[0]).not.toBe('0px')
  })

  it('formats numbers consistently: thousands separators, at most 2 decimals, trailing zeros trimmed', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([
          { id: 's1', type: 'stat', label: 'A', value: 147464.2 },
          { id: 's2', type: 'stat', label: 'B', value: 142225.12 },
          { id: 's3', type: 'stat', label: 'C', value: 1000 },
        ]),
      },
    })
    const values = wrapper.findAll('.a2ui-stat__value').map((v) => v.text())
    expect(values[0]).toBe('147,464.2')
    expect(values[1]).toBe('142,225.12')
    expect(values[2]).toBe('1,000')
  })

  it('clamps container gap to a numeric pixel value, bounded to [0,64]', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([
          { id: 'c1', type: 'container', gap: 999 },
          { id: 'c2', type: 'container', gap: -50 },
        ]),
      },
    })
    const containers = wrapper.findAll('.a2ui-container')
    expect(containers[0].attributes('style')).toContain('gap: 64px')
    expect(containers[1].attributes('style')).toContain('gap: 0px')
  })

  it('nests children under container by parent id, not declaration order', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([
          { id: 'child', type: 'text', text: 'inside', parent: 'box' },
          { id: 'box', type: 'container', layout: 'row' },
        ]),
      },
    })
    const box = wrapper.find('.a2ui-container--row')
    expect(box.find('.a2ui-text').text()).toBe('inside')
  })

  it('nests stat children under statRow', () => {
    const wrapper = mount(A2UIPage, {
      props: {
        page: page([
          { id: 'row', type: 'statRow' },
          { id: 's1', type: 'stat', parent: 'row', label: 'A', value: 1 },
          { id: 's2', type: 'stat', parent: 'row', label: 'B', value: 2 },
        ]),
      },
    })
    const row = wrapper.find('.a2ui-statrow')
    expect(row.findAll('.a2ui-stat')).toHaveLength(2)
  })
})

describe('A2UIPage — live "sql" bindings (RunA2UIQuery)', () => {
  it('stat: shows loading then the first column of the first row', async () => {
    setRunA2UIQuery(async () => ({ columns: ['n'], rows: [[7]] }))
    const wrapper = mount(A2UIPage, {
      props: { page: page([{ id: 's', type: 'stat', label: 'Count', sql: 'SELECT count(*) AS n FROM t' }]) },
    })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.a2ui-stat__value').text()).toBe('…')
    await flushPromises()
    expect(wrapper.find('.a2ui-stat__value').text()).toBe('7')
    expect((window as any).go.main.App.RunA2UIQuery).toHaveBeenCalledWith('SELECT count(*) AS n FROM t')
  })

  it('stat: renders a coral error state on a denied/failed query', async () => {
    setRunA2UIQuery(async () => {
      throw new Error('table "nope" not found')
    })
    const wrapper = mount(A2UIPage, {
      props: { page: page([{ id: 's', type: 'stat', label: 'Count', sql: 'SELECT * FROM nope' }]) },
    })
    await flushPromises()
    expect(wrapper.find('.a2ui-stat__value').text()).toBe('!')
    const err = wrapper.find('.a2ui-error')
    expect(err.exists()).toBe(true)
    expect(err.text()).toContain('not found')
  })

  it('table: renders live columns/rows', async () => {
    setRunA2UIQuery(async () => ({ columns: ['city', 'n'], rows: [['Austin', 3], ['Dallas', 1]] }))
    const wrapper = mount(A2UIPage, {
      props: { page: page([{ id: 't', type: 'table', sql: 'SELECT city, count(*) AS n FROM customers GROUP BY city' }]) },
    })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.a2ui-loading').exists()).toBe(true)
    await flushPromises()
    expect(wrapper.findAll('th').map((n) => n.text())).toEqual(['city', 'n'])
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
  })

  it('table: shows NO ROWS on an empty result', async () => {
    setRunA2UIQuery(async () => ({ columns: ['a'], rows: [] }))
    const wrapper = mount(A2UIPage, { props: { page: page([{ id: 't', type: 'table', sql: 'SELECT 1' }]) } })
    await flushPromises()
    expect(wrapper.find('.a2ui-empty').text()).toContain('NO ROWS')
  })

  it('barChart: builds points from a live query, preferring label/value-ish columns', async () => {
    setRunA2UIQuery(async () => ({ columns: ['city', 'n'], rows: [['Austin', 3], ['Dallas', 1]] }))
    const wrapper = mount(A2UIPage, {
      props: { page: page([{ id: 'b', type: 'barChart', sql: 'SELECT city, count(*) AS n FROM customers GROUP BY city' }]) },
    })
    await flushPromises()
    const bars = wrapper.findAll('.a2ui-bar')
    expect(bars).toHaveLength(2)
    expect(bars[0].find('.a2ui-bar__label').text()).toBe('Austin')
    expect(bars[0].find('.a2ui-bar__value').text()).toBe('3')
  })

  it('lineChart: renders a live-bound SVG', async () => {
    setRunA2UIQuery(async () => ({ columns: ['label', 'value'], rows: [['fall', 201], ['winter', 209]] }))
    const wrapper = mount(A2UIPage, { props: { page: page([{ id: 'l', type: 'lineChart', sql: 'SELECT label, value FROM scores' }]) } })
    await flushPromises()
    expect(wrapper.find('svg.a2ui-linechart__svg').exists()).toBe(true)
    expect(wrapper.findAll('circle.a2ui-linechart__dot')).toHaveLength(2)
  })
})
