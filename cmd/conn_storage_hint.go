package cmd

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/storageurl"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

var shellSafeRE = regexp.MustCompile(`^[A-Za-z0-9._~/:@=+-]+$`)

// shellQuote makes s one word of a pasted command: left alone when it is made
// of safe characters, single-quoted otherwise.
func shellQuote(s string) string {
	if shellSafeRE.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// printStorageHint tells the person, after a lake, Iceberg or Delta
// connection with a storage URL was saved, which "vibe conn add" would give it
// a storage connection. The save succeeded either way (a lake may be saved
// before its storage connection), so this is a hint only. It describes the
// definition just saved (def in scope), not whichever copy of the name wins,
// prints one command per connection to add, never an expanded ${ENV} value
// and nothing about a credential.
func printStorageHint(out io.Writer, ws workspace.Workspace, def workspace.ConnectionDef, scope workspace.Scope) {
	seen := map[int]bool{}
	for _, n := range workspace.StorageForConnection(ws, workspace.Connection{ConnectionDef: def, Scope: scope}, nil) {
		s := n.Suggest
		if n.Match != nil || s == nil || seen[n.Group] {
			continue
		}
		seen[n.Group] = true
		prefix, covers := shellQuote(s.Scope), s.Scope
		if s.Scope == "" {
			prefix, covers = "'<bucket URL>'", n.URL // as typed, never expanded
		}
		fmt.Fprintf(out, "No storage connection covers %s. Add one: vibe conn add %s --type %s --prefix %s%s --scope %s\n",
			covers, shellQuote(s.Name), s.Type, prefix, requiredOption(s), scope)
		if s.Type == "s3" {
			fmt.Fprintln(out, "  Optional: --region <region>, --option endpoint=<host:port> for an S3-compatible store.")
		}
		if n.What == "location" && !n.Templated && !strings.HasSuffix(n.URL, "/") {
			fmt.Fprintln(out, "  Write the location with a trailing slash to scope the connection to this table only; without it the whole parent folder is covered.")
		}
	}
}

// requiredOption is the --option the suggested type cannot be added without
// (an R2 account id, an Azure storage account name), so the printed command
// works when the placeholder is filled in.
func requiredOption(s *workspace.StorageSuggestion) string {
	switch s.Type {
	case "r2":
		return " --option 'account_id=<account id>'"
	case "azure":
		if acct, err := storageurl.AzureAccount(s.Scope); err == nil && acct != "" && s.Scope != "" {
			return " --option account_name=" + acct
		}
		return " --option 'account_name=<account name>'"
	}
	return ""
}
