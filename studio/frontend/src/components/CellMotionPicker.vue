<script setup lang="ts">
// CellMotionPicker — a notebook sql cell's chart animation: an easing preset,
// a speed, stagger on/off, and (for the custom spring) stiffness, damping and
// mass. Writes the same closed "motion" object an a2ui chart takes; the default
// (spring, 1x, staggered) is stored as no motion at all.
import { computed } from 'vue'
import { EASINGS, EASING_LABELS, resolveMotion, type MotionT } from '../a2ui/motion'

const model = defineModel<MotionT | null>({ required: true })
const m = computed(() => resolveMotion(model.value))
const SPEEDS = [0.5, 1, 1.5, 2]

function set(patch: MotionT) {
  const next: MotionT = { ...(model.value ?? {}), ...patch }
  if (next.easing !== 'custom') {
    delete next.stiffness
    delete next.damping
    delete next.mass
  }
  if (next.easing === 'spring') delete next.easing
  if (next.speed === 1) delete next.speed
  if (next.stagger === true) delete next.stagger
  model.value = Object.keys(next).length ? next : null
}
const SPRING = [
  { key: 'stiffness', label: 'Stiffness', min: 10, max: 500, step: 10 },
  { key: 'damping', label: 'Damping', min: 1, max: 100, step: 1 },
  { key: 'mass', label: 'Mass', min: 0.1, max: 10, step: 0.1 },
] as const
</script>

<template>
  <div class="row sh-mono" style="gap: var(--sh-space-2); align-items: center; flex-wrap: wrap; font-size: var(--sh-fs-xs); color: var(--sh-graphite);" data-testid="motion-picker">
    <span>MOTION</span>
    <select :value="m.easing" aria-label="Chart animation" data-testid="motion-easing" @change="set({ easing: ($event.target as HTMLSelectElement).value as MotionT['easing'] })">
      <option v-for="e in EASINGS" :key="e" :value="e">{{ EASING_LABELS[e] }}</option>
    </select>
    <template v-if="m.easing !== 'none'">
      <select :value="m.speed" aria-label="Animation speed" data-testid="motion-speed" @change="set({ speed: Number(($event.target as HTMLSelectElement).value) })">
        <option v-for="s in SPEEDS" :key="s" :value="s">{{ s }}×</option>
      </select>
      <label style="display: inline-flex; gap: 4px; align-items: center;">
        <input type="checkbox" :checked="m.stagger" data-testid="motion-stagger" @change="set({ stagger: ($event.target as HTMLInputElement).checked })" /> cascade
      </label>
    </template>
    <template v-if="m.easing === 'custom'">
      <label v-for="s in SPRING" :key="s.key" style="display: inline-flex; gap: 4px; align-items: center;">
        {{ s.label }}
        <input
          type="range" :min="s.min" :max="s.max" :step="s.step" :value="m[s.key]" :aria-label="s.label"
          :data-testid="`motion-${s.key}`" style="width: 72px;"
          @change="set({ [s.key]: Number(($event.target as HTMLInputElement).value) })"
        />
        <span style="min-width: 2.5em;">{{ m[s.key] }}</span>
      </label>
    </template>
  </div>
</template>
