// renderSafeMarkdown: the ONE markdown -> sanitized HTML renderer for every
// v-html sink that shows untrusted markdown — Skill bodies (model-authored
// via create_or_update_skill, or arriving inside a shared .vibe file from
// someone else) and agent chat output (useAgentChat.ts's renderMarkdown,
// which just delegates here — see the comment there). Never raw HTML, never
// a zero-click remote fetch, never a link that navigates the app away
// without the user choosing to.
//
// This uses an ALLOW-list (ALLOWED_TAGS/ALLOWED_ATTR), not a deny-list —
// deliberately, after multiple rounds of "add the tag/attr the reviewer
// just found" (script/style/iframe/object/embed/form/input/button/
// textarea/select/option, then area/map/video/audio/source/picture/track,
// each with its own zero-click-fetch attribute, then `class` itself —
// rendered content used `class="modal-overlay"` etc. to spoof the app's own
// UI, e.g. a fake full-screen "Session expired" dialog that swallowed every
// click). An allow-list means a tag or attribute markdown output has no
// legitimate reason to produce is excluded by construction, not by
// remembering to list it. `class` is NOT in ALLOWED_ATTR at all — the one
// narrow exception (a code fence's `language-xxx` tag, for a future syntax
// highlighter) is granted back, and ONLY on `<code>` with a value matching
// `^language-[\w-]+$`, by the uponSanitizeAttribute hook below, not by the
// static allow-list. `span` is not in ALLOWED_TAGS either — marked's own
// output never emits it, so its only purpose in rendered markdown is
// carrying a `class` for styling, i.e. spoofing — see spoof.md's payload,
// which used `<span class="card card-pad">`.
import { marked, Renderer } from 'marked'
import DOMPurify, { type Config } from 'dompurify'
import { BrowserOpenURL } from '../wailsjs/runtime/runtime'
import { askConfirm } from '../composables/useConfirm'

const ALLOWED_TAGS = [
  'p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'ul', 'ol', 'li',
  'a', 'code', 'pre',
  'em', 'strong', 'del',
  'blockquote', 'hr', 'br',
  'img', // src is further restricted to data: URIs by the hook below.
  // Table family.
  'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td', 'caption',
]

const CODE_LANGUAGE_CLASS_RE = /^language-[\w-]+$/

const SANITIZE_CONFIG: Config = {
  ALLOWED_TAGS,
  // src is further restricted to data: URIs only by the afterSanitizeAttributes
  // hook below — ALLOWED_ATTR has no "value must match this pattern" mode,
  // so the scheme check has to happen post-sanitize. `class` is
  // deliberately absent here; see the uponSanitizeAttribute hook.
  ALLOWED_ATTR: ['href', 'src'],
}

// A remote <img> is a zero-click way for rendered content to phone home the
// moment it's shown — no click required, unlike a link. Only a data: URI
// (the image bytes themselves, inline) is allowed; anything else is
// removed outright rather than left as a broken-image placeholder, so no
// empty box is left behind (a stripped element leaves nothing, not a gap —
// CSS has nothing to collapse because there's no element there to begin
// with). ALLOWED_TAGS already excludes every OTHER zero-click-fetch element
// (video/audio/source/picture/object/embed/svg/map/area/table[background]),
// but img legitimately needs to stay (inline diagrams etc.) with just its
// src constrained — hence the extra hook rather than an ALLOWED_ATTR rule
// (DOMPurify's ALLOWED_ATTR has no "value must match this pattern" mode).
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName?.toLowerCase() !== 'img') return
  const src = node.getAttribute('src') || ''
  if (!src.toLowerCase().startsWith('data:')) {
    node.remove()
  }
})

