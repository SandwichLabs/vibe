import { describe, it, expect } from 'vitest'
import { COLOR_ROLES, contrastRatio, idFromFilename, parseTheme, resolveTheme, themeToCssVars, ThemeError } from './schema'
import { BUILTIN_THEMES, SCHOOLHOUSE_LIGHT } from './builtin'

const registry = new Map(BUILTIN_THEMES.map((t) => [t.id, t]))

describe('built-in themes', () => {
  it('are complete and resolve without extends', () => {
    for (const t of BUILTIN_THEMES) {
      expect(t.extends).toBeUndefined()
      const r = resolveTheme(t, registry)
      for (const role of COLOR_ROLES) expect(r.colors[role], `${t.id} ${role}`).toMatch(/^#[0-9A-F]{6}([0-9A-F]{2})?$/)
    }
  })

  it('have unique ids', () => {
    expect(new Set(BUILTIN_THEMES.map((t) => t.id)).size).toBe(BUILTIN_THEMES.length)
  })

  it.each(BUILTIN_THEMES.map((t) => [t.id, t] as const))('%s meets AA contrast on its core pairs', (_id, t) => {
    const c = resolveTheme(t, registry).colors
    const aa = (fg: string, bg: string, min = 4.5) => expect(contrastRatio(fg, bg), `${fg} on ${bg}`).toBeGreaterThanOrEqual(min)
    aa(c['fg'], c['bg'])
    aa(c['fg'], c['surface'])
    aa(c['fg'], c['bg.alt'])
    aa(c['fg.muted'], c['bg'])
    aa(c['fg.muted'], c['surface'])
    aa(c['accent.fg'], c['accent'])
    aa(c['highlight.fg'], c['highlight'])
    aa(c['chrome.fg'], c['chrome.bg'])
    aa(c['sidebar.fg'], c['sidebar.bg'])
    aa(c['sidebar.active.fg'], c['sidebar.active.bg'])
    aa(c['code.fg'], c['code.bg'])
    aa(c['editor.fg'], c['editor.bg'])
    aa(c['error'], c['bg'])
    aa(c['success'], c['bg'])
    aa(c['syntax.keyword'], c['editor.bg'])
    aa(c['syntax.string'], c['editor.bg'])
    aa(c['syntax.number'], c['editor.bg'])
    aa(c['syntax.comment'], c['editor.bg'])
    // Connection status icons (#117, design review): the warn (needs credentials)
    // and info (extension needed) glyphs, and the not-connected ring in fg.muted,
    // are non-text marks on the sidebar and on content surfaces; 3:1 applies.
    // fg.disabled is deliberately not used for a status mark.
    for (const bg of ['sidebar.bg', 'surface', 'bg'] as const) {
      aa(c['warn'], c[bg], 3)
      aa(c['info'], c[bg], 3)
      aa(c['fg.muted'], c[bg], 3)
      aa(c['success'], c[bg], 3)
      aa(c['error'], c[bg], 3)
    }
    // Borders are structural; 3:1 is the non-text minimum.
    aa(c['border'], c['bg'], 3)
    aa(c['focus'], c['bg'], 3)
  })
})

describe('parseTheme', () => {
  it('accepts a minimal user theme that extends a built-in', () => {
    const t = parseTheme({ name: 'Mine', type: 'dark', extends: 'schoolhouse-dark', colors: { accent: '#7c9cff' } }, { id: 'mine', source: 'user', path: '/x/mine.json' })
    expect(t.colors.accent).toBe('#7C9CFF')
    const r = resolveTheme(t, registry)
    expect(r.colors.accent).toBe('#7C9CFF')
    expect(r.colors.bg).toBe(registry.get('schoolhouse-dark')!.colors.bg)
    expect(r.source).toBe('user')
  })

  it('rejects unknown roles, bad colours, and unknown top-level keys in one pass', () => {
    let err: ThemeError | undefined
    try {
      parseTheme({ name: 'Bad', type: 'dark', extends: 'signal', colors: { accent: 'red', nope: '#fff' }, weird: 1 }, { id: 'bad', source: 'user', path: '/x/bad.json' })
    } catch (e) {
      err = e as ThemeError
    }
    expect(err).toBeInstanceOf(ThemeError)
    expect(err!.message).toContain('/x/bad.json')
    expect(err!.message).toContain('colors.accent')
    expect(err!.message).toContain('colors.nope')
    expect(err!.message).toContain('unknown top-level key "weird"')
  })

  it('rejects a user theme with no extends and missing roles', () => {
    expect(() => parseTheme({ name: 'Partial', type: 'light', colors: { bg: '#fff' } }, { id: 'partial', source: 'user' }))
      .toThrow(/missing colours/)
  })

  it('rejects font values that could break out of a declaration', () => {
    expect(() => parseTheme({ name: 'F', type: 'light', extends: 'signal', fonts: { mono: "x; background: url(evil)" } }, { id: 'f', source: 'user' }))
      .toThrow(/fonts.mono/)
    const ok = parseTheme({ name: 'F', type: 'light', extends: 'signal', fonts: { mono: "'Fira Code', monospace" } }, { id: 'f', source: 'user' })
    expect(resolveTheme(ok, registry).fonts.mono).toBe("'Fira Code', monospace")
  })

  it('requires name and a valid type', () => {
    expect(() => parseTheme({ type: 'blue', extends: 'signal' }, { id: 'x', source: 'user' })).toThrow(/name is required.*type must be/)
  })
})

describe('resolveTheme', () => {
  it('detects unknown parents and cycles', () => {
    const a = parseTheme({ name: 'A', type: 'light', extends: 'b', colors: {} }, { id: 'a', source: 'user' })
    const b = parseTheme({ name: 'B', type: 'light', extends: 'a', colors: {} }, { id: 'b', source: 'user' })
    const reg = new Map([...registry, ['a', a], ['b', b]])
    expect(() => resolveTheme(a, reg)).toThrow(/cycle/)
    const orphan = parseTheme({ name: 'O', type: 'light', extends: 'missing', colors: {} }, { id: 'o', source: 'user' })
    expect(() => resolveTheme(orphan, registry)).toThrow(/unknown theme "missing"/)
  })

  it('walks a two-level chain child-wins', () => {
    const mid = parseTheme({ name: 'Mid', type: 'light', extends: 'schoolhouse-light', colors: { accent: '#111111', bg: '#EEEEEE' } }, { id: 'mid', source: 'user' })
    const leaf = parseTheme({ name: 'Leaf', type: 'light', extends: 'mid', colors: { accent: '#222222' } }, { id: 'leaf', source: 'user' })
    const reg = new Map([...registry, ['mid', mid], ['leaf', leaf]])
    const r = resolveTheme(leaf, reg)
    expect(r.colors.accent).toBe('#222222')
    expect(r.colors.bg).toBe('#EEEEEE')
    expect(r.colors.fg).toBe(SCHOOLHOUSE_LIGHT.colors.fg)
  })
})

describe('themeToCssVars', () => {
  it('emits one --vibe-* var per role plus fonts', () => {
    const vars = themeToCssVars(resolveTheme(SCHOOLHOUSE_LIGHT, registry))
    expect(vars['--vibe-accent-hover']).toBe('#3FA39B')
    expect(vars['--vibe-sidebar-active-fg']).toBe('#1F7268')
    expect(vars['--vibe-font-mono']).toContain('JetBrains Mono')
    expect(Object.keys(vars).length).toBe(COLOR_ROLES.length + 3)
  })
})

describe('idFromFilename', () => {
  it('slugs filenames', () => {
    expect(idFromFilename('My Theme.json')).toBe('my-theme')
    expect(idFromFilename('nord.JSON')).toBe('nord')
    expect(idFromFilename('___.json')).toBe('theme')
  })
})
