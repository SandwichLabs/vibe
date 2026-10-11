package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var connCmd = &cobra.Command{
	Use:   "conn",
	Short: "Add, list, test and remove a workspace's connections",
	Long: `A connection is a place data lives: a local DuckDB or SQLite file, Postgres,
MySQL, MotherDuck, a DuckLake, an Iceberg or Delta table, or an S3, Google
Cloud Storage, Cloudflare R2 or Azure prefix. The
definition (host, database, ...) is a plain setting; the credentials never are:
they go to your OS keychain, or are read from environment variables you name
(--sign-in env:PREFIX), and are never printed back.

Scopes: --scope local (default; this computer, this workspace), shared (in the
committed vibe.yaml, so teammates get the definition and enter their own
credentials) or user (every workspace you open). See docs/reference/connections.md.`,
}

func addConnWorkspaceFlag(c *cobra.Command) { addWorkspaceFlag(c) }

// openWS opens just the workspace (no DuckDB hub, no lock): connection
// management is configuration and the keychain.
func openWS(cmd *cobra.Command) (workspace.Workspace, error) {
	dir, _ := cmd.Flags().GetString("workspace")
	if isVibeFile(dir) {
		return nil, errLegacyFileArg
	}
	root, err := hub.ResolveRoot(dir, "")
	if err != nil {
		return nil, err
	}
	ws, err := workspace.Open(root, workspace.WithLogger(logger), workspace.WithResolver(cliResolver()))
	if err != nil {
		return nil, fmt.Errorf("failed to open the workspace: %w", err)
	}
	for _, w := range ws.Warnings() {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
	}
	if !isEnvCmd(cmd) {
		maybePromptDotEnv(ws, cmd.ErrOrStderr())
	}
	return ws, nil
}

