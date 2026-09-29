package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/importer"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init <file>",
	Short: "Create a new .vibe file",
	Long: `Create a new .vibe file at the specified path.

Prompts for a passphrase to encrypt the container. Optionally set
a title with --title, a description with --description.

Use --from to seed a bundled starter template (tables + sample data + a
starter notebook, e.g. "order-entry") or clone an existing .vibe file, or
--import to create with initial CSV data (--from and --import are mutually
exclusive). Run without any of these for a bare, empty workspace.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		// Ensure .vibe extension.
		if filepath.Ext(filePath) != ".vibe" {
			filePath += ".vibe"
		}

		title, _ := cmd.Flags().GetString("title")
		description, _ := cmd.Flags().GetString("description")
		fromTemplate, _ := cmd.Flags().GetString("from")
		importCSV, _ := cmd.Flags().GetString("import")

		// Validate mutually exclusive flags.
		if fromTemplate != "" && importCSV != "" {
			return fmt.Errorf("--from and --import are mutually exclusive")
		}

		if title == "" {
			// Derive title from filename.
			base := filepath.Base(filePath)
			title = strings.TrimSuffix(base, ".vibe")
		}

		// Prompt for passphrase.
		passphrase, err := promptPassphraseConfirm()
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()

		if fromTemplate != "" {
			return initFromTemplate(filePath, fromTemplate, passphrase, title, description, out)
		}

		fmt.Fprintf(out, "Creating %s...\n", filePath)

		opts := []container.Option{
			container.WithPassphrase(passphrase),
			container.WithTitle(title),
			container.WithLogger(logger),
		}
		if description != "" {
			opts = append(opts, container.WithDescription(description))
		}

		c, err := container.Create(filePath, opts...)
		if err != nil {
			return err
		}

		// Handle --import: the same importer.Import DuckDB path `vibe import`
		// uses, with create defaults (KeyAuto — a bare `init --import` has no
		// interactive key picker, so every table gets an editable primary key).
		if importCSV != "" {
			tableName := importer.NormalizeName(tableNameFromCSVPath(importCSV))
			src := importer.Source{Path: importCSV, HasHeader: true}
			sum, err := importer.Import(context.Background(), c.DuckDBDB(), importer.Options{
				Source: src, Table: tableName, Key: importer.KeyAuto,
			})
			if err != nil {
				c.Close()
				return fmt.Errorf("failed to import %q: %w", importCSV, err)
			}
			if !sum.OK {
				c.Close()
				if sum.Blocked != "" {
					return fmt.Errorf("failed to import %q: %s", importCSV, sum.Blocked)
				}
				return fmt.Errorf("failed to import %q: %d value(s) don't fit their column type", importCSV, sum.BadCount)
			}
			if err := agents.WriteImportSkill(c.SQLiteDB(), c.DuckDBDB(), tableName); err != nil {
				logger.Warn("failed to write import skill", "table", tableName, "error", err)
			}
			fmt.Fprintf(out, "Imported %d rows into %q\n", sum.Rows, tableName)
		}

		// Save and close.
		if err := c.Save(); err != nil {
			c.Close()
			return fmt.Errorf("failed to save container: %w", err)
		}
		if err := c.Close(); err != nil {
			return fmt.Errorf("failed to close container: %w", err)
		}

		fmt.Fprintf(out, "Created %s (title: %q)\n", filePath, title)
		return nil
	},
}

// initFromTemplate creates a new .vibe file from --from. The value is resolved
// deterministically: a registered bundled template name (P6.6 registry) is built
// from the template; otherwise it is treated as a path to an existing .vibe file
// to clone. An unknown value that is neither yields an actionable error listing
// the available template names.
func initFromTemplate(filePath, from, passphrase, title, description string, out io.Writer) error {
	if isRegisteredTemplate(from) {
		return initFromBundledTemplate(filePath, from, passphrase, title, description, out)
	}

	// Not a registered template name: treat as a path to clone. If it is also not
	// an existing file, surface an actionable error listing the template names.
	if _, err := os.Stat(from); err != nil {
		return fmt.Errorf(
			"unknown template or file %q: not a registered template and not an existing file; available templates: %s",
			from, strings.Join(container.ListTemplates(), ", "))
	}
	return initCloneFromFile(filePath, from, passphrase, title, description, out)
}

// isRegisteredTemplate reports whether name is a bundled starter template.
func isRegisteredTemplate(name string) bool {
	for _, n := range container.ListTemplates() {
		if n == name {
			return true
		}
	}
	return false
}

// initFromBundledTemplate creates a new encrypted .vibe seeded from a bundled
// starter template (P6.6 registry).
func initFromBundledTemplate(filePath, name, passphrase, title, description string, out io.Writer) error {
	fmt.Fprintf(out, "Creating %s from template %q...\n", filePath, name)

	opts := []container.Option{
		container.WithPassphrase(passphrase),
		container.WithTitle(title),
		container.WithLogger(logger),
		container.WithTemplate(name),
	}
	if description != "" {
		opts = append(opts, container.WithDescription(description))
	}

	c, err := container.Create(filePath, opts...)
	if err != nil {
		return fmt.Errorf("failed to create container from template %q: %w", name, err)
	}

	if err := c.Save(); err != nil {
		c.Close() //nolint:errcheck
		return fmt.Errorf("failed to save container: %w", err)
	}
	if err := c.Close(); err != nil {
		return fmt.Errorf("failed to close container: %w", err)
	}

	fmt.Fprintf(out, "Created %s (title: %q)\n", filePath, title)
	return nil
}

// initCloneFromFile creates a new .vibe file by copying a template file and rekeying.
func initCloneFromFile(filePath, templatePath, passphrase, title, description string, out io.Writer) error {

	fmt.Fprintf(out, "Creating %s from template %s...\n", filePath, templatePath)

	// Copy the template file.
	src, err := os.Open(templatePath)
	if err != nil {
		return fmt.Errorf("failed to open template: %w", err)
	}
	defer src.Close()

	// O_EXCL, like Create: a clone must never silently truncate an existing
	// file at filePath (it's also how a locked target's Open error below is
	// safe to react to by removing filePath — this command must always be
	// the one that created it).
	dst, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("failed to create file: file already exists at %s", filePath)
		}
		return fmt.Errorf("failed to create file: %w", err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(filePath)
		return fmt.Errorf("failed to copy template: %w", err)
	}
	dst.Close()

	// Open the copy with the template's passphrase to rekey it.
	// For now, the template passphrase is the same as the new one (user entered it).
	// In practice, the template might be unencrypted or use a known passphrase.
	c, err := container.Open(filePath,
		container.WithPassphrase(passphrase),
		container.WithLogger(logger),
	)
	if err != nil {
		os.Remove(filePath)
		var inUse *container.InUseError
		if errors.As(err, &inUse) {
			// Already carries container.Open's own "failed to open
			// container:" prefix — the passphrase-mismatch wording below
			// would be actively misleading for this error.
			return err
		}
		return fmt.Errorf("failed to open copied template (passphrase may not match template): %w", err)
	}

	// Update metadata.
	if err := c.SetMeta("title", title); err != nil {
		c.Close()
		os.Remove(filePath)
		return fmt.Errorf("failed to set title: %w", err)
	}
	if description != "" {
		if err := c.SetMeta("description", description); err != nil {
			c.Close()
			os.Remove(filePath)
			return fmt.Errorf("failed to set description: %w", err)
		}
	}

	if err := c.Save(); err != nil {
		c.Close()
		os.Remove(filePath)
		return fmt.Errorf("failed to save container: %w", err)
	}
	if err := c.Close(); err != nil {
		return fmt.Errorf("failed to close container: %w", err)
	}

	fmt.Fprintf(out, "Created %s (title: %q)\n", filePath, title)
	return nil
}

func init() {
	initCmd.Flags().String("title", "", "set the container title")
	initCmd.Flags().String("description", "", "set the container description")
	initCmd.Flags().String("from", "", "create from a bundled template name (e.g. order-entry) or clone from a .vibe file path")
	initCmd.Flags().String("import", "", "import CSV data into the new container")
	rootCmd.AddCommand(initCmd)
}
