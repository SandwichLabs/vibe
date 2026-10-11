// A2UI page/component types (#67). These mirror internal/a2ui's Go Page/
// Component types (which is the validator — Go validates, Vue renders): a
// component is its fixed id/type/parent plus whatever other typed fields its
// type declares, held generically here too since the field set varies by
// type and TypeScript has no equivalent of the Go validator's closed
// per-type field map at this layer (that check already happened server
// side, in present_data/RunA2UIQuery's caller).
export interface A2UIComponentT {
  id: string
  type: string
  parent?: string
  // Client-only, one DuckDB type name per data column/value, set by
  // Studio's notebook sql-cell synthesizer (views/NotebookView.vue) so the
  // table/stat renderer can format a synthesized page's values faithfully.
  // NEVER part of the A2UI schema (internal/a2ui rejects unknown fields on
  // any model-authored or saved page) and never persisted — synthesized
  // pages are render-only.
  types?: string[]
  [key: string]: any
}

export interface A2UIPageT {
  title: string
  components: A2UIComponentT[]
}
