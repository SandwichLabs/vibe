// useSignIn — AWS sign-in from the window (#131, step 8): which connections need
// the person to sign in with an AWS profile, the running device flows (the code
// to check), and the errors. A module singleton like useDotEnv; the banner
// (SignInBanner), the rail's "Sign in" action and the connection editor's card
// read the same state. Only the profile, the connection names, the page address
// and the user code cross the bridge, never a token.
//
// "Not now" lasts for this open of the workspace only: opening it again asks
// again, as the .env prompt does.
import { ref, computed } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { ssoProfileOf } from '../lib/connections'
import { App } from '../lib/bindings'
import { connectionList, refreshConnections, setAwsSignInReveal } from './useConnections'

export interface SignInItem {
  profile: string
  /** The connection a flow is started from. */
  connection: string
  /** Every connection that waits on the profile. */
  names: string[]
  reason: string
  prompt: SignInPrompt | null
  starting: boolean
  error: string
}

const prompts = ref<Record<string, SignInPrompt>>({})
const errors = ref<Record<string, string>>({})
const starting = ref<Set<string>>(new Set())
const notNow = ref<Set<string>>(new Set())
// Bumped when the workspace changes: an answer that arrives after it is dropped.
let generation = 0

/** One entry per profile that needs a sign-in or has a flow running, "Not now" aside. */
export const signInItems = computed<SignInItem[]>(() => {
  const byProfile = new Map<string, SignInItem>()
  for (const c of connectionList.value) {
    if (c.status !== 'needs-credentials' || c.signInNeeded?.kind !== 'aws-sso') continue
    const p = ssoProfileOf(c)
    const it = byProfile.get(p) ?? { profile: p, connection: c.name, names: [], reason: c.signInNeeded.reason, prompt: null, starting: false, error: '' }
    it.names.push(c.name)
    byProfile.set(p, it)
  }
  for (const [p, pr] of Object.entries(prompts.value)) {
    const it = byProfile.get(p) ?? { profile: p, connection: pr.connection, names: [pr.connection], reason: 'missing', prompt: null, starting: false, error: '' }
    it.prompt = pr
    byProfile.set(p, it)
  }
  const out: SignInItem[] = []
  for (const it of byProfile.values()) {
    it.starting = starting.value.has(it.profile)
    it.error = errors.value[it.profile] ?? ''
    if (notNow.value.has(it.profile) && !it.prompt && !it.starting && !it.error) continue
    out.push(it)
  }
  return out
})

/** The running flow for a profile, if any (the editor card shows its code too). */
export const promptFor = (profile: string): SignInPrompt | null => prompts.value[profile] ?? null
export const isStarting = (profile: string): boolean => starting.value.has(profile)
export const errorFor = (profile: string): string => errors.value[profile] ?? ''

function profileOfConnection(name: string): string {
  const c = connectionList.value.find((x) => x.name === name)
  return c ? ssoProfileOf(c) : ''
}

/** Start (or join) the sign-in for a connection that uses an AWS profile. */
export async function startSignIn(connection: string, profileHint = ''): Promise<void> {
  const profile = profileOfConnection(connection) || profileHint
  if (!profile || starting.value.has(profile) || prompts.value[profile]) return
  const gen = generation
  starting.value = new Set(starting.value).add(profile)
  errors.value = { ...errors.value, [profile]: '' }
  try {
    const pr = await App.StartSignIn({ name: connection, scope: '' })
    if (gen === generation) prompts.value = { ...prompts.value, [pr.profile || profile]: pr }
  } catch (e: unknown) {
    if (gen === generation) errors.value = { ...errors.value, [profile]: errorText(e) }
  }
  if (gen !== generation) return
  const s = new Set(starting.value)
  s.delete(profile)
  starting.value = s
}

export async function cancelSignIn(profile: string): Promise<void> {
  const pr = prompts.value[profile]
  const { [profile]: _gone, ...rest } = prompts.value
  prompts.value = rest
  if (!pr) return
  try { await App.CancelSignIn({ name: pr.connection, scope: '' }) } catch { /* the flow is already over */ }
}

export function signInNotNow(profile: string): void {
  notNow.value = new Set(notNow.value).add(profile)
}

/** Show the banner again (a use of the connection found it signed out); starts nothing. */
function revealSignIn(profile: string): void {
  const s = new Set(notNow.value)
  s.delete(profile)
  notNow.value = s
}

function clearFlow(profile: string, error: string): void {
  const { [profile]: _p, ...rest } = prompts.value
  prompts.value = rest
  errors.value = { ...errors.value, [profile]: error }
}

function reset(): void {
  generation++
  prompts.value = {}
  errors.value = {}
  starting.value = new Set()
  notNow.value = new Set()
}

let started = false
/** Called from SignInBanner's onMounted (as WorkspaceBanner starts useDotEnv's). */
export function startSignInTracking(): void {
  if (started || !hasWailsRuntime()) return
  started = true
  setAwsSignInReveal(revealSignIn)
  EventsOn('signin:done', (p: { profile?: string }) => {
    if (p?.profile) clearFlow(p.profile, '')
    void refreshConnections()
  })
  EventsOn('signin:failed', (p: { profile?: string; message?: string }) => {
    if (p?.profile) clearFlow(p.profile, p.message || 'The sign-in did not finish.')
    void refreshConnections()
  })
  EventsOn('workspace:opened', () => { reset() })
  EventsOn('workspace:closed', () => { reset() })
}

/** Test helper: forget everything. */
export function _resetSignInForTests(): void {
  reset()
  started = false
  setAwsSignInReveal(null)
}
