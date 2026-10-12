/**
 * useBundleImport — the Import .vibe dialog's state and actions (F10): pick and
 * preview a bundle (verified before anything is written), the passphrase step,
 * the per-item Rename / Overwrite / Skip decisions, optional new-workspace
 * fields, and the commit. BundleImportDialog only renders it.
 */
import { ref, computed, watch, nextTick, type Ref } from 'vue'
import { App } from '../lib/bindings'
import { bundleImportRequest, closeBundleImport } from './useBundle'
import { workspaceInfo, flushOrConfirm } from './useWorkspace'
import { useNewWorkspaceFields } from './useNewWorkspaceFields'
import { errorText } from '../lib/errors'

export const KIND_LABEL: Record<string, string> = { notebook: 'Notebook', query: 'Query', skill: 'Skill' }

export function useBundleImport(passEl: Ref<HTMLInputElement | null>) {
  const open = computed(() => bundleImportRequest.value !== null)
  const mode = computed(() => bundleImportRequest.value?.mode ?? 'here')

  const stage = ref<'picking' | 'passphrase' | 'preview' | 'done'>('picking')
  const path = ref('')
  const passphrase = ref('')
  const preview = ref<BundlePreview | null>(null)
  const busy = ref(false)
  const error = ref('')
  const result = ref<BundleResult | null>(null)

  // Decisions.
  const conflictChoice = ref<Record<string, BundleAction>>({})
  const included = ref<Record<string, boolean>>({})
  const connIncluded = ref<Record<string, boolean>>({})

  // New-workspace fields (launcher flow).
  const wsName = ref('')
  const { parentDir, git, gitAvailable, slug, reset: resetNewWorkspace, remember, choose: chooseFolder } = useNewWorkspaceFields(wsName)

  async function discard() {
    const h = preview.value?.handle
    if (h) { try { await App.DiscardBundleImport(h) } catch { /* expired */ } }
  }

  function close() {
    if (busy.value) return
    void discard()
    preview.value = null
    passphrase.value = ''
    closeBundleImport()
  }

  watch(bundleImportRequest, async (req) => {
    if (!req) return
    void discard() // a previous preview's handle, if a new request replaced it
    stage.value = 'picking'
    path.value = req.path
    passphrase.value = ''
    preview.value = null
    result.value = null
    error.value = ''
    busy.value = false
    conflictChoice.value = {}
    included.value = {}
    connIncluded.value = {}
    if (req.mode === 'new') await resetNewWorkspace()
    if (!path.value) {
      try {
        path.value = await App.SelectBundleFile()
      } catch (e: unknown) {
        error.value = errorText(e)
      }
      if (!path.value && !error.value) { closeBundleImport(); return }
    }
    if (path.value) await load()
  })

  async function load() {
    busy.value = true
    error.value = ''
    try {
      const pv = await App.PreviewBundleImport(path.value, passphrase.value)
      if (pv.needsPassphrase) {
        stage.value = 'passphrase'
        if (pv.wrongPassphrase) error.value = "That passphrase doesn't open this bundle. Check it and try again."
        passphrase.value = ''
      } else {
        preview.value = pv
        stage.value = 'preview'
        passphrase.value = ''
        for (const it of pv.items) {
          if (it.conflict) conflictChoice.value[it.key] = 'rename'
          included.value[it.key] = true
        }
        for (const c of pv.connections) connIncluded.value[c.name] = false
        if (!wsName.value || mode.value === 'new') wsName.value = pv.workspaceName
      }
    } catch (e: unknown) {
      error.value = errorText(e)
      stage.value = 'preview'
      preview.value = null
    }
    busy.value = false
    // The field is enabled again only now, so focus it now.
    if (stage.value === 'passphrase') { await nextTick(); passEl.value?.focus() }
  }

  const conflicts = computed(() => (preview.value?.items ?? []).filter((i) => i.conflict))
  const chosenCount = computed(() => (preview.value?.items ?? []).filter((i) => included.value[i.key] && !(i.conflict && conflictChoice.value[i.key] === 'skip')).length
    + Object.values(connIncluded.value).filter(Boolean).length)

  function setAllConflicts(a: BundleAction) {
    for (const i of conflicts.value) conflictChoice.value[i.key] = a
  }

  const validNew = computed(() => mode.value === 'here' || (wsName.value.trim() !== '' && parentDir.value.trim() !== ''))
  const canImport = computed(() => !!preview.value?.handle && chosenCount.value > 0 && validNew.value && !busy.value)

  function decisions(): BundleDecisions {
    const items: Record<string, BundleAction> = {}
    for (const it of preview.value?.items ?? []) {
      if (!included.value[it.key]) items[it.key] = 'skip'
      else if (it.conflict) items[it.key] = conflictChoice.value[it.key] ?? 'rename'
    }
    return { onConflict: 'rename', items, connections: Object.keys(connIncluded.value).filter((n) => connIncluded.value[n]) }
  }

  const choose = () => chooseFolder((m) => { error.value = m })

  async function submit() {
    if (!canImport.value || !preview.value) return
    busy.value = true
    error.value = ''
    try {
      if (mode.value === 'new') {
        const out = await App.ImportBundleIntoNewWorkspace(preview.value.handle, {
          name: wsName.value.trim(), dir: '', parentDir: parentDir.value.trim(),
          git: git.value && gitAvailable.value, template: '', inPlace: false,
        }, decisions())
        if (out.opened.info) workspaceInfo.value = out.opened.info
        remember()
        result.value = out.result
      } else {
        if (!(await flushOrConfirm('Import'))) { busy.value = false; return }
        result.value = await App.CommitBundleImport(preview.value.handle, decisions())
      }
      preview.value = { ...preview.value, handle: '' }
      stage.value = 'done'
    } catch (e: unknown) {
      error.value = errorText(e)
    }
    busy.value = false
  }

  const failed = computed(() => (result.value?.outcomes ?? []).filter((o) => o.error))
  const summary = computed(() => {
    const n: Record<string, number> = {}
    for (const o of result.value?.outcomes ?? []) if (!o.error) n[o.result] = (n[o.result] ?? 0) + 1
    return n
  })
  const summaryText = computed(() => {
    const parts = Object.entries(summary.value).map(([k, n]) => `${n} ${k}`)
    return parts.length ? parts.join(', ') + '.' : 'Nothing was changed.'
  })
  function when(iso: string): string {
    const d = new Date(iso)
    return isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
  }

  return {
    open, mode, stage, passphrase, preview, busy, error, result, conflictChoice, included, connIncluded, wsName, close, load, conflicts, chosenCount, setAllConflicts, canImport, choose, submit, failed, summaryText, when,
    parentDir, git, gitAvailable, slug,
  }
}
