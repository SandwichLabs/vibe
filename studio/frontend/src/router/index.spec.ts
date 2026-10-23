// The Library routes and the redirects from the old paths (docs/plans/library.md L4).
import { describe, it, expect, vi } from 'vitest'
import { router } from './index'

vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }))

async function land(to: string) {
  await router.push(to)
  await router.isReady()
  const r = router.currentRoute.value
  return { path: r.path, query: { ...r.query }, name: r.name }
}

describe('Library routes and redirects', () => {
  it('/notebooks lands on the notebook chip', async () => {
    expect(await land('/notebooks')).toMatchObject({ path: '/library', query: { type: 'notebook' } })
  })

  it('/notebooks?new=1 becomes ?new=notebook and keeps no type', async () => {
    const r = await land('/notebooks?new=1')
    expect(r.path).toBe('/library')
    expect(r.query).toEqual({ new: 'notebook' })
  })

  it('keeps other query keys on the list redirect', async () => {
    const r = await land('/notebooks?x=1')
    expect(r.query).toEqual({ x: '1', type: 'notebook' })
  })

  it('/artifacts lands on /library and keeps its query', async () => {
    const r = await land('/artifacts?type=page')
    expect(r.path).toBe('/library')
    expect(r.query).toEqual({ type: 'page' })
  })

  it('/notebooks/:id and /artifacts/:name land on the item routes', async () => {
    expect(await land('/notebooks/nb-1')).toMatchObject({ path: '/library/notebook/nb-1', name: 'library-notebook' })
    expect(await land('/artifacts/weekly-sales')).toMatchObject({ path: '/library/artifact/weekly-sales', name: 'library-artifact' })
  })

  it('keeps the query on an item redirect', async () => {
    const r = await land('/artifacts/weekly-sales?region=East')
    expect(r.path).toBe('/library/artifact/weekly-sales')
    expect(r.query).toEqual({ region: 'East' })
  })

  it('an id with an encoded slash stays one segment', async () => {
    expect((await land('/notebooks/a%2Fb')).path).toBe('/library/notebook/a%2Fb')
  })

  it('the new paths are not redirected', async () => {
    expect(await land('/library?type=live&new=live')).toMatchObject({ name: 'library', query: { type: 'live', new: 'live' } })
  })
})
