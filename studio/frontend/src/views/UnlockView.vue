<script setup lang="ts">
import { ref } from 'vue'

const emit = defineEmits<{ 'file-opened': [] }>()

const mode = ref<'choose' | 'open' | 'create'>('choose')
const filePath = ref('')
const passphrase = ref('')
const passphraseConfirm = ref('')
const title = ref('')
const description = ref('')
const error = ref('')
const loading = ref(false)

async function selectFile() {
  try {
    const path = await window.go.main.App.SelectVibeFile()
    if (path) {
      filePath.value = path
      mode.value = 'open'
    }
  } catch (e: any) {
    error.value = e.message || String(e)
  }
}

async function selectNewFile() {
  try {
    const path = await window.go.main.App.SelectNewVibeFile()
    if (path) {
      filePath.value = path
      mode.value = 'create'
    }
  } catch (e: any) {
    error.value = e.message || String(e)
  }
}

async function openFile() {
  if (!passphrase.value) {
    error.value = 'Passphrase is required'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await window.go.main.App.OpenFile(filePath.value, passphrase.value)
    emit('file-opened')
  } catch (e: any) {
    error.value = e.message || String(e)
  } finally {
    loading.value = false
  }
}

async function createFile() {
  if (!passphrase.value) {
    error.value = 'Passphrase is required'
    return
  }
  if (passphrase.value !== passphraseConfirm.value) {
    error.value = 'Passphrases do not match'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await window.go.main.App.CreateFile(filePath.value, passphrase.value, title.value, description.value)
    emit('file-opened')
  } catch (e: any) {
    error.value = e.message || String(e)
  } finally {
    loading.value = false
  }
}

function goBack() {
  mode.value = 'choose'
  passphrase.value = ''
  passphraseConfirm.value = ''
  error.value = ''
}
</script>

<template>
  <div class="flex items-center justify-center h-full">
    <div class="bg-white rounded-xl shadow-lg p-8 w-full max-w-md">
      <h1 class="text-2xl font-bold text-center mb-6">Vibe Studio</h1>

      <!-- File chooser -->
      <div v-if="mode === 'choose'" class="space-y-4">
        <button @click="selectFile" class="w-full py-3 px-4 bg-blue-600 text-white rounded-lg hover:bg-blue-700 font-medium">
          Open File
        </button>
        <button @click="selectNewFile" class="w-full py-3 px-4 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300 font-medium">
          Create New
        </button>
      </div>

      <!-- Open file form -->
      <div v-if="mode === 'open'" class="space-y-4">
        <div class="text-sm text-gray-500 truncate">{{ filePath }}</div>
        <input
          v-model="passphrase"
          type="password"
          placeholder="Enter passphrase"
          class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
          @keyup.enter="openFile"
          autofocus
        />
        <div class="flex gap-3">
          <button @click="goBack" class="flex-1 py-2 px-4 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300">
            Back
          </button>
          <button @click="openFile" :disabled="loading" class="flex-1 py-2 px-4 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50">
            {{ loading ? 'Opening...' : 'Open' }}
          </button>
        </div>
      </div>

      <!-- Create file form -->
      <div v-if="mode === 'create'" class="space-y-4">
        <div class="text-sm text-gray-500 truncate">{{ filePath }}</div>
        <input
          v-model="title"
          type="text"
          placeholder="Title (optional)"
          class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <input
          v-model="description"
          type="text"
          placeholder="Description (optional)"
          class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <input
          v-model="passphrase"
          type="password"
          placeholder="Set passphrase"
          class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <input
          v-model="passphraseConfirm"
          type="password"
          placeholder="Confirm passphrase"
          class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
          @keyup.enter="createFile"
        />
        <div class="flex gap-3">
          <button @click="goBack" class="flex-1 py-2 px-4 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300">
            Back
          </button>
          <button @click="createFile" :disabled="loading" class="flex-1 py-2 px-4 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50">
            {{ loading ? 'Creating...' : 'Create' }}
          </button>
        </div>
      </div>

      <div v-if="error" class="mt-4 p-3 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">
        {{ error }}
      </div>
    </div>
  </div>
</template>
