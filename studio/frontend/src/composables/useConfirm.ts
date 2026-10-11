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

interface ConfirmOptions {
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
export const confirmAltLabel = ref('')
export const confirmAltDestructive = ref(false)

type ConfirmResult = 'confirm' | 'alt' | 'cancel'

let pendingResolve: ((result: ConfirmResult) => void) | null = null

// openConfirm is the one place that actually populates the shared dialog
// state and returns a Promise<ConfirmResult> — askConfirm is a thin
// wrapper over it. A second call while one is already pending resolves the first as 'cancel' before
// showing the new one, rather than leaving it to hang forever unresolved.
function openConfirm(opts: ConfirmOptions, altLabel: string, altDestructive: boolean): Promise<ConfirmResult> {
  if (pendingResolve) {
    resolveConfirmChoice('cancel')
  }
  confirmTitle.value = opts.title ?? 'Confirm'
  confirmMessage.value = opts.message
  confirmVariant.value = opts.variant ?? 'destructive'
  confirmLabel.value = opts.confirmLabel ?? 'Confirm'
  confirmAltLabel.value = altLabel
  confirmAltDestructive.value = altDestructive
  confirmVisible.value = true
  return new Promise((resolve) => {
    pendingResolve = resolve
  })
}

// askConfirm is the two-button case: no altLabel (ConfirmDialog renders no
// third button when it's unset), resolves true/false.
export function askConfirm(opts: ConfirmOptions): Promise<boolean> {
  return openConfirm(opts, '', false).then((result) => result === 'confirm')
}

// resolveConfirmChoice is called by App.vue's ConfirmDialog
// @confirm/@alt/@cancel handlers.
export function resolveConfirmChoice(result: ConfirmResult): void {
  confirmVisible.value = false
  const resolve = pendingResolve
  pendingResolve = null
  resolve?.(result)
}

// resolveConfirm is the legacy true/false entry point — kept so existing
// @confirm="resolveConfirm(true)" / @cancel="resolveConfirm(false)" callers
// keep working unchanged.
export function resolveConfirm(confirmed: boolean): void {
  resolveConfirmChoice(confirmed ? 'confirm' : 'cancel')
}
