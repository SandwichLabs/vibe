import { describe, it, expect } from 'vitest'
import { sandboxIntro, SANDBOX_MODES } from './agentSandbox'

describe('sandboxIntro', () => {
  it('describes read-only', () => {
    expect(sandboxIntro('read-only')).toContain("can't change anything")
  })
  it('describes read-write', () => {
    expect(sandboxIntro('read-write')).toContain('add, change or delete rows')
    expect(sandboxIntro('read-write')).toContain("can't create or drop tables")
  })
  it('describes full', () => {
    expect(sandboxIntro('full')).toContain('create, alter and drop tables')
  })
  it('falls back for an unknown mode', () => {
    expect(sandboxIntro('bogus')).toContain('Every step it takes is shown below')
    expect(sandboxIntro('')).toContain('Every step it takes is shown below')
  })

  it('appends the ask-before-changes state to write-capable modes only', () => {
    expect(sandboxIntro('read-write', true)).toContain('It asks you before each change.')
    expect(sandboxIntro('read-write', false)).toContain('It makes changes without asking.')
    expect(sandboxIntro('full', true)).toContain('It asks you before each change.')
    expect(sandboxIntro('full', false)).toContain('It makes changes without asking.')
    // Read-only never writes, so ask is irrelevant either way.
    expect(sandboxIntro('read-only', true)).not.toContain('ask')
    expect(sandboxIntro('read-only', false)).not.toContain('asking')
  })
})

describe('SANDBOX_MODES', () => {
  it('has exactly the three Go sandbox mode strings, in order', () => {
    expect(SANDBOX_MODES.map((m) => m.value)).toEqual(['read-only', 'read-write', 'full'])
  })
  it('every mode has a label and help text', () => {
    for (const m of SANDBOX_MODES) {
      expect(m.label.length).toBeGreaterThan(0)
      expect(m.help.length).toBeGreaterThan(0)
    }
  })
})
