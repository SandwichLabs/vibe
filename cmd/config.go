package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SandwichLabs/vibe/internal/config"
	"github.com/SandwichLabs/vibe/internal/secrets"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage app-level settings that live outside a workspace",
}

var (
	llmKeyProvider string
	llmKeyForce    bool
)

var llmKeyCmd = &cobra.Command{
	Use:   "llm-key",
	Short: "Manage the agent's API key in the OS keychain",
	Long: `The agent's model API key lives in your OS keychain, never in a config file.
Where a key is read from, first match wins:

  1. VIBE_LLM_API_KEY
  2. the provider's standard variable (ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY,
     OLLAMA_API_KEY, CLOUDFLARE_API_TOKEN)
  3. the keychain (item "llm-<provider>")
  4. llm.api_key in ~/.vibe.yaml (legacy, used for whichever provider is configured;
     "vibe config llm-key migrate" moves it)

Vibe never prints a key back.`,
	PersistentPreRunE: checkLLMKeyProvider,
}

// validLLMProviders are the values --provider accepts: the same set Studio's
// key bindings accept (the provider list in internal/config).
var validLLMProviders = config.LLMProviderIDs()

// checkLLMKeyProvider rejects an unknown --provider before it can create a
// keychain item under a misspelt name.
func checkLLMKeyProvider(*cobra.Command, []string) error {
	if llmKeyProvider == "" || slices.Contains(validLLMProviders, llmKeyProvider) {
		return nil
	}
	return fmt.Errorf("unknown provider %q (want %s)", llmKeyProvider, strings.Join(validLLMProviders, ", "))
}

// newLLMKeyStore is a seam so tests never touch the real keychain.
var newLLMKeyStore = func() secrets.Store { return secrets.NewKeyringStore() }

func llmKeyStore() secrets.Store { return newLLMKeyStore() }

func resolveLLMKeyProvider() string {
	if llmKeyProvider != "" {
		return llmKeyProvider
	}
	return config.Load().LLM.Provider
}

var llmKeySetCmd = &cobra.Command{
	Use:   "set",
	Short: "Store the API key in the keychain (prompts without echo on a terminal, else reads one line from stdin)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		provider := resolveLLMKeyProvider()
		key, err := readSecretLine(cmd.InOrStdin(), cmd.ErrOrStderr(), fmt.Sprintf("API key for %s: ", provider))
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()
		cliRedactor.Add(key)
		if err := config.SetLLMKey(ctx, llmKeyStore(), provider, key); err != nil {
			return errors.New(cliRedactor.Redact(err.Error()))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Stored the %s API key in the keychain.\n", provider)
		return nil
	},
}

var llmKeyClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove the API key from the keychain",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		provider := resolveLLMKeyProvider()
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()
		if err := config.ClearLLMKey(ctx, llmKeyStore(), provider); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Removed the %s API key from the keychain.\n", provider)
		return nil
	},
}

var llmKeyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Say where the API key comes from (never the key itself)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg := config.Load()
		if llmKeyProvider != "" {
			cfg.LLM.Provider = llmKeyProvider
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()
		_, src := config.ResolveLLMKey(ctx, llmKeyStore(), cfg.LLM)
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "provider: %s\nsource:   %s\n", cfg.LLM.Provider, src)
		if config.HasFileKey(config.LLMConfigPath()) {
			fmt.Fprintln(out, "note:     a plaintext key is still in "+config.LLMConfigPath()+"; run \"vibe config llm-key migrate\"")
		}
		return nil
	},
}

var llmKeyMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Move llm.api_key from ~/.vibe.yaml into the keychain and blank it in the file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		provider := resolveLLMKeyProvider()
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()
		path := config.LLMConfigPath()
		if err := config.MigrateFileKey(ctx, llmKeyStore(), path, provider, llmKeyForce); err != nil {
			if errors.Is(err, config.ErrKeychainHasKey) {
				return fmt.Errorf("%w; re-run with --force to replace it", err)
			}
			return errors.New(cliRedactor.Redact(err.Error()))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Moved the API key into the keychain and blanked it in %s (backup: %s.bak).\n", path, path)
		return nil
	},
}

// readSecretLine reads a secret without echo from a terminal, or one line
// from in when it is not one.
func readSecretLine(in io.Reader, errW io.Writer, prompt string) (string, error) {
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(errW, prompt)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(errW)
		if err != nil {
			return "", fmt.Errorf("failed to read the key: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", errors.New("no key given on standard input")
	}
	return strings.TrimSpace(line), nil
}

func init() {
	llmKeyCmd.PersistentFlags().StringVar(&llmKeyProvider, "provider", "", "provider ("+strings.Join(validLLMProviders, ", ")+"); default: the configured one")
	llmKeyMigrateCmd.Flags().BoolVar(&llmKeyForce, "force", false, "replace a different key already in the keychain")
	llmKeyCmd.AddCommand(llmKeySetCmd, llmKeyClearCmd, llmKeyStatusCmd, llmKeyMigrateCmd)
	configCmd.AddCommand(llmKeyCmd, llmCmd)
	rootCmd.AddCommand(configCmd)
}
