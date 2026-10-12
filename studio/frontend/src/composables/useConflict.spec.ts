// useConflict — the conflict-banner flow shared by the notebook, query and skill
// editors (F3). These pin the behaviours the #57 fixes introduced, once, so the
// three editors cannot drift apart again.
import { describe, it, expect, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { useConflict, CONFLICT_AGAIN } from './useConflict'

function setup(fetchRev: (id: string) => Promise<string | undefined> = async () => 'disk-1') {
  let current: string | null = 'doc-a'
  const fetch = vi.fn(fetchRev)
  const cf = useConflict({ currentId: () => current, fetchRev: fetch })
  return { cf, fetch, setCurrent: (id: string | null) => { current = id } }
}

describe('useConflict', () => {
  it('raise() shows the banner and re-fetches the revision on disk every time', async () => {
    let n = 0
    const { cf, fetch } = setup(async () => `rev-${++n}`)
    cf.raise(false)
    await flushPromises()
    expect(cf.conflict.value).toEqual({ deleted: false })
    expect(cf.rev()).toBe('rev-1')
    // A second raise (say the file changed again) asks again, it never reuses the first answer.
    cf.raise(false)
    expect(cf.rev()).toBe('') // reset the moment the banner is raised, until the new answer arrives
    await flushPromises()
    expect(cf.rev()).toBe('rev-2')
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it('a deleted file has no revision to fetch', async () => {
    const { cf, fetch } = setup()
    cf.raise(true)
    await flushPromises()
    expect(cf.conflict.value).toEqual({ deleted: true })
    expect(cf.rev()).toBe('')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('drops a revision that arrives after the banner was cleared or another document opened', async () => {
    let resolve!: (v: string) => void
    const { cf, setCurrent } = setup(() => new Promise<string>((r) => { resolve = r }))
    cf.raise(false)
    cf.clear()
    resolve('late')
    await flushPromises()
    expect(cf.rev()).toBe('')

    cf.raise(false)
    setCurrent('doc-b')
    resolve('for-doc-a')
    await flushPromises()
    expect(cf.rev()).toBe('')
  })

  it('a failed revision lookup falls back to an unconditional keep (empty rev)', async () => {
    const { cf } = setup(async () => { throw new Error('gone') })
    cf.raise(false)
    await flushPromises()
    expect(cf.conflict.value).toEqual({ deleted: false })
    expect(cf.rev()).toBe('')
  })

  it('clear() is the success reset: banner gone, remembered revision dropped', async () => {
    const { cf } = setup()
    cf.raise(false)
    await flushPromises()
    expect(cf.rev()).toBe('disk-1')
    cf.clear()
    expect(cf.conflict.value).toBeNull()
    expect(cf.rev()).toBe('')
  })

  describe('resolving()', () => {
    it('holds busy for the duration and reports nothing on success', async () => {
      const { cf } = setup()
      const fail = vi.fn()
      let during = false
      await cf.resolving(async () => { during = cf.busy.value }, fail)
      expect(during).toBe(true)
      expect(cf.busy.value).toBe(false)
      expect(fail).not.toHaveBeenCalled()
    })

    it('a refusal because the file changed AGAIN re-raises the banner afresh, with the "again" text', async () => {
      let n = 0
      const { cf, fetch } = setup(async () => `rev-${++n}`)
      cf.raise(false)
      await flushPromises()
      const fail = vi.fn()
      await cf.resolving(async () => { throw new Error('conflict: notebook changed on disk') }, fail)
      await flushPromises()
      expect(fail).toHaveBeenCalledWith(CONFLICT_AGAIN)
      expect(CONFLICT_AGAIN).toMatch(/changed on disk again/)
      expect(cf.conflict.value).toEqual({ deleted: false })
      expect(cf.rev()).toBe('rev-2') // the banner is about the NEW version on disk
      expect(fetch).toHaveBeenCalledTimes(2)
      expect(cf.busy.value).toBe(false)
    })

    it('any other failure is reported with the prefix and the wrap-chain stripped, and leaves the banner alone', async () => {
      const { cf } = setup()
      cf.raise(false)
      await flushPromises()
      const fail = vi.fn()
      await cf.resolving(async () => { throw new Error('failed to write file: disk full') }, fail)
      expect(fail).toHaveBeenCalledWith('Failed: disk full')
      expect(cf.conflict.value).toEqual({ deleted: false })
      expect(cf.busy.value).toBe(false)

      await cf.resolving(async () => { throw new Error('disk full') }, fail, '')
      expect(fail).toHaveBeenLastCalledWith('disk full') // the skill editor shows the bare text
    })

    it('a write-back after a delete (Keep mine) goes through with no revision', async () => {
      const { cf } = setup()
      cf.raise(true)
      const sent: string[] = []
      await cf.resolving(async () => { sent.push(cf.rev()); cf.clear() }, vi.fn())
      expect(sent).toEqual([''])
      expect(cf.conflict.value).toBeNull()
    })
  })
})
