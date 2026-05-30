<script setup lang="ts">
import { ref, watch } from 'vue'
import Icon from '../components/Icon.vue'

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
  <div class="row" style="justify-content: center; height: 100%; background: var(--sh-paper);">
    <div class="card card-pad" style="width: 100%; max-width: 28rem; box-shadow: var(--sh-shadow-lg);">
      <div class="col" style="align-items: center; text-align: center; gap: var(--sh-space-1); margin-bottom: var(--sh-space-5);">
        <span style="width: 48px; height: 48px; background: var(--sh-teal); color: var(--sh-paper); border: 2px solid var(--sh-ink); border-radius: var(--sh-radius-sm); box-shadow: 3px 3px 0 0 var(--sh-ink); font-family: var(--sh-font-display); font-weight: 700; font-size: 26px; display: inline-flex; align-items: center; justify-content: center;">V</span>
        <h1 style="font-family: var(--sh-font-display); font-weight: 600; font-size: 32px; letter-spacing: -0.015em; margin-top: 14px;">
          Vibe <span style="border-bottom: 3px solid var(--sh-teal); padding-bottom: 1px;">Studio</span>
        </h1>
        <p class="sh-meta" style="margin-top: 6px;">Open an encrypted .vibe file to begin.</p>
      </div>

      <!-- File chooser -->
      <div v-if="mode === 'choose'" class="col" style="gap: var(--sh-space-3);">
        <button @click="selectFile" class="btn btn-primary" style="width: 100%; justify-content: center;">
          <Icon name="lock" :size="16" /> Open file
        </button>
        <button @click="selectNewFile" class="btn" style="width: 100%; justify-content: center;">
          <Icon name="plus" :size="16" /> Create new
        </button>
      </div>

      <!-- Open file form -->
      <div v-if="mode === 'open'" class="col" style="gap: var(--sh-space-4);">
        <div class="url-tile" style="font-size: 13px;">{{ filePath }}</div>
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
        <div class="row" style="gap: var(--sh-space-3);">
          <button @click="goBack" class="btn" style="flex: 1; justify-content: center;">Back</button>
          <button @click="openFile" :disabled="loading" class="btn btn-primary" style="flex: 1; justify-content: center;">
            {{ loading ? 'Opening…' : 'Open' }}
          </button>
        </div>
      </div>

      <!-- Create file form -->
      <div v-if="mode === 'create'" class="col" style="gap: var(--sh-space-4);">
        <div class="url-tile" style="font-size: 13px;">{{ filePath }}</div>
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
        <div class="row" style="gap: var(--sh-space-3);">
          <button @click="goBack" class="btn" style="flex: 1; justify-content: center;">Back</button>
          <button @click="createFile" :disabled="loading" class="btn btn-primary" style="flex: 1; justify-content: center;">
            {{ loading ? 'Creating…' : 'Create' }}
          </button>
        </div>
      </div>

      <div v-if="error" class="banner attn" style="margin-top: var(--sh-space-4);">
        <div class="ico"><Icon name="alert" :size="22" /></div>
        <div class="body"><div class="sub" style="color: var(--sh-ink);">{{ error }}</div></div>
      </div>
    </div>
  </div>
</template>
