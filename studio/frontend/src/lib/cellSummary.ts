// cellSummary — pure helpers for a notebook cell's minimized-row label and
// its "Show SQL" summary line (#95). No Vue, no store: every function takes
// plain data so it's cheap to unit test and cheap to call from a template.

interface SqlSummary {
  lines: number
  firstLine: string
}

// sqlSummary trims the source, counts its lines (0 for empty, after
// trimming), and joins every non-blank, non-`--`-comment line into
// firstLine (still the name — it reads as "the meaningful SQL, one line"),
// whitespace-collapsed and space-joined, not truncated (the caller
// ellipsises via CSS). Multi-line SQL is the norm (formatted by hand or
// authored by the AI), so summarizing as just the statement's first
// keyword ("SELECT") would tell the person nothing at rest. Handles \r\n by
// normalizing to \n before splitting.
export function sqlSummary(source: string): SqlSummary {
  const trimmed = (source ?? '').replace(/\r\n/g, '\n').trim()
  if (trimmed === '') return { lines: 0, firstLine: '' }
  const rawLines = trimmed.split('\n')
  const meaningful: string[] = []
  for (const line of rawLines) {
    const collapsed = line.trim().replace(/\s+/g, ' ')
    if (collapsed === '' || collapsed.startsWith('--')) continue
    meaningful.push(collapsed)
  }
  return { lines: rawLines.length, firstLine: meaningful.join(' ') }
}

interface CellLike {
  type: 'sql' | 'markdown' | 'a2ui'
  label?: string
  source?: string
  page?: { title?: string; components?: unknown[] } | null
}

// cellTitle picks the notebook-list / minimized-row title for a cell:
// sql prefers an explicit label, else the first real line of its source;
// markdown prefers the text of the first ATX heading (`#`..`######`)
// anywhere in the source, else its first non-blank line; a2ui uses the
// page's own title. Returns '' when there's nothing to show — callers
// (aiCellNotebookSummary, the minimized-row title) pick their own fallback.
export function cellTitle(cell: CellLike): string {
  if (cell.type === 'sql') {
    const label = (cell.label ?? '').trim()
    if (label !== '') return label
    return sqlSummary(cell.source ?? '').firstLine
  }
  if (cell.type === 'markdown') {
    const text = (cell.source ?? '').replace(/\r\n/g, '\n')
    const heading = text.match(/^ {0,3}#{1,6}\s+(.+?)\s*#*\s*$/m)
    if (heading) return heading[1].trim()
    for (const line of text.split('\n')) {
      const trimmed = line.trim()
      if (trimmed !== '') return trimmed
    }
    return ''
  }
  // a2ui
  return (cell.page?.title ?? '').trim()
}

export const CELL_FALLBACK_TITLE = {
  sql: 'Untitled query',
  markdown: 'Empty text',
  a2ui: 'Untitled card',
} as const

// cellMeta returns the muted meta text for a cell's minimized row (or the
// closed SQL drawer, if a caller wants it there too). sql: 'error' when the
// last run failed, 'N row(s)' when it succeeded, '' when it hasn't run.
// a2ui: 'N components'. markdown: always ''.
export function cellMeta(cell: CellLike & { result?: { rows: unknown[]; error?: string } | null }): string {
  if (cell.type === 'sql') {
    if (!cell.result) return ''
    if (cell.result.error) return 'error'
    const n = cell.result.rows.length
    return `${n} row${n === 1 ? '' : 's'}`
  }
  if (cell.type === 'a2ui') {
    const n = cell.page?.components?.length ?? 0
    return `${n} component${n === 1 ? '' : 's'}`
  }
  return ''
}
