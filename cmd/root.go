// Package cmd wires up the unleashctl Cobra command tree.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagContext     string
	flagURL         string
	flagToken       string
	flagEnvironment string
	flagOutput      string
	flagService        string
	flagYes            bool
	flagDryRun         bool
	flagFlagsDir       string
	flagArchiveMissing bool
	flagInteractive    bool
)

// defaultProject is the only project OSS Unleash has (spec §1: "One
// project" — OSS is effectively single-project).
const defaultProject = "default"

var rootCmd = &cobra.Command{
	Use:   "unleashctl",
	Short: "unleashctl manages Unleash feature flags as code",
	Long: `unleashctl wraps the Unleash Admin API. The core workflow is:
contexts (unleashctl config) -> flags/*.yaml -> diff/apply.`,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagContext, "context", "", "context to use (default: current-context from config)")
	rootCmd.PersistentFlags().StringVar(&flagURL, "url", "", "Unleash instance URL (overrides context)")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "Admin API token (overrides context; prefer env vars in CI)")
	rootCmd.PersistentFlags().StringVar(&flagEnvironment, "environment", "", "environment to target (default: context's configured environment)")
	rootCmd.PersistentFlags().StringVar(&flagOutput, "output", "table", "output format: table|json|yaml")
}

// Execute runs the root command. Called from main.go.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// connection is the resolved (context, URL, token, environment) an
// invocation targets.
type connection struct {
	ContextName string
	URL         string
	Token       string
	Environment string
	// UIManagedEnabled mirrors Context.UIManagedEnabled — see its doc comment.
	UIManagedEnabled bool
}

// resolveConnection implements spec §2's per-command precedence: flag > env
// (UNLEASHCTL_*) > context.
func resolveConnection() (*connection, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	contextName := flagContext
	if contextName == "" {
		contextName = cfg.CurrentContext
	}

	var ctx Context
	if contextName != "" {
		found, ok := cfg.FindContext(contextName)
		if !ok {
			return nil, fmt.Errorf("no context named %q in config", contextName)
		}
		ctx = *found
	}

	url := firstNonEmpty(flagURL, os.Getenv("UNLEASHCTL_URL"), ctx.URL)
	if url == "" {
		return nil, fmt.Errorf("no URL: pass --url, set UNLEASHCTL_URL, or configure a context")
	}

	token := flagToken
	if token == "" {
		token = os.Getenv("UNLEASHCTL_TOKEN")
	}
	if token == "" && ctx.TokenEnv != "" {
		token = os.Getenv(ctx.TokenEnv)
	}
	if token == "" {
		return nil, fmt.Errorf("no token: pass --token, set UNLEASHCTL_TOKEN, or set the context's token-env var")
	}

	environment := firstNonEmpty(flagEnvironment, ctx.Environment)
	if environment == "" {
		return nil, fmt.Errorf("no environment: pass --environment, or configure one on the context")
	}

	return &connection{
		ContextName:      contextName,
		URL:              url,
		Token:            token,
		Environment:      environment,
		UIManagedEnabled: ctx.UIManagedEnabled,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
