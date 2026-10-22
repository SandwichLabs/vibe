/**
 * usePageFrame — the address of a static HTML page's frame (#162, H3): asks Go
 * for a page token (only for a page that is allowed right now, with the files
 * that were hashed), and builds /vibe-view/<token>/page?<values> from the
 * artifact's parameter values. A change of a value changes the address, which
 * reloads the frame; the page is rendered in Go from that query string, always
 * through Bind. The frame is shown only if Studio's own frame policy (the meta
 * CSP, lib/framePolicy) allows this window's origin. Errors are the fixed texts
 * Go sends.
 */
import { computed, onMounted, ref, watch } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { FRAME_POLICY_TEXT, frameAllowed } from '../lib/framePolicy'
import type { ParamValue } from '../a2ui/artifactParams'

export interface PageFrameDeps {
  openPage(name: string, origin: string): Promise<ArtifactPageGrant>
  origin(): string
  frameOk(origin: string): boolean
}

const realDeps: PageFrameDeps = {
  openPage: (n, o) => App.OpenArtifactPage(n, o),
  origin: () => `${location.protocol}//${location.host}`,
  frameOk: (o) => frameAllowed(document, o),
}

/** The query string for the values that differ from their defaults ("" when none). */
export function pageQuery(detail: ArtifactDetail, values: Record<string, ParamValue>): string {
  const parts: string[] = []
  for (const p of detail.params) {
    const v = values[p.name]
    if (v === undefined || v === detail.defaults[p.name]) continue
    parts.push(`${encodeURIComponent(p.name)}=${encodeURIComponent(String(v))}`)
  }
  return parts.length ? `?${parts.join('&')}` : ''
}

export function usePageFrame(
  name: () => string,
  detail: () => ArtifactDetail,
  values: () => Record<string, ParamValue>,
  refresh: () => number,
  deps: Partial<PageFrameDeps> = {},
) {
  const d: PageFrameDeps = { ...realDeps, ...deps }
  const token = ref('')
  const failure = ref('')
  let seq = 0

  async function open() {
    const mine = ++seq
    token.value = ''
    failure.value = ''
    const origin = d.origin()
    if (!d.frameOk(origin)) {
      failure.value = FRAME_POLICY_TEXT
      return
    }
    try {
      const g = await d.openPage(name(), origin)
      if (mine === seq) token.value = g.token
    } catch (e) {
      if (mine === seq) failure.value = errorText(e)
    }
  }

  const src = computed(() => (token.value ? `/vibe-view/${token.value}/page${pageQuery(detail(), values())}` : ''))
  onMounted(open)
  // A refresh asks for a new token, so the frame loads again even if no value changed.
  watch(refresh, open)
  return { src, failure, reload: open }
}
