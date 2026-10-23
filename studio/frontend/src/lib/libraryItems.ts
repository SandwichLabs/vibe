// libraryItems: pure mapping from the two stores' summaries to one list of
// Library items, plus the chips, filter and sort (docs/plans/library.md L3, L5).
// Every string here reaches the page by interpolation only.
import type { LibraryType } from './libraryRoutes'

export interface LibraryItem {
  key: string
  source: 'notebook' | 'artifact'
  /** null: a broken artifact whose kind cannot be read (shown as "Artifact"). */
  type: LibraryType | null
  typeLabel: string
  icon: string
  /** Notebook id or artifact name. */
  id: string
  title: string
  detail: string
  updatedAt: string
  error?: string
}

const TYPE_LABEL: Record<LibraryType, string> = { notebook: 'Notebook', live: 'Live view', page: 'Page', script: 'Chart script' }
const TYPE_ICON: Record<LibraryType, string> = { notebook: 'layers', live: 'barChart', page: 'fileText', script: 'pieChart' }
const CHIP_LABEL: Record<LibraryType, string> = { notebook: 'Notebooks', live: 'Live views', page: 'Pages', script: 'Chart scripts' }

/** Maps ArtifactSummary.view (artifacts.ViewLabel) to a type. */
export function artifactType(view: string): LibraryType | null {
  if (view === 'a2ui') return 'live'
  if (view === 'script') return 'script'
  if (view.startsWith('html')) return 'page'
  return null
}

export function notebookItem(n: WailsNotebookSummary): LibraryItem {
  const cells = `${n.cellCount} cell${n.cellCount === 1 ? '' : 's'}`
  return {
    key: `notebook:${n.id}`, source: 'notebook', type: 'notebook', typeLabel: TYPE_LABEL.notebook, icon: TYPE_ICON.notebook,
    id: n.id, title: n.title || 'Untitled notebook', detail: n.error ? '' : cells, updatedAt: n.updatedAt, error: n.error || undefined,
  }
}

export function artifactItem(a: ArtifactSummary): LibraryItem {
  const type = a.error ? null : artifactType(a.view)
  const bits = [a.name]
  if (a.view === 'html (scripted)') bits.push('script')
  if (a.description) bits.push(a.description)
  return {
    key: `artifact:${a.name}`, source: 'artifact', type,
    typeLabel: type ? TYPE_LABEL[type] : 'Artifact', icon: a.error ? 'alert' : TYPE_ICON[type ?? 'live'],
    id: a.name, title: a.error ? a.name : a.title || a.name, detail: a.error ? '' : bits.join(' · '),
    updatedAt: a.updated_at, error: a.error || undefined,
  }
}

export interface Chip { type: LibraryType | null; label: string; count: number }

/** All, then the types; Chart scripts only when there is one (or it is the open chip). */
export function chipsFor(items: LibraryItem[], active: LibraryType | null): Chip[] {
  const count = (t: LibraryType) => items.filter((i) => i.type === t).length
  const chips: Chip[] = [{ type: null, label: 'All', count: items.length }]
  for (const t of ['notebook', 'live', 'page', 'script'] as const) {
    if (t === 'script' && count(t) === 0 && active !== 'script') continue
    chips.push({ type: t, label: CHIP_LABEL[t], count: count(t) })
  }
  return chips
}

export function filterItems(items: LibraryItem[], type: LibraryType | null, text: string): LibraryItem[] {
  const q = text.trim().toLowerCase()
  return items.filter((i) => (!type || i.type === type) && (!q || i.title.toLowerCase().includes(q) || i.id.toLowerCase().includes(q)))
}

function time(iso: string): number {
  const t = Date.parse(iso)
  return Number.isNaN(t) ? -Infinity : t
}

const byTitle = (a: LibraryItem, b: LibraryItem) => a.title.toLowerCase().localeCompare(b.title.toLowerCase()) || a.id.localeCompare(b.id)

export function sortItems(items: LibraryItem[], by: 'recent' | 'name'): LibraryItem[] {
  const out = [...items]
  if (by === 'name') return out.sort(byTitle)
  return out.sort((a, b) => {
    const d = time(b.updatedAt) - time(a.updatedAt)
    return d === 0 || Number.isNaN(d) ? byTitle(a, b) : d
  })
}

const UNITS: Array<[Intl.RelativeTimeFormatUnit, number]> = [['year', 31536000], ['month', 2592000], ['day', 86400], ['hour', 3600], ['minute', 60]]

/** "2 minutes ago"; '' when the time cannot be read. */
export function relativeTime(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const secs = Math.round((t - now) / 1000)
  const fmt = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  for (const [unit, size] of UNITS) {
    if (Math.abs(secs) >= size) return fmt.format(Math.round(secs / size), unit)
  }
  return fmt.format(0, 'second')
}

export function fullTime(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString()
}

/** For "No pages yet". */
export const CHIP_LABEL_LOWER: Record<LibraryType, string> = { notebook: 'notebooks', live: 'live views', page: 'pages', script: 'chart scripts' }
