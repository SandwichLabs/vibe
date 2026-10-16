<script setup lang="ts">
// S3PresetPicker — the "Store" select above an s3 connection's endpoint (#130).
// Picking fills Endpoint, URL style and TLS (and a region hint) and opens
// Advanced; nothing about the pick is saved. The fill itself is
// lib/storagePresets.presetFill. The select's own value is only what was last
// picked, so it starts empty on every load.
import { ref } from 'vue'

const props = defineProps<{ presets: ConnPreset[] }>()
const emit = defineEmits<{ pick: [preset: ConnPreset] }>()
const picked = ref('')

function onChange(e: Event) {
  const id = (e.target as HTMLSelectElement).value
  picked.value = id
  const p = props.presets.find((x) => x.id === id)
  if (p) emit('pick', p)
}
</script>

<template>
  <div class="field">
    <label class="field-label" for="f-preset">Store</label>
    <select id="f-preset" class="field-input" :value="picked" aria-describedby="f-preset-h" data-testid="f-preset" @change="onChange">
      <option value="">Choose a store to fill the settings…</option>
      <option v-for="p in presets" :key="p.id" :value="p.id" :data-testid="`preset-${p.id}`">{{ p.label }}</option>
    </select>
    <div id="f-preset-h" class="help">
      <template v-if="picked">{{ presets.find((p) => p.id === picked)?.help }}</template>
      <template v-else>Fills the endpoint, URL style and TLS for a common S3-compatible store. You can still change them.</template>
    </div>
  </div>
</template>
