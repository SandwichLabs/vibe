<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'

const props = withDefaults(defineProps<{
  visible: boolean
  title?: string
  message: string
  // 'destructive' (default) is the red delete-style button, for the
  // existing delete-confirmation call sites (Pages/Tables/Queries/Skills/
  // Forms/Schema). 'primary' is the ordinary teal action style, for a
  // confirm that isn't destructive — e.g. "Open this link in your
  // browser?" (composables/useConfirm.ts, used by lib/markdown.ts's link
  // handler) shouldn't look like a delete warning.
  variant?: 'destructive' | 'primary'
}>(), {
  variant: 'destructive',
})

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.visible) {
    e.preventDefault()
    emit('cancel')
  }
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => document.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div v-if="visible" class="modal-overlay" @click.self="$emit('cancel')">
    <div class="card card-pad" style="width: 100%; max-width: 384px;">
      <h3 v-if="title" class="card-title" style="font-size: 22px; margin-bottom: 8px;">{{ title }}</h3>
      <p class="sh-body" style="margin-bottom: 16px; color: var(--sh-graphite);">{{ message }}</p>
      <div class="row" style="justify-content: flex-end;">
        <button @click="$emit('cancel')" class="btn btn-ghost">Cancel</button>
        <button @click="$emit('confirm')" :class="variant === 'primary' ? 'btn btn-primary' : 'btn btn-destructive'">Confirm</button>
      </div>
    </div>
  </div>
</template>
