import { EditorView, keymap } from '@codemirror/view'
import { EditorState } from '@codemirror/state'
import { markdown } from '@codemirror/lang-markdown'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'

/** Neo-brutalist theme for the CodeMirror editor. */
const vibeTheme = EditorView.theme({
  '&': {
    backgroundColor: '#fffbea',
    color: '#000',
    fontFamily: "'Courier New', Courier, monospace",
    fontSize: '14px',
    height: '100%',
  },
  '.cm-content': {
    caretColor: '#000',
    padding: '16px',
    minHeight: '100%',
  },
  '.cm-cursor, .cm-dropCursor': {
    borderLeftColor: '#000',
    borderLeftWidth: '2px',
  },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': {
    backgroundColor: '#FFD700 !important',
  },
  '.cm-activeLine': {
    backgroundColor: 'rgba(255, 215, 0, 0.1)',
  },
  '.cm-gutters': {
    backgroundColor: '#f5f0e8',
    color: '#777',
    borderRight: '3px solid #000',
    fontFamily: "'Courier New', Courier, monospace",
    fontSize: '12px',
  },
  '.cm-activeLineGutter': {
    backgroundColor: '#FFD700',
    color: '#000',
  },
  '.cm-scroller': {
    overflow: 'auto',
  },
  '.cm-focused': {
    outline: 'none',
  },
})

/** Syntax highlighting colors matching the neo-brutalist palette. */
const vibeHighlightStyle = HighlightStyle.define([
  { tag: tags.heading, fontWeight: '700', color: '#000' },
  { tag: tags.emphasis, fontStyle: 'italic', color: '#333' },
  { tag: tags.strong, fontWeight: '700', color: '#000' },
  { tag: tags.keyword, color: '#8b0000' },
  { tag: tags.atom, color: '#8b0000' },
  { tag: tags.number, color: '#005f87' },
  { tag: tags.string, color: '#005f00' },
  { tag: tags.comment, color: '#777', fontStyle: 'italic' },
  { tag: tags.link, color: '#005f87', textDecoration: 'underline' },
  { tag: tags.url, color: '#005f87' },
  { tag: tags.monospace, fontFamily: "'Courier New', Courier, monospace" },
  { tag: tags.processingInstruction, color: '#8b0000', fontWeight: '700' },
  { tag: tags.meta, color: '#777' },
])

export interface VibeEditorOptions {
  /** Initial document content. */
  value?: string
  /** Called whenever the document changes. */
  onChange?: (value: string) => void
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

  const state = EditorState.create({
    doc: options.value ?? '',
    extensions: [
      vibeTheme,
      syntaxHighlighting(vibeHighlightStyle),
      markdown(),
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
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