func cmdCtx(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// scopeFlag reads a --scope style flag value; the wording is the CLI's.
func scopeFlag(s string) (workspace.Scope, error) {
	sc, ok := workspace.ParseScope(s)
	if !ok {
		return "", fmt.Errorf("unknown scope %q (want user, shared or local)", s)
	}
	return sc, nil
}

// --- add ---

var connAddCmd = &cobra.Command{
	Use:   "add <name> --type <type>",
	Short: "Add a connection (credentials are prompted for and kept in the keychain)",
	Long: `Add a connection definition and, on a terminal, prompt for its credentials
(no echo) and store them in the keychain. Types: ` + "duckdb, sqlite, postgres, mysql, motherduck, ducklake, s3, gcs, r2, azure, iceberg, delta" + `.

  vibe conn add pg --type postgres --host db.internal --database analytics
  vibe conn add lake --type s3 --prefix s3://acme-lake/raw/ --region us-east-1
  vibe conn add md --type motherduck --database my_db --sign-in env:MOTHERDUCK
  vibe conn add legacy --type sqlite --path data/legacy.sqlite
  vibe conn add shop --type mysql --host db.internal --database shop --option ssl_mode=required
  vibe conn add events --type azure --prefix az://events/2024/ --option sign_in=account_key --option account_name=acmelake
  vibe conn add wh --type ducklake --path data/wh.ducklake --option data_path=data/wh_files --read-only=false
  vibe conn add sales --type delta --path data/sales

Connections are read-only unless --read-only=false (MotherDuck is always
writable: it has no read-only attach). Without a terminal no
credentials are asked for; use "vibe conn credentials set <name>" (reads
stdin) or --sign-in env:PREFIX. Nothing secret ever goes on the command line.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runConnAdd,
	SilenceUsage: true,
}

func runConnAdd(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	typ, _ := f.GetString("type")
	scopeS, _ := f.GetString("scope")
	scope, err := scopeFlag(scopeS)
	if err != nil {
		return err
	}
	if typ == "" {
		return fmt.Errorf("--type is required (one of %s)", strings.Join(connections.AllTypeNames(), ", "))
	}
	if _, err := connections.TypeInfoFor(typ); err != nil {
		return err
	}
	def := workspace.ConnectionDef{Name: args[0], Type: typ}
	def.Path, _ = f.GetString("path")
	def.Host, _ = f.GetString("host")
	def.Port, _ = f.GetInt("port")
	def.Database, _ = f.GetString("database")
	def.Region, _ = f.GetString("region")
	def.Scope, _ = f.GetString("prefix")
	if ro, _ := f.GetBool("read-only"); !ro {
		def.ReadOnly = workspace.Bool(false)
	} else if typ == "motherduck" && f.Changed("read-only") {
		// MotherDuck has no read-only attach: refuse instead of saving a
		// definition that can never attach. Left unset it defaults to writable.
		return errors.New(connections.ErrMotherDuckReadOnly)
	}
	signIn, _ := f.GetString("sign-in")
	if signIn != "" {
		if err := workspace.ValidateSignIn(signIn); err != nil {
			return err
		}
	}
	opts, _ := f.GetStringArray("option")
	for _, kv := range opts {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return fmt.Errorf("--option %q: want key=value", kv)
		}
		if def.Options == nil {
			def.Options = map[string]string{}
		}
		def.Options[k] = v
	}

	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close() //nolint:errcheck
	ctx := cmdCtx(cmd)
	if err := ws.AddConnection(scope, def); err != nil {
		return err
	}
	if signIn != "" {
		if err := ws.SetSignIn(def.Name, signIn); err != nil {
			return err
		}
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Added %s connection %s (scope: %s).\n", typ, def.Name, scope)
	if !connections.NeedsCredential(typ) {
		return nil
	}
	c, _ := findConn(ws, def.Name)
	method := c.SignIn
	if kind, _, _ := secrets.ParseMethod(method); kind == "env" {
		names := secrets.EnvVarNames(strings.TrimPrefix(method, "env:"))
		if typ == "motherduck" {
			names = []string{strings.TrimPrefix(method, "env:") + "_TOKEN"} // a token is all MotherDuck takes
		}
		fmt.Fprintf(out, "Credentials come from the environment variables %s.\n", strings.Join(names, ", "))
		return nil
	}
	if !stdinIsTerminal() {
		fmt.Fprintf(out, "No terminal, so no credentials were asked for. Run: vibe conn credentials set %s\n", def.Name)
		return nil
	}
	cred, err := promptCredentials(cmd.InOrStdin(), cmd.ErrOrStderr(), typ, secrets.Credential{})
	if err != nil {
		return err
	}
	if err := ws.SetCredentials(ctx, def.Name, cred, false); err != nil {
		return errors.New(cliRedactor.Redact(err.Error()))
	}
	fmt.Fprintf(out, "Credentials saved (%s). Try: vibe conn test %s\n", method, def.Name)
	return nil
}

func findConn(ws workspace.Workspace, name string) (workspace.Connection, bool) {
	return ws.Connection(name)
}

// --- credential prompting ---

// lineReader hands out lines from one buffered reader, so several prompts can
// read a pipe without the first one swallowing the rest.
type lineReader struct {
	f  *os.File
	br *bufio.Reader
}

func newLineReader(in io.Reader) *lineReader {
	lr := &lineReader{}
	if f, ok := in.(*os.File); ok {
		lr.f = f
	}
	lr.br = bufio.NewReader(in)
	return lr
}

func (l *lineReader) tty() bool { return l.f != nil && term.IsTerminal(int(l.f.Fd())) }

func (l *lineReader) line(errW io.Writer, prompt string, secret bool) (string, error) {
	if l.tty() {
		fmt.Fprint(errW, prompt)
		if secret {
			b, err := term.ReadPassword(int(l.f.Fd()))
			fmt.Fprintln(errW)
			if err != nil {
				return "", fmt.Errorf("failed to read input: %w", err)
			}
			return strings.TrimSpace(string(b)), nil
		}
	}
	s, err := l.br.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || s == "") {
		return "", nil //nolint:nilerr // end of input: the remaining fields stay empty
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// promptCredentials asks for each credential field of the type. A blank answer
// keeps what is already stored (empty fields mean "keep" in SetCredentials).
func promptCredentials(in io.Reader, errW io.Writer, typ string, _ secrets.Credential) (secrets.Credential, error) {
	info, err := connections.TypeInfoFor(typ)
	if err != nil {
		return secrets.Credential{}, err
	}
	lr := newLineReader(in)
	var c secrets.Credential
	for _, f := range info.Credentials {
		label := f.Label
		if !f.Required {
			label += " (optional)"
		}
		v, err := lr.line(errW, label+": ", f.Secret)
		if err != nil {
			return secrets.Credential{}, err
		}
		if v != "" {
			cliRedactor.Add(secretOnly(f, v)...)
		}
		switch f.Name {
		case "user":
			c.User = v
		case "password":
			c.Password = v
		case "token":
			c.Token = v
		case "key_id":
			c.KeyID = v
		case "secret":
			c.Secret = v
		}
	}
	return c, nil
}

func secretOnly(f connections.Field, v string) []string {
	if f.Secret {
		return []string{v}
	}
	return nil
}

// --- list ---

var connListCmd = &cobra.Command{
	Use:   "list",
	Short: "List connections with scope, status and sign-in method",
	Long: `List the workspace's merged connections: where each is defined (a user entry
is hidden by a shared one, which a local one hides in turn), its read-only
flag, its sign-in method and its status:

  ok  not-connected  needs-credentials  extension-missing  locked-by  error

Nothing remote is dialled; a connection reads not-connected until it is used.
list reads local DuckDB files read-only and never takes the workspace.`,
	Args:         cobra.NoArgs,
	RunE:         runConnList,
	SilenceUsage: true,
}

type connRow struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Scope    string   `json:"scope"`
	Shadows  []string `json:"shadows,omitempty"`
	Status   string   `json:"status"`
	ReadOnly bool     `json:"read_only"`
	// ReadOnlyNote says when read-only is advisory (not enforced by attach).
	ReadOnlyNote string `json:"read_only_note,omitempty"`
	SignIn       string `json:"sign_in,omitempty"`
	Default      bool   `json:"default,omitempty"`
	Message      string `json:"message,omitempty"`
}

func runConnList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	o, err := openWorkspace(cmdCtx(cmd), flagString(cmd, "workspace"), true, false, "vibe conn", io.Discard)
	if err != nil {
		return err
	}
	defer o.Close()
	status := map[string]session.Status{}
	for _, s := range o.Sess.Connections() {
		status[s.Name] = s
	}
	var rows []connRow
	for _, c := range o.WS.Connections() {
		r := connRow{Name: c.Name, Type: c.Type, Scope: string(c.Scope), Default: c.Default, ReadOnly: c.IsReadOnly()}
		for _, s := range c.Shadows {
			r.Shadows = append(r.Shadows, string(s))
		}
		if connections.NeedsCredential(c.Type) {
			r.SignIn = c.SignIn
		}
		if info, err := connections.TypeInfoFor(c.Type); err == nil && r.ReadOnly && !info.Capabilities.ReadOnlyAttach && info.Capabilities.Catalog {
			r.ReadOnlyNote = "advisory"
			if c.Type == "postgres" || c.Type == "mysql" {
				r.ReadOnlyNote = "client-side; use a read-only role"
			}
			if c.Type == "motherduck" {
				r.ReadOnlyNote = "not enforced; use a read-scaling token"
			}
		}
		if s, ok := status[c.Name]; ok {
			r.Status, r.Message = string(s.Kind), s.Message
		} else if i, ok := o.issueFor(c.Name); ok {
			r.Status, r.Message = "error", i.Message
		} else {
			r.Status = "not-connected"
		}
		if i, ok := o.issueFor(c.Name); ok && r.Message == "" {
			r.Message = i.Message
		}
		rows = append(rows, r)
	}
	if ok, why := o.Sess.RelaunchRequired(); ok {
		fmt.Fprintln(cmd.ErrOrStderr(), "note:", why)
	}
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "No connections. Add one with: vibe conn add <name> --type <type>")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTYPE\tSCOPE\tSTATUS\tREAD-ONLY\tSIGN-IN\tNOTE")
	for _, r := range rows {
		name := r.Name
		if r.Default {
			name += " *"
		}
		note := r.Message
		if len(r.Shadows) > 0 {
			note = strings.TrimSpace("shadows " + strings.Join(r.Shadows, ", ") + "; " + note)
		}
		ro := "no"
		if r.ReadOnly {
			ro = "yes"
			if r.ReadOnlyNote != "" {
				ro += " (" + r.ReadOnlyNote + ")"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", name, r.Type, r.Scope, r.Status, ro, dash(r.SignIn), oneLine(note))
	}
	return tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 100 {
		s = s[:97] + "..."
	}
	return s
}

func flagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

// --- test ---

var connTestCmd = &cobra.Command{
	Use:   "test <name>",
	Short: "Try a connection in a throwaway session (10 s limit)",
	Long: `Attach the connection in a throwaway in-memory DuckDB (never the workspace's
own session), run a cheap probe and report ok with the latency and a table or
object count, or a redacted error. MotherDuck is tested in a subprocess.

A DuckDB extension the connection needs is only downloaded with --install; without it
a missing extension is reported and nothing is fetched.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runConnTest,
	SilenceUsage: true,
}

