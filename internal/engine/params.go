package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// paramNameRe matches valid parameter names: alphanumeric plus underscore.
var paramNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// paramPlaceholderRe matches $param_name placeholders in SQL.
var paramPlaceholderRe = regexp.MustCompile(`\$([a-zA-Z0-9_]+)`)

// ValidateParamName checks that a parameter name contains only
// alphanumeric characters and underscores.
func ValidateParamName(name string) error {
	if !paramNameRe.MatchString(name) {
		return fmt.Errorf("invalid parameter name %q: must be alphanumeric and underscores only", name)
	}
	return nil
}

// BindParams replaces $param_name placeholders in the SQL string with
// positional ? parameters and returns the transformed SQL along with an
// ordered args slice. It validates that all parameter names are safe and
// that all referenced parameters exist in the provided map.
func BindParams(sql string, params map[string]any) (string, []any, error) {
	// Validate all param names in the map.
	for name := range params {
		if err := ValidateParamName(name); err != nil {
			return "", nil, fmt.Errorf("failed to validate parameter: %w", err)
		}
	}

	// Find all placeholders in the SQL.
	matches := paramPlaceholderRe.FindAllStringSubmatchIndex(sql, -1)
	if len(matches) == 0 {
		return sql, nil, nil
	}

	// Collect unique parameter names in order of first appearance, then
	// build the positional mapping. Each unique param gets one ? position,
	// but the same param may appear multiple times.
	type occurrence struct {
		start, end int
		name       string
	}

	var occurrences []occurrence
	for _, loc := range matches {
		name := sql[loc[2]:loc[3]]
		occurrences = append(occurrences, occurrence{
			start: loc[0],
			end:   loc[1],
			name:  name,
		})
	}

	// Sort occurrences by position (they should already be, but be explicit).
	sort.Slice(occurrences, func(i, j int) bool {
		return occurrences[i].start < occurrences[j].start
	})

	// Validate referenced params exist and build args.
	var args []any
	var result strings.Builder
	prev := 0

	for _, occ := range occurrences {
		if err := ValidateParamName(occ.name); err != nil {
			return "", nil, fmt.Errorf("failed to validate placeholder: %w", err)
		}

		val, ok := params[occ.name]
		if !ok {
			return "", nil, fmt.Errorf("missing parameter %q referenced in query", occ.name)
		}

		result.WriteString(sql[prev:occ.start])
		result.WriteString("?")
		args = append(args, val)
		prev = occ.end
	}

	result.WriteString(sql[prev:])
	return result.String(), args, nil
}
