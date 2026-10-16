package connections

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPresets_OnlyS3_AndOnlyTheThreeOptions(t *testing.T) {
	for _, name := range AllTypeNames() {
		ti, err := TypeInfoFor(name)
		require.NoError(t, err)
		if name != "s3" {
			assert.Empty(t, ti.Presets, name)
			continue
		}
		require.Len(t, ti.Presets, 5)
		for _, p := range ti.Presets {
			for k := range p.Options {
				assert.Contains(t, []string{"endpoint", "url_style", "use_ssl"}, k, "preset %s", p.ID)
			}
		}
	}
}

func TestPresetOptions(t *testing.T) {
	o, err := PresetOptions("floci", "")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"endpoint": "localhost:4566", "url_style": "path", "use_ssl": "false"}, o)

	o, err = PresetOptions("b2", "us-west-004")
	require.NoError(t, err)
	assert.Equal(t, "s3.us-west-004.backblazeb2.com", o["endpoint"])
	assert.Equal(t, "vhost", o["url_style"])
	assert.True(t, endptRE.MatchString(o["endpoint"]))

	_, err = PresetOptions("b2", "")
	require.Error(t, err)
	for _, bad := range []string{"a/b", "x.evil.com#", "us west", "a?b", "<region>", "a@evil"} {
		_, err = PresetOptions("b2", bad)
		assert.Error(t, err, bad)
	}
	_, err = PresetOptions("nope", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aws, minio, b2, floci, other")
}

// A returned preset is a copy: editing it never changes the registry.
func TestPresets_AreCopies(t *testing.T) {
	ti, _ := TypeInfoFor("s3")
	ti.Presets[1].Options["endpoint"] = "evil"
	again, _ := TypeInfoFor("s3")
	assert.Equal(t, "localhost:9000", again.Presets[1].Options["endpoint"])
	// An unreplaced placeholder is not a valid endpoint.
	assert.False(t, endptRE.MatchString("s3.<region>.backblazeb2.com"))
}
