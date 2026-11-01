<script setup lang="ts">
// ReplTerminal — the Data Console: an xterm.js terminal on a Go console
// session (useReplTerminal). It opens when its tab is first shown and keeps
// the session while the tab is hidden; a session that ended offers Reopen.
import { ref, computed, watch, onBeforeUnmount, nextTick } from 'vue'
import { useReplTerminal } from '../composables/useReplTerminal'

const props = defineProps<{ connection: string; active: boolean }>()

const el = ref<HTMLElement | null>(null)
const { open, close, refit, useConnection, ended, id, renderer, error, current, refusal, refusedFor } = useReplTerminal()
// Why the console is not on the connection the picker shows: only while they differ.
const notice = computed(() => {
  const want = props.connection
  if (!refusal.value || !id.value || ended.value || want !== refusedFor.value) return ''
  return `Console still on ${current.value || 'the default connection'}: ${refusal.value}`
})
let started = false

async function start() {
  if (!el.value) return
  started = true
  await open(el.value, props.connection)
}

async function reopen() {
  close()
  await nextTick()
  await start()
}

watch(() => props.active, async (on) => {
  if (!on) return
  await nextTick()
  if (!started) await start()
  else refit()
}, { immediate: true })

// The picker moved: Go moves the console only at a clean prompt (see
// useConnection). When it refuses, the console stays put and the status line
// says so.
watch(() => props.connection, (name) => {
  if (!started || !id.value || ended.value || name === current.value) return
  void useConnection(name)
})

onBeforeUnmount(close)
</script>

<template>
  <div class="repl">
    <div
      ref="el"
      class="repl-term"
      data-testid="repl-terminal"
      :data-repl-id="id"
      :data-renderer="renderer"
    ></div>
    <div v-if="ended || error || notice" class="repl-status" data-testid="repl-status">
      <span v-if="ended">Console closed: {{ ended }}</span>
      <span v-if="error" class="repl-error">{{ error }}</span>
      <span v-if="notice" data-testid="repl-notice">{{ notice }}</span>
      <button v-if="ended || (error && !id)" class="btn btn-sm" data-testid="repl-reopen" @click="reopen">Reopen</button>
    </div>
  </div>
</template>

<style scoped>
.repl {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: var(--assay-editor-bg);
}
.repl-term {
  flex: 1;
  min-height: 0;
  padding: 6px 8px;
}
.repl-status {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 6px 12px;
  border-top: 1.5px solid var(--assay-border-subtle);
  background: var(--assay-bg-alt);
  color: var(--assay-fg-muted);
  font-family: var(--assay-font-mono);
  font-size: 12px;
}
.repl-error { color: var(--assay-error); }
</style>
