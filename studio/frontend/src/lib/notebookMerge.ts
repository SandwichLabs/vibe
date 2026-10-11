// notebookMerge — pure decision logic for NotebookView's "notebooks:changed"
// handler (#66 part B review fix, finding 2: a deleted cell coming back).
//
// The bug: the original handler treated ANY remote cell id the view didn't
// already have locally as something to merge in. That resurrected a cell
// the user had just deleted locally (removed from cells.value, but the
// debounced autosave hadn't persisted the deletion yet) the instant an
// unrelated "notebooks:changed" fired — including the view's OWN save echo,
// and an agent's create_dashboard/add_query_cell call that (racing the same
// window) read the pre-delete notebook from disk and re-saved the "deleted"
// cell right alongside its own new one.
//
// The fix has three parts, each represented here as a pure function so it's
// testable without mounting the component or mocking Wails:
//   1. cellsToMerge — merge ONLY the ids the backend event says were
//      appended (NotebooksChangedEvent.appendedCellIds), never "any id we
//      don't have". An event with no appendedCellIds (every non-append
//      emitter: CreateNotebook/SaveNotebook/DeleteNotebook/
//      ImportNotebook, including the view's own SaveNotebook echo) yields
//      nothing to merge.
//   2. Tombstones — a locally-deleted cell's id is remembered until a save
//      that excludes it succeeds, and a tombstoned id is never merged back
//      in even if it does appear in appendedCellIds (it never should, but
//      never trust that).
//   3. pruneConfirmedTombstones — once OUR OWN save has gone out without a
//      tombstoned id, that id's exclusion is now the durable truth on disk;
//      there's nothing left to protect against, so drop it.
interface MergeCandidateCell {
  id: string
  [key: string]: any
}

/**
 * Decide which remote cells to insert locally. Returns only cells that are
 * (a) named in appendedCellIds, (b) not already present locally, and (c) not
 * tombstoned (a pending local delete this merge must not undo).
 */
export function cellsToMerge(
  localCells: { id: string }[],
  remoteCells: MergeCandidateCell[],
  appendedCellIds: string[] | undefined | null,
  tombstonedIds: ReadonlySet<string>,
): MergeCandidateCell[] {
  if (!appendedCellIds || appendedCellIds.length === 0) return []
  const knownIds = new Set(localCells.map((c) => c.id))
  const appendedSet = new Set(appendedCellIds)
  return remoteCells.filter((c) => appendedSet.has(c.id) && !knownIds.has(c.id) && !tombstonedIds.has(c.id))
}

/**
 * Return a new Set with any tombstoned id that was confirmed absent from
 * justSavedCellIds removed — that deletion is now durably reflected on disk
 * (our own save just wrote a cells array without it), so the tombstone has
 * done its job. A tombstoned id that IS present in justSavedCellIds (a save
 * that was captured/in-flight before the delete happened) is kept, since
 * that save did not actually exclude it.
 */
export function pruneConfirmedTombstones(tombstonedIds: ReadonlySet<string>, justSavedCellIds: ReadonlySet<string>): Set<string> {
  const next = new Set(tombstonedIds)
  for (const id of tombstonedIds) {
    if (!justSavedCellIds.has(id)) next.delete(id)
  }
  return next
}

/** Cap a list to at most maxEntries items, each at most maxChars characters
 * (collapsing embedded newlines) — used for the tables/notebookSummary
 * context sent with a task request (review fix, finding 7). */
export function capPromptList(items: string[], maxEntries = 30, maxChars = 120): string[] {
  const out: string[] = []
  for (const raw of items) {
    if (out.length >= maxEntries) break
    const collapsed = raw.replace(/\s+/g, ' ').trim()
    if (!collapsed) continue
    out.push(collapsed.length > maxChars ? collapsed.slice(0, maxChars) + '…' : collapsed)
  }
  return out
}
