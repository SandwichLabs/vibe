import { describe, it, expect, beforeEach, vi } from 'vitest'
import { nextTick } from 'vue'
import { installTheme, useTheme, THEME_SYSTEM, __resetThemeStateForTests } from './useTheme'

function mockWails(opts: { prefs?: any; themes?: any[]; problems?: string[] } = {}) {
  const store = { prefs: { theme: 'schoolhouse-light', systemLight: 'schoolhouse-light', systemDark: 'schoolhouse-dark', reduceMotion: false, uiScale: 100, ...opts.prefs } }
  const App = {
    GetPreferences: vi.fn(async () => ({ ...store.prefs })),
    SetPreferences: vi.fn(async (p: any) => { store.prefs = { ...p } }),
    ListUserThemes: vi.fn(async () => ({ themes: opts.themes ?? [], problems: opts.problems ?? [], dir: '/cfg/themes' })),
    OpenThemesDir: vi.fn(async () => '/cfg/themes'),
  }
  ;(window as any).go = { main: { App } }
  return { App, store }
}

let dark = false
beforeEach(() => {
  __resetThemeStateForTests()
  localStorage.clear()
  dark = false
  window.matchMedia = vi.fn().mockImplementation((q: string) => ({
    matches: q.includes('dark') && dark,
    media: q,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })) as any
  document.documentElement.removeAttribute('style')
})

describe('installTheme', () => {
  it('applies the saved theme to <html> and persists changes through the bindings', async () => {
    const { App, store } = mockWails({ prefs: { theme: 'chalkboard' } })
    await installTheme()
    const root = document.documentElement
    expect(root.dataset.theme).toBe('chalkboard')
    expect(root.dataset.themeType).toBe('dark')
    expect(root.style.getPropertyValue('--vibe-accent')).toBe('#F2A93B')
    expect(root.style.colorScheme).toBe('dark')

    const t = useTheme()
    await t.setTheme('signal')
    await nextTick()
    expect(root.dataset.theme).toBe('signal')
    expect(root.style.getPropertyValue('--vibe-accent-fg')).toBe('#FFD700')
    expect(App.SetPreferences).toHaveBeenCalledTimes(1)
    expect(store.prefs.theme).toBe('signal')
  })

  it('follows the OS when theme is "system"', async () => {
    dark = true
    mockWails({ prefs: { theme: THEME_SYSTEM, systemDark: 'chalkboard' } })
    await installTheme()
    expect(document.documentElement.dataset.theme).toBe('chalkboard')
    const t = useTheme()
    await t.setSystemPair('signal', 'schoolhouse-dark')
    await nextTick()
    expect(document.documentElement.dataset.theme).toBe('schoolhouse-dark')
  })

  it('loads user theme files, lists broken ones with their error, and falls back for an unknown id', async () => {
    mockWails({
      prefs: { theme: 'nord' },
      themes: [
        { path: '/cfg/themes/nord.json', filename: 'nord.json', json: JSON.stringify({ name: 'Nord', type: 'dark', extends: 'schoolhouse-dark', colors: { accent: '#88C0D0' } }) },
        { path: '/cfg/themes/oops.json', filename: 'oops.json', json: JSON.stringify({ name: 'Oops', type: 'dark', extends: 'schoolhouse-dark', colors: { accent: 'blue' } }) },
        { path: '/cfg/themes/schoolhouse-light.json', filename: 'schoolhouse-light.json', json: JSON.stringify({ name: 'Clash', type: 'light', extends: 'signal' }) },
      ],
      problems: ['huge.json: file is larger than 256 KB'],
    })
    await installTheme()
    const t = useTheme()
    const ids = t.listings.value.map((l) => l.id)
    expect(ids.slice(0, 4)).toEqual(['schoolhouse-light', 'schoolhouse-dark', 'signal', 'chalkboard'])
    expect(ids).toContain('nord')
    expect(document.documentElement.dataset.theme).toBe('nord')
    expect(document.documentElement.style.getPropertyValue('--vibe-accent')).toBe('#88C0D0')

    const broken = t.listings.value.find((l) => l.id === 'oops')!
    expect(broken.resolved).toBeUndefined()
    expect(broken.error).toContain('colors.accent')
    expect(t.problems.value.join('\n')).toContain('huge.json')
    expect(t.problems.value.join('\n')).toContain('is a built-in theme')

    // Selecting a theme that later disappears falls back to the default, never a blank screen.
    await t.setTheme('gone')
    await nextTick()
    expect(document.documentElement.dataset.theme).toBe('schoolhouse-light')
  })

  it('works without the Wails runtime via localStorage', async () => {
    delete (window as any).go
    localStorage.setItem('vibe.preferences', JSON.stringify({ theme: 'signal' }))
    await installTheme()
    expect(document.documentElement.dataset.theme).toBe('signal')
    const t = useTheme()
    await t.setUiScale(125)
    expect(JSON.parse(localStorage.getItem('vibe.preferences')!).uiScale).toBe(125)
    expect(document.documentElement.style.getPropertyValue('--vibe-ui-scale')).toBe('1.25')
    await t.setReduceMotion(true)
    expect(document.documentElement.dataset.reduceMotion).toBe('true')
  })

  it('toggleDark flips between the system pair', async () => {
    mockWails({ prefs: { theme: 'signal', systemLight: 'signal', systemDark: 'chalkboard' } })
    await installTheme()
    const t = useTheme()
    await t.toggleDark()
    expect(t.activeTheme.value.id).toBe('chalkboard')
    await t.toggleDark()
    expect(t.activeTheme.value.id).toBe('signal')
  })
})