func runConnTest(cmd *cobra.Command, args []string) error {
	install, _ := cmd.Flags().GetBool("install")
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close() //nolint:errcheck
	ctx := cmdCtx(cmd)
	cred, rc, err := workspace.ConnectionCredential(ctx, ws, args[0], nil)
	out := cmd.OutOrStdout()
	if err != nil {
		var nc *secrets.ErrNeedsCredentials
		var fp *secrets.ErrFingerprintChanged
		if errors.As(err, &nc) || errors.As(err, &fp) || errors.Is(err, secrets.ErrUnavailable) {
			return fmt.Errorf("%s: needs credentials: %s", args[0], cliRedactor.Redact(err.Error()))
		}
		return errors.New(cliRedactor.Redact(err.Error()))
	}
	res := connections.Test(ctx, rc.Def(true), cred, connections.WithInstall(install), connections.WithTestRedactor(cliRedactor))
	if !res.OK {
		msg := fmt.Sprintf("%s: %s: %s", args[0], res.Status, res.Error)
		if res.InstallExtension != "" {
			msg += fmt.Sprintf("\n(re-run with --install to download the %q extension)", res.InstallExtension)
		}
		return errors.New(msg)
	}
	fmt.Fprintf(out, "%s: ok, %s (%d ms)\n", args[0], res.Detail, res.Latency.Milliseconds())
	return nil
}

