/**
 * useRailActions — what the Connections rail does with a row (F10): activate it
 * (open the datasheet, expand, fix, sign in, take over), open a query or the
 * editor for it, disconnect / refresh / test / duplicate / remove a connection,
 * and copy a name.
 */
import { useRouter } from 'vue-router'
import { App } from '../lib/bindings'
import { openPromptFor, refreshConnections } from './useConnections'
import { takeOverOpen } from './useWorkspace'
import { askConfirm } from './useConfirm'
import { errorText } from '../lib/errors'
import { statusKey, selectFrom, scopeLabel } from '../lib/connections'
import type { Row } from '../lib/railTree'
import type { ConnectionTree } from './useConnectionTree'

export function useRailActions(tree: ConnectionTree, opts: { closeDrawer: () => void; error: (message: string) => void }) {
  const router = useRouter()
  const { closeDrawer } = opts

  async function activate(r: Row) {
    if (r.kind === 'conn') {
      const c = r.conn
      switch (statusKey(c.status)) {
        case 'err': return router.push({ path: `/connections/${c.name}`, query: { test: '1' } })
        case 'creds': case 'ext': return void openPromptFor(c)
        case 'lock': takeOverOpen.value = true; return
        case 'ok': return tree.toggleRow(r)
        default: return tree.expandConn(c)
      }
    }
    if (r.kind === 'table' || r.kind === 'view') return openDatasheet(r)
    if (r.kind === 'schema') return tree.toggleRow(r)
  }

  function openDatasheet(r: Row) {
    if (!r.node?.qualified) return
    closeDrawer()
    void router.push({ path: '/tables', query: { table: r.node.qualified } })
  }

  function newQueryHere(r: Row) {
    closeDrawer()
    const query: Record<string, string> = { conn: r.conn.name }
    if ((r.kind === 'table' || r.kind === 'view') && r.node?.qualified) query.sql = selectFrom(r.node.qualified)
    void router.push({ path: '/queries', query: { ...query, new: String(Date.now()) } })
  }

  function onDetailAction(r: Row) {
    const d = r.detail
    if (!d) return
    if (d.action === 'fix') void router.push({ path: `/connections/${r.conn.name}`, query: { test: '1' } })
    else if (d.action === 'takeover') takeOverOpen.value = true
    else openPromptFor(r.conn)
  }

  async function disconnect(c: ConnectionInfo) {
    try {
      await App.DisconnectConnection({ name: c.name, scope: '' })
      tree.collapseConn(c)
      await refreshConnections()
    } catch (e: unknown) { opts.error(errorText(e)) }
  }

  async function refreshConn(c?: ConnectionInfo) {
    try {
      await App.RefreshCatalog({ name: c?.name ?? '', scope: '' })
      await refreshConnections()
      await tree.reloadOpen(c?.name)
    } catch (e: unknown) { opts.error(errorText(e)) }
  }

  async function testConn(c: ConnectionInfo) {
    closeDrawer()
    void router.push({ path: `/connections/${c.name}`, query: { test: '1' } })
  }

  function editConn(c: ConnectionInfo) {
    closeDrawer()
    void router.push(`/connections/${c.name}`)
  }

  function duplicateConn(c: ConnectionInfo) {
    closeDrawer()
    void router.push({ path: '/connections/new', query: { from: c.name } })
  }

  async function removeConn(c: ConnectionInfo) {
    const ok = await askConfirm({
      title: `Remove ${c.name}?`,
      message: `This removes the connection from ${scopeLabel(c.scope)}. Your database isn’t touched. Saved credentials in your keychain are also deleted.`,
      variant: 'destructive',
      confirmLabel: 'Remove',
    })
    if (!ok) return
    try {
      await App.DeleteConnection({ name: c.name, scope: c.scope })
      await refreshConnections()
    } catch (e: unknown) { opts.error(errorText(e)) }
  }

  async function copyName(r: Row) {
    try { await navigator.clipboard.writeText(r.node?.qualified ?? r.label) } catch { /* clipboard unavailable */ }
  }

  return { activate, openDatasheet, newQueryHere, onDetailAction, disconnect, refreshConn, testConn, editConn, duplicateConn, removeConn, copyName }
}

export type RailActions = ReturnType<typeof useRailActions>
