<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch, nextTick } from 'vue'

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
  // Label for the confirming button. Name the action ("Delete", "Drop
  // table") rather than "Confirm" so the user reads what will happen.
  confirmLabel?: string
}>(), {
  variant: 'destructive',
  confirmLabel: 'Confirm',
})

const confirmBtn = ref<HTMLButtonElement | null>(null)
let previouslyFocused: Element | null = null
watch(() => props.visible, async (v) => {
  if (v) {
    previouslyFocused = document.activeElement
    await nextTick()
    confirmBtn.value?.focus()
  } else {
    const prev = previouslyFocused as HTMLElement | null
    previouslyFocused = null
    if (prev && typeof prev.focus === 'function' && document.contains(prev)) prev.focus()
  }
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
    <div class="card card-pad" style="width: 100%; max-width: 384px;" role="dialog" aria-modal="true" aria-labelledby="confirm-dialog-title" aria-describedby="confirm-dialog-message">
      <h3 v-if="title" id="confirm-dialog-title" class="card-title" style="font-size: 22px; margin-bottom: 8px;">{{ title }}</h3>
      <p id="confirm-dialog-message" class="sh-body" style="margin-bottom: 16px; color: var(--sh-graphite);">{{ message }}</p>
      <div class="row" style="justify-content: flex-end;">
        <button @click="$emit('cancel')" class="btn btn-ghost">Cancel</button>
        <button ref="confirmBtn" @click="$emit('confirm')" :class="variant === 'primary' ? 'btn btn-primary' : 'btn btn-destructive'">{{ confirmLabel }}</button>
      </div>
    </div>
  </div>
</template>
