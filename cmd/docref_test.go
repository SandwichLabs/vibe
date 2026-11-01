package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestDocRefAt(t *testing.T) {
	const base = "https://github.com/SandwichLabs/assay/blob/"
	assert.Equal(t, base+"v0.5.0/docs/reference/semantics.md", docRefAt("v0.5.0", "semantics"))
	assert.Equal(t, base+"v0.5.0/docs/reference/semantics.md", docRefAt("0.5.0", "semantics"), "release.yml stamps the tag without its v")
	assert.Equal(t, base+"v0.5.0-rc3/docs/reference/semantics.md", docRefAt("0.5.0-rc3", "semantics"))
	for _, v := range []string{"dev", "", "v0.5", "0.5.0-test1", "v0.5.0-dirty", "v0.5.0-3-gabc1234", "v0.5.0-dirty+x", "v0.5.0 "} {
		assert.Equal(t, base+"main/docs/reference/semantics.md", docRefAt(v, "semantics"), v)
	}
}

// A path that exists only in a checkout is no help to someone with a binary.
func TestHelpTextHasNoBareDocsPath(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, text := range []string{c.Short, c.Long} {
			for _, line := range strings.Split(text, "\n") {
				if i := strings.Index(line, "docs/reference/"); i >= 0 {
					assert.True(t, strings.Contains(line[:i], "https://"), "%s: bare docs path in %q", c.CommandPath(), line)
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}
