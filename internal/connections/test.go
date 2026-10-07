package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sandwich-labs/vibe-app/internal/secrets"
	"github.com/sandwich-labs/vibe-app/internal/session"
)

var (
	_ session.CredentialedType = postgresType{}
	_ session.CredentialedType = motherduckType{}
	_ session.CredentialedType = sqliteType{}
	_ session.CredentialedType = storageType{}
	_ session.CataloglessType  = storageType{}
	_ session.HubTokenType     = motherduckType{}
	_ session.Refresher        = postgresType{}
	_ session.CredentialedType = mysqlType{}
	_ session.Refresher        = mysqlType{}
	_ session.CredentialedType = azureType{}
	_ session.CataloglessType  = azureType{}
	_ session.CredentialedType = ducklakeType{}
	_ session.PostAttacher     = ducklakeType{}
	_ session.CredentialedType = tableSourceType{}
	_ session.CataloglessType  = tableSourceType{}
)

// TestTimeout bounds one Test (secret, attach and probe).
const TestTimeout = 10 * time.Second

// TestResult is what Test returns. Every string in it is redacted.
type TestResult struct {
	OK bool `json:"ok"`
	// Status is the connection status the attempt ended in: ok,
	// needs-credentials, extension-missing or error.
	Status session.StatusKind `json:"status"`
	// Latency covers secret, attach and probe.
	Latency time.Duration `json:"latency"`
	// Count is the table count (catalogs) or object count (storage scopes).
	Count  int    `json:"count"`
	Detail string `json:"detail,omitempty"`
	// Error is the redacted failure message; it never holds a credential.
	Error string `json:"error,omitempty"`
	// InstallHint names the extension the person can install and retry.
	InstallExtension string `json:"installExtension,omitempty"`
}

// TestOption tunes Test.
type TestOption func(*testOpts)

type testOpts struct {
	extDir   string
	install  bool
	exe      string
	timeout  time.Duration
	redactor *secrets.Redactor
}

// WithTestExtensionDir pins the extension directory (default: the app's).
func WithTestExtensionDir(dir string) TestOption { return func(o *testOpts) { o.extDir = dir } }

// WithInstall is the person's consent to download a missing extension.
func WithInstall(yes bool) TestOption { return func(o *testOpts) { o.install = yes } }

// WithExecutable sets the binary that runs the MotherDuck subprocess test
// (default os.Executable); it must answer to `__conn-test`.
func WithExecutable(path string) TestOption { return func(o *testOpts) { o.exe = path } }

// WithTestTimeout replaces the 10 s limit (tests).
func WithTestTimeout(d time.Duration) TestOption { return func(o *testOpts) { o.timeout = d } }

// WithTestRedactor adds a redactor that also scrubs the result.
func WithTestRedactor(r *secrets.Redactor) TestOption { return func(o *testOpts) { o.redactor = r } }

