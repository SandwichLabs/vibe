package awssso

import (
	"errors"
	"net/url"
	"strings"
)

var errBadVerificationURL = errors.New("AWS returned a sign-in page address Vibe will not open")

// checkVerificationURL accepts only https URLs with a lower-case ASCII host, no
// user info, no port, no punycode label and no opaque form, whose host is one of
// the pages Identity Center serves the device page from, derived from the
// profile: device.sso.<region>.amazonaws.com (.amazonaws.com.cn in China
// regions), *.awsapps.com,
// *.awsapps.cn, and *.portal.<region>.app.aws. Generic AWS suffixes
// (amazonaws.com, *.aws, .on.aws) are not accepted: anyone can host a page
// there. testHost (the host:port of a test OIDC fake, empty in production) is
// also accepted, over http or https.
func checkVerificationURL(raw string, prof Profile, testHost string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" {
		return errBadVerificationURL
	}
	if testHost != "" && u.Host == testHost && (u.Scheme == "http" || u.Scheme == "https") {
		return nil
	}
	if u.Scheme != "https" || u.Port() != "" || !lexicallySafe(u.Host) || !hostAllowed(u.Host, prof) {
		return errBadVerificationURL
	}
	return nil
}

func lexicallySafe(host string) bool {
	for _, r := range host {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '.' {
			return false
		}
	}
	for _, l := range strings.Split(host, ".") {
		if l == "" || strings.HasPrefix(l, "xn--") || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return false
		}
	}
	return true
}

func hostAllowed(host string, prof Profile) bool {
	apex := "amazonaws.com"
	if strings.HasPrefix(prof.Region, "cn-") {
		apex = "amazonaws.com.cn"
	}
	if host == "device.sso."+prof.Region+"."+apex {
		return true
	}
	for _, s := range []string{".awsapps.com", ".awsapps.cn"} {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return strings.HasSuffix(host, ".portal."+prof.Region+".app.aws")
}
