// lineDiff — a line-by-line diff of two texts (longest common subsequence), for
// the proposals inbox: the current file beside the proposed one with changed
// lines marked. The same shape as Go's proposals.Diff, drawn client-side.

type DiffOp = ' ' | '-' | '+'
export interface DiffLine { op: DiffOp; text: string }

/** Above this many cell comparisons the texts are shown as all removed, all added. */
const MAX_CELLS = 4_000_000

function lines(s: string): string[] {
  if (!s) return []
  return s.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n')
}

export function diffLines(current: string, proposed: string): DiffLine[] {
  const a = lines(current)
  const b = lines(proposed)
  if (a.length * b.length > MAX_CELLS) {
    return [...a.map((text) => ({ op: '-' as const, text })), ...b.map((text) => ({ op: '+' as const, text }))]
  }
  // lcs[i][j]: length of the longest common subsequence of a[i:] and b[j:].
  const lcs: Uint32Array[] = Array.from({ length: a.length + 1 }, () => new Uint32Array(b.length + 1))
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1])
    }
  }
  const out: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) { out.push({ op: ' ', text: a[i] }); i++; j++ }
    else if (lcs[i + 1][j] >= lcs[i][j + 1]) { out.push({ op: '-', text: a[i] }); i++ }
    else { out.push({ op: '+', text: b[j] }); j++ }
  }
  for (; i < a.length; i++) out.push({ op: '-', text: a[i] })
  for (; j < b.length; j++) out.push({ op: '+', text: b[j] })
  return out
}

/** How many lines were added and removed. */
export function diffStats(d: DiffLine[]): { added: number; removed: number } {
  return { added: d.filter((l) => l.op === '+').length, removed: d.filter((l) => l.op === '-').length }
}