// The ONE narrow exception to "no class attribute anywhere": a fenced code
// block's language tag, so a future syntax highlighter has something to key
// off. Only `<code>` (never `<pre>`, never anything else), only a value
// shaped like `language-xxx`. Everything else named `class` is stripped
// regardless of tag or value — this is what closes spoof.md's
// `class="modal-overlay"` / `class="btn btn-primary"` / `class="card
// card-pad"` payload.
DOMPurify.addHook('uponSanitizeAttribute', (node, data) => {
  if (data.attrName !== 'class') return
  if (node.tagName?.toLowerCase() === 'code' && CODE_LANGUAGE_CLASS_RE.test(data.attrValue)) {
    data.keepAttr = true
    data.forceKeepAttr = true // required: `class` is not in ALLOWED_ATTR.
    return
  }
  data.keepAttr = false
})

// Task-list checkboxes (`- [ ] foo` / `- [x] foo`, GFM syntax marked
// supports) render as `<input type="checkbox" disabled>` by default — a
// form-control tag that's excluded from ALLOWED_TAGS on purpose (see the
// file header). Left alone, that would silently delete every task-list
// item's checked/unchecked indicator, not just the input element. Override
// marked's checkbox rendering to emit plain ☐/☑ text instead, so the state
// stays visible through sanitization.
const renderer = new Renderer()
renderer.checkbox = ({ checked }) => (checked ? '☑' : '☐')

export function renderSafeMarkdown(text: string): string {
  const raw = marked.parse(text ?? '', { async: false, renderer }) as string
  return DOMPurify.sanitize(raw, SANITIZE_CONFIG)
}

// vibeMdContentClass marks a v-html container as rendered-markdown content
// so installSafeLinkHandling knows to intercept clicks inside it — normal
// app chrome (sidebar links, router-links, buttons) is never touched.
// Add this class alongside any v-html="renderSafeMarkdown(...)" container.
export const vibeMdContentClass = 'vibe-md-content'

let installed = false

// handleLinkActivation is shared by the click and auxclick (middle-click)
// listeners below.
async function handleLinkActivation(e: MouseEvent) {
  const target = e.target as HTMLElement | null
  // <area> (an image-map hotspot inside <map>) is a real link too, but
  // Element.closest('a') never matches it — it has to be asked for
  // explicitly. ALLOWED_TAGS already excludes <map>/<area> entirely, so in
  // practice this branch is unreachable post-sanitization; it stays as a
  // second, independent layer in case that ever changes, and because the
  // click handler doesn't know whether the markup it's attached to always
  // went through renderSafeMarkdown.
  const anchor = target?.closest?.('a, area')
  if (!anchor || !anchor.closest(`.${vibeMdContentClass}`)) return
  e.preventDefault()
  e.stopPropagation()
  const href = anchor.getAttribute('href') || ''
  if (!/^https?:\/\//i.test(href.trim())) return // drop every other scheme (and a leading-space trick) silently.
  const ok = await askConfirm({
    title: 'Open link',
    message: `Open this link in your browser? ${href}`,
    variant: 'primary', // opening a link is a normal action, not a destructive one.
  })
  if (ok) {
    BrowserOpenURL(href)
  }
}

// installSafeLinkHandling installs ONE pair of document-level listeners
// (call once, at app startup — see main.ts) that intercept every link
// activation — a normal click AND a middle-click (auxclick), which opens a
// link in a new tab/window in a real browser without ever calling into the
// click handler — inside a .vibe-md-content container: an http(s) link is
// only opened after the user confirms (via the project's own SchoolHouse
// ConfirmDialog, never the native window.confirm — see
// composables/useConfirm.ts), through the Wails runtime's BrowserOpenURL
// (so it opens in the user's real browser, never inside — and never
// replacing — the Studio window); every other scheme (javascript:, file:,
// a bare "#", a relative app path someone crafted to look like navigation,
// a leading-space-obfuscated URL, etc.) is simply dropped. Before this, a
// rendered skill's or agent reply's link could navigate the whole Studio
// window away from the app with a single click — there was no separate
// browser tab to lose, only this one window.
export function installSafeLinkHandling(): void {
  if (installed) return
  installed = true
  document.addEventListener('click', handleLinkActivation, true)
  document.addEventListener('auxclick', handleLinkActivation, true)
}
