package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/strictyaml"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

var semanticsCmd = &cobra.Command{
	Use:   "semantics",
	Short: "List the workspace's semantic definitions and review proposed ones",
	Long: `The semantic layer is reviewed files in the workspace's semantics/ folder:
entities (what the data is about), metrics (named measures over a saved
query), joins (how entities relate) and a glossary. "list" shows them the way
Studio's Semantics view does, unreadable files included with their error, and
"show" prints one file as stored. Both only read files: no connection is
opened and no query is run. An agent can only propose a definition: "proposals"
lists what is waiting, "show-proposal" shows the change, and "accept" or
"reject" decides it. See docs/reference/semantics.md.`,
}

var semanticsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List entities, metrics, joins and the glossary",
	Long: `List every definition under semantics/: kind, name, a one-line summary and,
for a file that cannot be read, why. The list is complete: a broken file is
shown with its error, never hidden.`,
	Args:         cobra.NoArgs,
	RunE:         runSemanticsList,
	SilenceUsage: true,
}

var semanticsShowCmd = &cobra.Command{
	Use:   "show <kind> <name>",
	Short: "Print one definition's file as stored",
	Long: `Print one definition as it is stored (comments and layout kept; control
characters are shown as <U+XXXX>).
<kind> is entity, metric or join, then the name; "show glossary" takes no name.
A file that fails validation is still printed, with the reason on stderr and
a non-zero exit, so it can be read and fixed.`,
	Args:         cobra.RangeArgs(1, 2),
	RunE:         runSemanticsShow,
	SilenceUsage: true,
}

// semanticsRow is one line of the list.
type semanticsRow struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	Rev     string `json:"rev,omitempty"`
	Error   string `json:"error,omitempty"`
}

func semanticsRows(list *semanticsAll) []semanticsRow {
	rows := []semanticsRow{}
	for _, e := range list.entities {
		rows = append(rows, semanticsRow{Kind: string(semantics.KindEntity), Name: e.Name, Summary: e.Grain, Rev: e.Rev, Error: e.Error})
	}
	for _, m := range list.metrics {
		summary := m.Measure
		if len(m.Dimensions) > 0 {
			summary += " by " + strings.Join(m.Dimensions, ", ")
		}
		rows = append(rows, semanticsRow{Kind: string(semantics.KindMetric), Name: m.Name, Summary: summary, Rev: m.Rev, Error: m.Error})
	}
	for _, j := range list.joins {
		summary := ""
		if j.Left != "" {
			summary = fmt.Sprintf("%s to %s (%s)", j.Left, j.Right, j.Cardinality)
		}
		rows = append(rows, semanticsRow{Kind: string(semantics.KindJoin), Name: j.Name, Summary: summary, Rev: j.Rev, Error: j.Error})
	}
	if g := list.glossary; g.Rev != "" {
		rows = append(rows, semanticsRow{Kind: string(semantics.KindGlossary), Name: semantics.GlossaryFile,
			Summary: fmt.Sprintf("%d terms", len(g.Terms)), Rev: g.Rev, Error: g.Error})
	}
	return rows
}

type semanticsAll struct {
	entities []semantics.Entity
	metrics  []semantics.Metric
	joins    []semantics.Join
	glossary *semantics.Glossary
}

func loadSemantics(cmd *cobra.Command, st semantics.Store) (*semanticsAll, error) {
	ctx := cmdCtx(cmd)
	var (
		all semanticsAll
		err error
	)
	if all.entities, err = st.Entities(ctx); err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}
	if all.metrics, err = st.Metrics(ctx); err != nil {
		return nil, fmt.Errorf("failed to list metrics: %w", err)
	}
	if all.joins, err = st.Joins(ctx); err != nil {
		return nil, fmt.Errorf("failed to list joins: %w", err)
	}
	if all.glossary, err = st.Glossary(ctx); err != nil {
		return nil, fmt.Errorf("failed to read the glossary: %w", err)
	}
	return &all, nil
}

func runSemanticsList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	all, err := loadSemantics(cmd, ws.Semantics())
	if err != nil {
		return err
	}
	rows := semanticsRows(all)
	out := cmd.OutOrStdout()
	if !asJSON {
		defer hintProposals(cmd, ws)
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "No semantic definitions. Add files under semantics/ (see docs/reference/semantics.md).")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tNAME\tSUMMARY\tERROR")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Kind, strictyaml.EscapeHidden(r.Name), dash(strictyaml.EscapeHidden(oneLine(r.Summary))), dash(strictyaml.EscapeHidden(oneLine(r.Error))))
	}
	return tw.Flush()
}

// hintProposals says on stderr how many proposals wait, so a list does not
// hide the inbox.
func hintProposals(cmd *cobra.Command, ws workspace.Workspace) {
	if n, err := ws.Proposals().OpenCount(cmdCtx(cmd)); err == nil && n > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "%d proposed definition(s) waiting: vibe semantics proposals\n", n)
	}
}

func runSemanticsShow(cmd *cobra.Command, args []string) error {
	kind := semantics.Kind(strings.ToLower(args[0]))
	switch kind {
	case "entities":
		kind = semantics.KindEntity
	case "metrics":
		kind = semantics.KindMetric
	case "joins":
		kind = semantics.KindJoin
	}
	name := ""
	switch {
	case kind == semantics.KindGlossary && len(args) == 1:
	case kind == semantics.KindGlossary:
		return errors.New("the glossary has no name: use \"vibe semantics show glossary\"")
	case len(args) != 2:
		return fmt.Errorf("usage: vibe semantics show %s <name>", kind)
	case kind != semantics.KindEntity && kind != semantics.KindMetric && kind != semantics.KindJoin:
		return fmt.Errorf("unknown kind %q (want entity, metric, join or glossary)", args[0])
	default:
		name = args[1]
	}
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close()
	ctx := cmdCtx(cmd)
	src, err := ws.Semantics().Source(ctx, kind, name)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), strictyaml.EscapeHidden(src.Text))
	if problem := validateSemanticsSource(ctx, ws.Semantics(), kind, name); problem != "" {
		return fmt.Errorf("this file does not pass validation: %s", problem)
	}
	return nil
}

// validateSemanticsSource reads the definition through the store's strict
// decoder and returns why it is refused, or "".
func validateSemanticsSource(ctx context.Context, st semantics.Store, kind semantics.Kind, name string) string {
	var err error
	switch kind {
	case semantics.KindEntity:
		_, err = st.Entity(ctx, name)
	case semantics.KindMetric:
		_, err = st.Metric(ctx, name)
	case semantics.KindJoin:
		_, err = st.Join(ctx, name)
	default:
		g, gerr := st.Glossary(ctx)
		if gerr != nil {
			return gerr.Error()
		}
		return g.Error
	}
	if err == nil {
		return ""
	}
	return strings.TrimPrefix(err.Error(), "failed to get "+string(kind)+" "+fmt.Sprintf("%q", name)+": ")
}

func init() {
	semanticsListCmd.Flags().Bool("json", false, "output as JSON")
	addWorkspaceFlag(semanticsListCmd)
	addWorkspaceFlag(semanticsShowCmd)
	semanticsCmd.AddCommand(semanticsListCmd, semanticsShowCmd)
	rootCmd.AddCommand(semanticsCmd)
}
