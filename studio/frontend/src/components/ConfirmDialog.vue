<script setup lang="ts">
defineProps<{
  visible: boolean
  title?: string
  message: string
}>()

defineEmits<{
  confirm: []
  cancel: []
}>()
</script>

<template>
  <div v-if="visible" class="modal-overlay" @click.self="$emit('cancel')">
    <div class="card card-pad" style="width: 100%; max-width: 384px;">
      <h3 v-if="title" class="card-title" style="font-size: 22px; margin-bottom: 8px;">{{ title }}</h3>
      <p class="sh-body" style="margin-bottom: 16px; color: var(--sh-graphite);">{{ message }}</p>
      <div class="row" style="justify-content: flex-end;">
        <button @click="$emit('cancel')" class="btn btn-ghost">Cancel</button>
        <button @click="$emit('confirm')" class="btn btn-destructive">Confirm</button>
      </div>
    </div>
  </div>
</template>
