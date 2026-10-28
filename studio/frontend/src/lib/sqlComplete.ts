// The one CodeMirror completion source for SQL: the Query Explorer and the
// notebook SQL cells both use it. Candidates come from Go (`CompleteSQL`,
// which reads the live catalog and never connects or runs anything); with no
// workspace, or if the call fails, it falls back to SQL keywords so typing
// never feels dead.
import {
  autocompletion,
  acceptCompletion,
  type Completion,
  type CompletionContext,
  type CompletionResult,
} from '@codemirror/autocomplete'
import type { Extension } from '@codemirror/state'
import { keymap } from '@codemirror/view'
import { StandardSQL, keywordCompletionSource } from '@codemirror/lang-sql'
import { App, hasBinding } from './bindings'

export interface SqlCompletionOptions {
  /** The connection unqualified names resolve against ('' = workspace default). Read per request. */
  connection: () => string
}

/** kindType maps the Go completer's kinds to CodeMirror completion types. */
export const kindType: Record<string, string> = {
  keyword: 'keyword',
  connection: 'namespace',
  schema: 'namespace',
  table: 'class',
  view: 'interface',
  column: 'property',
  function: 'function',
  alias: 'variable',
}

/** kindBoost keeps what the statement names above the catalog above keywords. */
export const kindBoost: Record<string, number> = {
  column: 40, alias: 40, table: 30, view: 30, schema: 20, connection: 20, function: 10, keyword: 0,
}

/** A word being typed: letters, digits, `_`, `$`, and an opening double quote. */
const WORD = /["\w$]*$/
const PLAIN_WORD = /^[\w$]*$/

export function toCompletions(items: CompletionItem[]): Completion[] {
  return items.map((it) => ({
    label: it.label,
    type: kindType[it.kind] ?? 'text',
    detail: it.detail,
    apply: it.insert ?? it.label,
    boost: kindBoost[it.kind] ?? 0,
  }))
}

const standardKeywords = keywordCompletionSource(StandardSQL, true)

/** keywordFallback offers SQL keywords by prefix: what works with no workspace. */
export function keywordFallback(ctx: CompletionContext): CompletionResult | null {
  const word = ctx.matchBefore(WORD)
  if (!word || (word.from === word.to && !ctx.explicit)) return null
  return standardKeywords(ctx) as CompletionResult | null
}

export function sqlSource(opts: SqlCompletionOptions): (ctx: CompletionContext) => Promise<CompletionResult | null> {
  return async (ctx) => {
    const word = ctx.matchBefore(WORD)
    if (!word) return null
    const afterDot = word.from > 0 && ctx.state.sliceDoc(word.from - 1, word.from) === '.'
    if (!ctx.explicit && word.from === word.to && !afterDot) return null // nothing to offer on a bare space
    if (!hasBinding('CompleteSQL')) return keywordFallback(ctx)
    let res: CompleteSQLResult
    try {
      res = await App.CompleteSQL({ sql: ctx.state.doc.toString(), offset: ctx.pos, connection: opts.connection() })
    } catch {
      return ctx.aborted ? null : keywordFallback(ctx)
    }
    if (ctx.aborted) return null // the next keystroke superseded this request
    const options = toCompletions(res.items ?? [])
    const quoted = ctx.state.sliceDoc(res.from, ctx.pos).startsWith('"')
    // Go already filtered by the typed prefix. A quoted word cannot be
    // re-filtered here (the labels have no quote), and a list Go cut at its
    // item cap would hide matches, so in both cases ask Go again instead.
    return quoted || res.incomplete
      ? { from: res.from, options, filter: false }
      : { from: res.from, options, validFor: PLAIN_WORD }
  }
}

/** sqlCompletion is the extension set for one SQL editor. */
export function sqlCompletion(opts: SqlCompletionOptions): Extension[] {
  return [
    autocompletion({
      override: [sqlSource(opts)],
      activateOnTyping: true,
      activateOnTypingDelay: 60,
      interactionDelay: 0,
      icons: false,
      maxRenderedOptions: 60,
      defaultKeymap: true,
    }),
    // Tab accepts while the list is open; otherwise it is unbound, as before.
    keymap.of([{ key: 'Tab', run: acceptCompletion }]),
  ]
}
