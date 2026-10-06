import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { resolveConfirmChoice, confirmVisible } from '../composables/useConfirm'
import ConflictBanner from './ConflictBanner.vue'

const mountBanner = (props: Record<string, any> = {}) =>
  mount(ConflictBanner, { props: { kind: 'notebook', path: 'notebooks/weekly-revenue.vnb', ...props }, global: { stubs: { Icon: true } } })

describe('ConflictBanner', () => {
  it('is a role=alert with the plain-language copy and the file path', () => {
    const w = mountBanner()
    const el = w.find('[role="alert"]')
    expect(el.exists()).toBe(true)
    expect(el.text()).toContain('This notebook changed on disk.')
    expect(el.text()).toContain('notebooks/weekly-revenue.vnb')
    expect(el.text()).toContain('Nothing happens until you do.')
  })

  it('uses the right noun and says "deleted" when the file is gone', () => {
    const w = mountBanner({ kind: 'query', path: 'queries/x.sql', deleted: true })
    expect(w.text()).toContain('This query was deleted on disk.')
    expect(w.find('[data-testid="conflict-reload"]').text()).toBe('Close it')
  })

  it('"Reload from disk" confirms what is lost, then emits reload', async () => {
    const w = mountBanner()
    await w.find('[data-testid="conflict-reload"]').trigger('click')
    await flushPromises()
    expect(confirmVisible.value).toBe(true)
    expect(w.emitted('reload')).toBeUndefined()
    resolveConfirmChoice('confirm')
    await flushPromises()
    expect(w.emitted('reload')).toHaveLength(1)
  })

  it('declining the confirm emits nothing', async () => {
    const w = mountBanner()
    await w.find('[data-testid="conflict-overwrite"]').trigger('click')
    await flushPromises()
    resolveConfirmChoice('cancel')
    await flushPromises()
    expect(w.emitted('overwrite')).toBeUndefined()
  })

  it('"Keep mine" confirms then emits overwrite; "Save mine as a copy" emits at once', async () => {
    const w = mountBanner()
    await w.find('[data-testid="conflict-overwrite"]').trigger('click')
    await flushPromises()
    resolveConfirmChoice('confirm')
    await flushPromises()
    expect(w.emitted('overwrite')).toHaveLength(1)
    await w.find('[data-testid="conflict-copy"]').trigger('click')
    expect(w.emitted('saveCopy')).toHaveLength(1)
  })

  it('disables the choices while busy', () => {
    const w = mountBanner({ busy: true })
    for (const id of ['reload', 'overwrite', 'copy']) {
      expect((w.find(`[data-testid="conflict-${id}"]`).element as HTMLButtonElement).disabled).toBe(true)
    }
  })
})
