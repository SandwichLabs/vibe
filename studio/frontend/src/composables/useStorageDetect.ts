/**
 * useStorageDetect — the storage line of the lake, Iceberg and Delta forms
 * (#130 step 2). It watches the catalog / table location (`path`) and a lake's
 * `data_path`, and after 300 ms of quiet asks Go (FindStorageFor) whether a
 * storage connection covers each URL typed. Only values containing `://` are
 * sent (a local path is not a storage URL); a response that arrives after the
 * text changed again is dropped. Nothing is asked for other connection types.
 */
import { ref, computed, watch, onScopeDispose, type Ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'

export const DETECT_DELAY_MS = 300
const DETECT_TYPES = ['ducklake', 'iceberg', 'delta']
export type DetectKey = 'path' | 'data_path'

interface Form { path: string; options: Record<string, string> }

export function useStorageDetect(typeId: Ref<string>, f: Form) {
  const needs = ref<StorageNeedView[]>([])
  const failed = ref('')

  // The URLs to check, in a stable order, by the field they were typed into.
  const urls = computed(() => {
    const out: { key: DetectKey; url: string }[] = []
    if (!DETECT_TYPES.includes(typeId.value)) return out
    for (const [key, raw] of [['path', f.path], ['data_path', f.options['data_path'] ?? '']] as const) {
      const url = raw.trim()
      if (url.includes('://')) out.push({ key, url })
    }
    return out
  })

  let timer: ReturnType<typeof setTimeout> | undefined
  let seq = 0

  async function check(list: string[]): Promise<void> {
    const mine = ++seq
    try {
      const r = await App.FindStorageFor({ urls: list })
      if (mine !== seq) return // a newer question was asked meanwhile
      needs.value = r.needs
      failed.value = ''
    } catch (e: unknown) {
      if (mine !== seq) return
      needs.value = []
      failed.value = errorText(e)
    }
  }

  function schedule(list: string[]): void {
    clearTimeout(timer)
    seq++ // anything in flight is now stale
    if (!list.length) { needs.value = []; failed.value = ''; return }
    timer = setTimeout(() => { void check(list) }, DETECT_DELAY_MS)
  }

  // The same URL in both fields is asked once.
  const distinct = computed(() => [...new Set(urls.value.map((u) => u.url))])
  watch(() => distinct.value.join('\n'), () => schedule(distinct.value), { immediate: true })

  // A storage connection added in another window changes the answer without the
  // text changing: ask again when this window is back in front.
  const refocus = () => { if (distinct.value.length) { clearTimeout(timer); void check(distinct.value) } }
  if (typeof window !== 'undefined') window.addEventListener('focus', refocus)
  onScopeDispose(() => {
    clearTimeout(timer)
    seq++
    if (typeof window !== 'undefined') window.removeEventListener('focus', refocus)
  })

  // needFor is the answer for what a field holds now; nothing while the answer
  // is for an older value of it.
  function needFor(key: DetectKey): StorageNeedView | null {
    const mine = urls.value.find((u) => u.key === key)
    if (!mine) return null
    const need = needs.value.find((n) => n.url === mine.url) ?? null
    // One line per answer: the data path says nothing the path did not, when it is
    // the same URL, an uncovered URL in the same suggestion group, or a URL read by the same connection.
    if (need && key === 'data_path') {
      const first = urls.value.find((u) => u.key === 'path')
      const other = first && needs.value.find((n) => n.url === first.url)
      if (other && (other === need
        || (need.suggest && other.suggest && need.group && other.group === need.group)
        || (need.match && other.match && need.match.name === other.match.name))) return null
    }
    return need
  }

  return { needs, failed, needFor }
}
