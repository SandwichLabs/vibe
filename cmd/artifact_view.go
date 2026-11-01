package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/config"
	"github.com/SandwichLabs/slaw/internal/llm"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

var artifactViewCmd = &cobra.Command{
	Use:   "view",
	Short: "Allow or deny an artifact's custom view or HTML page",
	Long: `An artifact whose manifest says "view: script" draws itself with view.js,
code written by someone else or by an agent. Slaw shows it as source and only
runs it after you allow it (the view runs in a worker that cannot reach the
network or your files, and can only run this artifact's own queries):

  status   show where it stands, the view's hash and its source.
  allow    allow the view as shown. Any later change to artifact.yaml or
           view.js ends the allow and Slaw asks again. On a terminal it shows
           the source and asks; otherwise pass --hash and --yes from "status".
  deny     never run it, until revoked. Edits do not ask again.
  revoke   forget the decision (the next open asks again).

A view or page whose queries call metric() also lists the metrics it reads, and
the metric files and saved queries behind them are part of its hash: changing
one asks again.

An HTML page (view: html) goes through the same commands: status lists its
kind (static or scripted), the queries it uses with their connections, the
capabilities it declares (state, download, ask) in plain words, every file and
its source. No command opens or runs a page: it opens from Studio.

Your decision is kept in your own config directory (views.json), not in the
workspace folder, and never travels in a bundle. An agent cannot allow a view.`,
}

func printViewStatus(w io.Writer, st workspace.ArtifactViewStatus, withSource bool) {
	switch {
	case st.Error != "":
		fmt.Fprintf(w, "%s: cannot be used (%s)\n", st.Name, oneLine(st.Error))
		return
	case st.Kind != "script" && st.Kind != "html":
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
	printMetricsUsed(w, st)
	if st.Kind == "html" {
		printPageStatus(w, st, withSource)
		return
	}
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
			return "", errors.New("--hash also needs --yes, to say you read the source `slaw artifact view status " + name + "` showed")
		}
		return hash, nil
	}
	if !stdinIsTerminal() {
		return "", errors.New("not a terminal: read the view with `slaw artifact view status " + name + "`, then pass its hash with --hash --yes")
	}
	st, err := ws.ArtifactViewState(name)
	if err != nil {
		return "", err
	}
	if st.Hash == "" {
		return "", errors.New("this artifact has no custom view to allow")
	}
	printViewStatus(os.Stderr, st, true)
	if st.Kind == "html" {
		fmt.Fprint(os.Stderr, "This page was written by someone else or by an agent. Allow it? [y/N]: ")
	} else {
		fmt.Fprint(os.Stderr, "This view runs code written by someone else or by an agent. Allow it? [y/N]: ")
	}
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

// printMetricsUsed lists the metrics the view's queries call, each with the
// saved query it reads: their files are part of the hash (#282).
func printMetricsUsed(w io.Writer, st workspace.ArtifactViewStatus) {
	if len(st.MetricsUsed) == 0 {
		return
	}
	fmt.Fprintln(w, "  metrics it reads:")
	for _, m := range st.MetricsUsed {
		switch {
		case m.Missing:
			fmt.Fprintf(w, "    %s (no such metric yet)\n", m.Name)
		case m.Query != "":
			fmt.Fprintf(w, "    %s (saved query %s)\n", m.Name, m.Query)
		default:
			fmt.Fprintf(w, "    %s (its file cannot be read)\n", m.Name)
		}
	}
	fmt.Fprintf(w, "  %s\n", artifacts.MetricsLine)
}

// printPageStatus lists an HTML page's host, the queries it uses with their
// connections, the declared capabilities with their plain lines, and its
// files; with withSource it shows every text file, so what the person reads is
// what the hash covers.
func printPageStatus(w io.Writer, st workspace.ArtifactViewStatus, withSource bool) {
	fmt.Fprintf(w, "  page: %s\n", st.PageKind)
	fmt.Fprintf(w, "  %s\n", artifacts.HostLine(st.PageKind))
	if st.PageKind == artifacts.PageScripted {
		fmt.Fprintln(w, "  queries it can run (a script can run any of them, not only the ones its templates name):")
	} else {
		fmt.Fprintln(w, "  queries it uses:")
	}
	for _, q := range st.QueryUses {
		conn := q.Connection
		if conn == "" {
			conn = "the workspace database"
		}
		note := ""
		if st.PageKind == artifacts.PageScripted {
			note = ", script only"
			if q.Used {
				note = ", used by its templates"
			}
		}
		fmt.Fprintf(w, "    %s (on %s%s)\n", q.Name, conn, note)
	}
	if len(st.QueryUses) == 0 {
		fmt.Fprintln(w, "    none")
	}
	if st.PageKind == artifacts.PageScripted {
		target := cliAskTarget(context.Background())
		fmt.Fprintf(w, "  capabilities: %s\n", dash(strings.Join(st.Capabilities, ", ")))
		for _, c := range st.Capabilities {
			fmt.Fprintf(w, "    %s\n", artifacts.CapabilityLine(c, target))
		}
		if runtime.GOOS == "darwin" {
			fmt.Fprintf(w, "  %s\n", artifacts.NetworkDisclosure)
		}
	}
	fmt.Fprintf(w, "  %s\n", artifacts.CredentialLine)
	fmt.Fprintln(w, "  files:")
	for _, f := range st.Files {
		fmt.Fprintf(w, "    %s (%s, %d bytes)\n", f.Path, f.Role, f.Size)
	}
	if !withSource {
		return
	}
	fmt.Fprintf(w, "--- artifact.yaml\n%s\n", artifacts.EscapeHidden(strings.TrimRight(st.Manifest, "\n")))
	for _, f := range st.Files {
		if f.Text != "" {
			fmt.Fprintf(w, "--- html/%s\n%s\n", f.Path, artifacts.EscapeHidden(strings.TrimRight(f.Text, "\n")))
		}
	}
	fmt.Fprintln(w, "---")
}

// cliAskTarget names the model `ask` would use, with Studio's own test of
// "usable" (llm.Configured: a cloud provider needs its key, a local one an
// endpoint), or "" when none is.
func cliAskTarget(ctx context.Context) string {
	cfg := config.Load().LLM
	key, _ := config.ResolveLLMKey(ctx, llmKeyStore(), cfg)
	if !llm.Configured(llm.Config{Provider: cfg.Provider, Endpoint: cfg.Endpoint, AccountID: cfg.AccountID, Gateway: cfg.Gateway, Model: cfg.Model, APIKey: key}) {
		return ""
	}
	return cfg.AskTarget()
}
