package html

// P3.10 (plan §3.4 / mockup #11): a single shared SchoolHouse-aligned component
// CSS block, injected exactly once into rendered report/page output.
//
// Previously each band/page renderer carried its own ad-hoc <style> snippet,
// which meant duplicated (and drifting) styling whenever output spanned multiple
// bands or pages. This block consolidates the SchoolHouse design tokens + shared
// component styling (report bands, tables, metrics, charts) into one place and
// guards re-injection via a sentinel marker so multi-band / multi-page output
// still carries it exactly once.

import "strings"

// componentCSSMarker is the sentinel used to (a) detect whether the shared CSS
// has already been injected into a given output and (b) let tests assert the
// block appears exactly once. It MUST stay in sync with the comment in
// SharedComponentCSS.
const componentCSSMarker = "vibe-component-css:v1"

// SharedComponentCSS is the single SchoolHouse-aligned component stylesheet
// shared across banded reports and rendered pages. It opens with the sentinel
// marker comment so InjectComponentCSSOnce can detect prior injection and tests
// can assert single-injection.
const SharedComponentCSS = `<style data-vibe-component-css="v1">
/* ` + componentCSSMarker + ` */
:root {
  --sh-ink: #1F2330;
  --sh-paper: #FAF7F0;
  --sh-surface: #FFFFFF;
  --sh-teal: #2F8F87;
  --sh-marigold: #F2A93B;
  --sh-coral: #E5604A;
  --sh-border: #1F2330;
  --font-display: 'Fraunces', Georgia, serif;
  --font-body: 'Inter', system-ui, sans-serif;
  --font-mono: 'JetBrains Mono', ui-monospace, monospace;
}
.vibe-report { font-family: var(--font-body); color: var(--sh-ink); }
.vibe-report-title { font-family: var(--font-display); }
.vibe-report-header { font-weight: 700; }
.vibe-report-detail { display: flex; gap: 1rem; }
.vibe-report [data-field] { font-variant-numeric: tabular-nums; }
.vibe-report [class*="group-header"] { font-family: var(--font-mono); font-weight: 600; color: var(--sh-teal); }
.vibe-report [class*="group-footer"], .vibe-report [class*="report-footer"] { font-weight: 600; }
.vibe-report [class*="report-empty"] { padding: 1rem; color: #888; font-style: italic; }
</style>`

// InjectComponentCSSOnce prepends SharedComponentCSS to out unless out already
// contains the component-CSS sentinel marker, guaranteeing the shared block
// appears exactly once even when callers concatenate multiple rendered
// fragments (multi-band / multi-page output).
func InjectComponentCSSOnce(out string) string {
	if strings.Contains(out, componentCSSMarker) {
		return out
	}
	return SharedComponentCSS + "\n" + out
}
