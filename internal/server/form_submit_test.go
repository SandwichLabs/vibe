package server

// TDD RED — Phase 1 / unit P1.3: "Form submission pipeline -> InsertRow/UpdateRow
// with on_submit modes" (parity plan §1.1).
//
// This is a REAL integration test: it stands up the internal HTTP server backed
// by a real encrypted .vibe container (SQLCipher) + a real DuckDB engine, defines
// a form bound to a `customers` table, and drives the production
// POST /page/{id}/submit/{formID} endpoint end-to-end. No mocking of the engine
// or container.
//
// It asserts the acceptance behavior:
//   - a new-record POST (no bound PK value) INSERTs a customers row through the
//     transactional CRUD layer, and the response honors on_submit (redirect:home
//     => 3xx to /page/home)
//   - a POST carrying the hidden PK with changed values UPDATEs that row
//     (no new row inserted; value actually changed)
//   - a POST with a required field blank re-renders the form (HTTP 200) showing
//     the per-field error text and preserving the previously entered values,
//     with NO row written (no partial write)
//   - on_submit: noop writes the row but returns an empty/ack response (no redirect)

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const formTestPassphrase = "form-test-passphrase-12345"

// stubFormRenderer is a minimal PageRenderer test double. The container + engine
// are REAL; only the page renderer is stubbed so the test can stand alone of the
// full markdown render pipeline. It records what it was asked to render so we can
// confirm the handler re-renders the page (rather than redirecting) on a
// validation failure.
type stubFormRenderer struct {
	lastPageID string
	lastParams map[string]string
}

func (r *stubFormRenderer) RenderPageHTML(pageID string, params map[string]string) (string, error) {
	r.lastPageID = pageID
	r.lastParams = params
	return fmt.Sprintf("<page id=%q>rendered</page>", pageID), nil
}

// newFormTestServer builds a real .vibe container + DuckDB engine, creates a
// customers table, registers a bound form in _vibe_components, and returns a
// running server plus its base URL. The form's on_submit mode is configurable.
func newFormTestServer(t *testing.T, onSubmit string) (*Server, *container.Container, string, string) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	dir := t.TempDir()
	path := filepath.Join(dir, "form.vibe")
	c, err := container.Create(path,
		container.WithPassphrase(formTestPassphrase),
		container.WithLogger(logger),
		container.WithTitle("Form Test"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb)
	_, err = ddb.Exec(`CREATE TABLE customers (
		id INTEGER PRIMARY KEY,
		name VARCHAR,
		email VARCHAR
	);`)
	require.NoError(t, err, "create customers table")

	// A page to host the form, and a bound form definition. The form binds to the
	// customers table, declares id as the (hidden) primary key, marks name as
	// required, and carries the configured on_submit mode. The handler must
	// resolve this binding and route writes through the CRUD layer — NOT execute
	// arbitrary SQL.
	const pageID = "home"
	const formID = "cust_form"
	sdb := c.SQLiteDB()
	_, err = sdb.Exec(
		`INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at)
		 VALUES (?, ?, ?, 'form', 0, datetime('now'), datetime('now'));`,
		pageID, "Customers", "")
	require.NoError(t, err)

	formConfig := fmt.Sprintf(`{
		"table": "customers",
		"pk": "id",
		"on_submit": %q,
		"fields": [
			{"name": "id", "type": "hidden"},
			{"name": "name", "label": "Name", "required": true},
			{"name": "email", "label": "Email"}
		]
	}`, onSubmit)
	_, err = sdb.Exec(
		`INSERT INTO _vibe_components (id, type, config, created_at)
		 VALUES (?, 'form', ?, datetime('now'));`,
		formID, formConfig)
	require.NoError(t, err)

	eng := engine.NewManager(ddb, engine.WithLogger(logger))

	renderer := &stubFormRenderer{}
	s := New(
		WithAddress("127.0.0.1:0"),
		WithLogger(logger),
		WithContainer(c),
		WithEngine(eng),
		WithRenderer(renderer),
	)
	require.NoError(t, s.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5e9)
		defer cancel()
		_ = s.Stop(ctx)
	})

	base := "http://" + s.Addr()
	return s, c, base, fmt.Sprintf("%s/page/%s/submit/%s", base, pageID, formID)
}

