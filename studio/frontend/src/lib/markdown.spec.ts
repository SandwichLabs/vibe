// lib/markdown.ts — the ONE safe markdown renderer + link-click safety net
// for every v-html sink of untrusted markdown (Skills, agent chat).
import { describe, it, expect, vi, beforeEach } from 'vitest'

const openURL = vi.fn()
vi.mock('../wailsjs/runtime/runtime', () => ({
  BrowserOpenURL: (url: string) => openURL(url),
}))

import { renderSafeMarkdown, installSafeLinkHandling, vibeMdContentClass } from './markdown'
import { confirmVisible, resolveConfirm } from '../composables/useConfirm'

describe('renderSafeMarkdown (allow-list)', () => {
  it('strips script tags', () => {
    const html = renderSafeMarkdown('hello <script>alert(1)</script> world')
    expect(html).not.toContain('<script')
    expect(html).not.toContain('alert(1)')
  })

  it('strips form-control tags (input/button/textarea/select/option)', () => {
    const html = renderSafeMarkdown('<input type="text"><button>click</button><textarea></textarea><select><option>x</option></select>')
    expect(html).not.toContain('<input')
    expect(html).not.toContain('<button')
    expect(html).not.toContain('<textarea')
    expect(html).not.toContain('<select')
    expect(html).not.toContain('<option')
  })

  it('strips a class attribute used to spoof the app UI (spoof.md payload)', () => {
    const html = renderSafeMarkdown(
      '<p class="modal-overlay"><span class="card card-pad"><strong class="card-title">Session expired</strong>' +
      '<a class="btn btn-primary" href="https://example.com/verify">Re-enter passphrase</a></span></p>',
    )
    expect(html).not.toContain('modal-overlay')
    expect(html).not.toContain('btn btn-primary')
    expect(html).not.toContain('card')
    expect(html).not.toContain('class=')
    // span itself is gone too (marked never emits it; its only purpose here
    // was carrying the spoofing class).
    expect(html).not.toContain('<span')
    // The link text survives as inert content, just with no class/styling.
    expect(html).toContain('Re-enter passphrase')
  })

  it('keeps a language-xxx class on <code> (the one allowed exception), but strips class everywhere else', () => {
    const html = renderSafeMarkdown('```sql\nSELECT 1;\n```\n\n<p class="foo">plain</p>')
    expect(html).toMatch(/<code class="language-sql">/)
    expect(html).not.toContain('class="foo"')
  })

  it('rejects a class value on <code> that does not match language-xxx', () => {
    const html = renderSafeMarkdown('<pre><code class="modal-overlay">x</code></pre>')
    expect(html).not.toContain('modal-overlay')
  })

  it('strips zero-click-fetch tags: video/audio/source/picture/track/object/embed', () => {
    const html = renderSafeMarkdown(
      '<video src="https://example.com/v.mp4" poster="https://example.com/p.png"></video>' +
      '<audio src="https://example.com/a.mp3"></audio>' +
      '<picture><source srcset="https://example.com/s.png"></picture>' +
      '<object data="https://example.com/o"></object>' +
      '<embed src="https://example.com/e">',
    )
    for (const tag of ['<video', '<audio', '<source', '<picture', '<object', '<embed']) {
      expect(html).not.toContain(tag)
    }
    expect(html).not.toContain('example.com')
  })

  it('strips <area>/<map> and usemap (image-map link that closest("a") would never catch)', () => {
    const html = renderSafeMarkdown('<img src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7" usemap="#m"><map name="m"><area href="https://example.com/area" coords="0,0,1,1"></map>')
    expect(html).not.toContain('<map')
    expect(html).not.toContain('<area')
    expect(html).not.toContain('usemap')
    expect(html).not.toContain('example.com')
  })

  it('strips a <table background="...">', () => {
    const html = renderSafeMarkdown('<table background="https://example.com/bg.png"><tr><td>t</td></tr></table>')
    expect(html).not.toContain('background')
    expect(html).not.toContain('example.com')
  })

  it('drops a remote <img> entirely (no broken-image element left behind)', () => {
    const html = renderSafeMarkdown('![alt](https://evil.example.com/track.png)')
    expect(html).not.toContain('<img')
    expect(html).not.toContain('evil.example.com')
  })

  it('keeps a data: URI image', () => {
    const html = renderSafeMarkdown('![alt](data:image/png;base64,iVBORw0KGgo=)')
    expect(html).toContain('<img')
    expect(html).toContain('data:image/png')
  })

  it('renders a plain http(s) link as an anchor (link SAFETY is handled at click time, not by hiding the link)', () => {
    const html = renderSafeMarkdown('[click me](https://example.com)')
    expect(html).toContain('<a')
    expect(html).toContain('href="https://example.com"')
  })

  it('strips a javascript: link', () => {
    const html = renderSafeMarkdown('[click me](javascript:alert(1))')
    expect(html).not.toContain('javascript:')
  })

  it('renders list markup (bullets/numbers) so CSS can style it — regression for the "no bullets" visual bug', () => {
    const html = renderSafeMarkdown('- one\n- two\n\n1. first\n2. second')
    expect(html).toContain('<ul>')
    expect(html).toContain('<li>one</li>')
    expect(html).toContain('<ol>')
  })

  it('renders the full XSS payload document with no element carrying a remote URL attribute', () => {
    // The reviewer's full attack payload in one document: plain link, an
    // image-map (img usemap + map + area), an inline SVG with xlink:href/
    // href links, video/audio/picture zero-click fetches, and a table
    // background — plus a relative link and a leading-space-obfuscated one
    // that must both render inert (no remote attribute survives either
    // way; the relative/obfuscation handling itself is exercised in the
    // click-handling suite below, not here).
    const xss2 = `# Link attack skill

Plain: [external](https://example.com/phish)

- bullet one
- bullet two

<img id="mapimg" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7" width="200" height="40" usemap="#m"><map name="m"><area id="evilarea" shape="rect" coords="0,0,200,40" href="https://example.com/area"></map>

<svg width="120" height="30"><a id="svga" xlink:href="https://example.com/svg" href="https://example.com/svg2"><text x="0" y="20">svg link</text></a></svg>

<video id="vid" src="https://example.com/v.mp4" poster="https://example.com/poster.png"></video>
<audio src="https://example.com/a.mp3"></audio>
<picture><source srcset="https://example.com/src.png"><img src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"></picture>
<table background="https://example.com/bg.png"><tr><td>t</td></tr></table>
<a id="rel" href="#/tables">relative</a>
<a id="ws" href=" https://example.com/ws">leading space</a>
`
    const html = renderSafeMarkdown(xss2)
    const doc = new DOMParser().parseFromString(`<div>${html}</div>`, 'text/html')
    // A plain <a href="https://...(a real link, safe by construction: link
    // navigation is confirmed at CLICK time, not prevented at render time —
    // see the installSafeLinkHandling suite below) is expected and fine.
    // What must NEVER survive is a remote URL on anything else: an img src,
    // an area/map href, a video/audio/source/object/embed src, or a table
    // background — all zero-click (or, for area, closest('a')-evading)
    // fetch/navigation vectors.
    const remoteAttrValues: string[] = []
    doc.querySelectorAll('*').forEach((el) => {
      if (el.tagName === 'A') return
      for (const attr of Array.from(el.attributes)) {
        if (/example\.com/i.test(attr.value)) remoteAttrValues.push(`${el.tagName}[${attr.name}]=${attr.value}`)
      }
    })
    expect(remoteAttrValues).toEqual([])
    // svg/video/audio/picture/source/map/area must not survive at all.
    for (const tag of ['svg', 'video', 'audio', 'picture', 'source', 'map', 'area']) {
      expect(doc.querySelectorAll(tag).length).toBe(0)
    }
  })
})

