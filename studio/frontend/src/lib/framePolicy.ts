// The frame policy check of a static HTML page (docs/plans/html-artifacts.md, H3).
// A page is shown in a sandbox="" iframe, which is safe only because Studio's own
// page forbids every other frame navigation with a meta CSP narrowed to the view
// server's route (frame-src <origin>/slaw-view/, emitted at build time per origin:
// scripts/frame-csp.mjs). Before a page is opened this checks that the policy is
// really there and names this window's origin, and refuses otherwise: no policy
// means an outside link in the page could navigate its frame (the isolation the
// page relies on is missing), and a different origin means the frame would not load.

/** Why a page is not shown when the window's frame policy does not allow it. */
export const FRAME_POLICY_TEXT = "This page can't be shown in this window: Studio's frame policy does not allow pages from this window's address. Rebuild Studio, or open it from the app."

/** The frame-src sources of every Content-Security-Policy meta in doc. */
export function frameSources(doc: Document): string[][] {
  const out: string[][] = []
  for (const meta of Array.from(doc.querySelectorAll('meta[http-equiv]'))) {
    if ((meta.getAttribute('http-equiv') ?? '').toLowerCase() !== 'content-security-policy') continue
    for (const directive of (meta.getAttribute('content') ?? '').split(';')) {
      const [name, ...sources] = directive.trim().split(/\s+/)
      if (name?.toLowerCase() === 'frame-src') out.push(sources)
    }
  }
  return out
}

/**
 * True only when a frame-src policy exists and every such policy allows exactly
 * the view server's route on `origin` (a policy that also allows 'self', a
 * scheme or a wildcard does not narrow anything, so it is refused).
 */
export function frameAllowed(doc: Document, origin: string): boolean {
  const policies = frameSources(doc)
  const route = `${origin}/slaw-view/`
  return policies.length > 0 && policies.every((sources) => sources.length > 0 && sources.every((s) => s.endsWith('/slaw-view/')) && sources.includes(route))
}
