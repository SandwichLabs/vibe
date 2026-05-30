// useAppMode — the shared Studio/App mode flag (the Access dual-mode homage).
//
// Vibe is "one app, two modes": Studio Mode (builders — schema, SQL, editors)
// and App Mode (data-entry workers — a locked-down runtime). This is a runtime
// toggle inside the Studio app, not a separate binary (see CLAUDE.md).
//
// For P3.10 this single source of truth gates the Report View's "Edit SQL"
// affordance to Studio Mode only (plan §3.4 / mockup #11: "Edit SQL
// Studio-only"). It is intentionally minimal — a module-level reactive ref —
// so other surfaces can adopt it as the dual-mode work lands in Phase 4.
import { ref, computed, type Ref, type ComputedRef } from 'vue'

export type AppMode = 'studio' | 'app'

// Default to Studio Mode: the Studio shell opens in builder mode.
const mode: Ref<AppMode> = ref('studio')

const isStudio: ComputedRef<boolean> = computed(() => mode.value === 'studio')
const isAppMode: ComputedRef<boolean> = computed(() => mode.value === 'app')

export function useAppMode() {
  return {
    mode,
    isStudio,
    isAppMode,
    setMode(next: AppMode) {
      mode.value = next
    },
  }
}