// --- connect ---

var connConnectCmd = &cobra.Command{
	Use:   "connect <name>",
	Short: "Connect in a workspace session and show what is there",
	Long: `Open the workspace without taking ownership, connect the named connection in
that session (this is the same attach a query triggers on first use) and list
what it reached. Without --install a missing extension is reported and nothing
is downloaded.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runConnConnect,
	SilenceUsage: true,
}

func runConnConnect(cmd *cobra.Command, args []string) error {
	install, _ := cmd.Flags().GetBool("install")
	ctx := cmdCtx(cmd)
	name := args[0]
	var o *openedWorkspace
	var err error
	if isMotherDuck(cmd, name) {
		o, err = openWorkspaceRemoteOnly(ctx, flagString(cmd, "workspace"), io.Discard)
	} else {
		o, err = openWorkspace(ctx, flagString(cmd, "workspace"), true, false, "vibe conn", io.Discard)
	}
	if err != nil {
		return err
	}
	defer o.Close()
	c, ok := o.knownConnection(name)
	if !ok {
		return fmt.Errorf("unknown connection %q; this workspace has: %s", name, connectionNames(o))
	}
	if i, ok := o.issueFor(name); ok {
		return fmt.Errorf("connection %s is not available: %s", name, i.Message)
	}
	out := cmd.OutOrStdout()
	if install {
		if info, err := connections.TypeInfoFor(c.Type); err == nil {
			for _, e := range info.Extensions {
				fmt.Fprintf(cmd.ErrOrStderr(), "installing the DuckDB %q extension...\n", e)
				if err := o.Sess.InstallExtension(ctx, e); err != nil {
					return errors.New(cliRedactor.Redact(err.Error()))
				}
			}
		}
		o.Sess.Preflight(ctx, name)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = o.Sess.Connect(cctx, name)
	if relaunch, why := o.Sess.RelaunchRequired(); relaunch {
		return fmt.Errorf("%s: %s", name, why)
	}
	if err != nil {
		st := connStatus(o.Sess, name)
		msg := cliRedactor.Redact(err.Error())
		if st.Kind == session.StatusExtensionMissing {
			msg += "\n(re-run with --install to download it)"
		}
		return errors.New(msg)
	}
	if info, _ := connections.TypeInfoFor(c.Type); !info.Capabilities.Catalog && c.Type != "duckdb" {
		switch c.Type {
		case "iceberg":
			fmt.Fprintf(out, "%s: ready (read the table with iceberg_scan('%s'))\n", name, c.ConnectionDef.Path)
		case "delta":
			fmt.Fprintf(out, "%s: ready (read the table with delta_scan('%s'))\n", name, c.ConnectionDef.Path)
		default:
			fmt.Fprintf(out, "%s: connected (storage scope %s; read it with read_parquet('%s...'))\n", name, c.Scope, c.ConnectionDef.Scope)
		}
		return nil
	}
	tables, terr := o.Sess.ListTables(cctx, name, "")
	if terr != nil {
		fmt.Fprintf(out, "%s: connected\n", name)
		return nil
	}
	fmt.Fprintf(out, "%s: connected, %d tables in the default schema\n", name, len(tables))
	for i, t := range tables {
		if i >= 20 {
			fmt.Fprintf(out, "  ... and %d more\n", len(tables)-20)
			break
		}
		fmt.Fprintf(out, "  %s\n", t.Ref().String())
	}
	return nil
}

// isMotherDuck reports whether the named connection is a MotherDuck one, which
// cannot be attached by a read-only open.
func isMotherDuck(cmd *cobra.Command, name string) bool {
	ws, err := openWS(cmd)
	if err != nil {
		return false
	}
	defer ws.Close() //nolint:errcheck
	c, ok := findConn(ws, name)
	return ok && c.Type == "motherduck"
}

func connStatus(s session.Session, name string) session.Status {
	st, _ := s.Status(name)
	return st
}

// --- rm / move ---

var connRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove a connection and its saved credentials",
	Long: `Remove the definition from its scope (--scope, default: the one that is in
effect) and, when no other definition still uses them, clear its keychain
entry and sign-in choice. Data in the connection itself is untouched.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runConnRm,
	SilenceUsage: true,
}

