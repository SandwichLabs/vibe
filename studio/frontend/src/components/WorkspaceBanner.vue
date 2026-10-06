<script setup lang="ts">
// WorkspaceBanner — SPEC 07 C. Across the top of the main pane while the
// workspace is open read-only because another program (usually `vibe mcp serve`)
// owns its database. Read-only is explained here, not in the agent pill.
import { computed, ref } from 'vue'
import Icon from './Icon.vue'
import { workspaceInfo, takeOverOpen } from '../composables/useWorkspace'
import { errorText } from '../lib/errors'

const dismissed = ref('')
const info = computed(() => workspaceInfo.value)
const show = computed(() => !!info.value?.readOnly && dismissed.value !== info.value.root)
const who = computed(() => info.value?.holder?.text || 'Another program')

const error = ref('')
const busy = ref(false)

// A copied folder shares its original's id, and so its keychain entries.
const dupes = computed(() => info.value?.duplicateIds ?? [])
async function regenerate() {
  busy.value = true
  error.value = ''
  try { workspaceInfo.value = await window.go.main.App.RegenerateWorkspaceID() } catch (e: unknown) { error.value = errorText(e) }
  busy.value = false
}

// Saved per-person settings that were made for a different folder at this path.
async function trust() {
  busy.value = true
  error.value = ''
  try { workspaceInfo.value = await window.go.main.App.TrustLocalSettings() } catch (e: unknown) { error.value = errorText(e) }
  busy.value = false
}
</script>

<template>
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
