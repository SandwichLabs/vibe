package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/SandwichLabs/slaw/internal/secrets"
)

// ErrNotSSOSignIn is returned for a connection that does not sign in with an
// AWS SSO profile.
var ErrNotSSOSignIn = errors.New("this connection does not sign in with an AWS profile")

// SSOProfile returns the AWS profile the named connection signs in with, or
// *UnknownConnectionError / ErrNotSSOSignIn.
func SSOProfile(ws Workspace, name string) (string, error) {
	c, ok := ws.Connection(name)
	if !ok {
		return "", &UnknownConnectionError{Name: name}
	}
	kind, profile, err := secrets.ParseMethod(c.SignIn)
	if err != nil || kind != secrets.MethodAWSSSO {
		return "", fmt.Errorf("%s: %w", name, ErrNotSSOSignIn)
	}
	return profile, nil
}

// SSOConnections lists the connections that sign in with the AWS profile.
func SSOConnections(ws Workspace, profile string) []string {
	var out []string
	for _, c := range ws.Connections() {
		if kind, p, err := secrets.ParseMethod(c.SignIn); err == nil && kind == secrets.MethodAWSSSO && p == profile {
			out = append(out, c.Name)
		}
	}
	return out
}

// DeviceSignIn signs the person in to the AWS profile the named connection uses
// (the device flow), blocking until they approve, it fails or ctx ends. It is
// the only way a device flow starts: Studio's StartSignIn and `slaw conn
// credentials sign-in` call it; nothing that merely resolves a credential does.
// show gets the prompt once; it must not block.
func DeviceSignIn(ctx context.Context, ws Workspace, name string, show func(secrets.DevicePrompt)) (profile string, err error) {
	profile, err = SSOProfile(ws, name)
	if err != nil {
		return "", err
	}
	return profile, ws.Resolver().DeviceSignIn(ctx, secrets.MethodAWSSSOPref+profile, show)
}
