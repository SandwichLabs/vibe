type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const handlers: Record<string, (p: any) => void> = {}
vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: (n: string, f: (p: any) => void) => { handlers[n] = f; return () => {} }, EventsOff: () => {} }))

import SignInBanner from './SignInBanner.vue'
import { _resetSignInForTests } from '../composables/useSignIn'
import { connectionList, _resetConnectionsForTests } from '../composables/useConnections'

const conn = (over: Partial<ConnectionInfo> = {}): ConnectionInfo => ({
  name: 'store', type: 's3', scope: 'local', readOnly: true, default: false, status: 'needs-credentials', remote: true, catalogless: true,
  signIn: 'aws-sso:dev', signInNeeded: { kind: 'aws-sso', subject: 'dev', reason: 'expired' }, ...over,
})
const PROMPT: SignInPrompt = { connection: 'store', profile: 'dev', url: 'https://device.sso.us-east-1.amazonaws.com/?user_code=ABCD-EFGH', code: 'ABCD-EFGH', expiresAt: '2026-10-16T12:10:00Z' }

function makeApp(over: MockApp = {}) {
  const app: MockApp = {
    ListConnections: vi.fn(async () => ({ connections: connectionList.value, relaunchRequired: false })),
    StartSignIn: vi.fn(async () => PROMPT),
    CancelSignIn: vi.fn(async () => {}),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {}
  return app
}
const mountBanner = () => mount(SignInBanner, { global: { stubs: { Icon: true } } })

describe('SignInBanner', () => {
  beforeEach(() => {
    _resetSignInForTests(); _resetConnectionsForTests()
    for (const k of Object.keys(handlers)) delete handlers[k]
  })

  it('says the session expired and offers Sign in and Not now', async () => {
    makeApp()
    connectionList.value = [conn()]
    const w = mountBanner()
    await flushPromises()
    const b = w.find('[data-testid="signin-banner"]')
    expect(b.text()).toContain('AWS session for profile dev has expired.')
    expect(b.text()).toContain('store')
    expect(b.text()).toContain('aws sso login --profile dev')
    expect(b.find('[data-testid="signin-start"]').text()).toBe('Sign in')
    expect(b.find('[data-testid="signin-notnow"]').exists()).toBe(true)
  })

  it('a profile that was never signed in reads differently', async () => {
    makeApp()
    connectionList.value = [conn({ signInNeeded: { kind: 'aws-sso', subject: 'dev', reason: 'missing' } })]
    const w = mountBanner()
    expect(w.text()).toContain("You aren't signed in to AWS profile dev.")
  })

  it('is not there when nothing needs an AWS sign-in', () => {
    makeApp()
    connectionList.value = [conn({ status: 'ok', signInNeeded: undefined }), conn({ name: 'pg', type: 'postgres', signIn: 'keychain', signInNeeded: undefined })]
    expect(mountBanner().find('[data-testid="signin-banner"]').exists()).toBe(false)
  })

  it('Sign in shows the code (and the address as text) with Cancel; the flow ending removes the banner', async () => {
    const app = makeApp()
    connectionList.value = [conn()]
    const w = mountBanner()
    await w.find('[data-testid="signin-start"]').trigger('click')
    await flushPromises()
    expect(app.StartSignIn).toHaveBeenCalledWith({ name: 'store', scope: '' })
    expect(w.find('[data-testid="signin-code"]').text()).toBe('ABCD-EFGH')
    expect(w.find('[data-testid="signin-code-url"]').text()).toContain('device.sso.us-east-1.amazonaws.com')
    expect(w.find('a').exists()).toBe(false) // never a link: the page was opened by the app
    expect(w.find('[data-testid="signin-start"]').exists()).toBe(false)
    expect(w.find('[data-testid="signin-cancel"]').exists()).toBe(true)

    connectionList.value = [conn({ status: 'ok', signInNeeded: undefined })]
    handlers['signin:done']({ name: 'store', profile: 'dev' })
    await flushPromises()
    expect(w.find('[data-testid="signin-banner"]').exists()).toBe(false)
  })

  it('Cancel stops the flow and the banner offers Sign in again', async () => {
    const app = makeApp()
    connectionList.value = [conn()]
    const w = mountBanner()
    await w.find('[data-testid="signin-start"]').trigger('click')
    await flushPromises()
    await w.find('[data-testid="signin-cancel"]').trigger('click')
    await flushPromises()
    expect(app.CancelSignIn).toHaveBeenCalledWith({ name: 'store', scope: '' })
    expect(w.find('[data-testid="signin-start"]').exists()).toBe(true)
  })

  it('Not now hides it until the workspace is opened again', async () => {
    makeApp()
    connectionList.value = [conn()]
    const w = mountBanner()
    await flushPromises()
    await w.find('[data-testid="signin-notnow"]').trigger('click')
    expect(w.find('[data-testid="signin-banner"]').exists()).toBe(false)
    handlers['workspace:opened']({})
    await flushPromises()
    expect(w.find('[data-testid="signin-banner"]').exists()).toBe(true)
  })

  it('a failed sign-in shows its sentence in an alert and the buttons come back', async () => {
    makeApp()
    connectionList.value = [conn()]
    const w = mountBanner()
    await w.find('[data-testid="signin-start"]').trigger('click')
    await flushPromises()
    handlers['signin:failed']({ name: 'store', profile: 'dev', message: 'the AWS sign-in code expired before it was approved' })
    await flushPromises()
    expect(w.find('[data-testid="signin-error"]').text()).toContain('expired before it was approved')
    expect(w.find('[data-testid="signin-start"]').exists()).toBe(true)
  })
})
