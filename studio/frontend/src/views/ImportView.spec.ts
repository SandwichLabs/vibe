// Component tests for the Import wizard (#82): file → preview grid with
// per-column types and a key picker → (for an existing table) mode radios
// → a debounced dry-run summary → Import → success banner.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const pushMock = vi.fn()
vi.mock('vue-router', () => ({ useRouter: () => ({ push: pushMock }) }))

import ImportView from './ImportView.vue'

function inspection(over: Partial<ImportInspection> = {}): ImportInspection {
  return {
    columns: [
      { name: 'customer_id', source_name: 'customer_id', inferred: { type: 'integer' }, candidate: true },
      { name: 'name', source_name: 'name', inferred: { type: 'text' }, candidate: false },
      { name: 'credit_limit', source_name: 'credit_limit', inferred: { type: 'decimal', scale: 2 }, candidate: false },
    ],
    rows: [['1', 'Ada', '100.00'], ['2', 'Grace', '200.00']],
    row_count: 2,
    suggested_key: 'customer_id',
    ...over,
  }
}

function summary(over: Partial<ImportSummary> = {}): ImportSummary {
  return {
    ok: true,
    mode: 'create',
    table: 'customers',
    exists: false,
    existing_rows: 0,
    table_key: '',
    modes: ['create'],
    rows: 2,
    inserted: 2,
    updated: 0,
    unchanged: 0,
    replaced: 0,
    exact_dupes: 0,
    missing: null,
    extra: null,
    bad_values: null,
    bad_count: 0,
    blocked: '',
    ...over,
  }
}

interface AppMocks {
  SelectImportFile: ReturnType<typeof vi.fn>
  InspectImport: ReturnType<typeof vi.fn>
  ImportFile: ReturnType<typeof vi.fn>
  ListTables: ReturnType<typeof vi.fn>
}

