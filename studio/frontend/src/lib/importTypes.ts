// Friendly labels and small pure helpers for the Import wizard (#82).
// The type list mirrors internal/importer.Types (types.go) — keep the two in
// sync if either changes.

// Sentinel values for Options.Key / Inspection.SuggestedKey, mirroring
// importer.KeyAuto / importer.KeyNone.
export const KEY_AUTO = '+auto'
export const KEY_NONE = ''

export const IMPORT_TYPES: ImportFriendlyType[] = [
  'text',
  'integer',
  'decimal',
  'number',
  'date',
  'timestamp',
  'boolean',
]

// typeLabel renders a ColType in plain words for the preview grid's type
// selects. Decimal carries its scale in the label so "money" (scale 2) reads
// differently from a 4-place import.
export function typeLabel(t: ImportColType): string {
  switch (t.type) {
    case 'text': return 'Text'
    case 'integer': return 'Integer'
    case 'decimal': {
      const scale = t.scale ?? 2
      return `Decimal (${scale} place${scale === 1 ? '' : 's'})`
    }
    case 'number': return 'Number'
    case 'date': return 'Date'
    case 'timestamp': return 'Date & time'
    case 'boolean': return 'True/False'
    default: return t.type
  }
}

// normalizeIdentifier mirrors internal/importer.NormalizeName exactly: a
// string that's already a valid identifier is kept as-is (including case),
// so "Customers" stays "Customers"; anything else is lower-cased, every run
// of non-identifier characters collapses to one underscore, and a leading
// digit gets an underscore prefix so the result is always usable as a SQL
// identifier. This is the one place either a table or column default name
// is derived on the frontend — keep it in sync with NormalizeName if either
// changes.
const IDENT_RE = /^[a-zA-Z_][a-zA-Z0-9_]*$/
const NON_IDENT_RE = /[^a-zA-Z0-9_]+/g

export function normalizeIdentifier(s: string): string {
  const trimmed = s.trim()
  if (IDENT_RE.test(trimmed)) return trimmed
  // U+0130 (LATIN CAPITAL LETTER I WITH DOT ABOVE, "İ") folded to a plain
  // "i" BEFORE lower-casing, so this agrees with Go's NormalizeName on
  // "İstanbul": String.prototype.toLowerCase() decomposes İ into TWO
  // codepoints per Unicode's default (non-Turkish) case folding ("i" plus
  // a combining dot above, U+0307), which the non-identifier-char
  // collapse below would then replace with a "_" ("i_stanbul") — Go's
  // strings.ToLower collapses İ straight to one "i" ("istanbul") instead,
  // with no combining mark left to strip.
  const folded = trimmed.replace(/İ/g, 'i')
  let norm = folded.toLowerCase().replace(NON_IDENT_RE, '_').replace(/^_+|_+$/g, '')
  if (norm === '') return 'column'
  if (/^[0-9]/.test(norm)) norm = '_' + norm
  return norm
}

// deriveTableName turns a picked file path into a starting table name: the
// file's base name, run through normalizeIdentifier. The importer itself
// normalizes column names the same way (NormalizeName); this is only a
// friendly starting point for the table-name field, which the user can
// still edit.
export function deriveTableName(path: string): string {
  const base = path.split(/[/\\]/).pop()?.replace(/\.(csv|tsv|txt|xlsx)$/i, '') || ''
  if (!base.trim()) return 'table'
  return normalizeIdentifier(base)
}

// nextFreeName returns base if it's not already taken, else the first
// base_2, base_3, ... not in tables. Used by "Import as a new table" to
// pre-fill a name that won't collide with the table the file matched.
// Lower-cased compare, matching DuckDB's own case-insensitive quoted-
// identifier catalog lookup (a table stored as "Customers" collides with a
// candidate name "customers" just as much as an exact-case one would).
export function nextFreeName(base: string, tables: string[]): string {
  const taken = new Set(tables.map((t) => t.toLowerCase()))
  if (!taken.has(base.toLowerCase())) return base
  let n = 2
  while (taken.has(`${base}_${n}`.toLowerCase())) n++
  return `${base}_${n}`
}

// summarySentence renders a dry-run Summary as one plain sentence for the
// wizard's live summary line, in future tense (this is a preview of what
// Import is about to do). It does not repeat a Blocked reason or bad-value
// count — those get their own banner. Pass done: true after a real (non-dry-
// run) import completed, for the matching past-tense phrasing in the
// success banner.
export function summarySentence(s: ImportSummary, done = false): string {
  switch (s.mode) {
    case 'replace':
      return done
        ? `Replaced ${s.replaced} row${s.replaced === 1 ? '' : 's'} with ${s.rows}.`
        : `Will replace ${s.replaced} row${s.replaced === 1 ? '' : 's'} with ${s.rows}.`
    case 'append': {
      let out = done
        ? `Added ${s.rows} new row${s.rows === 1 ? '' : 's'}.`
        : `Will add ${s.rows} new row${s.rows === 1 ? '' : 's'}.`
      if (s.exact_dupes > 0) {
        out += ` ${s.exact_dupes} of ${s.rows === 1 ? 'it is' : 'them are'} already in the table exactly.`
      }
      return out
    }
    case 'upsert':
      return done
        ? `Added ${s.inserted} new row${s.inserted === 1 ? '' : 's'}, updated ${s.updated}, left ${s.unchanged} unchanged.`
        : `Will add ${s.inserted} new row${s.inserted === 1 ? '' : 's'}, update ${s.updated}, leave ${s.unchanged} unchanged.`
    case 'create':
    default:
      return done
        ? `Imported ${s.rows} row${s.rows === 1 ? '' : 's'} into ${s.table}.`
        : `Will import ${s.rows} row${s.rows === 1 ? '' : 's'} into ${s.table}.`
  }
}
