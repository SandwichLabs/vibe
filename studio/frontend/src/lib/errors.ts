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
