package bundle

import "context"

// MockService is a Service for tests of callers (Studio, the CLI).
type MockService struct {
	ExportFunc func(ctx context.Context, t Target, opts ExportOptions) (*ExportResult, error)
	OpenFunc   func(data []byte, passphrase string) (*Package, error)
	PeekFunc   func(data []byte) (Info, error)
}

var _ Service = (*MockService)(nil)

// Export implements Service.
func (m *MockService) Export(ctx context.Context, t Target, opts ExportOptions) (*ExportResult, error) {
	if m.ExportFunc != nil {
		return m.ExportFunc(ctx, t, opts)
	}
	return &ExportResult{}, nil
}

// Open implements Service.
func (m *MockService) Open(data []byte, passphrase string) (*Package, error) {
	if m.OpenFunc != nil {
		return m.OpenFunc(data, passphrase)
	}
	return &Package{}, nil
}

// Peek implements Service.
func (m *MockService) Peek(data []byte) (Info, error) {
	if m.PeekFunc != nil {
		return m.PeekFunc(data)
	}
	return Info{}, nil
}