// Test attaches def with cred in a throwaway, hardened, in-memory DuckDB and
// probes it. It never touches the workspace's session. A MotherDuck definition
// is tested in a subprocess because a process holds one MotherDuck token.
func Test(ctx context.Context, def Def, cred secrets.Credential, opts ...TestOption) TestResult {
	o := testOpts{timeout: TestTimeout}
	for _, f := range opts {
		f(&o)
	}
	red := secrets.NewRedactor()
	red.AddCredential(cred)
	if o.redactor != nil {
		o.redactor.AddCredential(cred)
	}
	fail := func(kind session.StatusKind, err error) TestResult {
		msg := secrets.RedactPatterns(red.Redact(err.Error()))
		return TestResult{Status: kind, Error: msg}
	}
	if def.Type == "motherduck" {
		def.ReadOnly = false // MotherDuck has no read-only attach; a test never writes anyway
	}
	if err := Validate(def); err != nil {
		return fail(session.StatusError, err)
	}
	if def.Type == "ducklake" && def.Path != "" {
		// A test never writes, and a lake whose catalog file does not exist yet
		// can only be attached by creating it.
		if _, err := os.Stat(def.Path); errors.Is(err, os.ErrNotExist) {
			return TestResult{OK: true, Status: session.StatusOK, Detail: "a new lake: its catalog file is created the first time something writes to it"}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	if def.Type == "motherduck" {
		return testInSubprocess(ctx, def, cred, o, red)
	}
	return runTest(ctx, def, cred, o, red)
}

func extensionDir(o testOpts) string {
	if o.extDir != "" {
		return o.extDir
	}
	d, _ := session.DefaultDirs()
	return d
}

// installBeforeOpen downloads the type's extensions into the extension
// directory in a plain instance, before the hardened session opens.
func installBeforeOpen(ctx context.Context, def Def, o testOpts) error {
	t, ok := registry[def.Type]
	if !ok {
		return nil
	}
	ct, ok := t.(session.CredentialedType)
	if !ok {
		return nil
	}
	db, err := session.OpenDB("", session.WithExtensionDirectory(extensionDir(o)))
	if err != nil {
		return err
	}
	defer db.Close() //nolint:errcheck
	for _, e := range ct.Extensions() {
		// e comes from the registry, never from the definition.
		if _, err := db.ExecContext(ctx, "INSTALL "+e); err != nil {
			return fmt.Errorf("the DuckDB %q extension could not be downloaded; check your network connection and try again: %w", e, err)
		}
	}
	return nil
}

// runTest is the in-process part: used directly for every type but MotherDuck,
// and by the subprocess for MotherDuck.
func runTest(ctx context.Context, def Def, cred secrets.Credential, o testOpts, red *secrets.Redactor) TestResult {
	fail := func(kind session.StatusKind, err error, ext string) TestResult {
		return TestResult{Status: kind, Error: secrets.RedactPatterns(red.Redact(err.Error())), InstallExtension: ext}
	}
	if o.install {
		if err := installBeforeOpen(ctx, def, o); err != nil {
			return fail(session.StatusError, err, "")
		}
	}
	sopts := []session.Option{
		session.WithConnectionTypes(Types()...),
		session.WithExtensionDirectory(extensionDir(o)),
		session.WithRedactor(red),
	}
	if def.Type == "motherduck" {
		sopts = append(sopts, session.WithMotherDuckToken(cred.Token))
	}
	sess, err := session.New(sopts...)
	if err != nil {
		return fail(session.StatusError, err, "")
	}
	defer sess.Close() //nolint:errcheck

	spec := def.Spec()
	spec.ReadOnly = def.Type != "motherduck" // a test never writes (MotherDuck cannot attach read-only)
	c := cred
	spec.Credential = func(context.Context) (secrets.Credential, error) { return c, nil }
	start := time.Now()
	if err := sess.Attach(ctx, spec); err != nil {
		kind, msg := session.StatusError, err.Error()
		for _, st := range sess.Connections() {
			if st.Name == def.Name {
				kind = st.Kind
			}
		}
		ext := ""
		if kind == session.StatusExtensionMissing {
			if ci, e := TypeInfoFor(def.Type); e == nil && len(ci.Extensions) > 0 {
				ext = ci.Extensions[0]
			}
		}
		if ctx.Err() != nil {
			msg = fmt.Sprintf("timed out after %s", o.timeout)
		}
		return fail(kind, errors.New(msg), ext)
	}
	var (
		n      int
		detail string
	)
	if t, ok := registry[def.Type]; ok {
		n, detail, err = t.Probe(ctx, sess, spec)
	} else {
		n, detail, err = duckdbProbe(ctx, sess, spec)
	}
	if err != nil {
		msg := err.Error()
		if ctx.Err() != nil {
			msg = fmt.Sprintf("timed out after %s", o.timeout)
		}
		kind := session.StatusError
		if authFailure(err) {
			kind = session.StatusNeedsCredentials
		}
		return fail(kind, errors.New(msg), "")
	}
	return TestResult{OK: true, Status: session.StatusOK, Latency: time.Since(start), Count: n, Detail: detail}
}

// --- MotherDuck subprocess ---

// ChildCommand is the hidden command the subprocess test answers to.
const ChildCommand = "__conn-test"

type childRequest struct {
	Def         Def                `json:"def"`
	Cred        secrets.Credential `json:"cred"`
	Install     bool               `json:"install"`
	ExtensionDr string             `json:"extension_dir,omitempty"`
	TimeoutMS   int64              `json:"timeout_ms"`
}

// childEnv is the environment for the subprocess: everything but MotherDuck's
// ambient token variables, so the token it uses is the one it was handed.
func childEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		k := strings.ToLower(strings.SplitN(kv, "=", 2)[0])
		if k == "motherduck_token" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func testInSubprocess(ctx context.Context, def Def, cred secrets.Credential, o testOpts, red *secrets.Redactor) TestResult {
	fail := func(err error) TestResult {
		return TestResult{Status: session.StatusError, Error: secrets.RedactPatterns(red.Redact(err.Error()))}
	}
	if cred.Token == "" {
		return TestResult{Status: session.StatusNeedsCredentials, Error: "No MotherDuck token is set for this connection."}
	}
	exe := o.exe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return fail(fmt.Errorf("failed to find the vibe executable: %w", err))
		}
	}
	req, err := json.Marshal(childRequest{Def: def, Cred: cred, Install: o.install, ExtensionDr: o.extDir, TimeoutMS: o.timeout.Milliseconds()})
	if err != nil {
		return fail(err)
	}
	// The credential travels over stdin: never argv, never the environment.
	cmd := exec.CommandContext(ctx, exe, ChildCommand)
	cmd.Env = childEnv(os.Environ())
	cmd.WaitDelay = 500 * time.Millisecond // do not wait on grandchildren holding the pipes
	cmd.Stdin = bytes.NewReader(req)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil && stdout.Len() == 0 {
		if ctx.Err() != nil {
			return fail(fmt.Errorf("timed out after %s", o.timeout))
		}
		return fail(fmt.Errorf("the MotherDuck test process failed: %w", err))
	}
	// MotherDuck may write other text to stdout: the result is the last line.
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var res TestResult
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		return fail(errors.New("the MotherDuck test process gave no result"))
	}
	res.Error = secrets.RedactPatterns(red.Redact(res.Error))
	res.Detail = red.Redact(res.Detail)
	return res
}

