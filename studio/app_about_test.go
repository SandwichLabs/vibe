package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetBuildInfoUsesStampedValues(t *testing.T) {
	oldV, oldC, oldD := version, commit, date
	t.Cleanup(func() { version, commit, date = oldV, oldC, oldD })
	version, commit, date = "v9.9.9", "deadbee", "2026-10-12T00:00:00Z"

	got := (&App{}).GetBuildInfo()
	assert.Equal(t, "v9.9.9", got.Version)
	assert.Equal(t, "deadbee", got.Commit)
	assert.Equal(t, "2026-10-12T00:00:00Z", got.Date)
	assert.NotEmpty(t, got.GoVersion)
	assert.NotEmpty(t, got.DuckDBVersion)
	assert.Contains(t, got.Platform, "/")
}

func TestGetThirdPartyNoticesIsEmbedded(t *testing.T) {
	got := (&App{}).GetThirdPartyNotices()
	assert.Contains(t, got, "# Third-party notices")
	assert.Contains(t, got, "motherduck")
}
