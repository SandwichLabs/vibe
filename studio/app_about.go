package main

import (
	assay "github.com/SandwichLabs/assay"
	"github.com/SandwichLabs/assay/internal/buildinfo"
)

// --- About (Settings > About) ---

// GetBuildInfo returns the version, commit and build date stamped into this binary by ldflags
// (the same values `assay --version` prints), plus the Go and DuckDB versions and the platform.
func (a *App) GetBuildInfo() buildinfo.Info {
	return buildinfo.Get(version, commit, date)
}

// GetThirdPartyNotices returns THIRD_PARTY_NOTICES.md, embedded at build time, for the notices viewer.
func (a *App) GetThirdPartyNotices() string {
	return assay.ThirdPartyNotices
}
