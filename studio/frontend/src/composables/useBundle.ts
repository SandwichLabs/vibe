// useBundle — shared state for the .vibe bundle dialogs (#118). Visibility is
// global, like the other workspace dialogs, so the workspace menu, the launcher
// and a .vibe file passed on the command line can all open them.
import { ref } from 'vue'

// Export workspace content… (needs an open workspace).
export const bundleExportOpen = ref(false)

interface BundleImportRequest {
  // 'here' imports into the open workspace; 'new' creates one (the launcher).
  mode: 'here' | 'new'
  // A file already chosen (a .vibe passed on the command line); '' asks.
  path: string
}

export const bundleImportRequest = ref<BundleImportRequest | null>(null)

export function openBundleExport(): void {
  bundleExportOpen.value = true
}

export function openBundleImport(mode: 'here' | 'new', path = ''): void {
  bundleImportRequest.value = { mode, path }
}

export function closeBundleImport(): void {
  bundleImportRequest.value = null
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}
