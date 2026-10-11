import { EditorView, keymap } from '@codemirror/view'
import { EditorState } from '@codemirror/state'
import { markdown } from '@codemirror/lang-markdown'
import { sql as sqlLang } from '@codemirror/lang-sql'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'

/**
 * Editor theme — every value is a `var(--vibe-*)` role so the editor follows
 * the active theme (Settings → Appearance) with no re-render; see
 * theme/schema.ts for the role list and theme/bridge.css for the fallbacks.
 */
const MONO = 'var(--vibe-font-mono)'
const vibeTheme = EditorView.theme({
  '&': {
    backgroundColor: 'var(--vibe-editor-bg)',
    color: 'var(--vibe-editor-fg)',
    fontFamily: MONO,
    fontSize: 'var(--sh-fs-sm)',
    height: '100%',
  },
  '.cm-content': {
    caretColor: 'var(--vibe-editor-cursor)',
    padding: '16px',
    minHeight: '100%',
  },
  '.cm-cursor, .cm-dropCursor': {
    borderLeftColor: 'var(--vibe-editor-cursor)',
    borderLeftWidth: '2px',
  },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': {
    backgroundColor: 'var(--vibe-editor-selection) !important',
  },
  '.cm-activeLine': {
    backgroundColor: 'var(--vibe-editor-line)',
  },
  '.cm-gutters': {
    backgroundColor: 'var(--vibe-editor-gutter-bg)',
    color: 'var(--vibe-editor-gutter-fg)',
    borderRight: '2px solid var(--vibe-border)',
    fontFamily: MONO,
    fontSize: 'var(--sh-fs-xs)',
  },
  '.cm-activeLineGutter': {
    backgroundColor: 'var(--vibe-editor-line)',
    color: 'var(--vibe-editor-fg)',
  },
  '.cm-scroller': {
    overflow: 'auto',
  },
  // Focus is drawn by theme/bridge.css (`.cm-editor.cm-focused`) so it
  // matches the app-wide focus ring.
  '.cm-focused': {
    outline: 'none',
  },
})

/** Syntax highlighting — colours come from the theme's `syntax.*` roles. */
const vibeHighlightStyle = HighlightStyle.define([
  { tag: tags.heading, fontWeight: '700', color: 'var(--vibe-syntax-heading)' },
  { tag: tags.emphasis, fontStyle: 'italic', color: 'var(--vibe-syntax-meta)' },
  { tag: tags.strong, fontWeight: '700', color: 'var(--vibe-syntax-heading)' },
  { tag: tags.keyword, color: 'var(--vibe-syntax-keyword)' },
  { tag: tags.atom, color: 'var(--vibe-syntax-keyword)' },
  { tag: tags.number, color: 'var(--vibe-syntax-number)' },
  { tag: tags.string, color: 'var(--vibe-syntax-string)' },
  { tag: tags.comment, color: 'var(--vibe-syntax-comment)', fontStyle: 'italic' },
  { tag: tags.link, color: 'var(--vibe-syntax-link)', textDecoration: 'underline' },
  { tag: tags.url, color: 'var(--vibe-syntax-link)' },
  { tag: tags.monospace, fontFamily: MONO },
  { tag: tags.processingInstruction, color: 'var(--vibe-syntax-keyword)', fontWeight: '700' },
  { tag: tags.meta, color: 'var(--vibe-syntax-meta)' },
])

interface VibeEditorOptions {
  /** Initial document content. */
  value?: string
  /** Called whenever the document changes. */
  onChange?: (value: string) => void
  /** Language mode — 'markdown' (default) or 'sql' (notebook sql cells, the Query Explorer). */
  lang?: 'markdown' | 'sql'
  /** Extra key bindings layered on top of the default keymap (e.g. Ctrl/Cmd+Enter to run). */
  extraKeymap?: import('@codemirror/view').KeyBinding[]
}

export interface VibeEditorInstance {
  /** Get the current document text. */
  getValue: () => string
  /** Replace the entire document. */
  setValue: (value: string) => void
  /** The underlying CodeMirror EditorView (for toolbar insertSnippet calls). */
  view: EditorView
  /** Tear down the editor. */
  destroy: () => void
}

/**
 * Initialise a CodeMirror 6 markdown editor inside `el`.
 * Returns helpers to read/write content and clean up.
 */
export function createVibeEditor(
  el: HTMLElement,
  options: VibeEditorOptions = {},
): VibeEditorInstance {
  const onChange = options.onChange

  const updateListener = EditorView.updateListener.of((update) => {
    if (update.docChanged && onChange) {
      onChange(update.state.doc.toString())
    }
  })

  const languageExtension = options.lang === 'sql' ? sqlLang() : markdown()
  const extraKeymap = options.extraKeymap ?? []

  const state = EditorState.create({
    doc: options.value ?? '',
    extensions: [
      vibeTheme,
      syntaxHighlighting(vibeHighlightStyle),
      languageExtension,
      history(),
      // extraKeymap first so a caller's binding (e.g. Ctrl/Cmd+Enter to run
      // a cell) takes priority over the default keymap's own bindings.
      keymap.of([...extraKeymap, ...defaultKeymap, ...historyKeymap]),
      updateListener,
      EditorView.lineWrapping,
    ],
  })

  const view = new EditorView({ state, parent: el })

  return {
    getValue: () => view.state.doc.toString(),
    setValue: (value: string) => {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: value },
      })
    },
    view,
    destroy: () => view.destroy(),
  }
}
