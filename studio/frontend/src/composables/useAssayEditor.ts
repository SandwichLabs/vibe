import { EditorView, keymap, highlightSpecialChars, placeholder as placeholderExt } from '@codemirror/view'
import { EditorState, type Extension } from '@codemirror/state'
import { markdown } from '@codemirror/lang-markdown'
import { sql as sqlLang } from '@codemirror/lang-sql'
import { json as jsonLang } from '@codemirror/lang-json'
import { javascript as jsLang } from '@codemirror/lang-javascript'
import { yaml as yamlLang } from '@codemirror/lang-yaml'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'

/**
 * Editor theme — every value is a `var(--assay-*)` role so the editor follows
 * the active theme (Settings → Appearance) with no re-render; see
 * theme/schema.ts for the role list and theme/bridge.css for the fallbacks.
 */
const MONO = 'var(--assay-font-mono)'
const assayTheme = EditorView.theme({
  '&': {
    backgroundColor: 'var(--assay-editor-bg)',
    color: 'var(--assay-editor-fg)',
    fontFamily: MONO,
    fontSize: 'var(--sh-fs-sm)',
    height: '100%',
  },
  '.cm-content': {
    caretColor: 'var(--assay-editor-cursor)',
    padding: '16px',
    minHeight: '100%',
  },
  '.cm-cursor, .cm-dropCursor': {
    borderLeftColor: 'var(--assay-editor-cursor)',
    borderLeftWidth: '2px',
  },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': {
    backgroundColor: 'var(--assay-editor-selection) !important',
  },
  '.cm-activeLine': {
    backgroundColor: 'var(--assay-editor-line)',
  },
  '.cm-gutters': {
    backgroundColor: 'var(--assay-editor-gutter-bg)',
    color: 'var(--assay-editor-gutter-fg)',
    borderRight: '2px solid var(--assay-border)',
    fontFamily: MONO,
    fontSize: 'var(--sh-fs-xs)',
  },
  '.cm-activeLineGutter': {
    backgroundColor: 'var(--assay-editor-line)',
    color: 'var(--assay-editor-fg)',
  },
  '.cm-scroller': {
    overflow: 'auto',
  },
  // The SQL completion list (lib/sqlComplete.ts), all `--assay-*` roles.
  '.cm-tooltip.cm-tooltip-autocomplete': {
    backgroundColor: 'var(--assay-surface)',
    color: 'var(--assay-fg)',
    border: '2px solid var(--assay-border)',
    boxShadow: 'var(--assay-shadow)',
    fontFamily: MONO,
    fontSize: 'var(--sh-fs-sm)',
  },
  '.cm-tooltip-autocomplete ul li[aria-selected]': {
    backgroundColor: 'var(--assay-selection)',
    color: 'var(--assay-selection-fg)',
  },
  '.cm-completionDetail': {
    color: 'var(--assay-fg-muted)',
    fontStyle: 'normal',
    marginLeft: '1em',
  },
  '.cm-tooltip-autocomplete ul li[aria-selected] .cm-completionDetail, .cm-tooltip-autocomplete ul li[aria-selected] .cm-completionMatchedText': {
    color: 'inherit',
  },
  '.cm-completionMatchedText': {
    color: 'var(--assay-accent)',
    textDecoration: 'none',
    fontWeight: '700',
  },
  // Focus is drawn by theme/bridge.css (`.cm-editor.cm-focused`) so it
  // matches the app-wide focus ring.
  '.cm-focused': {
    outline: 'none',
  },
})