func runConnRm(cmd *cobra.Command, args []string) error {
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close() //nolint:errcheck
	c, ok := findConn(ws, args[0])
	if !ok {
		return fmt.Errorf("unknown connection %q", args[0])
	}
	scope := c.Scope
	if s := flagString(cmd, "scope"); s != "" {
		if scope, err = scopeFlag(s); err != nil {
			return err
		}
	}
	if err := ws.RemoveConnection(cmdCtx(cmd), scope, args[0]); err != nil {
		return errors.New(cliRedactor.Redact(err.Error()))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s connection %s.\n", scope, args[0])
	return nil
}

var connMoveCmd = &cobra.Command{
	Use:   "move <name> --to <scope>",
	Short: "Move a connection between scopes (user, shared, local)",
	Long: `Change where a definition lives. The saved credential stays bound to the
workspace id, the name and the fingerprint, so nothing needs re-entering; the
destination's rules apply (a shared connection may not use an absolute path or
${ENV}).`,
	Args:         cobra.ExactArgs(1),
	RunE:         runConnMove,
	SilenceUsage: true,
}

func runConnMove(cmd *cobra.Command, args []string) error {
	to, err := scopeFlag(flagString(cmd, "to"))
	if err != nil {
		return err
	}
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close() //nolint:errcheck
	c, ok := findConn(ws, args[0])
	if !ok {
		return fmt.Errorf("unknown connection %q", args[0])
	}
	from := c.Scope
	if s := flagString(cmd, "from"); s != "" {
		if from, err = scopeFlag(s); err != nil {
			return err
		}
	}
	if err := ws.MoveConnection(cmdCtx(cmd), args[0], from, to); err != nil {
		return errors.New(cliRedactor.Redact(err.Error()))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Moved %s from %s to %s.\n", args[0], from, to)
	return nil
}

// --- credentials ---

var connCredentialsCmd = &cobra.Command{
	Use:   "credentials",
	Short: "Set, clear or inspect a connection's credentials",
}

var connCredsSetCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Store credentials in the keychain (prompts without echo; reads stdin when piped)",
	Long: `Ask for the connection's credential fields (user, password; token; key id,
secret) and store them in the keychain, bound to the connection's server. A
blank answer keeps the stored value. When input is piped, one line is read per
field, in the order the prompts would appear.

--sign-in chooses the method for this connection first: keychain (default),
prompt, or env:PREFIX, which reads PREFIX_USER, PREFIX_PASSWORD, PREFIX_TOKEN,
PREFIX_KEY_ID and PREFIX_SECRET at connect time and stores nothing.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runConnCredsSet,
	SilenceUsage: true,
}

func runConnCredsSet(cmd *cobra.Command, args []string) error {
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close() //nolint:errcheck
	c, ok := findConn(ws, args[0])
	if !ok {
		return fmt.Errorf("unknown connection %q", args[0])
	}
	if !connections.NeedsCredential(c.Type) {
		return fmt.Errorf("%s connections have no credentials", c.Type)
	}
	if m := flagString(cmd, "sign-in"); m != "" {
		err := ws.SetSignIn(c.Name, m)
		if errors.Is(err, workspace.ErrRebindNeedsConfirm) {
			if yes, _ := cmd.Flags().GetBool("confirm-rebind"); !yes {
				return fmt.Errorf("%w (run again with --confirm-rebind if %s should use %s for its new server)", err, c.Name, m)
			}
			err = ws.RebindSignIn(c.Name, m)
		}
		if err != nil {
			return err
		}
		c, _ = findConn(ws, args[0])
	}
	out := cmd.OutOrStdout()
	if kind, prefix, _ := secrets.ParseMethod(c.SignIn); kind == "env" {
		fmt.Fprintf(out, "%s signs in from the environment: %s. Nothing is stored.\n", c.Name, strings.Join(secrets.EnvVarNames(prefix), ", "))
		return nil
	}
	cred, err := promptCredentials(cmd.InOrStdin(), cmd.ErrOrStderr(), c.Type, secrets.Credential{})
	if err != nil {
		return err
	}
	if err := ws.SetCredentials(cmdCtx(cmd), c.Name, cred, false); err != nil {
		return errors.New(cliRedactor.Redact(err.Error()))
	}
	fmt.Fprintf(out, "Credentials for %s saved (%s).\n", c.Name, c.SignIn)
	return nil
}

var connCredsClearCmd = &cobra.Command{
	Use:          "clear <name>",
	Short:        "Remove the saved credentials of a connection",
	Args:         cobra.ExactArgs(1),
	RunE:         runConnCredsClear,
	SilenceUsage: true,
}

func runConnCredsClear(cmd *cobra.Command, args []string) error {
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close() //nolint:errcheck
	if err := ws.ClearCredentials(cmdCtx(cmd), args[0]); err != nil {
		return errors.New(cliRedactor.Redact(err.Error()))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Cleared the saved credentials of %s.\n", args[0])
	return nil
}

var connCredsStatusCmd = &cobra.Command{
	Use:          "status <name>",
	Short:        "Say whether credentials are set (never what they are)",
	Args:         cobra.ExactArgs(1),
	RunE:         runConnCredsStatus,
	SilenceUsage: true,
}

func runConnCredsStatus(cmd *cobra.Command, args []string) error {
	ws, err := openWS(cmd)
	if err != nil {
		return err
	}
	defer ws.Close() //nolint:errcheck
	st, err := ws.CredentialStatus(cmdCtx(cmd), args[0])
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "method:        %s\n", st.Method)
	if st.Username != "" {
		fmt.Fprintf(out, "user:          %s\n", st.Username)
	}
	if st.KeyID != "" {
		fmt.Fprintf(out, "key id:        %s\n", st.KeyID)
	}
	fmt.Fprintf(out, "secret set:    %t\n", st.SecretSet)
	fmt.Fprintf(out, "server match:  %t\n", st.FingerprintMatches)
	fmt.Fprintf(out, "available:     %t\n", st.Available)
	if st.Detail != "" {
		fmt.Fprintf(out, "note:          %s\n", st.Detail)
	}
	names := make([]string, 0, len(st.Env))
	for _, e := range st.Env {
		names = append(names, fmt.Sprintf("%s=%t", e.Name, e.Set))
	}
	sort.Strings(names)
	if len(names) > 0 {
		fmt.Fprintf(out, "env set:       %s\n", strings.Join(names, " "))
	}
	return nil
}

// --- hidden subprocess entry for the MotherDuck test ---

var connChildCmd = &cobra.Command{
	Use:    connections.ChildCommand,
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return connections.RunChild(cmdCtx(cmd), cmd.InOrStdin(), cmd.OutOrStdout())
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	f := connAddCmd.Flags()
	f.String("type", "", "connection type: "+strings.Join(connections.AllTypeNames(), ", "))
	f.String("scope", "local", "where to keep the definition: local (this computer), shared (vibe.yaml) or user (all workspaces)")
	f.String("path", "", "file path (duckdb, sqlite; ducklake: the metadata file; iceberg, delta: the table folder or storage URL)")
	f.String("host", "", "host (postgres, mysql)")
	f.Int("port", 0, "port (postgres, mysql; default 5432 / 3306)")
	f.String("database", "", "database (postgres, mysql, motherduck)")
	f.String("region", "", "region (s3, gcs, r2)")
	f.String("prefix", "", "storage scope: a URL prefix such as s3://bucket/path/ (s3, gcs, r2, azure)")
	f.StringArray("option", nil, "type option key=value (postgres: sslmode; mysql: ssl_mode; s3: endpoint, url_style; r2: account_id; azure: sign_in, account_name, tenant_id; ducklake: data_path); repeatable")
	f.Bool("read-only", true, "attach read-only (--read-only=false to allow writes)")
	f.String("sign-in", "", "how you sign in: keychain (default), prompt or env:PREFIX")
	addConnWorkspaceFlag(connAddCmd)

	addConnWorkspaceFlag(connListCmd)
	connListCmd.Flags().Bool("json", false, "output as JSON")

	connTestCmd.Flags().Bool("install", false, "download a missing DuckDB extension first")
	addConnWorkspaceFlag(connTestCmd)
	connConnectCmd.Flags().Bool("install", false, "download a missing DuckDB extension first")
	addConnWorkspaceFlag(connConnectCmd)

	connRmCmd.Flags().String("scope", "", "scope to remove from (default: the one in effect)")
	addConnWorkspaceFlag(connRmCmd)
	connMoveCmd.Flags().String("to", "", "destination scope: user, shared or local")
	connMoveCmd.Flags().String("from", "", "source scope (default: the one in effect)")
	addConnWorkspaceFlag(connMoveCmd)

	connCredsSetCmd.Flags().String("sign-in", "", "sign-in method: keychain, prompt or env:PREFIX")
	connCredsSetCmd.Flags().Bool("confirm-rebind", false, "use an env: sign-in for a server other than the one it was chosen for")
	addConnWorkspaceFlag(connCredsSetCmd)
	addConnWorkspaceFlag(connCredsClearCmd)
	addConnWorkspaceFlag(connCredsStatusCmd)
	connCredentialsCmd.AddCommand(connCredsSetCmd, connCredsClearCmd, connCredsStatusCmd)

	connCmd.AddCommand(connAddCmd, connListCmd, connTestCmd, connConnectCmd, connRmCmd, connMoveCmd, connCredentialsCmd)
	rootCmd.AddCommand(connCmd, connChildCmd)
}

// isEnvCmd is true for `workspace env ...`, which manages the decision itself.
func isEnvCmd(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c == workspaceEnvCmd {
			return true
		}
	}
	return false
}
