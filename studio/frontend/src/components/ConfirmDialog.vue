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
    <div class="card w-full max-w-sm">
      <h3 v-if="title" class="text-lg font-bold mb-2">{{ title }}</h3>
      <p class="text-sm mb-4">{{ message }}</p>
      <div class="flex gap-3 justify-end">
        <button @click="$emit('cancel')" class="btn btn-ghost">Cancel</button>
        <button @click="$emit('confirm')" class="btn btn-danger">Confirm</button>
      </div>
    </div>
  </div>
</template>
