package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/SandwichLabs/vibe/internal/semantics/proposals"
	"github.com/SandwichLabs/vibe/internal/strictyaml"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// The inbox of proposed definitions. An agent files a proposal with
// propose_definition; nothing in semantics/ changes until a person runs
// `accept` here (or accepts it in Studio). No agent tool or MCP method can.

var semanticsProposalsCmd = &cobra.Command{
	Use:   "proposals",
	Short: "List the proposed definitions waiting for you",
	Long: `List the semantic definitions an agent has proposed (propose_definition).
They wait in .vibe-local/proposals/ on this machine; nothing in semantics/
changes until you accept one. By default only open proposals are listed;
--all adds the accepted and rejected ones.

Review one with "vibe semantics show-proposal <id>", then "accept" or "reject"
it.`,
	Args:         cobra.NoArgs,
	RunE:         runSemanticsProposals,
	SilenceUsage: true,
}

var semanticsShowProposalCmd = &cobra.Command{
	Use:   "show-proposal <id>",
	Short: "Show a proposal and what accepting it would change",
	Long: `Print a proposal: who proposed it and why, the file it would write, and a diff
of the current file against the proposed one. A file that changed since the
proposal was made is flagged: accepting it then needs --overwrite.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runSemanticsShowProposal,
	SilenceUsage: true,
}

var semanticsAcceptCmd = &cobra.Command{
	Use:   "accept <id>",
	Short: "Write a proposed definition into semantics/",
	Long: `Accept a proposal: write its definition into semantics/ exactly as
"show-proposal" displayed it, and mark the proposal accepted. The write is
compare-and-write against the file the proposal was made against; if that file
changed since (or was created or deleted), nothing is written and the command
fails.

Plain "accept <id>" looks at the proposal right then, prints what it is about
to write (the same text as "show-proposal", with its Rev) to stderr, and writes
exactly that version. To accept the version you reviewed earlier, pass --rev
<rev>; if the file or the proposal changed since, it is refused ("changed again
since you looked").

To accept it anyway, pass --overwrite together with --rev <rev>, the "Rev"
that "show-proposal" printed for the version you reviewed. If the file or the
proposal changed after you looked, even --overwrite is refused ("changed again
since you looked"). Without --rev, --overwrite shows the proposal and refuses,
so a script cannot overwrite blind.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runSemanticsAccept,
	SilenceUsage: true,
}

var semanticsRejectCmd = &cobra.Command{
	Use:          "reject <id>",
	Short:        "Reject a proposal; semantics/ is not changed",
	Args:         cobra.ExactArgs(1),
	RunE:         runSemanticsReject,
	SilenceUsage: true,
}

func runSemanticsProposals(cmd *cobra.Command, _ []string) error {
	all, _ := cmd.Flags().GetBool("all")
	asJSON, _ := cmd.Flags().GetBool("json")
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	list, err := workspace.ListProposals(cmdCtx(cmd), ws, all)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(list)
	}
	if len(list) == 0 {
		fmt.Fprintln(out, "No proposals are waiting.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tKIND\tNAME\tSTATUS\tBY\tWHY")
	for _, p := range list {
		if p.Error != "" {
			fmt.Fprintf(tw, "%s\t-\t-\tunreadable\t-\t%s\n", strictyaml.EscapeHidden(p.ID), strictyaml.EscapeHidden(oneLine(p.Error)))
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Kind, p.Name, p.Status, p.ProposedBy, strictyaml.EscapeHidden(clip(oneLine(p.Rationale), 60)))
	}
	return tw.Flush()
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func runSemanticsShowProposal(cmd *cobra.Command, args []string) error {
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	pv, err := workspace.GetProposal(cmdCtx(cmd), ws, args[0])
	if err != nil {
		return err
	}
	printPreview(cmd.OutOrStdout(), pv)
	return nil
}

func printPreview(w io.Writer, pv *proposals.Preview) {
	p := pv.Proposal
	esc := strictyaml.EscapeHidden
	fmt.Fprintf(w, "Proposal  %s\n", p.ID)
	fmt.Fprintf(w, "Proposes  %s %s  (%s, %s, by %s)\n", p.Kind, p.Name, p.Status, p.ProposedAt, p.ProposedBy)
	fmt.Fprintf(w, "Writes    %s\n", pv.Target)
	fmt.Fprintf(w, "Rev       %s\n", pv.ShownRev)
	fmt.Fprintf(w, "Why       %s\n", esc(strings.ReplaceAll(strings.TrimSpace(p.Rationale), "\n", "\n          ")))
	for _, e := range p.Evidence {
		fmt.Fprintf(w, "Evidence  %s\n", esc(e))
	}
	for _, warn := range pv.Warnings {
		fmt.Fprintf(w, "WARNING   %s\n", esc(warn))
	}
	switch {
	case pv.Stale:
		fmt.Fprintf(w, "\nWARNING: %s. To accept it anyway: vibe semantics accept %s --overwrite --rev %s\n", proposals.StaleSentence(pv.Target, pv.StaleReason), p.ID, pv.ShownRev)
	case !pv.Exists:
		fmt.Fprintf(w, "\n%s is new.\n", pv.Target)
	}
	diff := proposals.Unified(proposals.Diff(pv.CurrentText, pv.ProposedText), 3)
	if diff == "" {
		fmt.Fprintln(w, "\nNo change: the file already reads as proposed.")
		return
	}
	fmt.Fprintf(w, "\n%s\n", esc(diff))
}

func runSemanticsAccept(cmd *cobra.Command, args []string) error {
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	rev, _ := cmd.Flags().GetString("rev")
	if rev == "" && !overwrite {
		// No --rev: look now, show exactly what is about to be written, and pin
		// that version, so the person sees what is accepted.
		seen, err := workspace.GetProposal(cmdCtx(cmd), ws, args[0])
		if err != nil {
			return err
		}
		printPreview(cmd.ErrOrStderr(), seen)
		rev = seen.ShownRev
	}
	pv, err := workspace.AcceptProposal(cmdCtx(cmd), ws, args[0], rev, overwrite)
	if err != nil {
		var stale *proposals.StaleError
		if errors.As(err, &stale) && pv != nil {
			printPreview(cmd.ErrOrStderr(), pv)
			return fmt.Errorf("not accepted: %w (to accept it anyway: vibe semantics accept %s --overwrite --rev %s)", err, pv.Proposal.ID, pv.ShownRev)
		}
		if overwrite && rev == "" && pv != nil {
			printPreview(cmd.ErrOrStderr(), pv)
			return fmt.Errorf("not accepted: --overwrite needs --rev %s, the version shown above; run again with it if you accept that", pv.ShownRev)
		}
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Accepted %s: wrote %s\n", pv.Proposal.ID, pv.Target)
	return nil
}

func runSemanticsReject(cmd *cobra.Command, args []string) error {
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	if err := workspace.RejectProposal(cmdCtx(cmd), ws, args[0]); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Rejected %s. Nothing in semantics/ changed.\n", args[0])
	return nil
}

func init() {
	semanticsProposalsCmd.Flags().Bool("all", false, "include accepted and rejected proposals")
	semanticsProposalsCmd.Flags().Bool("json", false, "output as JSON")
	semanticsAcceptCmd.Flags().Bool("overwrite", false, "accept even though the file changed since it was proposed (needs --rev)")
	semanticsAcceptCmd.Flags().String("rev", "", "the Rev show-proposal printed for the version you reviewed (required with --overwrite)")
	for _, c := range []*cobra.Command{semanticsProposalsCmd, semanticsShowProposalCmd, semanticsAcceptCmd, semanticsRejectCmd} {
		addWorkspaceFlag(c)
		semanticsCmd.AddCommand(c)
	}
}
