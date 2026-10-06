package session

import (
	"context"
	"database/sql"
	"errors"
)

// Mock is a Session for tests of packages that depend on one. Set the *Fn
// fields you need; an unset method returns the zero value or an error saying
// it is not mocked. Catalog defaults to "mock".
type Mock struct {
	Catalog string
	DBFn    func() *sql.DB

	QueryFn         func(ctx context.Context, query string, args []any, opts []QueryOption) (*Rows, error)
	ExecFn          func(ctx context.Context, query string, args []any, opts []QueryOption) (sql.Result, error)
	ConnectionsFn   func() []Status
	ListCatalogsFn  func(ctx context.Context) ([]CatalogInfo, error)
	ListSchemasFn   func(ctx context.Context, catalog string) ([]string, error)
	ListTablesFn    func(ctx context.Context, catalog, schema string) ([]TableInfo, error)
	DescribeTableFn func(ctx context.Context, qualified string) (*TableDescription, error)
	AttachFn        func(ctx context.Context, spec Spec) error
	DetachFn        func(ctx context.Context, name string) error
}

var _ Session = (*Mock)(nil)

var errNotMocked = errors.New("session mock: method not mocked")

func (m *Mock) DefaultCatalog() string {
	if m.Catalog == "" {
		return "mock"
	}
	return m.Catalog
}

func (m *Mock) SetDefaultCatalog(_ context.Context, name string) error {
	m.Catalog = name
	return nil
}

func (m *Mock) DefaultSchema(context.Context, string) (string, error) { return "main", nil }

func (m *Mock) Resolve(_ context.Context, ref TableRef) (TableRef, error) {
	if ref.Catalog == "" {
		ref.Catalog = m.DefaultCatalog()
	}
	if ref.Schema == "" {
		ref.Schema = "main"
	}
	return ref, nil
}

func (m *Mock) DB() *sql.DB {
	if m.DBFn != nil {
		return m.DBFn()
	}
	return nil
}

func (m *Mock) Query(ctx context.Context, q string, args ...any) (*Rows, error) {
	return m.QueryWith(ctx, q, args)
}

func (m *Mock) QueryWith(ctx context.Context, q string, args []any, opts ...QueryOption) (*Rows, error) {
	if m.QueryFn == nil {
		return nil, errNotMocked
	}
	return m.QueryFn(ctx, q, args, opts)
}

func (m *Mock) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return m.ExecWith(ctx, q, args)
}

func (m *Mock) ExecWith(ctx context.Context, q string, args []any, opts ...QueryOption) (sql.Result, error) {
	if m.ExecFn == nil {
		return nil, errNotMocked
	}
	return m.ExecFn(ctx, q, args, opts)
}

func (m *Mock) Conn(context.Context, ...QueryOption) (*Conn, error) { return nil, errNotMocked }
func (m *Mock) Register(Spec) error                                 { return nil }
func (m *Mock) Connect(context.Context, string) error               { return nil }

func (m *Mock) Attach(ctx context.Context, s Spec) error {
	if m.AttachFn != nil {
		return m.AttachFn(ctx, s)
	}
	return nil
}

func (m *Mock) Detach(ctx context.Context, n string) error {
	if m.DetachFn != nil {
		return m.DetachFn(ctx, n)
	}
	return nil
}

func (m *Mock) Connections() []Status {
	if m.ConnectionsFn != nil {
		return m.ConnectionsFn()
	}
	return []Status{{Name: m.DefaultCatalog(), Type: "duckdb", Kind: StatusOK, Default: true}}
}

func (m *Mock) ListCatalogs(ctx context.Context) ([]CatalogInfo, error) {
	if m.ListCatalogsFn != nil {
		return m.ListCatalogsFn(ctx)
	}
	return []CatalogInfo{{Name: m.DefaultCatalog(), Type: "duckdb", Default: true}}, nil
}

func (m *Mock) ListSchemas(ctx context.Context, c string) ([]string, error) {
	if m.ListSchemasFn != nil {
		return m.ListSchemasFn(ctx, c)
	}
	return []string{"main"}, nil
}

func (m *Mock) ListTables(ctx context.Context, c, s string) ([]TableInfo, error) {
	if m.ListTablesFn != nil {
		return m.ListTablesFn(ctx, c, s)
	}
	return nil, nil
}

func (m *Mock) DescribeTable(ctx context.Context, q string) (*TableDescription, error) {
	if m.DescribeTableFn != nil {
		return m.DescribeTableFn(ctx, q)
	}
	return nil, errNotMocked
}

func (m *Mock) InvalidateSchema(string) {}
func (m *Mock) Close() error            { return nil }
