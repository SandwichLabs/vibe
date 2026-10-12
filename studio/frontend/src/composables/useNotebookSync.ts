/**
 * useNotebookSync — keeps the open notebook in step with writes made elsewhere
 * (F10): an agent tool appending cells ("notebooks:changed"), and another
 * program changing or deleting the file ("workspace:changed-on-disk").
 *
 * An append merges in ONLY the cells the event names in appendedCellIds, at the
 * end, touching nothing else — a person mid-edit in cell 3 keeps every unsaved
 * keystroke. Our own save echo carries no appendedCellIds and is a fast no-op.
 * Tombstoned ids (deleted locally, not yet confirmed excluded by a save) are
 * never merged back in, even if a racing append read the pre-delete notebook.
 */
import { nextTick, type Ref } from 'vue'
import { App } from '../lib/bindings'
import { cellsToMerge } from '../lib/notebookMerge'
import { buildSavePayload, newCellState, type RawCell } from '../lib/notebookCells'
import type { useNotebookSave } from './useNotebookSave'
import type { useNotebookCells } from './useNotebookCells'

interface NotebooksChangedPayload {
  notebookId: string
  appendedCellIds?: string[]
}

interface NotebookSyncOptions {
  loading: Ref<boolean>
  title: Ref<string>
  save: ReturnType<typeof useNotebookSave>
  nb: ReturnType<typeof useNotebookCells>
  /** Replaces the open copy with what is on disk now. */
  reload: () => Promise<void>
  /** Leaves the editor for the notebook list. */
  leave: () => void
  status: (msg: string) => void
  info: (msg: string) => void
}

export function useNotebookSync(opts: NotebookSyncOptions) {
  const { save, nb } = opts

  // Append events that arrived while a load was in flight (an agent's
  // add_query_cell/create_dashboard can race a notebook load/switch): merged
  // once the load settles instead of dropped. Cleared at the START of every
  // load, since a fresh load already reflects current server state.
  let queue: NotebooksChangedPayload[] = []
  const clearQueue = () => { queue = [] }

  async function handleNotebooksChanged(payload?: NotebooksChangedPayload) {
    if (!payload || !save.id() || payload.notebookId !== save.id()) return
    if (opts.loading.value) {
      queue.push(payload)
      return
    }
    await applyNotebooksChanged(payload)
  }

  // replayPending runs once load(id) has finished (cells populated, editors
  // mounted). Bails if another load has since superseded this one, including
  // mid-replay, since applyNotebooksChanged itself awaits a fetch.
  async function replayPending(id: string) {
    if (save.id() !== id) return
    const queued = queue.filter((p) => p.notebookId === id)
    queue = []
    for (const p of queued) {
      if (save.id() !== id) return
      await applyNotebooksChanged(p)
    }
  }

  async function applyNotebooksChanged(payload: NotebooksChangedPayload) {
    if (!payload.appendedCellIds || payload.appendedCellIds.length === 0) return

    const requestedId = save.id()
    let remote: { title?: string; cells?: RawCell[]; rev?: string } | null = null
    try {
      remote = await App.GetNotebook(requestedId)
    } catch {
      return // notebook deleted, or a transient error — nothing safe to merge
    }
    // The route may have switched to a different notebook while this fetch was
    // in flight — discard rather than merging a stale notebook's cells.
    if (!remote || save.id() !== requestedId) return

    const cells = nb.cells
    const incoming = cellsToMerge(cells.value, remote.cells || [], payload.appendedCellIds, save.tombstoned())
    if (incoming.length === 0) return
    const appended = new Set(payload.appendedCellIds)
    save.adoptRemote(
      buildSavePayload(
        requestedId,
        remote.title ?? opts.title.value,
        (remote.cells || []).filter((c) => !appended.has(c.id)).map((c) => newCellState(c as RawCell)),
      ),
      remote.rev,
    )
    for (const raw of incoming) cells.value.push(newCellState(raw as RawCell))
    await nextTick()
    nb.mountEditors()
    nb.setupCellObserver()
    for (const raw of incoming) {
      const cell = cells.value.find((c) => c.id === raw.id)
      if (cell && cell.type === 'sql' && cell.source.trim()) nb.runCell(cell)
    }
  }

  // handleChangedOnDisk: something other than this window wrote a file in the
  // workspace. For THIS notebook: with no unsaved edit it is quietly reloaded;
  // with one, the conflict banner shows and nothing is overwritten.
  async function handleChangedOnDisk(p: { kind: string; id: string; op: string }) {
    if (p?.kind !== 'notebook' || !save.id() || p.id !== save.id()) return
    const hasEdit = save.hasEdit()
    if (p.op === 'deleted') {
      if (!hasEdit) {
        opts.status('This notebook was deleted on disk.')
        opts.leave()
      } else {
        save.raiseConflict(true)
      }
      return
    }
    if (!hasEdit && !save.conflict.value) {
      await opts.reload()
      opts.info(`"${opts.title.value || 'This notebook'}" changed on disk and was reloaded.`)
      return
    }
    save.raiseConflict(false)
  }

  return { clearQueue, replayPending, handleNotebooksChanged, handleChangedOnDisk }
}
