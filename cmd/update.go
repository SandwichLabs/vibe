package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SandwichLabs/vibe/internal/prefs"
	"github.com/SandwichLabs/vibe/internal/selfupdate"
)

// newUpdateService builds the update service for this binary. A seam so tests use a fake GitHub and
// a temp config dir.
var newUpdateService = func() (*selfupdate.Service, error) {
	dir, err := prefs.DefaultDir(logger)
	if err != nil {
		return nil, err
	}
	rec := prefs.NewRecents(prefs.NewStore(dir), logger)
	return selfupdate.NewService(selfupdate.New(version), rec, logger), nil
}

var (
	updateCheckOnly bool
	updateYes       bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for a newer Vibe release and, if you confirm, install it",
	Long: `Looks up the latest stable Vibe release on GitHub (a plain request to github.com; running this
command is your consent for it) and says whether it is newer than this one.

When this Vibe was put in place by the Vibe installer, it offers to upgrade by running that release's
own install.sh after verifying it against the release's checksums.txt. Nothing is installed without
your answer: "Install vX? [y/N]" on a terminal, or --yes. Without a terminal and without --yes it only
checks. A Vibe installed any other way (go install, a package manager, a dev build) is told to update
the way it was installed.

Exit status: 0 when the check worked (up to date or not) and any requested install succeeded; 1 on an
error (no network, a failed or unverified install).

Weekly checks from Studio are a separate, opt-in setting: vibe config updates [off|weekly].`,
	Args: cobra.NoArgs,
	RunE: runUpdate,
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	svc, err := newUpdateService()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	res, err := svc.CheckNow(ctx)
	cancel()
	if err != nil {
		return err
	}
	printCheck(out, res)
	if !res.UpdateAvailable || !res.Updatable || updateCheckOnly {
		return nil
	}
	if !updateYes {
		ok, asked := confirmInstall(cmd.InOrStdin(), cmd.ErrOrStderr(), res.Latest)
		if !asked {
			fmt.Fprintf(out, "\nRun `vibe update --yes` to install %s.\n", res.Latest)
			return nil
		}
		if !ok {
			fmt.Fprintln(out, "Not installing.")
			return nil
		}
	}
	return applyUpdate(cmd, svc, res.Latest)
}

func printCheck(out io.Writer, res selfupdate.Result) {
	fmt.Fprintf(out, "Current: %s\nLatest:  %s\n", res.Current, res.Latest)
	switch {
	case !res.UpdateAvailable && res.Current == "dev":
		fmt.Fprintln(out, "This is a development build; it does not compare itself with releases.")
	case !res.UpdateAvailable:
		fmt.Fprintln(out, "Vibe is up to date.")
	default:
		fmt.Fprintf(out, "A newer release is available: %s\n", res.ReleaseURL)
		if res.Skipped {
			fmt.Fprintln(out, "(You skipped this version in Studio; this check still offers it.)")
		}
		if res.Notes != "" {
			fmt.Fprintf(out, "\n%s\n", indentLines(res.Notes, "  "))
		}
		if !res.Updatable {
			fmt.Fprintf(out, "\n%s\n", res.ManagedNote)
		}
	}
}

func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 40 {
		lines = append(lines[:40], "…")
	}
	return prefix + strings.Join(lines, "\n"+prefix)
}

// confirmInstall asks "Install tag? [y/N]" on a terminal. asked is false when stdin is not one.
func confirmInstall(in io.Reader, errW io.Writer, tag string) (yes, asked bool) {
	f, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return false, false
	}
	fmt.Fprintf(errW, "\nInstall %s? [y/N] ", tag)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, true
	}
	return false, true
}

func applyUpdate(cmd *cobra.Command, svc *selfupdate.Service, tag string) error {
	out := cmd.OutOrStdout()
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()
	fmt.Fprintf(out, "\nInstalling %s with its own install.sh (verified against the release's checksums)...\n", tag)
	res, err := svc.Apply(ctx, tag)
	if res.Output != "" {
		fmt.Fprintln(out, res.Output)
	}
	if err != nil {
		if errors.Is(err, selfupdate.ErrExternal) {
			return errors.New(selfupdate.ExternalNote)
		}
		return err
	}
	fmt.Fprintf(out, "\nUpdated %s -> %s.\nA running Vibe Studio or `vibe mcp serve` keeps the old version until you restart it.\n", res.From, res.To)
	return nil
}

var configUpdatesCmd = &cobra.Command{
	Use:   "updates [off|weekly]",
	Short: "Show or set whether Studio checks GitHub for new releases weekly",
	Long: `Mirrors Settings > About > Updates in Studio. Off by default until you answer (Studio asks once).
"weekly" lets Studio make one plain request to github.com at startup when the last check is over
a week old. "vibe update" and Studio's "Check now" always work, whatever this says. Nothing is ever
installed without your confirmation.`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{prefs.UpdateCheckOff, prefs.UpdateCheckWeekly},
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newUpdateService()
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(args) == 1 {
			if err := svc.SetCheck(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(out, "Update checks: %s\n", args[0])
			return nil
		}
		st := svc.State()
		switch st.Check {
		case "":
			fmt.Fprintln(out, "Update checks: not set (Studio asks once; set it with `vibe config updates off|weekly`)")
		default:
			fmt.Fprintf(out, "Update checks: %s\n", st.Check)
		}
		if st.LastChecked != "" {
			fmt.Fprintf(out, "Last checked:  %s\n", st.LastChecked)
		}
		if st.SkippedVersion != "" {
			fmt.Fprintf(out, "Skipped:       %s\n", st.SkippedVersion)
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "only check; never offer to install")
	updateCmd.Flags().BoolVar(&updateYes, "yes", false, "install without asking (non-interactive)")
	configCmd.AddCommand(configUpdatesCmd)
	rootCmd.AddCommand(updateCmd)
}
