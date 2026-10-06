<script setup lang="ts">
// CredentialField — the write-only presentation of a secret (SPEC 06):
//   set:      dots + "In your keychain" + Replace + Clear. No reveal, no copy.
//   entering: a password input; what is typed is sent once and never read back.
// The dots are decoration and carry aria-label="Saved, hidden".
import { ref, computed, watch } from 'vue'
import Icon from './Icon.vue'

const props = defineProps<{
  id: string
  label: string
  isSet: boolean
  where?: string // "In your keychain" | "For this session"
  help?: string
  modelValue: string
  required?: boolean
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [v: string]; clear: []; replacing: [v: boolean] }>()

const replacing = ref(false)
const showEntry = computed(() => !props.isSet || replacing.value)
watch(() => props.isSet, (s) => { if (!s) replacing.value = false })

function replace() {
  replacing.value = true
  emit('replacing', true)
}
function cancelReplace() {
  replacing.value = false
  emit('update:modelValue', '')
  emit('replacing', false)
}
</script>

<template>
  <div class="field" :data-testid="`cred-${id}`">
    <label class="field-label" :for="`${id}-in`" :id="`${id}-lab`">{{ label }}<span v-if="required" aria-hidden="true"> *</span></label>
    <div v-if="!showEntry" class="cred" role="group" :aria-labelledby="`${id}-lab`">
      <span class="dots" role="img" aria-label="Saved, hidden">••••••••</span>
      <span class="where"><Icon name="circleCheck" :size="14" /> {{ where || 'In your keychain' }}</span>
      <span class="acts">
        <button :id="`${id}-in`" type="button" class="linkbtn" :disabled="disabled" data-testid="cred-replace" @click="replace">Replace</button>
        <span class="sepdot" aria-hidden="true">·</span>
        <button type="button" class="linkbtn danger" :disabled="disabled" data-testid="cred-clear" @click="emit('clear')">Clear</button>
      </span>
    </div>
    <template v-else>
      <input
        :id="`${id}-in`"
        class="field-input"
        type="password"
        autocomplete="new-password"
        :value="modelValue"
        :disabled="disabled"
        :aria-describedby="`${id}-h`"
        :data-testid="`cred-input-${id}`"
        @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      />
      <div :id="`${id}-h`" class="help">
        Saved to your keychain when you press Save. Vibe can't show it again after you save. You can only replace or clear it.
        <button v-if="isSet" type="button" class="linkbtn" @click="cancelReplace">Keep the saved one</button>
      </div>
    </template>
    <div v-if="help && showEntry" class="help">{{ help }}</div>
  </div>
</template>
