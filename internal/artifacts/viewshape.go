package artifacts

import "github.com/SandwichLabs/assay/internal/a2ui"

// CheckViewShape checks an A2UI view (m.View a2ui; view already passed
// ValidateView) against the result columns of the manifest's queries: a pivot
// or drill must name a column the query returns, and a chart's query must
// return the columns the chart reads. columns maps a query name to its result
// column names in order; a query not in the map is skipped. Each problem names
// the component, the bad name and the columns available. A script or html view
// has no A2UI page to check and returns nil.
func CheckViewShape(m *Manifest, view []byte, columns map[string][]string) []string {
	if m.View == ViewScript || m.View == ViewHTML || len(columns) == 0 {
		return nil
	}
	page, err := validatedPage(m, view)
	if err != nil {
		return nil
	}
	return a2ui.CheckViewColumns(page, columns)
}
