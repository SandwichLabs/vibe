import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const { createVibeEditor, editors } = vi.hoisted(() => {
  const editors: Array<{ opts: any; doc: string; setValue: any; destroy: any }> = []
  return {
    editors,
    createVibeEditor: vi.fn((_el: HTMLElement, opts: any) => {
      const ed: any = { opts, doc: opts.value ?? '', destroy: vi.fn() }
      ed.setValue = vi.fn((v: string) => { ed.doc = v })
      editors.push(ed)
      return { view: {}, setValue: ed.setValue, destroy: ed.destroy, getValue: () => ed.doc }
    }),
  }
})
vi.mock('../composables/useVibeEditor', () => ({ createVibeEditor }))
const sqlCompletion = vi.hoisted(() => vi.fn(() => ['ext']))
vi.mock('../lib/sqlComplete', () => ({ sqlCompletion }))

import SqlEditor from './SqlEditor.vue'

// closeCompletion only reads the completion state field.
const fakeView = { state: { field: () => null } }

describe('SqlEditor', () => {
  beforeEach(() => { editors.length = 0; vi.clearAllMocks() })

  it('mounts a sql editor with the value, placeholder and completion', () => {
    mount(SqlEditor, { props: { modelValue: 'SELECT 1', connection: 'shop', placeholder: 'type' } })
    const o = editors[0].opts
    expect(o).toMatchObject({ value: 'SELECT 1', lang: 'sql', placeholder: 'type', extensions: ['ext'] })
    const { connection } = (sqlCompletion.mock.calls[0] as any)[0]
    expect(connection()).toBe('shop')
  })

  it('a prop change reaches the editor; an editor change emits update:modelValue', async () => {
    const w = mount(SqlEditor, { props: { modelValue: 'a', connection: '' } })
    await w.setProps({ modelValue: 'b' })
    expect(editors[0].setValue).toHaveBeenCalledWith('b', { history: false })

    editors[0].doc = 'c'
    editors[0].opts.onChange('c')
    expect(w.emitted('update:modelValue')).toEqual([['c']])

    // The echo of the editor's own text does not dispatch back into it.
    editors[0].setValue.mockClear()
    await w.setProps({ modelValue: 'c' })
    await nextTick()
    expect(editors[0].setValue).not.toHaveBeenCalled()
  })

  it('Mod-Enter emits run', () => {
    const w = mount(SqlEditor, { props: { modelValue: '', connection: '' } })
    const b = editors[0].opts.extraKeymap.find((k: any) => k.key === 'Mod-Enter')
    expect(b.run(fakeView)).toBe(true)
    expect(w.emitted('run')).toHaveLength(1)
  })

  it('destroys the editor on unmount', () => {
    const w = mount(SqlEditor, { props: { modelValue: '', connection: '' } })
    w.unmount()
    expect(editors[0].destroy).toHaveBeenCalled()
  })
})

// The real editor: a parent-driven replacement (a saved query loaded) must
// leave nothing to undo, even when the user edited before it.
describe('SqlEditor undo history (real editor)', () => {
  it('Ctrl+Z after a loaded replacement leaves exactly the loaded text', async () => {
    const actual = await vi.importActual<typeof import('../composables/useVibeEditor')>('../composables/useVibeEditor')
    const { undo, undoDepth } = await import('@codemirror/commands')
    const el = document.createElement('div')
    document.body.appendChild(el)
    const ed = actual.createVibeEditor(el, { lang: 'sql', value: 'SELECT foo FROM a' })
    ed.view.dispatch({ changes: { from: 7, to: 11, insert: '' }, userEvent: 'delete' }) // user deletes "foo "
    expect(undoDepth(ed.view.state)).toBeGreaterThan(0)
    ed.setValue('SELECT 1', { history: false })
    expect(ed.getValue()).toBe('SELECT 1')
    expect(undoDepth(ed.view.state)).toBe(0)
    undo(ed.view)
    expect(ed.getValue()).toBe('SELECT 1')
    // Typing after the load works and is undoable again.
    ed.view.dispatch({ changes: { from: 8, insert: '0' }, userEvent: 'input.type' })
    expect(undoDepth(ed.view.state)).toBe(1)
    ed.destroy()
  })
})