// noRedirectClient returns an *http.Client that does NOT follow redirects, so we
// can assert on the 3xx status + Location of the on_submit:redirect path.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func countCustomers(t *testing.T, c *container.Container) int {
	t.Helper()
	var n int
	require.NoError(t, c.DuckDBDB().QueryRow(`SELECT COUNT(*) FROM customers;`).Scan(&n))
	return n
}

// TestFormSubmitInsertUpdate drives the full form POST pipeline: insert, update,
// validation re-render, and the on_submit modes, all against a real container.
func TestFormSubmitInsertUpdate(t *testing.T) {
	_, c, _, submitURL := newFormTestServer(t, "redirect:home")
	client := noRedirectClient()

	// --- (1) INSERT: new record (no PK value) -> row written; redirect honored ---
	insert := url.Values{
		"id":    {""}, // hidden PK present but empty => INSERT
		"name":  {"Acme Corp"},
		"email": {"buyer@acme.test"},
	}
	resp, err := client.PostForm(submitURL, insert)
	require.NoError(t, err)
	body := readBody(t, resp)

	assert.True(t, resp.StatusCode >= 300 && resp.StatusCode < 400,
		"on_submit redirect:home must produce a 3xx redirect, got %d (%s)", resp.StatusCode, body)
	assert.Contains(t, resp.Header.Get("Location"), "/page/home",
		"redirect must target the configured page")

	require.Equal(t, 1, countCustomers(t, c), "exactly one customers row inserted")

	var (
		insertedID    int
		insertedName  string
		insertedEmail string
	)
	require.NoError(t, c.DuckDBDB().
		QueryRow(`SELECT id, name, email FROM customers LIMIT 1;`).
		Scan(&insertedID, &insertedName, &insertedEmail))
	assert.Equal(t, "Acme Corp", insertedName)
	assert.Equal(t, "buyer@acme.test", insertedEmail)

	// --- (2) UPDATE: same PK present with a changed value -> UPDATE, not INSERT ---
	update := url.Values{
		"id":    {fmt.Sprintf("%d", insertedID)}, // hidden PK present => UPDATE
		"name":  {"Acme Corporation"},
		"email": {"buyer@acme.test"},
	}
	resp, err = client.PostForm(submitURL, update)
	require.NoError(t, err)
	_ = readBody(t, resp)

	assert.Equal(t, 1, countCustomers(t, c), "update must not insert a new row")
	var updatedName string
	require.NoError(t, c.DuckDBDB().
		QueryRow(`SELECT name FROM customers WHERE id = ?;`, insertedID).
		Scan(&updatedName))
	assert.Equal(t, "Acme Corporation", updatedName, "the bound row must be updated in place")

	// --- (3) VALIDATION FAILURE: required name blank -> 200 re-render, no write ---
	bad := url.Values{
		"id":    {""},
		"name":  {""}, // required -> must fail
		"email": {"keepme@acme.test"},
	}
	resp, err = client.PostForm(submitURL, bad)
	require.NoError(t, err)
	body = readBody(t, resp)

	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"validation failure must re-render the form (200), not redirect")
	assert.Equal(t, 1, countCustomers(t, c), "no row may be written on validation failure")
	// Per-field error feedback + preserved input must appear in the re-rendered form.
	assert.Contains(t, strings.ToLower(body), "name",
		"re-rendered form must surface a per-field error referencing the offending field")
	assert.Contains(t, body, "keepme@acme.test",
		"re-rendered form must preserve the previously entered values")
}

// TestFormSubmitNoop verifies on_submit:noop writes the row but does NOT redirect
// and returns an empty/ack response.
func TestFormSubmitNoop(t *testing.T) {
	_, c, _, submitURL := newFormTestServer(t, "noop")
	client := noRedirectClient()

	v := url.Values{
		"id":    {""},
		"name":  {"Noop Inc"},
		"email": {"noop@acme.test"},
	}
	resp, err := client.PostForm(submitURL, v)
	require.NoError(t, err)
	_ = readBody(t, resp)

	require.Equal(t, 1, countCustomers(t, c), "noop mode must still write the row")
	assert.Less(t, resp.StatusCode, 300,
		"noop mode must NOT redirect (expected 2xx ack), got %d", resp.StatusCode)
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}
