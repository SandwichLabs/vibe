// useMotion — animates a list of numbers toward their targets with the chart's
// motion preset (the KINETIC library's useAnimatedValues, as a composable).
// On mount and on replay() it sweeps up from zero; when the targets change
// (a live query re-ran, a stream ticked) it glides from where it is. With
// animation off (reduced motion, easing "none", no browser) the values are
// simply the targets.
import { ref, watch, onBeforeUnmount, type Ref } from 'vue'
import { animationsEnabled, frameAt, resolveMotion, totalMs, type MotionT } from './motion'

export function useMotion(targets: () => number[], motion: () => MotionT | null | undefined) {
  const values: Ref<number[]> = ref(targets().slice())
  const enabled = ref(animationsEnabled(resolveMotion(motion())))
  let frame = 0

  function stop() {
    if (frame) cancelAnimationFrame(frame)
    frame = 0
  }

  function run(from: number[]) {
    stop()
    const m = resolveMotion(motion())
    const to = targets().slice()
    enabled.value = animationsEnabled(m)
    if (!enabled.value) {
      values.value = to
      return
    }
    const total = totalMs(to.length, m)
    const start = performance.now()
    const tick = (now: number) => {
      const elapsed = now - start
      if (elapsed >= total) {
        values.value = to
        frame = 0
        return
      }
      values.value = frameAt(from, to, elapsed, m)
      frame = requestAnimationFrame(tick)
    }
    values.value = from.slice()
    frame = requestAnimationFrame(tick)
  }

  function replay() {
    run(targets().map(() => 0))
  }

  watch(
    () => JSON.stringify(targets()),
    () => {
      const to = targets()
      // Glide from the current frame; values with no current position start at 0.
      run(to.map((_, i) => values.value[i] ?? 0))
    },
  )
  watch(() => JSON.stringify(motion() ?? null), replay)

  replay()
  onBeforeUnmount(stop)

  return { values, replay, enabled }
}
