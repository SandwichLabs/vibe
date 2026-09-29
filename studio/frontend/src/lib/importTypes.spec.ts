import { describe, it, expect } from 'vitest'
import { typeLabel, deriveTableName, nextFreeName, summarySentence, normalizeIdentifier, KEY_AUTO } from './importTypes'

describe('normalizeIdentifier', () => {
  it('matches internal/importer.NormalizeName case-by-case (see TestNormalizeName)', () => {
    expect(normalizeIdentifier('Order Date')).toBe('order_date')
    expect(normalizeIdentifier('2024 Sales')).toBe('_2024_sales')
    expect(normalizeIdentifier('')).toBe('column')
    expect(normalizeIdentifier('customer_id')).toBe('customer_id')
    // Already-valid identifiers are returned unchanged, case included.
    expect(normalizeIdentifier('Customers')).toBe('Customers')
  })

  // Shared with the Go side (internal/importer.TestNormalizeName_SharedNameTable)
  // — every pair must match exactly, since both derive default table/column
  // names from the same rule.
  it('matches Go NormalizeName on the shared name table, including non-ASCII input', () => {
    const cases: [string, string][] = [
      ['Order Date', 'order_date'],
      ['order_date', 'order_date'],
      ['Customers', 'Customers'],
      ['2024 Sales', '_2024_sales'],
      ['  x  ', 'x'],
      ['', 'column'],
      ['___', '___'],
      ['Zoë', 'zo'],
      ['ÄÖÜ name', 'name'],
      // U+0130 (İ): JS's toLowerCase() decomposes it into "i" + a
      // combining dot above (U+0307) where Go's strings.ToLower collapses
      // it straight to "i" — normalizeIdentifier folds İ to plain "i"
      // before lower-casing specifically to agree with Go here.
      ['İstanbul', 'istanbul'],
      ['Straße', 'stra_e'],
      ['ǅemal', 'emal'],
      ['a-b-c', 'a_b_c'],
      ['A__B', 'A__B'],
      ['_vibe_x', '_vibe_x'],
      ['select', 'select'],
      ['   ', 'column'],
      ['日本', 'column'],
      ['x\ty', 'x_y'],
      ['Ω', 'column'],
      ['ſ', 'column'],
      ['K', 'K'],
      ['Ǆ', 'column'],
    ]
    for (const [input, want] of cases) {
      expect(normalizeIdentifier(input), `normalizeIdentifier(${JSON.stringify(input)})`).toBe(want)
    }
  })
})

describe('typeLabel', () => {
  it('renders each friendly type in plain words', () => {
    expect(typeLabel({ type: 'text' })).toBe('Text')
    expect(typeLabel({ type: 'integer' })).toBe('Integer')
    expect(typeLabel({ type: 'decimal', scale: 2 })).toBe('Decimal (2 places)')
    expect(typeLabel({ type: 'decimal', scale: 1 })).toBe('Decimal (1 place)')
    expect(typeLabel({ type: 'decimal' })).toBe('Decimal (2 places)')
    expect(typeLabel({ type: 'number' })).toBe('Number')
    expect(typeLabel({ type: 'date' })).toBe('Date')
    expect(typeLabel({ type: 'timestamp' })).toBe('Date & time')
    expect(typeLabel({ type: 'boolean' })).toBe('True/False')
  })
})

describe('deriveTableName', () => {
  it('slugs the file base name, dropping the extension', () => {
    expect(deriveTableName('/tmp/customers.csv')).toBe('customers')
    expect(deriveTableName('/tmp/Order Items.xlsx')).toBe('order_items')
    // A leading digit after slugging gets an underscore prefix, matching
    // NormalizeName — without it the name fails ValidIdentifier.
    expect(deriveTableName('C:\\data\\2024 Sales.tsv')).toBe('_2024_sales')
  })

  it('keeps the case of a base name that is already a valid identifier, matching NormalizeName', () => {
    // internal/importer.NormalizeName only touches a string that ISN'T
    // already a valid identifier — this is what made the CLI's
    // tableNameFromCSVPath("Customers.csv") and Studio's old, always-
    // lower-cased deriveTableName disagree.
    expect(deriveTableName('/tmp/Customers.csv')).toBe('Customers')
  })

  it('falls back to "table" for an unusable name', () => {
    expect(deriveTableName('/tmp/.csv')).toBe('table')
  })
})

describe('nextFreeName', () => {
  it('returns the base name when free', () => {
    expect(nextFreeName('customers', ['orders'])).toBe('customers')
  })

  it('appends _2, _3, ... until a free name is found', () => {
    expect(nextFreeName('customers', ['customers'])).toBe('customers_2')
    expect(nextFreeName('customers', ['customers', 'customers_2'])).toBe('customers_3')
  })
})

describe('summarySentence', () => {
  const base = {
    ok: true,
    table: 'customers',
    exists: true,
    existing_rows: 40,
    table_key: 'customer_id',
    modes: ['upsert'] as ImportMode[],
    rows: 40,
    inserted: 0,
    updated: 0,
    unchanged: 40,
    replaced: 0,
    exact_dupes: 0,
    missing: null,
    extra: null,
    bad_values: null,
    bad_count: 0,
    blocked: '',
  }

  it('describes a create', () => {
    expect(summarySentence({ ...base, mode: 'create', exists: false, rows: 5 }))
      .toBe('Will import 5 rows into customers.')
  })

  it('describes a replace', () => {
    expect(summarySentence({ ...base, mode: 'replace', replaced: 40, rows: 32 }))
      .toBe('Will replace 40 rows with 32.')
  })

  it('describes an append, noting exact dupes', () => {
    expect(summarySentence({ ...base, mode: 'append', rows: 40, exact_dupes: 40 }))
      .toBe('Will add 40 new rows. 40 of them are already in the table exactly.')
    expect(summarySentence({ ...base, mode: 'append', rows: 5, exact_dupes: 0 }))
      .toBe('Will add 5 new rows.')
  })

  it('describes an upsert', () => {
    expect(summarySentence({ ...base, mode: 'upsert', inserted: 5, updated: 3, unchanged: 789 }))
      .toBe('Will add 5 new rows, update 3, leave 789 unchanged.')
    expect(summarySentence({ ...base, mode: 'upsert', inserted: 0, updated: 0, unchanged: 40 }))
      .toBe('Will add 0 new rows, update 0, leave 40 unchanged.')
  })
})

describe('summarySentence(s, done)', () => {
  const base = {
    ok: true,
    table: 'customers_2',
    exists: false,
    existing_rows: 0,
    table_key: '',
    modes: ['create'] as ImportMode[],
    rows: 40,
    inserted: 0,
    updated: 0,
    unchanged: 0,
    replaced: 0,
    exact_dupes: 0,
    missing: null,
    extra: null,
    bad_values: null,
    bad_count: 0,
    blocked: '',
  }

  it('uses past tense for a completed import, future tense for a dry run', () => {
    expect(summarySentence({ ...base, mode: 'create' }, true)).toBe('Imported 40 rows into customers_2.')
    expect(summarySentence({ ...base, mode: 'create' }, false)).toBe('Will import 40 rows into customers_2.')
    expect(summarySentence({ ...base, mode: 'upsert', inserted: 5, updated: 3, unchanged: 789 }, true))
      .toBe('Added 5 new rows, updated 3, left 789 unchanged.')
  })
})

describe('KEY_AUTO', () => {
  it('is the importer sentinel', () => {
    expect(KEY_AUTO).toBe('+auto')
  })
})
