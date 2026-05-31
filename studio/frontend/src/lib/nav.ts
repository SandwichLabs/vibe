// Shared switchboard-navigation helpers.
//
// Both the App-Mode top tab strip (AppShell.vue) and the runtime switchboard
// (AppShellView.vue) turn a NavItem into a router destination. Keeping the
// mapping here means the locked-shell tabs and the switchboard buttons always
// route to the SAME place for a given nav item — they cannot drift apart.

export interface NavItem {
  id: string
  parent_id: string
  label: string
  action_kind: string
  target_id: string
  sort_order: number
}

// destinationFor maps a nav item to a vue-router destination, or null when the
// item has no direct route (e.g. run_macro, which dispatches through the macro
// runner instead of navigating).
export function destinationFor(item: NavItem): Record<string, unknown> | null {
  switch (item.action_kind) {
    case 'open_form':
      return { path: '/forms', query: { id: item.target_id } }
    case 'open_report':
      return { path: '/reports', query: { id: item.target_id } }
    case 'open_page':
      // Runtime page viewer — NOT the builderOnly /pages/:id/edit editor, which
      // is locked out in App Mode.
      return { path: `/page/${item.target_id}`, query: { id: item.target_id } }
    case 'open_switchboard':
      return { path: '/', query: { switchboard: item.target_id } }
    default:
      return null
  }
}

// iconFor picks a top-bar tab icon for a nav item's action kind. Names match the
// SchoolHouse icon set (icons.ts).
export function iconFor(item: NavItem): string {
  switch (item.action_kind) {
    case 'open_form':
      return 'form'
    case 'open_report':
      return 'barChart'
    case 'open_page':
      return 'fileText'
    case 'open_switchboard':
      return 'apps'
    case 'run_macro':
      return 'play'
    default:
      return 'apps'
  }
}