function makeApp(over: Partial<AppMocks> = {}): AppMocks {
  const app: AppMocks = {
    SelectImportFile: vi.fn(async () => '/tmp/customers.csv'),
    InspectImport: vi.fn(async () => inspection()),
    ImportFile: vi.fn(async (o: ImportOptions) => summary({ table: o.table })),
    ListTables: vi.fn(async () => []),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

function mountView() {
  return mount(ImportView, { global: { stubs: { Icon: true } }, attachTo: document.body })
}

async function pickFile(wrapper: ReturnType<typeof mountView>) {
  await wrapper.find('[data-testid="select-file"]').trigger('click')
  await flushPromises()
}

describe('ImportView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  // Some flows chain two debounced dry runs (e.g. discovering an existing
  // table's modes, then re-running once the default mode is picked), so
  // drain every pending timer rather than advancing by one 300ms tick.
  async function runDebounce() {
    await vi.runAllTimersAsync()
    await flushPromises()
  }

  it('inspects a picked file: preview grid, friendly type labels, key pre-selected to the suggestion', async () => {
    makeApp()
    const wrapper = mountView()
    await pickFile(wrapper)

    const grid = wrapper.find('[data-testid="preview-grid"]')
    expect(grid.exists()).toBe(true)
    expect(grid.text()).toContain('Integer')
    expect(grid.text()).toContain('Decimal (2 places)')
    expect(grid.text()).toContain('Ada')

    const keySelect = wrapper.find('[data-testid="key-select"]').element as HTMLSelectElement
    expect(keySelect.value).toBe('customer_id')
  })

  it('debounces a dry run on a type change and ignores a stale response', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce() // the initial dry run from inspect()

    app.ImportFile.mockClear()
    let resolveFirst: (s: ImportSummary) => void = () => {}
    app.ImportFile.mockImplementationOnce(() => new Promise((r) => { resolveFirst = r }))

    const typeSelect = wrapper.find('[data-testid="type-select-credit_limit"]')
    await typeSelect.setValue('number')
    await vi.advanceTimersByTimeAsync(300)

    // A second change fires before the first dry run resolves.
    app.ImportFile.mockResolvedValueOnce(summary({ rows: 999 }))
    await typeSelect.setValue('text')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    // Now let the first (stale) request resolve; it must not clobber the
    // second, newer summary.
    resolveFirst(summary({ rows: 1 }))
    await flushPromises()

    expect(wrapper.find('[data-testid="summary-line"]').text()).toContain('999')
  })

  it('shows the mode radios for an existing table, pre-selecting modes[0]', async () => {
    makeApp({
      ListTables: vi.fn(async () => ['customers']),
      ImportFile: vi.fn(async () => summary({
        exists: true,
        existing_rows: 40,
        table_key: 'customer_id',
        modes: ['upsert', 'append'],
        mode: 'upsert',
        inserted: 1,
        updated: 1,
        unchanged: 38,
      })),
    })
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce()

    const group = wrapper.find('[data-testid="mode-group"]')
    expect(group.exists()).toBe(true)
    expect(group.text()).toContain('customers already exists (40 rows)')

    const upsert = wrapper.find('[data-testid="mode-upsert"]').element as HTMLInputElement
    expect(upsert.checked).toBe(true)

    const replace = wrapper.find('[data-testid="mode-replace"]').element as HTMLInputElement
    expect(replace.disabled).toBe(true)
    expect(group.text()).toContain('foreign key')

    expect(wrapper.find('[data-testid="summary-line"]').text()).toContain('Will add 1 new row, update 1, leave 38 unchanged.')
  })

  it('never auto-selects a mode when Update matching rows is unavailable — an explicit choice is required', async () => {
    // Modes lists upsert (if eligible), then replace, then append, in that
    // order — so naively pre-selecting modes[0] could silently land on
    // "drop the table" (replace). A second reviewer round caught the
    // follow-on bug: defaulting to append instead isn't safe either — it's
    // only idempotent when the table's key IS in the file (upsert's own
    // precondition), and without one, append's own pre-flight can only
    // WARN about exact duplicates, not prevent them from being pre-checked
    // (they're a warning, not a block) — a silent default that re-adds
    // every row on a re-import is the exact bug the issue exists to fix.
    // No mode may be pre-selected when upsert isn't offered.
    makeApp({
      ListTables: vi.fn(async () => ['customers']),
      ImportFile: vi.fn(async (o: ImportOptions) => summary({
        exists: true,
        existing_rows: 40,
        modes: ['replace', 'append'],
        mode: (o.mode as ImportMode) || 'replace',
        ok: !!o.mode, // no mode chosen yet -> a summary with mode:"" (not OK) is what the backend would send too
        // The exact CLI-worded Blocked sentence the backend sends for a
        // mode:"" dry run against an existing table — this banner text
        // must never reach the UI while no mode is chosen (a reviewer
        // caught it rendering here, alongside a contradictory "Will
        // import N rows" summary line).
        blocked: o.mode ? '' : 'table "customers" already exists (40 rows); pass mode replace|append',
        rows: 5,
      })),
    })
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce()

    const append = wrapper.find('[data-testid="mode-append"]').element as HTMLInputElement
    const replace = wrapper.find('[data-testid="mode-replace"]').element as HTMLInputElement
    expect(append.checked).toBe(false)
    expect(replace.checked).toBe(false)

    const importBtn = wrapper.find('[data-testid="import-btn"]').element as HTMLButtonElement
    expect(importBtn.disabled).toBe(true)

    // The neutral prompt shows instead of the backend's CLI-worded Blocked
    // banner or the (contradictory, mode-"" has no real preview) summary
    // line.
    const prompt = wrapper.find('[data-testid="mode-prompt"]')
    expect(prompt.exists()).toBe(true)
    expect(prompt.text()).toContain('Choose what to do with the existing table above')
    expect(wrapper.find('[data-testid="problem-banner"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('pass mode')
    expect(wrapper.text()).not.toContain('Will import')
  })

  it('"Import as a new table" swaps the table name and hides the mode group', async () => {
    makeApp({
      ListTables: vi.fn(async () => ['customers']),
      ImportFile: vi.fn(async () => summary({ exists: true, existing_rows: 40, modes: ['upsert'], mode: 'upsert' })),
    })
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce()

    expect(wrapper.find('[data-testid="mode-group"]').exists()).toBe(true)
    await wrapper.find('[data-testid="mode-new"]').trigger('change')
    await flushPromises()

    const nameInput = wrapper.find('[data-testid="table-name"]').element as HTMLInputElement
    expect(nameInput.value).toBe('customers_2')
    expect(wrapper.find('[data-testid="mode-group"]').exists()).toBe(false)
  })

  it('a bad-values summary disables Import; fixing the type clears it', async () => {
    const app = makeApp({
      ImportFile: vi.fn(async () => summary({
        ok: false,
        bad_values: [{ row: 2, column: 'credit_limit', value: '12.5.0', want: 'Decimal (2 places)', reason: 'type' }],
        bad_count: 1,
      })),
    })
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce()

    const banner = wrapper.find('[data-testid="problem-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('credit_limit')
    expect(banner.text()).toContain('12.5.0')
    expect((wrapper.find('[data-testid="import-btn"]').element as HTMLButtonElement).disabled).toBe(true)

    app.ImportFile.mockResolvedValue(summary({ ok: true }))
    await wrapper.find('[data-testid="type-select-credit_limit"]').setValue('text')
    await runDebounce()

    expect(wrapper.find('[data-testid="problem-banner"]').exists()).toBe(false)
    expect((wrapper.find('[data-testid="import-btn"]').element as HTMLButtonElement).disabled).toBe(false)
  })

  it('Import runs a real (non-dry-run) call and shows the success banner with Open table → / Import another file', async () => {
    const app = makeApp()
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce()

    await wrapper.find('[data-testid="import-btn"]').trigger('click')
    await flushPromises()

    const realCall = app.ImportFile.mock.calls.find((c: any[]) => c[0].dry_run === false)
    expect(realCall).toBeTruthy()

    const success = wrapper.find('[data-testid="success-banner"]')
    expect(success.exists()).toBe(true)

    await wrapper.find('[data-testid="open-table-btn"]').trigger('click')
    expect(pushMock).toHaveBeenCalledWith({ path: '/tables', query: { table: 'customers' } })

    await wrapper.find('[data-testid="import-another-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="success-banner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="select-file"]').exists()).toBe(true)
  })

  it('hides the wizard body (not a stale "already exists" mode group) once a create import succeeds', async () => {
    // ListTables reports the table as absent until after the real import
    // succeeds — mirroring the file just having created it — so the mode
    // group would otherwise briefly reappear using the pre-import summary.
    let created = false
    makeApp({
      ListTables: vi.fn(async () => (created ? ['customers'] : [])),
      ImportFile: vi.fn(async (o: ImportOptions) => {
        if (o.dry_run === false) created = true
        return summary({ exists: false, existing_rows: 0, modes: ['create'], mode: 'create' })
      }),
    })
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce()

    await wrapper.find('[data-testid="import-btn"]').trigger('click')
    await runDebounce()

    expect(wrapper.find('[data-testid="mode-group"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="success-banner"]').exists()).toBe(true)
  })

  it('surfaces a thrown dry-run error without claiming "Import failed" (the user never clicked Import)', async () => {
    makeApp({
      ImportFile: vi.fn(async () => { throw new Error('sheet not found') }),
    })
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce()

    const banner = wrapper.find('[data-testid="error-banner"]')
    expect(banner.text()).toContain('sheet not found')
    expect(banner.text()).not.toContain('Import failed')
  })

  it('titles a real Import failure "Import failed"', async () => {
    makeApp({
      ImportFile: vi.fn(async (o: ImportOptions) => {
        if (o.dry_run === false) throw new Error('disk full')
        return summary({})
      }),
    })
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce()

    await wrapper.find('[data-testid="import-btn"]').trigger('click')
    await flushPromises()

    const banner = wrapper.find('[data-testid="error-banner"]')
    expect(banner.text()).toContain('Import failed')
    expect(banner.text()).toContain('disk full')
  })

  it('shows the exact-dupes append warning in the attn banner, not the plain summary line', async () => {
    makeApp({
      ListTables: vi.fn(async () => ['customers']),
      ImportFile: vi.fn(async (o: ImportOptions) => summary({
        exists: true, existing_rows: 40, modes: ['append'],
        mode: (o.mode as ImportMode) || 'append',
        ok: !!o.mode,
        rows: 40, exact_dupes: o.mode ? 40 : 0,
      })),
    })
    const wrapper = mountView()
    await pickFile(wrapper)
    await runDebounce()

    // Only 'append' is available (no upsert): pickDefaultMode leaves no
    // mode pre-selected (round-3 fix), so nothing renders yet but the
    // neutral prompt — an explicit click is required first.
    expect(wrapper.find('[data-testid="mode-prompt"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="problem-banner"]').exists()).toBe(false)

    await wrapper.find('[data-testid="mode-append"]').setValue(true)
    await runDebounce()

    expect(wrapper.find('[data-testid="mode-prompt"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="summary-line"]').exists()).toBe(false)
    const banner = wrapper.find('[data-testid="problem-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('already in the table exactly')
    // Not blocking — Import stays enabled.
    expect((wrapper.find('[data-testid="import-btn"]').element as HTMLButtonElement).disabled).toBe(false)
  })

  it('shows the sheet select for xlsx and re-inspects on change', async () => {
    const app = makeApp({
      InspectImport: vi.fn(async () => inspection({ sheets: ['People', 'Orders'], sheet: 'People' })),
    })
    const wrapper = mountView()
    await pickFile(wrapper)

    const sheetSel = wrapper.find('[data-testid="sheet-select"]')
    expect(sheetSel.exists()).toBe(true)

    const callsBefore = app.InspectImport.mock.calls.length
    await sheetSel.setValue('Orders')
    await flushPromises()
    expect(app.InspectImport.mock.calls.length).toBeGreaterThan(callsBefore)
    expect(app.InspectImport.mock.calls.at(-1)![0].sheet).toBe('Orders')
  })
})
