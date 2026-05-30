// useMacroIntents (P5.6) — the single, shared dispatcher for the ordered UI
// intents returned by App.RunMacro. Both the AppShell switchboard (P5.4) and the
// page/form runtime (P5.6) route macro results through this composable so there
// is exactly ONE intent applier in the Studio frontend.
//
// Intents are applied in order. open_form/open_report/open_page navigate via the
// supplied router; set_filter is merged as a `where` route query onto the
// matching target's navigation (the AppShell contract); msg_box raises a dialog
// surfaced through the reactive `msgBox` ref.
import { ref, type Ref } from 'vue'

// MacroIntent is one UI instruction returned by App.RunMacro: a navigation
// (open_form/open_report/open_page), a message dialog (msg_box), or a filter to
// apply to a subsequently-opened target (set_filter).
export interface MacroIntent {
  kind: string
  target: string
  args?: Record<string, string>
}

export interface MacroResult {
  intents?: MacroIntent[]
}

// A minimal router shape — only push() is needed to navigate. vue-router's
// Router satisfies this, and the unit specs supply a recording mock.
export interface MacroRouter {
  push: (to: unknown) => unknown
}

// Map an intent's target to a router destination. JSON of the destination
// carries both the route segment (forms/reports/pages) and the bound target id —
// the same mapping AppShellView.destinationFor uses.
function destinationFor(
  kind: string,
  target: string,
): { path: string; query: Record<string, string> } | null {
  switch (kind) {
    case 'open_form':
      return { path: '/forms', query: { id: target } }
    case 'open_report':
      return { path: '/reports', query: { id: target } }
    case 'open_page':
      // Runtime page viewer — NOT the builderOnly /pages/:id/edit editor.
      return { path: `/page/${target}`, query: { id: target } }
    default:
      return null
  }
}

export function useMacroIntents(router: () => MacroRouter | undefined) {
  // msgBox holds the message for a MsgBox dialog raised by a msg_box intent;
  // null when no dialog is showing.
  const msgBox: Ref<string | null> = ref(null)

  function applyMacroIntents(intents: MacroIntent[]) {
    // First collect any set_filter where-clauses keyed by their target so they
    // can be merged onto the matching navigation.
    const filters = new Map<string, string>()
    for (const intent of intents) {
      if (intent.kind === 'set_filter' && intent.args?.where) {
        filters.set(intent.target, intent.args.where)
      }
    }

    for (const intent of intents) {
      switch (intent.kind) {
        case 'open_form':
        case 'open_report':
        case 'open_page': {
          const dest = destinationFor(intent.kind, intent.target)
          if (!dest) break
          const where = filters.get(intent.target)
          if (where) dest.query = { ...dest.query, where }
          router()?.push(dest)
          break
        }
        case 'msg_box':
          msgBox.value = intent.args?.message ?? ''
          break
        default:
          break
      }
    }
  }

  return { msgBox, applyMacroIntents }
}
