import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import PageFiles from './PageFiles.vue'

const files: ArtifactViewFile[] = [
  { path: 'index.html.tmpl', role: 'entry', size: 40, text: '<h1>{{ param "n" }}</h1>' },
  { path: 'assets/style.css', role: 'style', size: 2048, text: 'body{margin:0}' },
  { path: 'assets/logo.png', role: 'image', size: 5000 },
]

describe('PageFiles', () => {
  it('lists the manifest and every file with role and size, and shows the selected text', async () => {
    const w = mount(PageFiles, { props: { manifest: 'version: 1\ntitle: T\n', files } })
    const items = w.findAll('.page-files__item')
    expect(items.map((i) => i.get('.page-files__path').text())).toEqual(['artifact.yaml', 'index.html.tmpl', 'assets/style.css', 'assets/logo.png'])
    expect(items[1].text()).toContain('entry · 40 B')
    expect(items[2].text()).toContain('style · 2.0 KB')
    expect(w.get('[data-testid="page-file-source"]').text()).toContain('version: 1')
    await w.get('[data-testid="page-file-index.html.tmpl"]').trigger('click')
    expect(w.get('[data-testid="page-file-source"]').text()).toContain('{{ param "n" }}')
  })

  it('says a binary asset is covered by the hash and not shown', async () => {
    const w = mount(PageFiles, { props: { manifest: 'version: 1\n', files } })
    await w.get('[data-testid="page-file-assets/logo.png"]').trigger('click')
    expect(w.get('[data-testid="page-file-binary"]').text()).toContain('binary file (4.9 KB)')
    expect(w.get('[data-testid="page-file-source"]').isVisible()).toBe(false)
  })

  it('shows hostile file text as text', async () => {
    const hostile: ArtifactViewFile[] = [{ path: 'index.html.tmpl', role: 'entry', size: 30, text: '<script>alert(1)</script><img src=x onerror=alert(2)>' }]
    const w = mount(PageFiles, { props: { manifest: 'v', files: hostile } })
    await w.get('[data-testid="page-file-index.html.tmpl"]').trigger('click')
    expect(w.find('img').exists()).toBe(false)
    expect(w.element.querySelector('script')).toBeNull()
  })
})
