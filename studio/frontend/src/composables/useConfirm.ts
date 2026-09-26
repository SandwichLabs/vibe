// useConfirm — a promise-based confirm() built on the project's own
// ConfirmDialog.vue, so every "are you sure?" in the app (including
// non-component call sites like lib/markdown.ts's link-click handler) goes
// through the same SchoolHouse-styled dialog. Project convention forbids
// native window.confirm/alert (ConfirmDialog.vue's own header comment: "all
// window.confirm() calls" are replaced by it) — and WebKitGTK, the engine
// Studio's Linux build runs on, is documented to sometimes not show the
// native confirm dialog at all, which would make a window.confirm-gated
// action silently un-confirmable.
//
// State lives in this module (a singleton), not a Pinia store — the app has
// no store; a top-level ref shared via a plain export is the pattern used
// throughout the composables directory. ONE <ConfirmDialog> bound to this
// state is rendered once in App.vue, so every askConfirm() caller shares it
// rather than each mounting its own.
import { ref } from 'vue'

export interface ConfirmOptions {
  title?: string
  message: string
  // See ConfirmDialog.vue's own prop doc: 'destructive' (default) is the
  // red delete-style button; 'primary' is the ordinary teal action style.
  variant?: 'destructive' | 'primary'
  // Button label naming the action ("Delete"). Defaults to "Confirm".
  confirmLabel?: string
}

export const confirmVisible = ref(false)
export const confirmTitle = ref('')
export const confirmMessage = ref('')
export const confirmVariant = ref<'destructive' | 'primary'>('destructive')
export const confirmLabel = ref('Confirm')

let pendingResolve: ((confirmed: boolean) => void) | null = null

// askConfirm shows the shared ConfirmDialog and resolves true/false when
// the user picks Confirm/Cancel (or dismisses via the overlay, or presses
// Escape — both of which ConfirmDialog treats as cancel). Only one confirm
// can be pending at a time; a second call while one is showing resolves the
// first as false (cancelled) before showing the new one, rather than
// leaving it to hang forever unresolved.
export function askConfirm(opts: ConfirmOptions): Promise<boolean> {
  if (pendingResolve) {
    resolveConfirm(false)
  }
  confirmTitle.value = opts.title ?? 'Confirm'
  confirmMessage.value = opts.message
  confirmVariant.value = opts.variant ?? 'destructive'
  confirmLabel.value = opts.confirmLabel ?? 'Confirm'
  confirmVisible.value = true
  return new Promise((resolve) => {
    pendingResolve = resolve
  })
}

// resolveConfirm is called by App.vue's ConfirmDialog @confirm/@cancel
// handlers.
export function resolveConfirm(confirmed: boolean): void {
  confirmVisible.value = false
  const resolve = pendingResolve
  pendingResolve = null
  resolve?.(confirmed)
}
