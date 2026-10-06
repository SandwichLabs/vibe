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
  // altLabel, when set, renders a THIRD button between Cancel and the
  // confirm button (e.g. "Don't save" for the unsaved-changes prompt, #81)
  // that emits 'alt' on click. Omitted entirely when unset — the
  // regression case (two buttons) is unchanged.
  altLabel?: string
  // true gives the alt button the same red delete-style tint as
  // 'destructive' confirm buttons — e.g. "Don't save" discards data, so it
  // reads as the destructive choice even though "Save" is the (primary)
  // confirm button here. Defaults to the plain ghost style.
  altDestructive?: boolean
}>(), {
  variant: 'destructive',
  confirmLabel: 'Confirm',
  altDestructive: false,
})

const confirmBtn = ref<HTMLButtonElement | null>(null)
const cancelBtn = ref<HTMLButtonElement | null>(null)
let previouslyFocused: Element | null = null
watch(() => props.visible, async (v) => {
  if (v) {
    previouslyFocused = document.activeElement
    await nextTick()
    // A destructive confirm (the default) focuses Cancel, not the button
    // that discards data — Enter must not be able to destroy something by
    // accident. The #81 Save/Don't save/Cancel prompt passes
    // variant: 'primary' (Save isn't destructive) and keeps focusing the
    // confirm button, so Enter still means "Save" there.
    if (props.variant === 'primary') confirmBtn.value?.focus()
    else cancelBtn.value?.focus()
  } else {
    const prev = previouslyFocused as HTMLElement | null
    previouslyFocused = null
    if (prev && typeof prev.focus === 'function' && document.contains(prev)) prev.focus()
  }
})

const emit = defineEmits<{
  confirm: []
  cancel: []
  alt: []
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
      <p id="confirm-dialog-message" class="sh-body" style="margin-bottom: 16px; color: var(--sh-graphite); white-space: pre-line; overflow-wrap: anywhere;">{{ message }}</p>
      <div class="row" style="justify-content: flex-end;">
        <button ref="cancelBtn" @click="$emit('cancel')" class="btn btn-ghost">Cancel</button>
        <button v-if="altLabel" @click="$emit('alt')" :class="altDestructive ? 'btn btn-destructive' : 'btn btn-ghost'">{{ altLabel }}</button>
        <button ref="confirmBtn" @click="$emit('confirm')" :class="variant === 'primary' ? 'btn btn-primary' : 'btn btn-destructive'">{{ confirmLabel }}</button>
      </div>
    </div>
  </div>
</template>
