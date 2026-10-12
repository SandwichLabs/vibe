package main

import (
	vibe "github.com/SandwichLabs/vibe"
	"github.com/SandwichLabs/vibe/internal/buildinfo"
)

// --- About (Settings > About) ---

// GetBuildInfo returns the version, commit and build date stamped into this binary by ldflags
// (the same values `vibe --version` prints), plus the Go and DuckDB versions and the platform.
func (a *App) GetBuildInfo() buildinfo.Info {
	return buildinfo.Get(version, commit, date)
}

// GetThirdPartyNotices returns THIRD_PARTY_NOTICES.md, embedded at build time, for the notices viewer.
func (a *App) GetThirdPartyNotices() string {
	return vibe.ThirdPartyNotices
}
