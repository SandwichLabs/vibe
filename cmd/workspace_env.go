package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/spf13/cobra"
)

var workspaceEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "Allow or deny loading the workspace's .env file",
	Long: `A workspace can keep connection credentials in a git-ignored .env file
(for example MOTHERDUCK_TOKEN for a connection that signs in with
"env:MOTHERDUCK"). Vibe only loads it after you allow it, like direnv:

  allow    load this .env as it is now. Any later change to the file ends the
           approval and Vibe asks again.
  deny     never load it, until revoked. Edits do not ask again.
  revoke   forget the decision (the next open asks again).
  status   show where it stands: variable names and the env: sign-ins they
           would satisfy, never values.

The file is read as plain KEY=value lines (export, quotes and comments are
understood; nothing is executed). Its values are used only for this workspace's
env: sign-ins, never put in the process environment. Your decision is kept in
your own config directory, not in the workspace folder. Commands that open a
workspace ask once on a terminal; without one they use only an existing allow.`,
}

func newEnvActionCmd(use, short string, run func(cmd *cobra.Command, ws workspace.Workspace) error) *cobra.Command {
	c := &cobra.Command{
		Use:          use,
		Short:        short,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := openWS(cmd)
			if err != nil {
				return err
			}
			defer ws.Close() //nolint:errcheck
			return run(cmd, ws)
		},
	}
	addWorkspaceFlag(c)
	return c
}

func printDotEnvStatus(w io.Writer, st workspace.DotEnvStatus) {
	switch st.State {
	case "none":
		if st.Ignored != "" {
			fmt.Fprintf(w, ".env: ignored (%s)\n", st.Ignored)
		} else {
			fmt.Fprintln(w, ".env: none in this workspace")
		}
		return
	case "allowed":
		fmt.Fprintf(w, ".env: allowed (%s)\n", st.Path)
	case "changed":
		fmt.Fprintf(w, ".env: changed since you allowed it (%s)\n", st.Path)
		if st.Loaded {
			fmt.Fprintln(w, "  the values from before the change stay loaded in a running Studio until you allow it again")
		}
	case "denied":
		fmt.Fprintf(w, ".env: denied (%s)\n", st.Path)
	default:
		fmt.Fprintf(w, ".env: not decided (%s)\n", st.Path)
	}
	fmt.Fprintf(w, "  variables: %s\n", orNone(strings.Join(st.Names, ", ")))
	for _, s := range st.Satisfies {
		fmt.Fprintf(w, "  feeds connection %s (%s): %s\n", s.Connection, s.SignIn, strings.Join(s.Variables, ", "))
	}
	for _, x := range st.Warnings {
		fmt.Fprintf(w, "  warning: %s\n", x)
	}
	if st.TrackedByGit {
		fmt.Fprintln(w, "  warning: git tracks this .env, so a credential is committed. Remove it from git (git rm --cached .env) and rotate the credential.")
	}
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func init() {
	statusCmd := newEnvActionCmd("status", "Show the .env file's state and the variable names it sets", func(cmd *cobra.Command, ws workspace.Workspace) error {
		st := ws.DotEnv()
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}
		printDotEnvStatus(cmd.OutOrStdout(), st)
		return nil
	})
	statusCmd.Flags().Bool("json", false, "output as JSON (names only, never values)")

	allowCmd := newEnvActionCmd("allow", "Load this .env as it is now (a later change asks again)", func(cmd *cobra.Command, ws workspace.Workspace) error {
		if err := ws.AllowDotEnv(); err != nil {
			return err
		}
		printDotEnvStatus(cmd.OutOrStdout(), ws.DotEnv())
		return nil
	})
	denyCmd := newEnvActionCmd("deny", "Never load this .env, until revoked", func(cmd *cobra.Command, ws workspace.Workspace) error {
		if err := ws.DenyDotEnv(); err != nil {
			return err
		}
		printDotEnvStatus(cmd.OutOrStdout(), ws.DotEnv())
		return nil
	})
	revokeCmd := newEnvActionCmd("revoke", "Forget the decision about this .env", func(cmd *cobra.Command, ws workspace.Workspace) error {
		if err := ws.RevokeDotEnv(); err != nil {
			return err
		}
		printDotEnvStatus(cmd.OutOrStdout(), ws.DotEnv())
		return nil
	})
	workspaceEnvCmd.AddCommand(statusCmd, allowCmd, denyCmd, revokeCmd)
	workspaceCmd.AddCommand(workspaceEnvCmd)
}

// maybePromptDotEnv asks, on a terminal, whether to load a workspace's .env
// that is undecided or changed. Without a terminal it never prompts: only an
// existing allow applies. "Not now" (anything but allow or deny) asks again
// next time. It prints names only.
func maybePromptDotEnv(ws workspace.Workspace, _ io.Writer) {
	if suppressDotEnvPrompt || dotenvPrompted || !stdinIsTerminal() {
		return
	}
	st := ws.DotEnv()
	if st.State != "undecided" && st.State != "changed" {
		return
	}
	dotenvPrompted = true
	// Always on the real stderr: some callers hand in io.Discard, and a prompt
	// nobody can see would block on stdin silently.
	askDotEnv(ws, st, os.Stdin, os.Stderr)
}

// dotenvPrompted limits the question to once per process; suppressDotEnvPrompt
// turns it off for probes that open a workspace on the person's behalf.
var dotenvPrompted, suppressDotEnvPrompt bool

func askDotEnv(ws workspace.Workspace, st workspace.DotEnvStatus, in io.Reader, errW io.Writer) {
	if st.State == "changed" {
		fmt.Fprintf(errW, "This workspace's .env changed since you allowed it (%s).\n", st.Path)
	} else {
		fmt.Fprintf(errW, "This workspace has a .env file (%s).\n", st.Path)
	}
	fmt.Fprintf(errW, "  variables: %s\n", orNone(strings.Join(st.Names, ", ")))
	for _, s := range st.Satisfies {
		fmt.Fprintf(errW, "  would feed connection %s (%s): %s\n", s.Connection, s.SignIn, strings.Join(s.Variables, ", "))
	}
	if st.TrackedByGit {
		fmt.Fprintln(errW, "  warning: git tracks this .env, so a credential is committed. Remove it from git and rotate the credential.")
	}
	fmt.Fprint(errW, "Load it for this workspace? [a]llow / [d]eny / [N]ot now: ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "a", "allow":
		if err := ws.AllowDotEnv(); err != nil {
			fmt.Fprintln(errW, "warning:", err)
		}
	case "d", "deny":
		if err := ws.DenyDotEnv(); err != nil {
			fmt.Fprintln(errW, "warning:", err)
		}
	}
}
