package pages

import (
	"fmt"
	"strings"
)

// ResolveLinks replaces page link references in content with resolved URLs.
// Page links use the format [[page_name]] and are resolved against the provided
// pages map (name -> URL).
func ResolveLinks(content string, pages map[string]string) string {
	result := content
	for name, url := range pages {
		placeholder := "[[" + name + "]]"
		link := fmt.Sprintf("[%s](%s)", name, url)
		result = strings.ReplaceAll(result, placeholder, link)
	}
	return result
}

// ValidateParams checks that all required parameters defined in the page's
// frontmatter are present in the provided params map.
func ValidateParams(page *Page, provided map[string]any) error {
	if provided == nil {
		provided = make(map[string]any)
	}

	var missing []string
	for _, p := range page.Frontmatter.Params {
		if !p.Required {
			continue
		}
		if _, ok := provided[p.Name]; !ok {
			// Check if there's a default value.
			if p.Default != "" {
				continue
			}
			missing = append(missing, p.Name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required parameters: %s", strings.Join(missing, ", "))
	}
	return nil
}
