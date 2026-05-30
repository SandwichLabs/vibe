// P6.4 component test for ImportView (Import Spreadsheet Wizard, mockup-driven).
// Mounts the real ImportView component against a mocked window.go.main.App that
// stubs the P6.3 spreadsheet bindings (SelectSpreadsheetFile / PreviewSpreadsheet
// / ImportSpreadsheet), then asserts the Access-style wizard acceptance behaviour:
//   - selecting an xlsx file calls PreviewSpreadsheet and renders the sheet
//     selector + a preview grid of detected columns and sample rows
//   - toggling the header switch re-previews (hasHeader flips)
//   - editing a column's type + choosing create-vs-append, then clicking Import,
//     calls ImportSpreadsheet with the assembled mapping payload
//   - a backend error surfaces via the error toast/banner
//   - a success shows the rows-imported message
//
// RED step: ImportView.vue is currently CSV-only, so these data-testid hooks and
// the spreadsheet bindings are not wired yet. The test must FAIL on the
// assertions (missing wizard UI), not on an unrelated compile error.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ImportView from './ImportView.vue'

interface AppMocks {
  SelectSpreadsheetFile: ReturnType<typeof vi.fn>
  PreviewSpreadsheet: ReturnType<typeof vi.fn>
  ImportSpreadsheet: ReturnType<typeof vi.fn>
  SelectCSVFile: ReturnType<typeof vi.fn>
  ImportCSV: ReturnType<typeof vi.fn>
}

function makeApp(over: Partial<AppMocks> = {}): AppMocks {
  const preview = (path: string, sheet: string, hasHeader: boolean) => ({
    sheets: ['People', 'Orders'],
    sheet: sheet || 'People',
    columns: hasHeader
      ? [
          { name: 'name', type: 'VARCHAR' },
          { name: 'age', type: 'BIGINT' },
        ]
      : [
          { name: 'column1', type: 'VARCHAR' },
          { name: 'column2', type: 'VARCHAR' },
        ],
    rows: [
      ['Ada', 36],
      ['Grace', 45],
    ],
  })
  const app: AppMocks = {
    SelectSpreadsheetFile: vi.fn(async () => '/tmp/contacts.xlsx'),
    PreviewSpreadsheet: vi.fn(async (path: string, sheet: string, hasHeader: boolean) =>
      preview(path, sheet, hasHeader),
    ),
    ImportSpreadsheet: vi.fn(async () => ({ rows_imported: 2, table_name: 'people' })),
    SelectCSVFile: vi.fn(async () => ''),
    ImportCSV: vi.fn(async () => ({ rows_imported: 0, table_name: '' })),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

function mountView() {
  return mount(ImportView, { global: { stubs: { Icon: true } }, attachTo: document.body })
}

// Drive the wizard up through the loaded xlsx preview so individual tests can
// start from a consistent state.
async function pickXlsx(wrapper: ReturnType<typeof mountView>) {
  await wrapper.find('[data-testid="select-xlsx"]').trigger('click')
  await flushPromises()
}

describe('ImportView (P6.4 Import Spreadsheet Wizard)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('previews an xlsx file, rendering the sheet selector and preview grid', async () => {
    const app = makeApp()
    const wrapper = mountView()

    await pickXlsx(wrapper)

    // PreviewSpreadsheet called for the chosen workbook.
    expect(app.PreviewSpreadsheet).toHaveBeenCalled()
    expect(app.PreviewSpreadsheet.mock.calls[0][0]).toBe('/tmp/contacts.xlsx')

    // Sheet selector lists the workbook's sheets.
    const sheetSel = wrapper.find('[data-testid="sheet-select"]')
    expect(sheetSel.exists()).toBe(true)
    expect(sheetSel.findAll('option').map((o) => o.text())).toEqual(
      expect.arrayContaining(['People', 'Orders']),
    )

    // Preview grid reflects detected columns + sample rows.
    const grid = wrapper.find('[data-testid="preview-grid"]')
    expect(grid.exists()).toBe(true)
    expect(grid.text()).toContain('name')
    expect(grid.text()).toContain('Ada')
    expect(grid.text()).toContain('Grace')
  })

  it('re-previews when the header toggle changes', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await pickXlsx(wrapper)

    const firstCallCount = app.PreviewSpreadsheet.mock.calls.length

    const headerToggle = wrapper.find('[data-testid="header-toggle"]')
    expect(headerToggle.exists()).toBe(true)
    // Default has-header is true; flip it off.
    await headerToggle.setValue(false)
    await flushPromises()

    expect(app.PreviewSpreadsheet.mock.calls.length).toBeGreaterThan(firstCallCount)
    // Re-preview requested without a header row.
    const lastCall = app.PreviewSpreadsheet.mock.calls.at(-1)!
    expect(lastCall[2]).toBe(false)

    // Grid now reflects the header-less detection.
    expect(wrapper.find('[data-testid="preview-grid"]').text()).toContain('column1')
  })

  it('sends the assembled mapping + create mode to ImportSpreadsheet on Import', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await pickXlsx(wrapper)

    // Edit a column's target type.
    const typeSel = wrapper.find('[data-testid="coltype-age"]')
    expect(typeSel.exists()).toBe(true)
    await typeSel.setValue('DOUBLE')

    // Choose create-vs-append: pick create explicitly.
    const modeCreate = wrapper.find('[data-testid="mode-create"]')
    expect(modeCreate.exists()).toBe(true)
    await modeCreate.setValue('create')

    await wrapper.find('[data-testid="import-btn"]').trigger('click')
    await flushPromises()

    expect(app.ImportSpreadsheet).toHaveBeenCalledTimes(1)
    const opts = app.ImportSpreadsheet.mock.calls[0][0]
    expect(opts.path).toBe('/tmp/contacts.xlsx')
    expect(opts.sheet).toBe('People')
    expect(opts.has_header).toBe(true)
    expect(opts.mode).toBe('create')
    // The edited type is carried in the column mapping.
    const ageMap = opts.columns.find((c: any) => c.source === 'age' || c.target === 'age')
    expect(ageMap).toBeTruthy()
    expect(ageMap.type).toBe('DOUBLE')
    // Untouched column keeps its detected type.
    const nameMap = opts.columns.find((c: any) => c.source === 'name' || c.target === 'name')
    expect(nameMap.type).toBe('VARCHAR')
  })

  it('surfaces a backend error via the error toast/banner', async () => {
    const app = makeApp({
      ImportSpreadsheet: vi.fn(async () => {
        throw new Error('sheet not found')
      }),
    })
    const wrapper = mountView()
    await pickXlsx(wrapper)

    await wrapper.find('[data-testid="import-btn"]').trigger('click')
    await flushPromises()

    expect(app.ImportSpreadsheet).toHaveBeenCalled()
    expect(wrapper.text()).toContain('sheet not found')
  })

  it('shows the rows-imported success message', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await pickXlsx(wrapper)

    await wrapper.find('[data-testid="import-btn"]').trigger('click')
    await flushPromises()

    expect(app.ImportSpreadsheet).toHaveBeenCalled()
    expect(wrapper.text()).toContain('2')
    expect(wrapper.text().toLowerCase()).toContain('import complete')
  })
})
