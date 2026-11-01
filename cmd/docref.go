package cmd

import (
	"regexp"
)

// releaseTag matches a release version: 0.5.0 as release.yml stamps it (the
// tag without its v), v0.5.0, or a release candidate such as 0.5.0-rc3 (rc tags
// stay published). -test tags and `git describe` versions (v0.5.0-dirty,
// v0.5.0-3-gabc) are not tags that last, so they point at main.
var releaseTag = regexp.MustCompile(`^v?(\d+\.\d+\.\d+(?:-rc\d+)?)$`)

// docRef is the public URL of a docs/reference page ("semantics" for
// docs/reference/semantics.md), so --help can name docs that exist only in a
// checkout. A release build points at its own tag; a development build
// (version "dev", empty, or anything else) points at main.
func docRef(page string) string {
	return docRefAt(version, page)
}

func docRefAt(ver, page string) string {
	ref := "main"
	if m := releaseTag.FindStringSubmatch(ver); m != nil {
		ref = "v" + m[1]
	}
	return "https://github.com/SandwichLabs/assay/blob/" + ref + "/docs/reference/" + page + ".md"
}
