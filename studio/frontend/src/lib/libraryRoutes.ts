// libraryRoutes: the one place the Library's paths are written (docs/plans/library.md L4).
// A later move of the routes is a change to this file.

export type LibraryType = 'notebook' | 'live' | 'page' | 'script'

const LIBRARY_TYPES: readonly LibraryType[] = ['notebook', 'live', 'page', 'script']

export function isLibraryType(v: unknown): v is LibraryType {
  return typeof v === 'string' && (LIBRARY_TYPES as readonly string[]).includes(v)
}

export function notebookPath(id: string): string {
  return `/library/notebook/${encodeURIComponent(id)}`
}

export function artifactPath(name: string): string {
  return `/library/artifact/${encodeURIComponent(name)}`
}

/** The list, optionally filtered to a type and/or opening the New flow for a type. */
export function libraryPath(type?: LibraryType, newType?: LibraryType): string {
  const q = new URLSearchParams()
  if (type) q.set('type', type)
  if (newType) q.set('new', newType)
  const s = q.toString()
  return s ? `/library?${s}` : '/library'
}

/** Back button: return to the list the person came from (keeps its chip), else the default list. */
export function backToLibrary(router: { back(): void; push(to: string): unknown }, fallback: string): void {
  const prev = (globalThis.history?.state as { back?: unknown } | null)?.back
  if (typeof prev === 'string' && prev.startsWith('/library')) router.back()
  else void router.push(fallback)
}