// RunChild is the subprocess side of the MotherDuck test: it reads one JSON
// request from in, runs the test in this process (which holds exactly one
// MotherDuck token) and writes one JSON line to out. The caller is the hidden
// `vibe __conn-test` command.
func RunChild(ctx context.Context, in io.Reader, out io.Writer) error {
	var req childRequest
	if err := json.NewDecoder(in).Decode(&req); err != nil {
		return fmt.Errorf("failed to read the test request: %w", err)
	}
	to := time.Duration(req.TimeoutMS) * time.Millisecond
	if to <= 0 || to > 2*TestTimeout {
		to = TestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	red := secrets.NewRedactor()
	red.AddCredential(req.Cred)
	var res TestResult
	if req.Def.Type == "motherduck" {
		req.Def.ReadOnly = false
	}
	if err := Validate(req.Def); err != nil {
		res = TestResult{Status: session.StatusError, Error: red.Redact(err.Error())}
	} else if req.Def.Type != "motherduck" {
		res = TestResult{Status: session.StatusError, Error: "the subprocess test only handles MotherDuck"}
	} else {
		res = runTest(ctx, req.Def, req.Cred, testOpts{extDir: req.ExtensionDr, install: req.Install, timeout: to}, red)
	}
	return json.NewEncoder(out).Encode(res)
}

func duckdbProbe(ctx context.Context, sess session.Session, s session.Spec) (int, string, error) {
	var n int
	err := sess.DB().QueryRowContext(ctx, `SELECT count(*) FROM duckdb_tables() WHERE database_name = $1`, s.Name).Scan(&n)
	if err != nil {
		return 0, "", fmt.Errorf("attached, but listing tables failed: %w", err)
	}
	return n, fmt.Sprintf("%d tables", n), nil
}

// authFailure recognises a rejected key: S3-compatible stores answer 403 with
// InvalidAccessKeyId, SignatureDoesNotMatch or AccessDenied; Azure with
// AuthenticationFailed, AuthorizationFailure or an AADSTS error; MySQL with
// "Access denied for user".
func authFailure(err error) bool {
	m := strings.ToLower(err.Error())
	for _, k := range []string{"403", "forbidden", "invalidaccesskeyid", "signaturedoesnotmatch", "accessdenied", "access denied", "not authenticated", "authentication failed", "authenticationfailed", "authorizationfailure", "authorizationpermissionmismatch", "aadsts", "invalid_client"} {
		if strings.Contains(m, k) {
			return true
		}
	}
	return false
}
