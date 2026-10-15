package connections

import "sort"

// DuckLakeMetadataTables lists the fixed tables of the catalog allow-list,
// sorted, for the drift test.
func DuckLakeMetadataTables() []string {
	out := make([]string, 0, len(ducklakeMetadataTables))
	for n := range ducklakeMetadataTables {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SetMaxCatalogBytes lowers the snapshot size cap; it returns the restore func.
func SetMaxCatalogBytes(n int64) func() {
	old := maxCatalogBytes
	maxCatalogBytes = n
	return func() { maxCatalogBytes = old }
}
