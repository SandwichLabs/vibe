package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

var artifactViewCmd = &cobra.Command{
	Use:   "view",
	Short: "Allow or deny an artifact's custom (script) view",
	Long: `An artifact whose manifest says "view: script" draws itself with view.js,
code written by someone else or by an agent. Vibe shows it as source and only
runs it after you allow it (the view runs in a worker that cannot reach the
network or your files, and can only run this artifact's own queries):

  status   show where it stands, the view's hash and its source.
  allow    allow the view as shown. Any later change to artifact.yaml or
           view.js ends the allow and Vibe asks again. On a terminal it shows
           the source and asks; otherwise pass --hash and --yes from "status".
  deny     never run it, until revoked. Edits do not ask again.
  revoke   forget the decision (the next open asks again).

Your decision is kept in your own config directory (views.json), not in the
workspace folder, and never travels in a bundle. An agent cannot allow a view.`,
}

func printViewStatus(w io.Writer, st workspace.ArtifactViewStatus, withSource bool) {
	switch {
	case st.Error != "":
		fmt.Fprintf(w, "%s: cannot be used (%s)\n", st.Name, oneLine(st.Error))
		return
	case st.Kind != "script":
		fmt.Fprintf(w, "%s: view kind %q, nothing to allow\n", st.Name, st.Kind)
		return
	}
	switch st.State {
	case "allowed":
		fmt.Fprintf(w, "%s: allowed\n", st.Name)
	case "changed":
		fmt.Fprintf(w, "%s: changed since you allowed it; allow it again after reading it\n", st.Name)
	case "denied":
		fmt.Fprintf(w, "%s: denied\n", st.Name)
	default:
		fmt.Fprintf(w, "%s: not decided (the view does not run)\n", st.Name)
	}
	fmt.Fprintf(w, "  hash: %s\n", st.Hash)
	if withSource {
		fmt.Fprintf(w, "--- artifact.yaml\n%s\n--- view.js\n%s\n---\n", artifacts.EscapeHidden(strings.TrimRight(st.Manifest, "\n")), artifacts.EscapeHidden(strings.TrimRight(st.Source, "\n")))
	}
}

func newViewActionCmd(use, short string, run func(cmd *cobra.Command, ws workspace.Workspace, name string) error) *cobra.Command {
	c := &cobra.Command{
		Use:          use + " <name>",
		Short:        short,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openWS(cmd)
			if err != nil {
				return err
			}
			defer ws.Close() //nolint:errcheck
			return run(cmd, ws, args[0])
		},
	}
	addWorkspaceFlag(c)
	return c
}

// confirmViewAllow shows the source on stderr and asks, returning the hash the
// person was shown. Without a terminal the caller must pass the hash.
func confirmViewAllow(cmd *cobra.Command, ws workspace.Workspace, name string) (string, error) {
	if hash, _ := cmd.Flags().GetString("hash"); hash != "" {
		if yes, _ := cmd.Flags().GetBool("yes"); !yes {
			return "", errors.New("--hash also needs --yes, to say you read the source `vibe artifact view status " + name + "` showed")
		}
		return hash, nil
	}
	if !stdinIsTerminal() {
		return "", errors.New("not a terminal: read the view with `vibe artifact view status " + name + "`, then pass its hash with --hash --yes")
	}
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return "", err
	}
	if st.Hash == "" {
		return "", errors.New("this artifact has no script view to allow")
	}
	printViewStatus(os.Stderr, st, true)
	fmt.Fprint(os.Stderr, "This view runs code written by someone else or by an agent. Allow it? [y/N]: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		return "", errors.New("not allowed")
	}
	return st.Hash, nil
}

func init() {
	statusCmd := newViewActionCmd("status", "Show the view's state, hash and source", func(cmd *cobra.Command, ws workspace.Workspace, name string) error {
		st, err := ws.ArtifactViewState(name)
		if err != nil {
			return err
		}
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}
		printViewStatus(cmd.OutOrStdout(), st, true)
		return nil
	})
	statusCmd.Flags().Bool("json", false, "output as JSON")

	allowCmd := newViewActionCmd("allow", "Allow the view as shown (a later change asks again)", func(cmd *cobra.Command, ws workspace.Workspace, name string) error {
		hash, err := confirmViewAllow(cmd, ws, name)
		if err != nil {
			return err
		}
		if err := ws.AllowArtifactView(name, hash); err != nil {
			return err
		}
		return printViewAfter(cmd, ws, name)
	})
	allowCmd.Flags().String("hash", "", "the view hash shown by `status` (required without a terminal, with --yes)")
	allowCmd.Flags().Bool("yes", false, "with --hash: confirm you read the source that status showed")

	denyCmd := newViewActionCmd("deny", "Never run this view, until revoked", func(cmd *cobra.Command, ws workspace.Workspace, name string) error {
		if err := ws.DenyArtifactView(name); err != nil {
			return err
		}
		return printViewAfter(cmd, ws, name)
	})
	revokeCmd := newViewActionCmd("revoke", "Forget the decision about this view", func(cmd *cobra.Command, ws workspace.Workspace, name string) error {
		if err := ws.RevokeArtifactView(name); err != nil {
			return err
		}
		return printViewAfter(cmd, ws, name)
	})
	artifactViewCmd.AddCommand(statusCmd, allowCmd, denyCmd, revokeCmd)
	artifactCmd.AddCommand(artifactViewCmd)
}

func printViewAfter(cmd *cobra.Command, ws workspace.Workspace, name string) error {
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return err
	}
	printViewStatus(cmd.OutOrStdout(), st, false)
	return nil
}
