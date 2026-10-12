// errorText turns a thrown binding error into user-facing text: the
// message, minus Vibe's Go wrap-chain prefixes ("query failed: ",
// "failed to ...: "). Every catch site that shows an error to the user
// should go through this helper instead of reading e.message ad hoc, so the
// stripping rule lives in exactly one place (#91).
//
// Only these two wrapper shapes are stripped — deliberately not "could not"
// / "unable to", which DuckDB's own root-cause messages start with, so
// stripping those would eat real information.
const WRAP_PREFIX = /^(?:query failed|failed to (?:[^:"]|"[^"]*")*): /i

// settingsError renders a failed settings save: a refusal because the committed
// file changed meanwhile drops the internal "conflict: " marker and says what to
// do (the panels offer a "Reload settings" button when stale is true).
export function settingsError(e: unknown): { text: string; stale: boolean } {
  const t = errorText(e)
  if (isConflict(e)) return { text: t.slice(CONFLICT_PREFIX.length), stale: true }
  return { text: t, stale: false }
}

// CONFLICT_PREFIX is the wire marker studio/workspace.go's conflictErr puts on
// a save refused because the file changed on disk (filestore.ErrConflict). The
// Wails bridge carries errors as plain strings, so this is the one place the
// marker is known on the frontend; views call isConflict instead of matching it.
const CONFLICT_PREFIX = 'conflict: '

// isConflict reports whether a thrown binding error is a compare-and-write
// conflict (the file changed or vanished on disk since it was loaded).
export function isConflict(e: unknown): boolean {
  return errorText(e).startsWith(CONFLICT_PREFIX.trimEnd())
}

// isDeletedConflict is a conflict where the file is gone, not just changed.
export function isDeletedConflict(e: unknown): boolean {
  return isConflict(e) && /deleted/.test(errorText(e))
}

export function errorText(e: unknown): string {
  const raw = typeof e === 'string' ? e : (e as { message?: string } | null | undefined)?.message ?? String(e)
  let message = raw.trim()

  // Strip repeatedly: a wrap chain like "failed to X: failed to Y: query
  // failed: Parser Error: ..." nests more than one wrapper.
  for (;;) {
    const next = message.replace(WRAP_PREFIX, '').trim()
    // If stripping the wrapper would leave nothing, the message was only
    // ever the wrapper itself — stop and keep what we had.
    if (next === '' || next === message) break
    message = next
  }
  return message
}
