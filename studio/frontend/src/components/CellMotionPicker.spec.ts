// CellMotionPicker — writes a sql cell's closed motion object, storing the
// default (spring, 1x, cascade) as no motion at all.
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import CellMotionPicker from './CellMotionPicker.vue'
import type { MotionT } from '../a2ui/motion'

function picker(modelValue: MotionT | null) {
  const updates: (MotionT | null)[] = []
  const w = mount(CellMotionPicker, { props: { modelValue, 'onUpdate:modelValue': (v: MotionT | null) => updates.push(v) } })
  return { w, updates }
}

describe('CellMotionPicker', () => {
  it('picks an easing and a speed, and collapses back to null at the defaults', async () => {
    const { w, updates } = picker(null)
    await w.find('[data-testid="motion-easing"]').setValue('expo')
    expect(updates.at(-1)).toEqual({ easing: 'expo' })
    await w.setProps({ modelValue: { easing: 'expo' } })
    await w.find('[data-testid="motion-speed"]').setValue('2')
    expect(updates.at(-1)).toEqual({ easing: 'expo', speed: 2 })
    await w.setProps({ modelValue: { speed: 2 } })
    await w.find('[data-testid="motion-speed"]').setValue('1')
    expect(updates.at(-1)).toBeNull()
  })
  it('shows spring tuning only for the custom spring, and drops it when leaving custom', async () => {
    const { w, updates } = picker({ easing: 'custom', stiffness: 300 })
    expect(w.find('[data-testid="motion-stiffness"]').exists()).toBe(true)
    await w.find('[data-testid="motion-damping"]').setValue('20')
    expect(updates.at(-1)).toEqual({ easing: 'custom', stiffness: 300, damping: 20 })
    await w.find('[data-testid="motion-easing"]').setValue('cinema')
    expect(updates.at(-1)).toEqual({ easing: 'cinema' })
  })
  it('hides speed and cascade when animation is off', () => {
    const { w } = picker({ easing: 'none' })
    expect(w.find('[data-testid="motion-speed"]').exists()).toBe(false)
    expect(w.find('[data-testid="motion-stagger"]').exists()).toBe(false)
  })
})
