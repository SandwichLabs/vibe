// toolStep — formats a ToolStep's raw JSON `input` for ToolStepChip's
// expanded body (#91 P-F12/M-F21): the full pretty-printed input plus a
// one-line preview for the collapsed chip, and the `sql` field pulled out
// separately when present so the chip can offer a "Copy SQL" button.
interface FormattedToolInput {
  pretty: string
  sql?: string
  preview: string
}

export function formatToolInput(input?: string): FormattedToolInput {
  if (!input) return { pretty: '', preview: '' }

  let obj: any
  try {
    obj = JSON.parse(input)
  } catch {
    // Not JSON (or malformed) — show the raw string as-is.
    const collapsed = input.replace(/\s+/g, ' ').trim()
    return { pretty: input, preview: collapsed }
  }

  const pretty = JSON.stringify(obj, null, 2)
  const sql = typeof obj?.sql === 'string' ? obj.sql : undefined
  const previewSource = sql ?? input
  const preview = previewSource.replace(/\s+/g, ' ').trim()
  return { pretty, sql, preview }
}
