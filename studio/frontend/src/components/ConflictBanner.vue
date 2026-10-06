<script setup lang="ts">
// ConflictBanner — SPEC 07 D. Sticky until resolved, role="alert". Shown by a
// notebook, saved query or skill whose file changed on disk while it had unsaved
// edits (a git pull, another editor, the agent). Typing is never blocked while it
// shows. Each choice that loses something states what in a one-line confirm.
import { computed } from 'vue'
import Icon from './Icon.vue'
import { askConfirm } from '../composables/useConfirm'

const props = defineProps<{
  kind: 'notebook' | 'query' | 'skill'
  // Workspace-relative file, e.g. notebooks/weekly-revenue.vnb.
  path: string
  // The file was deleted on disk rather than changed.
  deleted?: boolean
  busy?: boolean
}>()
const emit = defineEmits<{ reload: []; overwrite: []; saveCopy: [] }>()

const thing = computed(() => ({ notebook: 'notebook', query: 'query', skill: 'skill' })[props.kind])

async function reload() {
  const ok = await askConfirm({
    title: 'Reload from disk?',
    message: `Your unsaved edit to this ${thing.value} will be dropped and the version on disk will be shown instead.`,
    variant: 'destructive',
    confirmLabel: 'Reload from disk',
  })
  if (ok) emit('reload')
}

async function overwrite() {
  const ok = await askConfirm({
    title: props.deleted ? 'Write it back?' : 'Keep yours?',
    message: props.deleted
      ? `The file was deleted on disk. Your version will be written back.`
      : `Your version will replace the one on disk. The other change to this ${thing.value} will be lost.`,
    variant: 'destructive',
    confirmLabel: 'Keep mine',
  })
  if (ok) emit('overwrite')
}
</script>

<template>
  <div class="ws-banner attn" role="alert" data-testid="conflict-banner">
    <Icon name="alert" :size="20" class="lead-ico" />
    <div class="bt">
      <template v-if="deleted">
        <b>This {{ thing }} was deleted on disk.</b> <code>{{ path }}</code> was removed while you had it open: a git checkout, another editor, or the agent.
      </template>
      <template v-else>
        <b>This {{ thing }} changed on disk.</b> <code>{{ path }}</code> was saved by something else while you had it open: a git pull, another editor, or the agent.
      </template>
      <span class="sub">Your unsaved edit and the new version differ. Pick one. Nothing happens until you do.</span>
    </div>
    <div class="row">
      <button class="btn btn-primary btn-sm" type="button" :disabled="busy" data-testid="conflict-reload" @click="reload">{{ deleted ? 'Close it' : 'Reload from disk' }}</button>
      <button class="btn btn-sm" type="button" :disabled="busy" data-testid="conflict-overwrite" @click="overwrite">Keep mine</button>
      <button class="btn btn-sm" type="button" :disabled="busy" data-testid="conflict-copy" @click="emit('saveCopy')">Save mine as a copy</button>
    </div>
  </div>
</template>
