package vibe

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmbeddedLegalTextsAreShipped(t *testing.T) {
	assert.Contains(t, License, "Apache License")
	assert.Contains(t, Notice, "Sandwich Labs")
	assert.Contains(t, ThirdPartyNotices, "# Third-party notices")
	assert.Contains(t, ThirdPartyNotices, "duckdb-go")
	assert.Contains(t, ThirdPartyNotices, "motherduck")
	assert.Contains(t, ThirdPartyNotices, "Fraunces")
	assert.Greater(t, len(ThirdPartyNotices), 100_000)
}
