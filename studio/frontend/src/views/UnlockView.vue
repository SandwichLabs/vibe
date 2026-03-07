<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{ initialFile?: string }>()
const emit = defineEmits<{ 'file-opened': [] }>()

const mode = ref<'choose' | 'open' | 'create'>('choose')
const filePath = ref('')
const passphrase = ref('')
const passphraseConfirm = ref('')
const title = ref('')
const description = ref('')
const error = ref('')
const loading = ref(false)

// If initialFile is provided (e.g. from CLI arg), skip to open mode.
watch(() => props.initialFile, (val) => {
  if (val) {
    filePath.value = val
    mode.value = 'open'
  }
}, { immediate: true })

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
    <div class="card w-full max-w-md p-8">
      <h1 class="text-2xl font-bold text-center mb-6">Vibe Studio</h1>

      <!-- File chooser -->
      <div v-if="mode === 'choose'" class="space-y-4">
        <button @click="selectFile" class="btn btn-primary w-full text-center">
          Open File
        </button>
        <button @click="selectNewFile" class="btn w-full text-center">
          Create New
        </button>
      </div>

      <!-- Open file form -->
      <div v-if="mode === 'open'" class="space-y-4">
        <div class="text-sm font-mono text-muted truncate">{{ filePath }}</div>
        <div>
          <label class="field-label">Passphrase</label>
          <input
            v-model="passphrase"
            type="password"
            placeholder="Enter passphrase"
            class="field-input"
            @keyup.enter="openFile"
            autofocus
          />
        </div>
        <div class="flex gap-3">
          <button @click="goBack" class="btn flex-1 text-center">Back</button>
          <button @click="openFile" :disabled="loading" class="btn btn-primary flex-1 text-center">
            {{ loading ? 'Opening...' : 'Open' }}
          </button>
        </div>
      </div>

      <!-- Create file form -->
      <div v-if="mode === 'create'" class="space-y-4">
        <div class="text-sm font-mono text-muted truncate">{{ filePath }}</div>
        <div>
          <label class="field-label">Title</label>
          <input v-model="title" type="text" placeholder="Title (optional)" class="field-input" />
        </div>
        <div>
          <label class="field-label">Description</label>
          <input v-model="description" type="text" placeholder="Description (optional)" class="field-input" />
        </div>
        <div>
          <label class="field-label">Passphrase</label>
          <input v-model="passphrase" type="password" placeholder="Set passphrase" class="field-input" />
        </div>
        <div>
          <label class="field-label">Confirm Passphrase</label>
          <input
            v-model="passphraseConfirm"
            type="password"
            placeholder="Confirm passphrase"
            class="field-input"
            @keyup.enter="createFile"
          />
        </div>
        <div class="flex gap-3">
          <button @click="goBack" class="btn flex-1 text-center">Back</button>
          <button @click="createFile" :disabled="loading" class="btn btn-primary flex-1 text-center">
            {{ loading ? 'Creating...' : 'Create' }}
          </button>
        </div>
      </div>

      <div v-if="error" class="mt-4 p-3 bg-error/10 border-3 border-error text-error text-sm font-semibold">
        {{ error }}
      </div>
    </div>
  </div>
</template>
