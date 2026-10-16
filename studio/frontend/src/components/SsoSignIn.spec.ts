type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const handlers: Record<string, (p: any) => void> = {}
vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: (n: string, f: (p: any) => void) => { handlers[n] = f; return () => {} }, EventsOff: () => {} }))

import SsoSignIn from './SsoSignIn.vue'
import { _resetSignInForTests } from '../composables/useSignIn'
import { connectionList, _resetConnectionsForTests } from '../composables/useConnections'

const PROMPT: SignInPrompt = { connection: 'store', profile: 'dev', url: 'https://device.sso.us-east-1.amazonaws.com/', code: 'ABCD-EFGH', expiresAt: '2026-10-16T12:10:00Z' }
const signedOut = { method: 'aws-sso:dev', profile: 'dev', signedIn: false, secretSet: false, fingerprintMatches: true, available: true, detail: 'AWS session for profile dev has expired. Sign in, or run `aws sso login --profile dev`.' }

function makeApp(over: MockApp = {}) {
  const app: MockApp = {
    GetConnectionCredentialStatus: vi.fn(async () => signedOut),
    StartSignIn: vi.fn(async () => PROMPT),
    CancelSignIn: vi.fn(async () => {}),
    ListConnections: vi.fn(async () => ({ connections: connectionList.value, relaunchRequired: false })),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {}
  return app
}
const mountCard = (props: { connection: string; saved: boolean; profile?: string }) =>
  mount(SsoSignIn, { props: { connection: props.connection, saved: props.saved, profile: props.profile ?? 'dev', 'onUpdate:profile': () => {} }, global: { stubs: { Icon: true } } })

describe('SsoSignIn (the editor card)', () => {
  beforeEach(() => {
    _resetSignInForTests(); _resetConnectionsForTests()
    for (const k of Object.keys(handlers)) delete handlers[k]
  })

  it('has a profile input and no key fields; says the session state of a saved connection', async () => {
    makeApp()
    const w = mountCard({ connection: 'store', saved: true })
    await flushPromises()
    expect((w.find('[data-testid="f-sso-profile"]').element as HTMLInputElement).value).toBe('dev')
    expect(w.find('input[type="password"]').exists()).toBe(false)
    expect(w.find('[data-testid="sso-state"]').text()).toContain('AWS session for profile dev has expired')
    expect(w.find('[data-testid="sso-state"] code').text()).toBe('aws sso login --profile dev')
    expect(w.find('[data-testid="sso-state"]').text()).not.toContain('`')
  })

  it('a signed-in session says until when', async () => {
    makeApp({ GetConnectionCredentialStatus: vi.fn(async () => ({ ...signedOut, signedIn: true, expiresAt: '2026-10-16T20:00:00Z', detail: 'Signed in with AWS profile dev.' })) })
    const w = mountCard({ connection: 'store', saved: true })
    await flushPromises()
    expect(w.find('[data-testid="sso-state"]').text()).toMatch(/^Signed in\. The session lasts until/)
  })

  it('Sign in now runs the flow and shows the code; Cancel stops it', async () => {
    const app = makeApp()
    connectionList.value = [{ name: 'store', type: 's3', scope: 'local', readOnly: true, default: false, status: 'needs-credentials', remote: true, signIn: 'aws-sso:dev' }]
    const w = mountCard({ connection: 'store', saved: true })
    await flushPromises()
    await w.find('[data-testid="sso-signin-now"]').trigger('click')
    await flushPromises()
    expect(app.StartSignIn).toHaveBeenCalledWith({ name: 'store', scope: '' })
    expect(w.find('[data-testid="sso-code"]').text()).toBe('ABCD-EFGH')
    await w.find('[data-testid="sso-cancel"]').trigger('click')
    await flushPromises()
    expect(app.CancelSignIn).toHaveBeenCalled()
    expect(w.find('[data-testid="sso-code"]').exists()).toBe(false)
  })

  it('an unsaved profile cannot sign in yet, and a new connection says to save first', async () => {
    const app = makeApp()
    const w = mountCard({ connection: 'store', saved: false })
    await flushPromises()
    expect((w.find('[data-testid="sso-signin-now"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.find('[data-testid="sso-state"]').text()).toContain('Save the connection with this profile')
    expect(app.GetConnectionCredentialStatus).not.toHaveBeenCalled()
    const fresh = mountCard({ connection: '', saved: false })
    expect(fresh.find('[data-testid="sso-state"]').text()).toContain('Save the connection to check')
  })

  it('refuses a profile name AWS would not have', async () => {
    makeApp()
    const w = mountCard({ connection: 'store', saved: true, profile: 'a b' })
    await flushPromises()
    expect(w.find('[data-testid="sso-profile-invalid"]').exists()).toBe(true)
    expect((w.find('[data-testid="sso-signin-now"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('re-reads the session line when a sign-in finishes', async () => {
    const app = makeApp()
    const w = mountCard({ connection: 'store', saved: true })
    await flushPromises()
    app.GetConnectionCredentialStatus.mockImplementation(async () => ({ ...signedOut, signedIn: true, detail: 'Signed in with AWS profile dev.' }))
    handlers['signin:done']({ name: 'store', profile: 'dev' })
    await flushPromises()
    expect(w.find('[data-testid="sso-state"]').text()).toContain('Signed in')
  })
})