/** Syntax highlighting — colours come from the theme's `syntax.*` roles. */
const assayHighlightStyle = HighlightStyle.define([
  { tag: tags.heading, fontWeight: '700', color: 'var(--assay-syntax-heading)' },
  { tag: tags.emphasis, fontStyle: 'italic', color: 'var(--assay-syntax-meta)' },
  { tag: tags.strong, fontWeight: '700', color: 'var(--assay-syntax-heading)' },
  { tag: tags.keyword, color: 'var(--assay-syntax-keyword)' },
  { tag: tags.atom, color: 'var(--assay-syntax-keyword)' },
  { tag: tags.bool, color: 'var(--assay-syntax-keyword)' },
  { tag: tags.propertyName, color: 'var(--assay-syntax-heading)' },
  { tag: tags.number, color: 'var(--assay-syntax-number)' },
  { tag: tags.string, color: 'var(--assay-syntax-string)' },
  { tag: tags.comment, color: 'var(--assay-syntax-comment)', fontStyle: 'italic' },
  { tag: tags.link, color: 'var(--assay-syntax-link)', textDecoration: 'underline' },
  { tag: tags.url, color: 'var(--assay-syntax-link)' },
  { tag: tags.monospace, fontFamily: MONO },
  { tag: tags.processingInstruction, color: 'var(--assay-syntax-keyword)', fontWeight: '700' },
  { tag: tags.meta, color: 'var(--assay-syntax-meta)' },
])

interface AssayEditorOptions {
  /** Initial document content. */
  value?: string
  /** Called whenever the document changes. */
  onChange?: (value: string) => void
  /** Language mode — 'markdown' (default), 'sql' (notebook sql cells, the Query Explorer), or 'json' / 'yaml' / 'javascript' (the artifact Source tab and the custom-view trust prompt). */
  lang?: 'markdown' | 'sql' | 'json' | 'yaml' | 'javascript'
  /** Read-only: the text can be selected and copied but not edited (the custom-view trust prompt). */
  readOnly?: boolean
  /** Extra key bindings layered on top of the default keymap (e.g. Ctrl/Cmd+Enter to run). */
  extraKeymap?: import('@codemirror/view').KeyBinding[]
  /** Extra CodeMirror extensions (e.g. SQL completion), appended after the built-ins. */
  extensions?: Extension[]
  /** Greyed text shown while the document is empty. */
  placeholder?: string
}

export interface AssayEditorInstance {
  /** Get the current document text. */
  getValue: () => string
  /**
   * Replace the entire document. `history: false` is for text loaded from
   * outside (a saved query): the editor state is rebuilt, so the undo history
   * starts empty and Ctrl+Z cannot bring the previous text back. The change
   * listener is not called for it (the caller already has the text).
   */
  setValue: (value: string, opts?: { history?: boolean }) => void
  /** The underlying CodeMirror EditorView (for toolbar insertSnippet calls). */
  view: EditorView
  /** Tear down the editor. */
  destroy: () => void
}

/**
 * Initialise a CodeMirror 6 markdown editor inside `el`.
 * Returns helpers to read/write content and clean up.
 */
export function createAssayEditor(
  el: HTMLElement,
  options: AssayEditorOptions = {},
): AssayEditorInstance {
  const onChange = options.onChange

  const updateListener = EditorView.updateListener.of((update) => {
    if (update.docChanged && onChange) {
      onChange(update.state.doc.toString())
    }
  })

  const languages = { markdown, sql: sqlLang, json: jsonLang, yaml: yamlLang, javascript: jsLang }
  const languageExtension = languages[options.lang ?? 'markdown']()
  const extraKeymap = options.extraKeymap ?? []

  const extensions: Extension[] = [
      assayTheme,
      syntaxHighlighting(assayHighlightStyle),
      languageExtension,
      history(),
      // extraKeymap first so a caller's binding (e.g. Ctrl/Cmd+Enter to run
      // a cell) takes priority over the default keymap's own bindings.
      keymap.of([...extraKeymap, ...defaultKeymap, ...historyKeymap]),
      updateListener,
      EditorView.lineWrapping,
      ...(options.placeholder ? [placeholderExt(options.placeholder)] : []),
      ...(options.extensions ?? []),
      // Read-only is the trust prompt: show every control and invisible character.
      ...(options.readOnly ? [EditorState.readOnly.of(true), highlightSpecialChars()] : []),
  ]
  const state = EditorState.create({ doc: options.value ?? '', extensions })

  const view = new EditorView({ state, parent: el })

  return {
    getValue: () => view.state.doc.toString(),
    setValue: (value: string, opts?: { history?: boolean }) => {
      if (opts?.history === false) {
        view.setState(EditorState.create({ doc: value, extensions }))
        return
      }
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
    },
    view,
    destroy: () => view.destroy(),
  }
}
