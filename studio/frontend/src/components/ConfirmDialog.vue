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
    <div class="card card-pad w-full max-w-sm">
      <h3 v-if="title" class="card-title mb-2" style="font-size: 22px;">{{ title }}</h3>
      <p class="text-sm mb-4" style="color: var(--sh-graphite);">{{ message }}</p>
      <div class="flex gap-3 justify-end">
        <button @click="$emit('cancel')" class="btn btn-ghost">Cancel</button>
        <button @click="$emit('confirm')" class="btn btn-danger">Confirm</button>
      </div>
    </div>
  </div>
</template>
