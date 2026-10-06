<script setup lang="ts">
// TakeOverDialog — SPEC 07 C2. Asks the program that owns the workspace to let
// go, then reopens it for editing. Copy follows #112: a client that was using the
// other program must reconnect itself, and will then go through Studio.
import { ref, computed, watch } from 'vue'
import Icon from './Icon.vue'
import { useModalKeys } from '../composables/useModal'
import { takeOverOpen, takeOverWorkspace, workspaceInfo, flushOrConfirm } from '../composables/useWorkspace'

const root = ref<HTMLElement | null>(null)
const busy = ref(false)
const error = ref('')
useModalKeys(takeOverOpen, () => close(), root)

const name = computed(() => workspaceInfo.value?.name ?? 'this workspace')
const who = computed(() => workspaceInfo.value?.holder?.text || 'the other program')

watch(takeOverOpen, (o) => { if (o) { busy.value = false; error.value = '' } })

function close() {
  if (!busy.value) takeOverOpen.value = false
}

async function confirm() {
  busy.value = true
  error.value = ''
  // Save open notebooks first: taking over reopens the workspace.
  if (!(await flushOrConfirm('Take over'))) { busy.value = false; return }
  const res = await takeOverWorkspace()
  busy.value = false
  if (res.ok) takeOverOpen.value = false
  else error.value = res.error || "Couldn't take over the workspace."
}
</script>

<template>
  <div v-if="takeOverOpen" class="modal-overlay" role="presentation" @click.self="close">
    <div ref="root" class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="to-h" aria-describedby="to-d" data-testid="takeover-dialog">
      <div class="d-head"><h2 id="to-h">Take over {{ name }}?</h2></div>
      <div id="to-d" class="d-body">
        <p>Vibe will ask <code>{{ who }}</code> to let go of this workspace, then open it for editing.</p>
        <p>Claude Desktop or Claude Code sessions that were using it will need to reconnect, and will then go through Vibe Studio. Nothing is deleted, and open notebooks aren't affected.</p>
        <div class="note info"><Icon name="info" :size="16" /><span>After you take over, any change Claude makes needs your approval in Vibe Studio, and closing Studio ends the session it was passing along.</span></div>
        <div v-if="error" class="note attn" role="alert" data-testid="takeover-error"><Icon name="alert" :size="16" /><span>{{ error }}</span></div>
      </div>
      <div class="d-foot">
        <button class="btn" type="button" :disabled="busy" @click="close">Cancel</button>
        <button class="btn btn-primary" type="button" :disabled="busy" data-testid="takeover-confirm" @click="confirm">{{ busy ? 'Taking over…' : 'Take over' }}</button>
      </div>
    </div>
  </div>
</template>
