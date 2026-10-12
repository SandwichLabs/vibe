<script setup lang="ts">
import { App } from '../lib/bindings'
// CredentialsPromptDialog — SPEC 07 A, A2, A3: sign in to a connection that has
// no usable sign-in on this computer (first use, "ask each time", or a server
// that changed since the saved sign-in). Secrets are typed here and sent to the
// backend once; nothing ever comes back. "Remember in my keychain" is on for
// keychain mode and off for "ask me each time".
import { ref, computed, watch, nextTick } from 'vue'
import Icon from './Icon.vue'
import { useModalKeys } from '../composables/useModal'
import { signInFor, refreshConnections } from '../composables/useConnections'
import { errorText } from '../lib/errors'
import { CRED_KEY } from '../lib/connections'
import { useRouter } from 'vue-router'

const router = useRouter()
const open = computed(() => signInFor.value !== null)
const root = ref<HTMLElement | null>(null)
useModalKeys(open, () => close(), root)

const detail = ref<ConnectionDetail | null>(null)
const spec = ref<ConnTypeSpec | null>(null)
const values = ref<Record<string, string>>({})
const remember = ref(true)
const busy = ref(false)
const error = ref('')

const name = computed(() => signInFor.value ?? '')
const serverChanged = computed(() => detail.value?.info.serverChanged ?? null)
const cred = computed(() => detail.value?.credential ?? null)
const keychainDown = computed(() => !!cred.value && cred.value.available === false)
const isEnv = computed(() => (detail.value?.signIn ?? '').startsWith('env:'))
const fields = computed(() => spec.value?.credentials ?? [])

watch(signInFor, async (n) => {
  error.value = ''
  busy.value = false
  detail.value = null
  values.value = {}
  if (!n) return
  try {
    const [d, types] = await Promise.all([App.GetConnection(n), App.ListConnectionTypes()])
    detail.value = d
    spec.value = types.find((t) => t.id === d.def.type) ?? null
    remember.value = !d.signIn.startsWith('prompt')
    // Username and key id are not secrets: show what is saved.
    values.value = { user: d.credential.username ?? '', key_id: d.credential.keyId ?? '' }
    // The form only exists once the connection has loaded: focus its first field now.
    await nextTick()
    root.value?.querySelector<HTMLElement>('input:not([type=checkbox])')?.focus()
  } catch (e: unknown) {
    error.value = errorText(e)
  }
}, { immediate: true })

function close() {
  if (busy.value) return
  signInFor.value = null
}

const title = computed(() => (serverChanged.value ? `Sign in to ${name.value} again` : `Sign in to ${name.value}`))
const canSubmit = computed(() => fields.value.filter((f) => f.required).every((f) => (values.value[f.name] ?? '').trim() !== ''))

async function submit() {
  if (!detail.value || busy.value) return
  busy.value = true
  error.value = ''
  const req: ConnectWithCredentialsRequest = { name: name.value, username: '', keyId: '', password: '', token: '', secret: '', remember: remember.value }
  for (const f of fields.value) {
    const k = CRED_KEY[f.name]
    // Only the readable fields are trimmed; a password, token or secret is kept exactly as typed.
    const raw = values.value[f.name] ?? ''
    if (k) (req as any)[k] = f.secret ? raw : raw.trim()
  }
  try {
    await App.ConnectWithCredentials(req)
    values.value = {} // drop the typed secrets from memory
    signInFor.value = null
    await refreshConnections()
  } catch (e: unknown) {
    error.value = errorText(e)
    await refreshConnections()
  }
  busy.value = false
}

async function tryAgain() {
  const n = name.value
  signInFor.value = null
  await new Promise((r) => setTimeout(r, 0))
  signInFor.value = n
}

function editConnection() {
  const n = name.value
  signInFor.value = null
  void router.push(`/connections/${n}`)
}
</script>

