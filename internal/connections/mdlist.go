package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

const childActionListDatabases = "list-databases"

// MDDatabase is one database a MotherDuck token can attach.
type MDDatabase struct {
	// Name is the alias to use as the connection's database.
	Name string `json:"name"`
	// Share is true for a database shared with the account by someone else.
	Share bool `json:"share,omitempty"`
}

// MDDatabasesResult is what ListMotherDuckDatabases returns. Every string in
// it is redacted.
type MDDatabasesResult struct {
	OK        bool               `json:"ok"`
	Status    session.StatusKind `json:"status"`
	Databases []MDDatabase       `json:"databases,omitempty"`
	Error     string             `json:"error,omitempty"`
	// InstallExtension names the extension the person can install and retry.
	InstallExtension string `json:"installExtension,omitempty"`
	// TimedOut is set when the deadline, not a driver message, ended the call.
	TimedOut bool `json:"timedOut,omitempty"`
}

// ListMotherDuckDatabases lists the databases (and shares) the token can
// attach. Like Test it runs in a subprocess, because a process holds one
// MotherDuck token; the token travels on stdin only.
func ListMotherDuckDatabases(ctx context.Context, cred secrets.Credential, opts ...TestOption) MDDatabasesResult {
	o := testOpts{timeout: TestTimeout}
	for _, f := range opts {
		f(&o)
	}
	red := secrets.NewRedactor()
	red.AddCredential(cred)
	if o.redactor != nil {
		o.redactor.AddCredential(cred)
	}
	fail := func(kind session.StatusKind, msg string) MDDatabasesResult {
		return MDDatabasesResult{Status: kind, Error: secrets.RedactPatterns(red.Redact(msg))}
	}
	if cred.Token == "" {
		return fail(session.StatusNeedsCredentials, "No MotherDuck token is set for this connection.")
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	line, err := runChildProcess(ctx, childRequest{Action: childActionListDatabases, Cred: cred, Install: o.install, ExtensionDr: o.extDir, TimeoutMS: o.timeout.Milliseconds()}, o)
	if err != nil {
		res := fail(session.StatusError, err.Error())
		res.TimedOut = ctx.Err() != nil
		return res
	}
	var res MDDatabasesResult
	if err := json.Unmarshal(line, &res); err != nil {
		return fail(session.StatusError, "the MotherDuck process gave no result")
	}
	res.Error = secrets.RedactPatterns(red.Redact(res.Error))
	for i := range res.Databases {
		res.Databases[i].Name = red.Redact(res.Databases[i].Name)
	}
	return res
}

// runListChild is the subprocess side of ListMotherDuckDatabases.
func runListChild(ctx context.Context, req childRequest, out io.Writer) error {
	to := time.Duration(req.TimeoutMS) * time.Millisecond
	if to <= 0 || to > 2*TestTimeout {
		to = TestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	red := secrets.NewRedactor()
	red.AddCredential(req.Cred)
	res := listDatabases(ctx, req, red, to)
	return json.NewEncoder(out).Encode(res)
}

func listDatabases(ctx context.Context, req childRequest, red *secrets.Redactor, to time.Duration) MDDatabasesResult {
	fail := func(kind session.StatusKind, err error, ext string) MDDatabasesResult {
		msg := err.Error()
		timedOut := ctx.Err() != nil
		if timedOut {
			msg = fmt.Sprintf("timed out after %s", to)
		}
		return MDDatabasesResult{Status: kind, Error: secrets.RedactPatterns(red.Redact(msg)), InstallExtension: ext, TimedOut: timedOut}
	}
	o := testOpts{extDir: req.ExtensionDr, install: req.Install, timeout: to}
	if o.install {
		if err := installBeforeOpen(ctx, Def{Type: "motherduck"}, o); err != nil {
			return fail(session.StatusError, err, "")
		}
	}
	sess, err := session.New(
		session.WithConnectionTypes(Types()...),
		session.WithExtensionDirectory(extensionDir(o)),
		session.WithRedactor(red),
		session.WithMotherDuckToken(req.Cred.Token),
	)
	if err != nil {
		return fail(session.StatusError, err, "")
	}
	defer sess.Close() //nolint:errcheck
	var loaded int
	if err := sess.DB().QueryRowContext(ctx, `SELECT count(*) FROM duckdb_extensions() WHERE extension_name = 'motherduck' AND loaded`).Scan(&loaded); err != nil {
		return fail(session.StatusError, err, "")
	}
	if loaded == 0 {
		return fail(session.StatusExtensionMissing, errors.New("the MotherDuck extension is not installed"), "motherduck")
	}
	// Only MotherDuck's own databases and shares: the hub's in-memory catalog is
	// not something a connection can name.
	rows, err := sess.DB().QueryContext(ctx, `SELECT alias, type FROM MD_ALL_DATABASES() WHERE type LIKE 'motherduck%' ORDER BY alias`)
	if err != nil {
		kind := session.StatusError
		if authFailure(err) {
			kind = session.StatusNeedsCredentials
		}
		return fail(kind, err, "")
	}
	defer rows.Close() //nolint:errcheck
	var dbs []MDDatabase
	for rows.Next() {
		var alias, typ string
		if err := rows.Scan(&alias, &typ); err != nil {
			return fail(session.StatusError, err, "")
		}
		// Validate's rule: a name outside it could not be saved as a connection.
		if !mdDBRE.MatchString(alias) {
			continue
		}
		dbs = append(dbs, MDDatabase{Name: alias, Share: typ == "motherduck share"})
	}
	if err := rows.Err(); err != nil {
		return fail(session.StatusError, err, "")
	}
	sort.Slice(dbs, func(i, j int) bool { return dbs[i].Name < dbs[j].Name })
	return MDDatabasesResult{OK: true, Status: session.StatusOK, Databases: dbs}
}
