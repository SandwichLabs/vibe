package cmd

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/spf13/cobra"
)

var connCredsSignInCmd = &cobra.Command{
	Use:   "sign-in <name>",
	Short: "Sign in to the AWS profile an S3 connection uses (prints a page and a code; Ctrl-C cancels)",
	Long: `Run the AWS IAM Identity Center sign-in for the connection's profile: Vibe asks
AWS for a device code, prints the page to open and the code that page must show,
and waits (until the code expires) for you to approve it in a browser. It opens
no browser and starts no other program. The session is saved where the AWS CLI
keeps it (~/.aws/sso/cache), so "aws sso login --profile PROFILE" is the same
thing and each sees the other's session. Nothing secret is printed or stored by
Vibe itself.

Only connections that sign in with aws-sso:PROFILE (see "conn credentials set
--sign-in") have this; others use "conn credentials set".`,
	Args:         cobra.ExactArgs(1),
	RunE:         runConnCredsSignIn,
	SilenceUsage: true,
}

// ssoTestKey carries the resolver options a test hands the command through its
// context (a loopback fake in place of AWS). Production never sets it.
type ssoTestKey struct{}

// ssoTestOptions are the resolver options the command's context carries.
func ssoTestOptions(ctx context.Context) []workspace.ResolverOption {
	o, _ := ctx.Value(ssoTestKey{}).([]workspace.ResolverOption)
	return o
}

func runConnCredsSignIn(cmd *cobra.Command, args []string) error {
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close() //nolint:errcheck
	profile, err := workspace.SSOProfile(ws, args[0])
	if errors.Is(err, workspace.ErrNotSSOSignIn) {
		return fmt.Errorf("%w; it signs in with its keychain, the environment or a prompt: use `vibe conn credentials set %s`", err, args[0])
	}
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmdCtx(cmd), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	out := cmd.OutOrStdout()
	_, err = workspace.DeviceSignIn(ctx, ws, args[0], func(p secrets.DevicePrompt) {
		fmt.Fprintf(out, "To sign in to AWS profile %s, open this page:\n\n  %s\n\nand check that it shows the code:\n\n  %s\n\n", profile, p.URL, p.Code)
		fmt.Fprintf(out, "Waiting for you to approve it (the code expires at %s). Ctrl-C cancels.\n", p.ExpiresAt.Local().Format(time.TimeOnly))
	})
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New("sign-in cancelled")
	case err != nil:
		return errors.New(cliRedactor.Redact(err.Error()))
	}
	fmt.Fprintf(out, "Signed in to AWS profile %s. %s reads with it from now on; a running Vibe Studio or `vibe mcp serve` notices on its next statement.\n", profile, args[0])
	return nil
}
