// A2UI page/component types (#67). These mirror internal/a2ui's Go Page/
// Component types (which is the validator — Go validates, Vue renders): a
// component is its fixed id/type/parent plus whatever other typed fields its
// type declares, held generically here too since the field set varies by
// type and TypeScript has no equivalent of the Go validator's closed
// per-type field map at this layer (that check already happened server
// side, in present_data/RunA2UIQuery's caller).
export type A2UIColor = 'yellow' | 'black' | 'error' | 'muted'

export interface A2UIComponentT {
  id: string
  type: string
  parent?: string
  [key: string]: any
}

export interface A2UIPageT {
  title: string
  components: A2UIComponentT[]
}
