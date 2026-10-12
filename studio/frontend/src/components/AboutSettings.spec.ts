// Settings > About (#40): the stamped version, the notices viewer, "Report a bug" and the copy button.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import type { VueWrapper } from '@vue/test-utils'
import { mount, flushPromises } from '@vue/test-utils'

const openURL = vi.hoisted(() => vi.fn())
vi.mock('../wailsjs/runtime/runtime', () => ({ BrowserOpenURL: openURL, EventsOn: () => () => {}, EventsOff: () => {} }))

import AboutSettings from './AboutSettings.vue'
import { buildInfoText } from '../composables/useAbout'

const INFO: BuildInfo = { version: 'v0.1.0', commit: 'abc1234', date: '2026-10-12T00:00:00Z', goVersion: 'go1.26.9', duckdbVersion: 'v1.4.1', platform: 'linux/amd64' }
const NOTICES = '# Third-party notices\n\n## Go modules\n\n#### MIT\n\n- github.com/zalando/go-keyring v0.2.6\n\n```text\n## not a heading\nMIT body\n```\n\n## Fonts\n\n#### Fraunces (OFL-1.1)\n\n```text\nSIL Open Font License\n```\n'

function makeApp() {
  const app = {
    GetBuildInfo: vi.fn(async () => INFO),
    GetThirdPartyNotices: vi.fn(async () => NOTICES),
  }
  ;(window as any).go = { main: { App: app } }
  return app
}

let mounted: VueWrapper | null = null
async function mountAbout() {
  const w = mount(AboutSettings, { attachTo: document.body, global: { stubs: { Icon: true } } })
  mounted = w
  await flushPromises()
  return w
}

describe('AboutSettings', () => {
  beforeEach(() => { openURL.mockClear() })
  afterEach(() => { mounted?.unmount(); mounted = null })

  it('shows the stamped version, commit and build details from GetBuildInfo', async () => {
    const app = makeApp()
    const w = await mountAbout()
    expect(app.GetBuildInfo).toHaveBeenCalled()
    expect(w.find('[data-testid="about-version"]').text()).toBe('v0.1.0')
    expect(w.find('[data-testid="about-commit"]').text()).toBe('abc1234')
    expect(w.text()).toContain('go1.26.9')
    expect(w.text()).toContain('v1.4.1')
    expect(w.text()).toContain('Apache License 2.0')
  })

  it('opens the notices viewer with the embedded text, and searching narrows it to matching sections', async () => {
    const app = makeApp()
    const w = await mountAbout()
    expect(w.find('[data-testid="notices-dialog"]').exists()).toBe(false)
    await w.find('[data-testid="about-notices"]').trigger('click')
    await flushPromises()
    expect(app.GetThirdPartyNotices).toHaveBeenCalledTimes(1)
    const text = w.find('[data-testid="notices-text"]')
    expect(text.text()).toContain('go-keyring')
    expect(text.text()).toContain('Fraunces')

    await w.find('[data-testid="notices-search"]').setValue('fraunces')
    expect(w.find('[data-testid="notices-text"]').text()).toContain('Fraunces')
    expect(w.find('[data-testid="notices-text"]').text()).not.toContain('go-keyring')
    expect(w.find('[data-testid="notices-count"]').text()).toContain('1 section match')

    await w.find('[data-testid="notices-close"]').trigger('click')
    expect(w.find('[data-testid="notices-dialog"]').exists()).toBe(false)
  })

  it('a heading-looking line inside a licence text does not split a section', async () => {
    makeApp()
    const w = await mountAbout()
    await w.find('[data-testid="about-notices"]').trigger('click')
    await flushPromises()
    await w.find('[data-testid="notices-search"]').setValue('not a heading')
    expect(w.find('[data-testid="notices-text"]').text()).toContain('go-keyring')
  })

  it('Report a bug opens the issue chooser with nothing prefilled; the licence and privacy links go to GitHub', async () => {
    makeApp()
    const w = await mountAbout()
    await w.find('[data-testid="about-report"]').trigger('click')
    expect(openURL).toHaveBeenLastCalledWith('https://github.com/SandwichLabs/vibe/issues/new/choose')
    await w.find('[data-testid="about-privacy"]').trigger('click')
    expect(openURL).toHaveBeenLastCalledWith('https://github.com/SandwichLabs/vibe/blob/main/docs/privacy.md')
    await w.find('[data-testid="about-license"]').trigger('click')
    expect(openURL).toHaveBeenLastCalledWith('https://github.com/SandwichLabs/vibe/blob/main/LICENSE')
  })

  it('Copy build info puts the build details (and nothing else) on the clipboard', async () => {
    makeApp()
    const writeText = vi.fn(async (_t: string) => {})
    Object.assign(navigator, { clipboard: { writeText } })
    const w = await mountAbout()
    await w.find('[data-testid="about-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(buildInfoText(INFO))
    expect(writeText.mock.calls[0][0]).toContain('Vibe: v0.1.0')
    expect(writeText.mock.calls[0][0]).toContain('DuckDB: v1.4.1')
  })

  it('still renders when GetBuildInfo fails', async () => {
    const app = makeApp()
    app.GetBuildInfo.mockRejectedValue(new Error('boom'))
    const w = await mountAbout()
    expect(w.find('[data-testid="about-version"]').text()).toBe('unknown')
    expect(w.find('[data-testid="about-error"]').exists()).toBe(true)
  })
})
