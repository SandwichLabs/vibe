import { describe, it, expect } from 'vitest'
import { controlsFor } from './viewControls'

const p = (name: string, type: string) => ({ name, type, default: null }) as ArtifactParamT

describe('controlsFor', () => {
  it('pairs consecutive date parameters only', () => {
    const c = controlsFor([p('a', 'date'), p('b', 'date'), p('c', 'date')])
    expect(c.map((x) => [x.type, x.start, x.end])).toEqual([['dateRange', 'a', 'b'], ['dateRange', 'c', undefined]])
    const split = controlsFor([p('a', 'date'), p('n', 'integer'), p('b', 'date')])
    expect(split.map((x) => [x.type, x.start, x.end, x.param])).toEqual([
      ['dateRange', 'a', undefined, undefined], ['numberInput', undefined, undefined, 'n'], ['dateRange', 'b', undefined, undefined],
    ])
  })
  it('maps the other types to the level-1 controls', () => {
    expect(controlsFor([p('e', 'enum'), p('f', 'boolean'), p('t', 'text'), p('x', 'number')]).map((x) => x.type)).toEqual(['select', 'select', 'textInput', 'numberInput'])
  })
})
