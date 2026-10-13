package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/mod/semver"
)

// maxNotesRunes bounds the release notes handed to a UI or terminal.
const maxNotesRunes = 20000

// tagRE is the only shape of release tag accepted anywhere (also what reaches
// install.sh's argv): v1.2.3 with an optional prerelease.
var tagRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

// Result is what a check found.
type Result struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"updateAvailable"`
	// Skipped: Latest is the version the person chose to skip. UpdateAvailable is
	// still true; callers that check unprompted stay quiet about it.
	Skipped    bool   `json:"skipped"`
	ReleaseURL string `json:"releaseUrl"`
	// Notes is untrusted plain text (the release body); render it as text only.
	Notes string `json:"notes"`
	// Managed is ManagedInstaller or ManagedExternal; Updatable is true only for the former.
	Managed   string `json:"managed"`
	Updatable bool   `json:"updatable"`
	// ManagedNote explains an external install (empty otherwise).
	ManagedNote string `json:"managedNote"`
	CheckedAt   string `json:"checkedAt"` // RFC 3339
}

// normalizeVersion returns v in semver's "vX.Y.Z" spelling, or "" when it is not one.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && v[0] >= '0' && v[0] <= '9' {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	return v
}

// ValidTag reports whether tag is an acceptable release tag.
func ValidTag(tag string) bool { return tagRE.MatchString(tag) && semver.IsValid(tag) }

func (u *Updater) releaseURL(tag string) string {
	return u.webBase + "/" + Repo + "/releases/tag/" + tag
}

// Check asks GitHub for the latest stable release and compares it with the
// running version. skipped is the version the person chose to skip, if any.
// It sends only plain GET requests (see the package doc).
func (u *Updater) Check(ctx context.Context, skipped string) (Result, error) {
	tag, err := u.latestTag(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("failed to look up the latest release: %w", err)
	}
	r := Result{
		Current:    u.current,
		Latest:     tag,
		ReleaseURL: u.releaseURL(tag),
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if cur := normalizeVersion(u.current); cur != "" {
		r.UpdateAvailable = semver.Compare(tag, cur) > 0
	}
	r.Skipped = r.UpdateAvailable && skipped != "" && skipped == tag
	if _, err := u.Detect(); err == nil {
		r.Managed, r.Updatable = ManagedInstaller, true
	} else {
		r.Managed, r.ManagedNote = ManagedExternal, ExternalNote
	}
	if r.UpdateAvailable {
		notes, htmlURL := u.releaseNotes(ctx, tag)
		r.Notes = notes
		if htmlURL != "" {
			r.ReleaseURL = htmlURL
		}
	}
	return r, nil
}

// latestTag reads the tag from the redirect of releases/latest (no API needed).
func (u *Updater) latestTag(ctx context.Context) (string, error) {
	resp, _, err := u.get(ctx, u.webBase+"/"+Repo+"/releases/latest", 4<<10, checkTimeout, false)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", fmt.Errorf("unexpected status %d (no redirect to a release)", resp.StatusCode)
	}
	loc, err := resp.Location()
	if err != nil {
		return "", fmt.Errorf("the redirect has no Location: %w", err)
	}
	return u.tagFromLocation(loc)
}

// tagFromLocation extracts the tag from .../SandwichLabs/vibe/releases/tag/<tag>.
func (u *Updater) tagFromLocation(loc *url.URL) (string, error) {
	web, err := url.Parse(u.webBase)
	if err != nil || loc.Host != web.Host {
		return "", fmt.Errorf("the redirect points at %q, not %q", loc.Host, web.Host)
	}
	prefix := "/" + Repo + "/releases/tag/"
	if len(loc.Path) <= len(prefix) || !strings.EqualFold(loc.Path[:len(prefix)], prefix) {
		return "", fmt.Errorf("the redirect is not a release page")
	}
	tag := loc.Path[len(prefix):]
	if !ValidTag(tag) {
		return "", fmt.Errorf("%w: %q", ErrBadTag, tag)
	}
	return tag, nil
}

// releaseNotes is best effort: any failure yields no notes and no URL.
func (u *Updater) releaseNotes(ctx context.Context, tag string) (notes, htmlURL string) {
	body, err := u.getOK(ctx, u.apiBase+"/repos/"+Repo+"/releases/tags/"+tag, maxNotesBytes, checkTimeout, "api.github.com")
	if err != nil {
		return "", ""
	}
	var rel struct {
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	}
	if json.Unmarshal(body, &rel) != nil {
		return "", ""
	}
	return sanitizeText(rel.Body, maxNotesRunes), u.safeReleaseURL(rel.HTMLURL)
}

// safeReleaseURL accepts the API's html_url only when it is a page of this repo.
func (u *Updater) safeReleaseURL(raw string) string {
	p, err := u.checkURL(raw)
	if err != nil {
		return ""
	}
	web, err := url.Parse(u.webBase)
	if err != nil || p.Host != web.Host || !strings.HasPrefix(strings.ToLower(p.Path), strings.ToLower("/"+Repo+"/")) {
		return ""
	}
	return p.String()
}

// sanitizeText makes untrusted text safe to print or render as plain text: it
// drops control characters (terminal escapes) other than newline and tab,
// normalises line endings and truncates to max runes.
func sanitizeText(s string, max int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r == utf8.RuneError || (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			continue
		}
		if n >= max {
			b.WriteString("\n…")
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
