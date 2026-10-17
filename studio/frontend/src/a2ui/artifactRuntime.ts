// The artifact runtime: what an open data artifact provides to the A2UI
// components under it (provide('a2uiArtifact', …)). Notebooks and chat never
// provide one, so their components behave as before. A data-bound component
// with `query` reads its rows from here; a control reads and sets a parameter.
import type { InjectionKey } from 'vue'
import type { LiveQueryResult } from './query'
import type { Coerced } from './artifactParams'

export interface ArtifactQueryState {
  loading: boolean
  error: string
  data: LiveQueryResult | null
  fetched: boolean
  truncated: boolean
}

interface ArtifactParamsApi {
  /** The manifest's declaration of a parameter, or undefined for an unknown name. */
  param(name: string): ArtifactParamT | undefined
  /** What a control shows: the draft being typed, else the committed value. */
  display(name: string): unknown
  /** Validation message for the draft, or ''. */
  error(name: string): string
  isDefault(name: string): boolean
  /** Offer a raw candidate; it only becomes a value if it validates. */
  set(name: string, raw: unknown, debounce?: boolean): Coerced | undefined
  reset(name: string): void
}

export interface ArtifactRuntime {
  /** Shared state of a named query; the first call for a name starts its run. */
  result(query: string): ArtifactQueryState
  params: ArtifactParamsApi
}

export const ARTIFACT_RUNTIME: InjectionKey<ArtifactRuntime> = Symbol('a2uiArtifact')

