package workspace

// Test-only helpers: kept out of the production build.

// OpenFrom discovers the workspace containing start and opens it.
func OpenFrom(start string, opts ...Option) (Workspace, error) {
	root, err := Discover(start)
	if err != nil {
		return nil, err
	}
	return Open(root, opts...)
}
