// A2UIControl: a text or number parameter is a free identifier, so macOS must not
// capitalise, autocorrect or spell-check what is typed (it changed the values a
// page sent: "main" became "Main").
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import A2UIControl from './A2UIControl.vue'
import { ARTIFACT_RUNTIME } from './artifactRuntime'

function mountControl(type: 'textInput' | 'numberInput') {
  const params = {
    param: () => ({ name: 'group', label: 'Group', type: type === 'textInput' ? 'string' : 'number' }),
    display: () => '',
    error: () => '',
    isDefault: () => true,
    set: () => {},
    reset: () => {},
  }
  const rt = { params, result: () => undefined } as any
  return mount(A2UIControl, { props: { component: { type, param: 'group' } as any }, global: { provide: { [ARTIFACT_RUNTIME as symbol]: rt } } })
}

describe('A2UIControl text fields', () => {
  for (const type of ['textInput', 'numberInput'] as const) {
    it(`${type} turns the macOS text substitutions off`, () => {
      const el = mountControl(type).get('input').element
      expect(el.getAttribute('autocapitalize')).toBe('off')
      expect(el.getAttribute('autocorrect')).toBe('off')
      expect(el.getAttribute('spellcheck')).toBe('false')
      expect(el.getAttribute('autocomplete')).toBe('off')
    })
  }
})
