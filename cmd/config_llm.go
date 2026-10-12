package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SandwichLabs/vibe/internal/config"
	"github.com/SandwichLabs/vibe/internal/llm"
)

var llmCmd = &cobra.Command{
	Use:   "llm",
	Short: "Show or change the agent's model provider (mirrors Studio's Settings → App)",
	Long: `Show or change which model Vibe's own agent uses. This edits llm.provider,
llm.model, llm.endpoint, llm.account_id and llm.gateway in the config file
(~/.vibe.yaml, or $VIBE_CONFIG_FILE), keeping its comments. API keys are not set
here; use "vibe config llm-key".`,
}

var llmShowCmd = &cobra.Command{
	Use:          "show",
	SilenceUsage: true,
	Short:        "Show the provider, model and where each setting comes from",
	Args:         cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()
		c := config.Load().LLM
		info, _ := config.LLMProvider(c.Provider)
		ov := config.LLMEnvOverrides()
		by := func(field string) string {
			if name, ok := ov[field]; ok {
				return "  (set by " + name + ")"
			}
			return ""
		}
		fmt.Fprintf(out, "provider: %s (%s)%s\n", c.Provider, info.Label, by("provider"))
		switch {
		case c.Model != "":
			fmt.Fprintf(out, "model:    %s%s\n", c.Model, by("model"))
		case config.DefaultModelFor(c.Provider) != "":
			fmt.Fprintf(out, "model:    %s (default)\n", config.DefaultModelFor(c.Provider))
		default:
			fmt.Fprintf(out, "model:    (none; %s has no default, run \"vibe config llm set --model ...\")\n", c.Provider)
		}
		if info.HasEndpoint {
			fmt.Fprintf(out, "endpoint: %s%s\n", c.Endpoint, by("endpoint"))
		}
		if info.HasAccount {
			acct := c.AccountID
			if acct == "" {
				acct = "(not set)"
			}
			note := by("accountId")
			if c.AccountIDFromEnv {
				note = "  (from CLOUDFLARE_ACCOUNT_ID)"
			}
			fmt.Fprintf(out, "account:  %s%s\n", acct, note)
			gw := c.Gateway
			if gw == "" {
				gw = "(default)"
			}
			fmt.Fprintf(out, "gateway:  %s%s\n", gw, by("gateway"))
		}
		return nil
	},
}

var llmSet struct {
	provider, model, endpoint, accountID, gateway string
	yes                                           bool
}

var llmSetCmd = &cobra.Command{
	Use:          "set",
	SilenceUsage: true,
	Short:        "Change the provider, model, endpoint, Cloudflare account or gateway",
	Long: `Changes only the flags you pass; the rest of llm.* stays. Pass an empty value to
clear one (for example --model "" to use the provider's default).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		fl := cmd.Flags()
		if !(fl.Changed("provider") || fl.Changed("model") || fl.Changed("endpoint") || fl.Changed("account-id") || fl.Changed("gateway")) {
			return fmt.Errorf("nothing to change: pass --provider, --model, --endpoint, --account-id or --gateway")
		}
		path := config.LLMConfigPath()
		s := config.ReadLLMSettings(path)
		if fl.Changed("provider") {
			s.Provider = llmSet.provider
		}
		if fl.Changed("model") {
			s.Model = llmSet.model
		}
		if fl.Changed("endpoint") {
			s.Endpoint = llmSet.endpoint
		}
		if fl.Changed("account-id") {
			s.AccountID = llmSet.accountID
		}
		if fl.Changed("gateway") {
			s.Gateway = llmSet.gateway
		}
		if s.Provider == "" {
			s.Provider = config.LLMProviderLocal
		}
		if prev := config.ReadLLMSettings(path).Provider; s.Provider != prev && !llmSet.yes {
			if err := checkFileKeyHazard(cmd, s.Provider); err != nil {
				return err
			}
		}
		if err := config.SaveLLMSettings(path, s); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Saved to %s.\n", path)
		for field, name := range config.LLMEnvOverrides() {
			fmt.Fprintf(cmd.ErrOrStderr(), "note: %s is set in the environment (%s) and overrides the file\n", field, name)
		}
		return nil
	},
}

// checkFileKeyHazard refuses a provider change that would send the legacy
// plaintext llm.api_key from the config file to the new provider.
func checkFileKeyHazard(cmd *cobra.Command, provider string) error {
	cfg := config.Load().LLM
	cfg.Provider = provider
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	if _, src := config.ResolveLLMKey(ctx, llmKeyStore(), cfg); src != config.KeySourceFile {
		return nil
	}
	label := provider
	if p, ok := config.LLMProvider(provider); ok {
		label = p.Label
	}
	return fmt.Errorf("the plaintext key in %s would be sent to %s. Move it to the keychain first with \"vibe config llm-key migrate\", save a key for %s with \"vibe config llm-key set --provider %s\", or pass --yes to use it anyway",
		config.LLMConfigPath(), label, label, provider)
}

var llmModelsCmd = &cobra.Command{
	Use:          "models",
	SilenceUsage: true,
	Short:        "List the models Settings offers for the configured provider",
	Long: `Lists the models a local server or Ollama has installed, or the suggested models
for a cloud provider (the same list Settings shows).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()
		c := config.Load().LLM
		switch c.Provider {
		case config.LLMProviderLocal, config.LLMProviderOllama:
			l := llm.ListModels(cmd.Context(), c.Provider, c.Endpoint)
			if l.Error != "" {
				return fmt.Errorf("%s", l.Error)
			}
			if len(l.Models) == 0 {
				fmt.Fprintln(out, "(no models installed)")
			}
			for _, m := range l.Models {
				fmt.Fprintln(out, m)
			}
		default:
			info, _ := config.LLMProvider(c.Provider)
			names := info.SuggestedModels
			fmt.Fprintf(out, "# suggestions for %s; any model id the provider accepts works\n%s\n", info.Label, strings.Join(names, "\n"))
		}
		return nil
	},
}

func init() {
	f := llmSetCmd.Flags()
	f.StringVar(&llmSet.provider, "provider", "", "provider ("+strings.Join(config.LLMProviderIDs(), ", ")+")")
	f.StringVar(&llmSet.model, "model", "", "model id (empty = the provider's default)")
	f.StringVar(&llmSet.endpoint, "endpoint", "", "base URL for local and ollama")
	f.StringVar(&llmSet.accountID, "account-id", "", "Cloudflare account ID (cloudflare)")
	f.StringVar(&llmSet.gateway, "gateway", "", "Cloudflare AI Gateway ID (cloudflare; empty = the default gateway)")
	f.BoolVar(&llmSet.yes, "yes", false, "use the plaintext llm.api_key from the config file for the new provider")
	llmCmd.AddCommand(llmShowCmd, llmSetCmd, llmModelsCmd)
}
