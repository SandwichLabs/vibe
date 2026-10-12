/**
 * useConflict — the conflict-banner flow shared by the notebook, query and skill
 * editors (F3). The open document's file changed on disk while it had unsaved
 * edits (a git pull, another editor, the agent), or a save was refused for that
 * reason: `conflict` holds the banner state, sticky until resolved. Typing is
 * never blocked while it shows.
 *
 *  - raise(deleted) shows the banner and re-fetches the revision on disk EVERY
 *    time, so "Keep mine" only overwrites the version the banner is about (a
 *    stale answer for another document is dropped).
 *  - rev() is that revision, sent back with ResolveConflict.
 *  - resolving(fn, fail) runs one resolution under `busy`; a refusal (the file
 *    changed AGAIN) re-raises the banner afresh and reports CONFLICT_AGAIN.
 *  - clear() is the success reset: banner gone, remembered revision dropped.
 */
import { ref } from 'vue'
import { errorText, isConflict } from '../lib/errors'

export const CONFLICT_AGAIN = 'The file changed on disk again, so nothing was overwritten. Choose again.'

interface ConflictOptions {
  /** The id of the document currently open, or null/'' when none. */
  currentId: () => string | null | undefined
  /** Reads the revision now on disk for id ('' / undefined when unknown). */
  fetchRev: (id: string) => Promise<string | undefined>
}

export function useConflict(opts: ConflictOptions) {
  const conflict = ref<{ deleted: boolean } | null>(null)
  const busy = ref(false)
  // The revision on disk when the banner was raised.
  let rev = ''

  function raise(deleted: boolean) {
    conflict.value = { deleted }
    rev = ''
    const id = opts.currentId()
    if (deleted || !id) return
    opts
      .fetchRev(id)
      .then((r) => {
        if (conflict.value && opts.currentId() === id) rev = r ?? ''
      })
      .catch(() => { /* no rev: falls back to an unconditional keep */ })
  }

  function clear() {
    conflict.value = null
    rev = ''
  }

  async function resolving(run: () => Promise<void>, fail: (text: string) => void, failPrefix = 'Failed: ') {
    busy.value = true
    try {
      await run()
    } catch (e) {
      if (isConflict(e)) {
        // The file changed again after the banner was raised: show it afresh.
        raise(false)
        fail(CONFLICT_AGAIN)
      } else {
        fail(failPrefix + errorText(e))
      }
    } finally {
      busy.value = false
    }
  }

  return { conflict, busy, rev: () => rev, raise, clear, resolving }
}
