//go:build !ssofake

package main

import "github.com/SandwichLabs/slaw/internal/workspace"

// ssoDevOptions is nothing in every real build: AWS sign-in talks to AWS only.
// The `ssofake` build tag (task studio:dev:ssofake, ssofake.go) is the one
// place that points it at local fakes.
func ssoDevOptions() []workspace.ResolverOption { return nil }

// ssoDevOpenURL is nil in every real build: the sign-in page opens through the
// Wails runtime.
func ssoDevOpenURL() func(string) { return nil }
