import { EditorView, keymap } from '@codemirror/view'
import { EditorState } from '@codemirror/state'
import { markdown } from '@codemirror/lang-markdown'
import { sql as sqlLang } from '@codemirror/lang-sql'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'

/** SchoolHouse theme for the CodeMirror editor. */
const MONO = "'JetBrains Mono', 'SF Mono', Menlo, monospace"
const vibeTheme = EditorView.theme({
  '&': {
    backgroundColor: '#FAF7F0',
    color: '#1F2330',
    fontFamily: MONO,
    fontSize: '14px',
    height: '100%',
  },
  '.cm-content': {
    caretColor: '#1F2330',
    padding: '16px',
    minHeight: '100%',
  },
  '.cm-cursor, .cm-dropCursor': {
    borderLeftColor: '#1F2330',
    borderLeftWidth: '2px',
  },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': {
    backgroundColor: 'rgba(47, 143, 135, 0.18) !important',
  },
  '.cm-activeLine': {
    backgroundColor: 'rgba(47, 143, 135, 0.07)',
  },
  '.cm-gutters': {
    backgroundColor: '#F2EDDF',
    color: '#5C6275',
    borderRight: '2px solid #1F2330',
    fontFamily: MONO,
    fontSize: '12px',
  },
  '.cm-activeLineGutter': {
    backgroundColor: 'rgba(47, 143, 135, 0.14)',
    color: '#1F2330',
  },
  '.cm-scroller': {
    overflow: 'auto',
  },
  '.cm-focused': {
    outline: 'none',
  },
})

/** Syntax highlighting colors matching the SchoolHouse palette. */
const vibeHighlightStyle = HighlightStyle.define([
  { tag: tags.heading, fontWeight: '700', color: '#1F2330' },
  { tag: tags.emphasis, fontStyle: 'italic', color: '#5C6275' },
  { tag: tags.strong, fontWeight: '700', color: '#1F2330' },
  { tag: tags.keyword, color: '#C25A1F' },
  { tag: tags.atom, color: '#C25A1F' },
  { tag: tags.number, color: '#2F6FA8' },
  { tag: tags.string, color: '#2F7D4A' },
  { tag: tags.comment, color: '#5C6275', fontStyle: 'italic' },
  { tag: tags.link, color: '#2F8F87', textDecoration: 'underline' },
  { tag: tags.url, color: '#2F8F87' },
  { tag: tags.monospace, fontFamily: MONO },
  { tag: tags.processingInstruction, color: '#C25A1F', fontWeight: '700' },
  { tag: tags.meta, color: '#5C6275' },
])

export interface VibeEditorOptions {
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
