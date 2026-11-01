import { describe, it, expect } from 'vitest'
import { DEV_ORIGINS, frameCspMeta, injectFrameCsp, studioOrigins } from '../../scripts/frame-csp.mjs'

const index = '<!DOCTYPE html>\n<html lang="en">\n<head>\n  <meta charset="UTF-8" />\n  <title>Assay</title>\n</head>\n<body></body>\n</html>'

describe('frame policy of Studio\'s page (a1)', () => {
  it('narrows frame-src to the view server route on the window\'s origin, and nothing else', () => {
    const meta = frameCspMeta(['wails://wails'])
    expect(meta).toBe('<meta http-equiv="Content-Security-Policy" content="frame-src wails://wails/assay-view/" />')
    expect(meta).not.toContain('default-src')
    expect(meta).not.toContain("'self'")
  })

  it('emits the production origin per platform and the dev origins when serving', () => {
    expect(studioOrigins('build', {}, 'linux')).toEqual(['wails://wails'])
    expect(studioOrigins('build', {}, 'darwin')).toEqual(['wails://wails'])
    expect(studioOrigins('build', {}, 'win32')).toEqual(['http://wails.localhost'])
    expect(studioOrigins('build', { ASSAY_STUDIO_ORIGIN: 'wails://wails' }, 'win32')).toEqual(['wails://wails'])
    expect(studioOrigins('serve', {}, 'linux')).toEqual(DEV_ORIGINS)
  })

  it('never lets the QA origin into a build, whatever the environment says', () => {
    for (const platform of ['linux', 'darwin', 'win32']) {
      const origins = studioOrigins('build', {}, platform) as string[]
      expect(origins.join(' ')).not.toContain('localhost:34115')
    }
    expect(studioOrigins('serve', {}, 'linux')).toContain('http://localhost:34115')
  })

  it('refuses an origin that is not plain, so nothing can be injected into the policy', () => {
    for (const bad of ['', 'wails://wails; script-src *', 'wails://wails/', 'javascript:alert(1)', 'wails://wa ils', "wails://x'", 'file:///x']) {
      expect(() => studioOrigins('build', { ASSAY_STUDIO_ORIGIN: bad || ' ' }, 'linux')).toThrow()
    }
    expect(() => frameCspMeta([])).toThrow()
    expect(() => frameCspMeta(['wails://wails" onload="x'])).toThrow()
  })

  it('puts the meta first in <head>, once', () => {
    const out = injectFrameCsp(index, ['wails://wails'])
    expect(out.indexOf('Content-Security-Policy')).toBeLessThan(out.indexOf('charset'))
    expect(out.match(/Content-Security-Policy/g)).toHaveLength(1)
    expect(() => injectFrameCsp(out, ['wails://wails'])).toThrow()
    expect(() => injectFrameCsp('<html></html>', ['wails://wails'])).toThrow()
  })
})