<template>
  <div v-if="open" class="modal-overlay" role="presentation" @click.self="close">
    <div ref="root" class="dialog" role="dialog" aria-modal="true" aria-labelledby="cp-h" aria-describedby="cp-d" data-testid="signin-dialog">
      <!-- A2: the keychain is not answering -->
      <template v-if="keychainDown && !isEnv">
        <div class="d-head"><h2 id="cp-h">Can't reach your keychain</h2></div>
        <div id="cp-d" class="d-body">
          <p>Vibe stores passwords in this computer's keychain, but it isn't answering. That's common over SSH or when no one is signed in to the desktop.</p>
          <div class="note info"><Icon name="info" :size="16" /><span>You can still connect. Set the variables in the environment Vibe starts from, then choose <b>Environment variables</b> for this connection. Vibe never writes passwords to a file instead.</span></div>
        </div>
        <div class="d-foot">
          <button class="btn" type="button" data-testid="signin-retry" @click="tryAgain">Try again</button>
          <button class="btn btn-primary" type="button" data-testid="signin-edit" @click="editConnection">Edit connection</button>
        </div>
      </template>

      <!-- Environment variables: nothing to type here -->
      <template v-else-if="isEnv">
        <div class="d-head"><h2 id="cp-h">{{ name }} signs in from environment variables</h2></div>
        <div id="cp-d" class="d-body">
          <p>{{ cred?.detail || 'The variables below are not all set in the environment Vibe started from.' }}</p>
          <div v-for="e in cred?.env ?? []" :key="e.name" class="envvar"><code>{{ e.name }}</code><span class="set" :class="e.set ? 'yes' : 'no'">{{ e.set ? 'Set' : 'Not set' }}</span></div>
        </div>
        <div class="d-foot">
          <button class="btn" type="button" @click="close">Close</button>
          <button class="btn btn-primary" type="button" @click="editConnection">Edit connection</button>
        </div>
      </template>

      <template v-else>
        <form @submit.prevent="submit">
          <div class="d-head"><h2 id="cp-h">{{ title }}</h2></div>
          <div id="cp-d" class="d-body">
            <template v-if="serverChanged">
              <p><b>{{ name }}</b>'s server changed since you signed in ({{ serverChanged.old }} to {{ serverChanged.new }}). Sign in again to confirm it's the one you mean.</p>
              <div class="note info"><Icon name="info" :size="16" /><span>This usually happens after you pull a change from your team. Your saved password is only used with the server it was saved for, so it isn't sent to the new one.</span></div>
            </template>
            <p v-else>Vibe doesn't have {{ spec?.id === 'motherduck' ? 'a token' : spec?.id === 'postgres' ? 'a username and password' : 'access keys' }} for this connection on this computer yet.</p>

            <div v-for="(f, i) in fields" :key="f.name" class="field">
              <label class="field-label" :for="`cp-${f.name}`">{{ f.label }}</label>
              <input
                :id="`cp-${f.name}`"
                v-model="values[f.name]"
                class="field-input"
                :type="f.secret ? 'password' : 'text'"
                :autocomplete="f.secret ? 'new-password' : 'off'"
                :aria-describedby="f.help ? `cp-${f.name}-h` : undefined"
                :data-testid="`signin-${f.name}`"
                :autofocus="i === 0"
              />
              <div v-if="f.help" :id="`cp-${f.name}-h`" class="help">{{ f.help }}</div>
            </div>

            <label class="switch-row" style="align-items: center;">
              <input v-model="remember" type="checkbox" data-testid="signin-remember" />
              <span>
                <span class="st-t">Remember in my keychain</span>
                <span class="st-d">{{ remember ? "Saved on this computer only. Vibe can't show it again, only replace or clear it." : 'Kept for this session only, then forgotten. Your sign-in method does not change.' }}</span>
              </span>
            </label>

            <div v-if="error" class="note attn" role="alert" data-testid="signin-error"><Icon name="alert" :size="16" /><span>{{ error }}</span></div>
          </div>
          <div class="d-foot">
            <button class="btn" type="button" :disabled="busy" @click="close">Cancel</button>
            <button class="btn btn-primary" type="submit" :disabled="busy || !canSubmit" data-testid="signin-submit">{{ busy ? 'Connecting…' : serverChanged ? 'Confirm and connect' : 'Connect' }}</button>
          </div>
        </form>
      </template>
    </div>
  </div>
</template>
