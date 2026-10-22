import { describe, it, expect, beforeEach } from 'vitest'
import { frameAllowed, frameSources } from './framePolicy'

function meta(content: string) {
  const m = document.createElement('meta')
  m.setAttribute('http-equiv', 'Content-Security-Policy')
  m.setAttribute('content', content)
  document.head.appendChild(m)
}

describe('frame policy check', () => {
  beforeEach(() => { document.head.innerHTML = '' })

  it('allows a page only under a frame-src narrowed to the view server route on this origin', () => {
    meta('frame-src wails://wails/vibe-view/')
    expect(frameAllowed(document, 'wails://wails')).toBe(true)
    expect(frameAllowed(document, 'http://localhost:34115')).toBe(false)
  })

  it('fails closed when there is no policy, or it is wider than the route', () => {
    expect(frameAllowed(document, 'wails://wails')).toBe(false)
    for (const wide of ["frame-src 'self'", 'frame-src *', 'frame-src wails:', 'frame-src wails://wails/vibe-view/ wails://wails/', 'frame-src', "default-src 'self'"]) {
      document.head.innerHTML = ''
      meta(wide)
      expect(frameAllowed(document, 'wails://wails'), wide).toBe(false)
    }
  })

  it('every policy must narrow (a second, wider one does not widen, but does not pass either)', () => {
    meta('frame-src wails://wails/vibe-view/')
    meta("frame-src 'self'")
    expect(frameAllowed(document, 'wails://wails')).toBe(false)
  })

  it('accepts the dev meta with both dev origins', () => {
    meta('frame-src wails://wails.localhost:34115/vibe-view/ http://localhost:34115/vibe-view/')
    expect(frameAllowed(document, 'http://localhost:34115')).toBe(true)
    expect(frameAllowed(document, 'wails://wails.localhost:34115')).toBe(true)
  })

  it('reads only Content-Security-Policy metas', () => {
    const m = document.createElement('meta')
    m.setAttribute('http-equiv', 'refresh')
    m.setAttribute('content', 'frame-src wails://wails/vibe-view/')
    document.head.appendChild(m)
    expect(frameSources(document)).toEqual([])
  })
})
