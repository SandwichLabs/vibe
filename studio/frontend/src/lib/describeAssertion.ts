// describeAssertion — one line for a declared assertion on a definition card:
// its kind, the column it names and what it is held to (accepted values, range
// bounds, freshness window, minimum rows). All of it is file text shown as text.
export function describeAssertion(a: SemanticAssertion): string {
  let out = a.kind + (a.column ? ` on ${a.column}` : '')
  if (a.values?.length) out += ` (${a.values.join(', ')})`
  if (a.kind === 'range') {
    const lo = a.min != null ? `min ${a.min}` : ''
    const hi = a.max != null ? `max ${a.max}` : ''
    out += ` (${[lo, hi].filter(Boolean).join(', ')})`
  }
  if (a.max_age) out += ` (max age ${a.max_age})`
  if (a.min_rows != null) out += ` (min ${a.min_rows} ${a.min_rows === 1 ? 'row' : 'rows'})`
  return out
}
