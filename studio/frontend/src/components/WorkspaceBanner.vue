<script setup lang="ts">
import { App } from '../lib/bindings'
// WorkspaceBanner — SPEC 07 C. Across the top of the main pane while the
// workspace is open read-only because another program (usually `vibe mcp serve`)
// owns its database. Read-only is explained here, not in the agent pill.
import { computed, ref, onMounted } from 'vue'
import Icon from './Icon.vue'
import { workspaceInfo, takeOverOpen, openWorkspacePath } from '../composables/useWorkspace'
import { relaunch, refreshConnections } from '../composables/useConnections'
import { errorText } from '../lib/errors'
import { dotenv, dotenvPrompt, dotenvBusy, dotenvError, allowDotEnv, denyDotEnv, dotenvNotNow, startDotEnvTracking } from '../composables/useDotEnv'

onMounted(startDotEnvTracking)

const dismissed = ref('')
const info = computed(() => workspaceInfo.value)
const show = computed(() => !!info.value?.readOnly && dismissed.value !== info.value.root)
const who = computed(() => info.value?.holder?.text || 'Another program')

const error = ref('')
const busy = ref(false)

// MotherDuck accepts one token per process (D10): after its extension is
// installed or its token changed, the workspace session has to be closed and
// reopened. That is cheaper than relaunching the app, and a fresh session is
// all it needs.
const reopening = ref(false)
async function reopen() {
  if (!info.value) return
  reopening.value = true
  error.value = ''
  const res = await openWorkspacePath(info.value.root)
  if (!res.ok && res.error) error.value = res.error
  await refreshConnections()
  reopening.value = false
}

// A copied folder shares its original's id, and so its keychain entries.
const dupes = computed(() => info.value?.duplicateIds ?? [])
async function regenerate() {
  busy.value = true
  error.value = ''
  try { workspaceInfo.value = await App.RegenerateWorkspaceID() } catch (e: unknown) { error.value = errorText(e) }
  busy.value = false
}

// Saved per-person settings that were made for a different folder at this path.
async function trust() {
  busy.value = true
  error.value = ''
  try { workspaceInfo.value = await App.TrustLocalSettings() } catch (e: unknown) { error.value = errorText(e) }
  busy.value = false
}
</script>

<template>
  <!-- The workspace's .env (#168): ask once, direnv-style. Names only, never values. -->
  <div v-if="dotenvPrompt && dotenv" class="ws-banner" role="status" data-testid="dotenv-banner">
    <Icon name="key" :size="20" class="lead-ico" />
    <div class="bt">
      <b v-if="dotenv.state === 'changed'">This workspace's .env changed since you allowed it.</b> <template v-if="dotenv.loaded">The previous values stay loaded until you allow it again.</template>
      <b v-else>This workspace has a .env file. Load it?</b>
      Loading it lets this workspace's <code>env:</code> sign-ins read the variables below. They are used only here, are never shown, and are not shared with other workspaces or with the agent.
      <span class="sub" data-testid="dotenv-names">Variables: <template v-if="dotenv.names.length"><template v-for="(n, i) in dotenv.names" :key="n"><code>{{ n }}</code><template v-if="i < dotenv.names.length - 1">, </template></template></template><template v-else>none</template></span>
      <span v-for="s in dotenv.satisfies" :key="s.connection" class="sub" data-testid="dotenv-feeds">Would sign in <b>{{ s.connection }}</b> with <code>{{ s.signIn }}</code> ({{ s.variables.join(', ') }}).</span>
      <span v-if="dotenv.trackedByGit" class="sub" role="alert" style="color: var(--sh-error); font-weight: 600;" data-testid="dotenv-tracked">Git tracks this file, so a credential is committed. Remove it from git (<code>git rm --cached .env</code>) and rotate the credential.</span>
      <span v-for="w in dotenv.warnings ?? []" :key="w" class="sub">{{ w }}</span>
      <span v-if="dotenvError" class="sub" role="alert" style="color: var(--sh-error);">{{ dotenvError }}</span>
    </div>
    <div class="row">
      <button class="btn btn-primary btn-sm" type="button" :disabled="dotenvBusy" data-testid="dotenv-allow" @click="allowDotEnv">{{ dotenvBusy ? 'Loading…' : 'Allow' }}</button>
      <button class="btn btn-sm" type="button" :disabled="dotenvBusy" data-testid="dotenv-deny" @click="denyDotEnv">Deny</button>
      <button class="btn btn-sm" type="button" :disabled="dotenvBusy" data-testid="dotenv-notnow" @click="dotenvNotNow">Not now</button>
    </div>
  </div>
  <div v-if="relaunch.required && info" class="ws-banner" role="status" data-testid="relaunch-banner">
    <Icon name="refresh" :size="20" class="lead-ico" />
    <div class="bt">
      <b>Reopen this workspace to finish connecting.</b> {{ relaunch.message || 'MotherDuck signs in once, when the workspace opens.' }}
      <span class="sub">It takes a moment and keeps your notebooks. Unsaved edits are saved first. {{ error }}</span>
    </div>
    <div class="row"><button class="btn btn-primary btn-sm" type="button" :disabled="reopening" data-testid="relaunch" @click="reopen">{{ reopening ? 'Reopening…' : 'Reopen workspace' }}</button></div>
  </div>
  <div v-if="info?.localUntrusted" class="ws-banner attn" role="alert" data-testid="untrusted-banner">
    <Icon name="alert" :size="20" class="lead-ico" />
    <div class="bt">
      <b>This folder isn't the one your saved settings were made for.</b> It may have been deleted and cloned again, so the agent limits you loosened, your sign-in choices and your own connections are being ignored.
      <span class="sub">If this is the same project, confirm your saved settings. {{ error }}</span>
    </div>
    <div class="row"><button class="btn btn-primary btn-sm" type="button" :disabled="busy" data-testid="trust-local" @click="trust">Use my saved settings</button></div>
  </div>
  <div v-if="dupes.length" class="ws-banner" role="status" data-testid="dup-banner">
    <Icon name="alert" :size="20" class="lead-ico" />
    <div class="bt">
      <b>This workspace has the same id as another folder.</b> <code>{{ dupes[0] }}</code> looks like the original, so the two would share saved passwords and settings.
      <span class="sub">Give this copy its own id. The original keeps what it has saved; this copy starts without it. {{ error }}</span>
    </div>
    <div class="row"><button class="btn btn-primary btn-sm" type="button" :disabled="busy" data-testid="regenerate-id" @click="regenerate">Regenerate id</button></div>
  </div>
  <div v-if="show && info" class="ws-banner" role="status" data-testid="readonly-banner">
    <Icon name="lock" :size="20" class="lead-ico" />
    <div class="bt">
      <b>Opened read-only.</b> <code>{{ who }}</code> is already running this workspace, so Vibe can look at it but not change it.
      <span class="sub">Notebooks, queries and skills are plain files and stay editable, and remote connections work. Local database files are in use and can't be edited until it lets go.</span>
    </div>
    <div class="row">
      <button class="btn btn-primary btn-sm" type="button" data-testid="take-over" @click="takeOverOpen = true">Take over…</button>
      <button class="btn btn-sm" type="button" @click="dismissed = info.root">Keep read-only</button>
    </div>
  </div>
</template>
