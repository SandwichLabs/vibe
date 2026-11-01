//go:build ssofake

package main

// The ssofake dev build (task studio:dev:ssofake): AWS sign-in is pointed at a
// local fake OIDC server and portal (tools/ssofake) so the sign-in can be driven
// end to end without an AWS account. It exists only under the `ssofake` build
// tag; scripts/check-no-ssofake.sh keeps that tag out of every release and CI
// build. The endpoints come from two variables read once, and only loopback
// URLs are accepted (awssso.WithTestEndpoints refuses the rest). It also lifts
// the AWS-endpoint rule, so a connection can point at a local MinIO.

import (
	"fmt"
	"os"

	"github.com/SandwichLabs/assay/internal/workspace"
)

func ssoDevOptions() []workspace.ResolverOption {
	// A stamped (release) build never runs the fakes, whatever tags it was built with.
	if version != "dev" {
		fmt.Fprintln(os.Stderr, "ssofake build: refusing to point AWS sign-in at fakes in a stamped build")
		return nil
	}
	env := workspace.SnapshotEnv()
	oidc, portal := env("ASSAY_SSOFAKE_OIDC"), env("ASSAY_SSOFAKE_PORTAL")
	if oidc == "" || portal == "" {
		return nil
	}
	fmt.Fprintf(os.Stderr, "ssofake build: AWS sign-in goes to local fakes (oidc %s, portal %s)\n", oidc, portal)
	return []workspace.ResolverOption{workspace.WithSSOTestEndpoints(oidc, portal)}
}

// ssoDevOpenURL does not open a browser: the fake's page is approved from a
// script, and the dev machine's own browser must not be launched for it.
func ssoDevOpenURL() func(string) {
	if version != "dev" {
		return nil
	}
	return func(u string) { fmt.Fprintf(os.Stderr, "ssofake build: would open the sign-in page %s\n", u) }
}
