// SettingsView is split in two (SPEC 09): Workspace (this folder, the team's
// agent limit, MCP) and App (theme, the agent's model). The tab is in the URL.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {}, EventsOff: () => {}, BrowserOpenURL: vi.fn() }))

import SettingsView from './SettingsView.vue'
import { workspaceInfo, _resetForTests } from '../composables/useWorkspace'

const INFO = {
  root: '/w/a', id: 'i', name: 'Acme', sessionId: 's', gitBranch: '', isGit: false, defaultConnection: 'local',
  connections: [{ name: 'local', type: 'duckdb', scope: 'shared', readOnly: false, default: true, status: 'ok' }],
  counts: { notebooks: 0, queries: 0, skills: 0, tables: 0 },
  policy: { sandbox: 'read-write', askBeforeWrites: true }, team: { sandbox: 'full', askBeforeWrites: false }, teamSandboxSet: false,
  local: { sandbox: 'read-write', askBeforeWrites: true },
  readOnly: false, issues: [], warnings: [], duplicateIds: [], localUntrusted: false, mcpRunning: false,
} as unknown as WorkspaceInfo

function makeApp(mcp: Partial<MCPStatus> = {}) {
  const app = {
    GetMCPStatus: vi.fn(async () => ({ running: false, url: '', canStart: true, reason: '', ...mcp })),
    StartMCP: vi.fn(async () => ({ running: true, url: 'http://127.0.0.1:5555/sse', canStart: true, reason: '' })),
    StopMCP: vi.fn(async () => {}),
    GetClaudeConfig: vi.fn(async () => ({ name: 'vibe-a', desktop: '{"mcpServers":{}}', code: 'claude mcp add vibe-a -- vibe mcp serve --workspace /w/a' })),
    SetWorkspaceSettings: vi.fn(async (s: WorkspaceSettings) => ({ ...INFO, name: s.name })),
    GetAgentConfig: vi.fn(async () => ({ provider: 'local', model: 'llama', endpoint: 'http://x', sandbox: 'read-write', askBeforeWrites: true })),
    GetLLMKeyStatus: vi.fn(async () => ({ provider: 'local', source: 'none', set: false, fileKey: false, canMigrate: false, keychainAvailable: true, keychainHasKey: false })),
    GetPreferences: vi.fn(async () => ({ theme: 'schoolhouse', systemLight: 'schoolhouse', systemDark: 'schoolhouse-dark', reduceMotion: false, uiScale: 100 })),
    ListUserThemes: vi.fn(async () => ({ themes: [], problems: [], dir: '' })),
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

const wrappers: Array<{ unmount(): void }> = []
afterEach(() => { wrappers.splice(0).forEach((w) => w.unmount()) })

async function mountAt(path = '/settings') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/settings', component: SettingsView }] })
  await router.push(path)
  await router.isReady()
  const w = mount(SettingsView, { attachTo: document.body, global: { plugins: [router], stubs: { Icon: true, AppearanceSettings: true } } })
  wrappers.push(w)
  await flushPromises()
  return { w, router }
}

describe('SettingsView', () => {
  beforeEach(() => { _resetForTests(); workspaceInfo.value = INFO })

  it('has Workspace, App and About tabs wired with aria; Workspace is selected by default', async () => {
    makeApp()
    const { w } = await mountAt()
    const tabs = w.findAll('[role="tab"]')
    expect(tabs).toHaveLength(3)
    expect(tabs[0].attributes('aria-selected')).toBe('true')
    expect(tabs[0].attributes('aria-controls')).toBe('settings-panel-workspace')
    expect(w.find('#settings-panel-workspace').attributes('aria-labelledby')).toBe('settings-tab-workspace')
    expect(w.text()).toContain('This workspace')
    expect(w.text()).toContain('Claude and other tools')
  })

  it('the App tab is in the URL and shows the model, not the workspace form', async () => {
    makeApp()
    const { w, router } = await mountAt('/settings?tab=app')
    expect(w.find('#settings-tab-app').attributes('aria-selected')).toBe('true')
    expect(w.text()).toContain("The agent's model")
    expect(w.text()).not.toContain('This workspace')
    await w.find('#settings-tab-workspace').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBeUndefined()
    expect(w.text()).toContain('This workspace')
  })

  it('arrow keys switch tabs', async () => {
    makeApp()
    const { w, router } = await mountAt()
    await w.find('[role="tablist"]').trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBe('app')
  })

  it('arrow keys move focus to the newly selected tab (not the old one)', async () => {
    makeApp()
    const { w } = await mountAt()
    await w.find('[role="tablist"]').trigger('keydown', { key: 'End' })
    await flushPromises()
    expect(document.activeElement?.id).toBe('settings-tab-about')
    await w.find('[role="tablist"]').trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(document.activeElement?.id).toBe('settings-tab-workspace')
  })

  it('saves the name and default connection', async () => {
    const app = makeApp()
    const { w } = await mountAt()
    await w.find('[data-testid="ws-name-input"]').setValue('Renamed')
    await w.find('[data-testid="ws-save"]').trigger('click')
    await flushPromises()
    expect(app.SetWorkspaceSettings).toHaveBeenCalledWith(expect.objectContaining({ name: 'Renamed', defaultConnection: 'local' }))
  })

  it('MCP: Start/Stop, the address, and the Claude configs (no token anywhere)', async () => {
    const app = makeApp()
    const { w } = await mountAt()
    const writeText = vi.fn(async () => {})
    Object.assign(navigator, { clipboard: { writeText } })
    expect(w.find('[data-testid="mcp-panel"]').text()).toContain('Off')

    app.GetMCPStatus.mockResolvedValue({ running: true, url: 'http://127.0.0.1:5555/sse', canStart: true, reason: '' })
    await w.find('[data-testid="mcp-toggle"]').trigger('click')
    await flushPromises()
    expect(app.StartMCP).toHaveBeenCalled()
    expect(w.find('[data-testid="mcp-panel"]').text()).toContain('Running, only on this computer')
    expect(w.find('[data-testid="mcp-panel"]').text()).toContain('http://127.0.0.1:5555/sse')
    expect(w.find('[data-testid="mcp-panel"]').text()).not.toContain('Auth token')

    await w.find('[data-testid="copy-claude-code"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining('claude mcp add vibe-a'))
    await w.find('[data-testid="copy-claude-desktop"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith('{"mcpServers":{}}')

    app.GetMCPStatus.mockResolvedValue({ running: false, url: '', canStart: true, reason: '' })
    await w.find('[data-testid="mcp-toggle"]').trigger('click')
    await flushPromises()
    expect(app.StopMCP).toHaveBeenCalled()
  })

  it('MCP cannot start while read-only, and says why', async () => {
    makeApp({ canStart: false, reason: 'This window opened the workspace read-only. Take over the workspace to serve MCP.' })
    const { w } = await mountAt()
    expect((w.find('[data-testid="mcp-toggle"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.text()).toContain('Take over the workspace to serve MCP.')
  })
})
