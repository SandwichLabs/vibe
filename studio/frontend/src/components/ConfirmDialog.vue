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
  <div v-if="visible" class="fixed inset-0 bg-black/30 flex items-center justify-center z-50" @click.self="$emit('cancel')">
    <div class="bg-white rounded-lg shadow-xl p-6 w-full max-w-sm">
      <h3 v-if="title" class="text-lg font-semibold mb-2">{{ title }}</h3>
      <p class="text-sm text-gray-600 mb-4">{{ message }}</p>
      <div class="flex gap-3 justify-end">
        <button @click="$emit('cancel')" class="px-4 py-2 text-gray-600 hover:text-gray-800">Cancel</button>
        <button @click="$emit('confirm')" class="px-4 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700">Confirm</button>
      </div>
    </div>
  </div>
</template>
