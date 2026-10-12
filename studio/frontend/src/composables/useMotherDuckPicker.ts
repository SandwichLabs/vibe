/**
 * useMotherDuckPicker — the MotherDuck database picker of the connection editor
 * (F10). The list comes from the account the token belongs to, loaded in a
 * separate process like Test. A typed token goes over the bridge once and is
 * never returned; typing the name by hand stays possible whatever happens here.
 */
import { ref, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import type { ConnectionForm } from './useConnectionForm'

export function useMotherDuckPicker(form: ConnectionForm) {
  const route = useRoute()
  const { f, typed, method, status, isNew, connScope, methodString, result } = form

  const mdDbs = ref<MotherDuckDatabase[]>([])
  const mdLoading = ref(false)
  const mdLoaded = ref(false)
  const mdError = ref('')
  const mdInstall = ref('')
  const mdTokenReady = computed(() => !!typed.token.trim() || method.value === 'env' || (!isNew.value && method.value !== 'prompt' && !!status.value?.secretSet))
  let mdReq = 0

  function resetMD() { mdReq++; mdLoading.value = false; mdLoaded.value = false; mdDbs.value = []; mdError.value = ''; mdInstall.value = '' }

  async function loadDatabases() {
    if (mdLoading.value) return
    const my = ++mdReq
    const key = JSON.stringify([route.path, route.params.name, methodString.value, typed.token])
    const stale = () => my !== mdReq || key !== JSON.stringify([route.path, route.params.name, methodString.value, typed.token])
    mdLoading.value = true
    mdError.value = ''
    mdInstall.value = ''
    try {
      const r = await App.ListMotherDuckDatabases({
        name: isNew.value ? '' : f.name,
        scope: connScope.value,
        signIn: methodString.value,
        credentials: method.value === 'env' ? { username: '', keyId: '', password: '', token: '', secret: '' } : { ...typed },
        install: false,
      })
      if (stale()) return
      mdLoaded.value = r.ok
      mdDbs.value = r.databases ?? []
      if (!r.ok) { mdError.value = r.message ?? 'Couldn\'t load your databases.'; mdInstall.value = r.installExtension ?? '' }
    } catch (e: unknown) {
      if (stale()) return
      mdLoaded.value = false
      mdError.value = errorText(e)
    }
    mdLoading.value = false
  }

  function pickDatabase(name: string) {
    f.database = name
    result.value = null
  }

  // A different token or sign-in means a different account: the old list is stale.
  watch(() => [typed.token, methodString.value], resetMD)

  return { mdDbs, mdLoading, mdLoaded, mdError, mdInstall, mdTokenReady, resetMD, loadDatabases, pickDatabase }
}

export type MotherDuckPicker = ReturnType<typeof useMotherDuckPicker>
