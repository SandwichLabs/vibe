/**
 * useArtifactSource — the Source tab's state and actions for one data artifact
 * (#162, step 6a): the two files as text (artifact.yaml and its view file, view.a2ui.json or view.js), a
 * save that sends the one revision over both, the warnings the agent policy
 * gives a saved query, delete, and the changed-on-disk conflict flow
 * (useConflict + ResolveConflict kind "artifact").
 *
 * What is edited is the file text, as on disk, so a broken artifact (which the
 * view cannot render) can be opened and fixed here. A structural error refuses
 * the save and says why; a policy objection saves and is listed as a warning.
 */
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { App } from '../lib/bindings'
import { errorText, isConflict, isDeletedConflict } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { useRouter } from 'vue-router'
import { askConfirm } from './useConfirm'
import { workspaceInfo } from './useWorkspace'
import { register as registerPendingSave } from './usePendingSaves'
import { useConflict } from './useConflict'

interface Hooks {
  /** A save or resolution changed the files: the view re-reads the artifact. */
  saved: () => void
  /** The artifact is gone (deleted here, or "Close it" after an outside delete). */
  gone: () => void
}

export function useArtifactSource(name: () => string, hooks: Hooks) {
  const manifest = ref('')
  const view = ref('')
  // The view file's name: view.a2ui.json, or view.js for a custom (script) view.
  const viewFile = ref('view.a2ui.json')
  // The text last read from or written to disk: what "unsaved" is measured against.
  const base = ref({ manifest: '', view: '' })
  const rev = ref('')
  const loading = ref(true)
  const loadError = ref('')
  const saving = ref(false)
  const saveError = ref('')
  const notice = ref('')
  const warnings = ref<string[]>([])
  const dirty = computed(() => manifest.value !== base.value.manifest || view.value !== base.value.view)

  // Leaving with unsaved text (the back arrow, the sidebar, another artifact) asks first.
  const router = useRouter() as ReturnType<typeof useRouter> | undefined
  // A workspace switch already asked (flushOrConfirm) and is about to drop this tab, so
  // once the workspace is not the one this tab was opened in, the guard lets go.
  const openedIn = workspaceInfo.value?.sessionId
  const stopGuard = router?.beforeEach(async (to, from) => {
    if (workspaceInfo.value?.sessionId !== openedIn) return true
    if (!dirty.value || to.fullPath === from.fullPath) return true
    return askConfirm({
      title: 'Discard unsaved changes?',
      message: `Your edits to "${name()}" are not saved. Leaving drops them.`,
      variant: 'destructive',
      confirmLabel: 'Discard',
    })
  })

  const cf = useConflict({
    currentId: name,
    fetchRev: async (id) => (await App.GetArtifactSource(id)).rev,
  })

  // A refusal belongs to the text that was refused: typing past it clears it.
  watch([manifest, view], () => { saveError.value = '' })

  function adopt(src: ArtifactSource) {
    warnings.value = [] // they described other text
    manifest.value = src.manifest
    view.value = src.view
    viewFile.value = src.view_file || 'view.a2ui.json'
    base.value = { manifest: src.manifest, view: src.view }
    rev.value = src.rev
  }

  // load reads the files from disk into the editors, dropping unsaved text.
  async function load() {
    const n = name()
    try {
      const src = await App.GetArtifactSource(n)
      if (n !== name()) return
      adopt(src)
      loadError.value = ''
      if (checked) void refreshWarnings() // the disk text changed under a check already shown
    } catch (e) {
      if (n === name()) loadError.value = errorText(e)
    } finally {
      loading.value = false
    }
  }

  async function refreshWarnings() {
    try { warnings.value = (await App.CheckArtifact(name())) ?? [] } catch { /* the save itself reported what matters */ }
  }

  // checked: the policy check of what is on disk runs once, when the tab is first
  // shown (it is the view's own queries again, so it waits until asked for).
  let checked = false
  async function checkOnce() {
    if (checked) return
    checked = true
    await refreshWarnings()
  }

  async function save() {
    if (saving.value || cf.conflict.value) return
    saving.value = true
    saveError.value = ''
    notice.value = ''
    try {
      const res = await App.SaveArtifact(name(), manifest.value, view.value, rev.value)
      base.value = { manifest: manifest.value, view: view.value }
      rev.value = res.rev
      warnings.value = res.warnings ?? []
      hooks.saved()
    } catch (e) {
      warnings.value = [] // they described the version before this refused save
      if (isConflict(e)) cf.raise(isDeletedConflict(e))
      else saveError.value = errorText(e)
    } finally {
      saving.value = false
    }
  }

  // Registered with usePendingSaves so a workspace switch or quit saves an open
  // edit first; a conflict or a refusal makes it fail so the caller can ask.
  async function flushPending() {
    if (!dirty.value) return
    if (cf.conflict.value) throw new Error('this artifact changed on disk and your edit is not saved yet')
    await save()
    if (cf.conflict.value) throw new Error('this artifact changed on disk and your edit is not saved yet')
    if (saveError.value) throw new Error(saveError.value)
  }

  async function resolveConflict(action: 'reload' | 'overwrite' | 'saveCopy') {
    const id = name()
    if (action === 'reload' && cf.conflict.value?.deleted) {
      cf.clear()
      base.value = { manifest: manifest.value, view: view.value } // nothing left to save
      hooks.gone() // "Close it": the artifact is gone
      return
    }
    await cf.resolving(async () => {
      const res = await App.ResolveConflict({
        kind: 'artifact', id, action, rev: cf.rev(),
        data: { manifest: manifest.value, view: view.value },
      })
      cf.clear()
      saveError.value = ''
      if (action === 'overwrite') {
        base.value = { manifest: manifest.value, view: view.value }
        rev.value = res.rev
        await refreshWarnings()
      } else {
        await load() // the disk version is what stays open
        if (action === 'saveCopy') notice.value = `Saved your version as "${res.id}".`
      }
      hooks.saved()
    }, (t) => { saveError.value = t }, '')
  }

  async function remove() {
    const ok = await askConfirm({
      title: 'Delete artifact',
      message: `Delete "${name()}"? Both of its files (artifact.yaml and view.a2ui.json) are removed. This cannot be undone.`,
      variant: 'destructive',
      confirmLabel: 'Delete',
    })
    if (!ok) return
    try {
      await App.DeleteArtifact(name(), rev.value)
      base.value = { manifest: manifest.value, view: view.value }
      hooks.gone()
    } catch (e) {
      saveError.value = isConflict(e)
        ? 'This artifact changed on disk since you opened it, so nothing was deleted. Check the change, then delete it again.'
        : errorText(e)
      if (isConflict(e) && !dirty.value) void load()
    }
  }

  // The artifact's files changed outside this tab (git, an editor, the agent).
  // Untouched text follows the disk quietly; an edit in progress gets the banner.
  async function onDiskChange(p?: { kind?: string; id?: string; op?: string }) {
    if (p?.kind !== 'artifact' || p.id !== name()) return
    const deleted = p.op === 'deleted'
    if (!dirty.value && !deleted) return load()
    if (!deleted) {
      const disk = await App.GetArtifactSource(name()).catch(() => null)
      if (disk && disk.rev === rev.value) return // our own write, or no real change
    }
    cf.raise(deleted)
  }

  const offs: Array<() => void> = []
  onMounted(() => {
    void load()
    offs.push(registerPendingSave(() => 'The artifact editor', flushPending))
    if (hasWailsRuntime()) offs.push(EventsOn('workspace:changed-on-disk', onDiskChange))
  })
  onUnmounted(() => { stopGuard?.(); for (const off of offs) off() })

  return {
    viewFile,
    manifest, view, rev, loading, loadError, saving, saveError, notice, warnings, dirty,
    conflict: cf.conflict, conflictBusy: cf.busy,
    load, save, checkOnce, resolveConflict, remove,
  }
}
