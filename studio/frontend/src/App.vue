<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { EventsOn, EventsOff } from './wailsjs/runtime/runtime'
import UnlockView from './views/UnlockView.vue'
import AppShell from './components/AppShell.vue'

const isOpen = ref(false)
const initialFile = ref('')

async function checkFileOpen() {
  try {
    isOpen.value = await window.go.main.App.IsFileOpen()
  } catch {
    isOpen.value = false
  }
}

async function loadInitialFile() {
  try {
    const f = await window.go.main.App.GetInitialFile()
    if (f && !isOpen.value) {
      initialFile.value = f
    }
  } catch {
    // ignore
  }
}

function handleFileOpened() {
  isOpen.value = true
}

async function handleFileClosed() {
  isOpen.value = false
}

onMounted(async () => {
  await checkFileOpen()
  await loadInitialFile()
  EventsOn('file:opened', handleFileOpened)
  EventsOn('file:closed', handleFileClosed)
})

onUnmounted(() => {
  EventsOff('file:opened')
  EventsOff('file:closed')
})
</script>

<template>
  <div class="h-screen w-screen bg-cream text-black">
    <UnlockView v-if="!isOpen" :initial-file="initialFile" @file-opened="handleFileOpened" />
    <AppShell v-else @file-closed="handleFileClosed" />
  </div>
</template>