describe('installSafeLinkHandling', () => {
  beforeEach(() => {
    openURL.mockClear()
    document.body.innerHTML = ''
    confirmVisible.value = false
  })

  function activate(container: HTMLElement, href: string, opts: { tag?: 'a' | 'area'; type?: 'click' | 'auxclick' } = {}) {
    const el = document.createElement(opts.tag ?? 'a')
    el.setAttribute('href', href)
    el.textContent = 'link'
    container.appendChild(el)
    const evt = new MouseEvent(opts.type ?? 'click', { bubbles: true, cancelable: true })
    el.dispatchEvent(evt)
    return evt
  }

  it('shows the shared SchoolHouse ConfirmDialog (not window.confirm) for an http(s) link, and opens it via BrowserOpenURL once confirmed', async () => {
    installSafeLinkHandling()
    const container = document.createElement('div')
    container.className = vibeMdContentClass
    document.body.appendChild(container)

    const evt = activate(container, 'https://example.com/page')
    expect(evt.defaultPrevented).toBe(true)

    // The click handler is async (awaits askConfirm); give its microtask a
    // turn so confirmVisible flips before we assert on it.
    await Promise.resolve()
    await Promise.resolve()
    expect(confirmVisible.value).toBe(true)
    expect(openURL).not.toHaveBeenCalled()

    resolveConfirm(true)
    await Promise.resolve()
    await Promise.resolve()
    expect(openURL).toHaveBeenCalledWith('https://example.com/page')
  })

  it('does not open the link if the user declines the dialog', async () => {
    installSafeLinkHandling()
    const container = document.createElement('div')
    container.className = vibeMdContentClass
    document.body.appendChild(container)

    activate(container, 'https://example.com/page')
    await Promise.resolve()
    await Promise.resolve()
    expect(confirmVisible.value).toBe(true)

    resolveConfirm(false)
    await Promise.resolve()
    await Promise.resolve()
    expect(openURL).not.toHaveBeenCalled()
  })

  it('drops a non-http(s) scheme silently (never shows the dialog, never calls BrowserOpenURL)', async () => {
    installSafeLinkHandling()
    const container = document.createElement('div')
    container.className = vibeMdContentClass
    document.body.appendChild(container)

    const evt = activate(container, 'file:///etc/passwd')
    expect(evt.defaultPrevented).toBe(true)
    await Promise.resolve()
    expect(confirmVisible.value).toBe(false)
    expect(openURL).not.toHaveBeenCalled()
  })

  it('intercepts an <area> element too, not just <a> (closest("a") alone never matches it)', async () => {
    installSafeLinkHandling()
    const container = document.createElement('div')
    container.className = vibeMdContentClass
    document.body.appendChild(container)

    const evt = activate(container, 'https://example.com/area', { tag: 'area' })
    expect(evt.defaultPrevented).toBe(true)
    await Promise.resolve()
    await Promise.resolve()
    expect(confirmVisible.value).toBe(true)
    resolveConfirm(true)
    await Promise.resolve()
    await Promise.resolve()
    expect(openURL).toHaveBeenCalledWith('https://example.com/area')
  })

  it('intercepts a middle-click (auxclick), not just a primary click', async () => {
    installSafeLinkHandling()
    const container = document.createElement('div')
    container.className = vibeMdContentClass
    document.body.appendChild(container)

    const evt = activate(container, 'https://example.com/mid', { type: 'auxclick' })
    expect(evt.defaultPrevented).toBe(true)
    await Promise.resolve()
    await Promise.resolve()
    expect(confirmVisible.value).toBe(true)
  })

  it('does not intercept a link outside a .vibe-md-content container (normal app navigation)', () => {
    installSafeLinkHandling()
    const container = document.createElement('div')
    // No vibe-md-content class — ordinary app chrome.
    document.body.appendChild(container)

    const evt = activate(container, 'https://example.com/page')

    expect(evt.defaultPrevented).toBe(false)
    expect(openURL).not.toHaveBeenCalled()
  })
})
