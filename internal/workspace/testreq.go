package workspace

import (
	"context"
	"errors"

	"github.com/SandwichLabs/slaw/internal/connections"
	"github.com/SandwichLabs/slaw/internal/secrets"
	"github.com/SandwichLabs/slaw/internal/session"
)

// TestWithRequirements is the one Test of a connection, for Studio and `slaw
// conn test`: it finds rc's credential (TestCredential) and, for a lake or table
// read with storage connections (rc.Requires), each storage connection's own
// saved credential by its own sign-in, with the same server-changed guard, and
// runs connections.Test with them. The storage part is tried first and a
// failure there is the result's Requirement; the lake or table is then read for
// real through the storage secret, never anonymously and never with a scope but
// the storage connection's own.
//
// The returned error is rc's own credential problem. A *secrets.ErrFingerprintChanged
// means no test ran (the zero result). Any other error means rc needs a
// credential nobody supplied: the result is then that failure and nothing was
// dialled. opts are connections.TestOptions (install consent, redactor).
func TestWithRequirements(ctx context.Context, ws Workspace, rc ResolvedConnection, signIn string, typed secrets.Credential, opts ...connections.TestOption) (connections.TestResult, error) {
	cred, credErr := TestCredential(ctx, ws, rc, signIn, typed)
	var fp *secrets.ErrFingerprintChanged
	if errors.As(credErr, &fp) {
		return connections.TestResult{}, credErr
	}
	if credErr != nil {
		return needsCredentials(rc, credErr), credErr
	}
	for _, name := range rc.Requires {
		opt, blocked := requiredStorageOption(ctx, ws, name)
		if blocked != nil {
			return *blocked, nil
		}
		opts = append(opts, opt)
	}
	// The test never reads a read-only flag a person might have off.
	return connections.Test(ctx, rc.Def(true), cred, opts...), nil
}

// needsCredentials is the result of a connection that needs a credential none
// was found for: worded like Test words a turned-down sign-in.
func needsCredentials(rc ResolvedConnection, err error) connections.TestResult {
	raw := secrets.RedactPatterns(err.Error())
	res := connections.TestResult{Status: session.StatusNeedsCredentials, Error: raw}
	res.Code, res.Message = connections.Classify(connections.Failure{Raw: raw, Status: res.Status, Def: rc.Def(true)})
	// A sign-in that is not a typed key (an AWS SSO profile) has its own,
	// already plain, wording: "the service turned down these keys" would be wrong.
	var nc *secrets.ErrNeedsCredentials
	var mf *secrets.ErrMethodFailed
	if (errors.As(err, &nc) && nc.SignIn != nil) || errors.As(err, &mf) {
		res.Message = raw
	}
	return res
}

// requiredStorageOption resolves the saved storage connection name and its saved
// credential. blocked is the result when it cannot be used: not defined, not
// resolvable, or its saved sign-in no longer matches its definition (a pull
// changed the server: the credential is withheld and the lake is not attempted).
func requiredStorageOption(ctx context.Context, ws Workspace, name string) (opt connections.TestOption, blocked *connections.TestResult) {
	block := func(status session.StatusKind, code, msg, raw string) *connections.TestResult {
		return &connections.TestResult{Status: status, Requirement: name, Code: code, Message: msg, Error: raw}
	}
	c, ok := ws.Connection(name)
	if !ok {
		return nil, block(session.StatusError, connections.CodeInvalid, "This connection is not defined in the workspace.", "it is not defined")
	}
	rc, err := ResolveConnection(ws, c, nil)
	if err != nil {
		return nil, block(session.StatusError, connections.CodeInvalid, "This connection's definition is not valid.", secrets.RedactPatterns(err.Error()))
	}
	// Its own saved sign-in only: nothing typed in the lake's form applies to it.
	cred, err := TestCredential(ctx, ws, rc, "", secrets.Credential{})
	var fp *secrets.ErrFingerprintChanged
	if errors.As(err, &fp) {
		return nil, block(session.StatusNeedsCredentials, connections.CodeServerChanged, connections.ServerChangedMessage, "")
	}
	return connections.WithRequiredStorage(rc.Def(true), cred), nil
}
