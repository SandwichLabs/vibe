package cmd

import (
	"path/filepath"
	"strings"
)

// tableNameFromCSVPath derives a table name from a data file path (used as
// the default --table for `vibe import` and `vibe init --import`).
func tableNameFromCSVPath(csvPath string) string {
	base := filepath.Base(csvPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	// Replace non-alphanumeric chars with underscores.
	var sb strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			sb.WriteRune(c)
		} else {
			sb.WriteRune('_')
		}
	}
	result := sb.String()
	if result == "" {
		return "imported"
	}
	return result
}
