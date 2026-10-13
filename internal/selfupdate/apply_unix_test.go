//go:build unix

package selfupdate

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A cancelled install signals the script's whole process group: the foreground
// child (install.sh's curl, here a sleep) dies too, so the script's trap runs.
func TestApply_CancelSignalsTheScriptsProcessGroup(t *testing.T) {
	r := newApplyRig(t, true)
	r.script = []byte(`#!/bin/sh
trap 'touch "$OUT/trapped"; exit 1' TERM
sleep 30
`)
	r.publish()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.updater().Apply(ctx, "v0.1.2")
	require.Error(t, err)
	assert.Less(t, time.Since(start), 4*time.Second, "the sleep child was signalled, not waited out")
	assert.FileExists(t, filepath.Join(r.out, "trapped"), "install.sh's TERM trap ran")
}
