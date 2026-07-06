package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Inspect contexts configured in .unleashctl.yaml / ~/.unleashctl/config.yaml",
}

var contextListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured contexts",
	RunE:  runContextList,
}

var contextGetCurrentCmd = &cobra.Command{
	Use:   "get-current",
	Short: "Print the current-context name",
	RunE:  runContextGetCurrent,
}

func init() {
	contextCmd.AddCommand(contextListCmd)
	contextCmd.AddCommand(contextGetCurrentCmd)
	rootCmd.AddCommand(contextCmd)
}

// contextRow is the display shape for `context list`, separate from the
// on-disk Context struct so it can carry derived fields (AllowSync resolved,
// Current) without changing the config schema.
type contextRow struct {
	Name        string `json:"name" yaml:"name"`
	Current     bool   `json:"current" yaml:"current"`
	URL         string `json:"url" yaml:"url"`
	Environment string `json:"environment,omitempty" yaml:"environment,omitempty"`
	TokenEnv    string `json:"tokenEnv,omitempty" yaml:"tokenEnv,omitempty"`
	AllowSync   bool   `json:"allowSync" yaml:"allowSync"`
}

func runContextList(_ *cobra.Command, _ []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	rows := make([]contextRow, len(cfg.Contexts))
	for i, c := range cfg.Contexts {
		rows[i] = contextRow{
			Name:        c.Name,
			Current:     c.Name == cfg.CurrentContext,
			URL:         c.URL,
			Environment: c.Environment,
			TokenEnv:    c.TokenEnv,
			AllowSync:   c.AllowSyncResolved(),
		}
	}
	return renderContexts(rows)
}

func runContextGetCurrent(_ *cobra.Command, _ []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if cfg.CurrentContext == "" {
		return fmt.Errorf("no current-context set in config")
	}

	switch flagOutput {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"currentContext": cfg.CurrentContext})
	case "yaml":
		enc := yaml.NewEncoder(os.Stdout)
		defer enc.Close()
		return enc.Encode(map[string]string{"currentContext": cfg.CurrentContext})
	default:
		fmt.Println(cfg.CurrentContext)
		return nil
	}
}

func renderContexts(rows []contextRow) error {
	switch flagOutput {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	case "yaml":
		enc := yaml.NewEncoder(os.Stdout)
		defer enc.Close()
		return enc.Encode(rows)
	default:
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "CURRENT\tNAME\tURL\tENVIRONMENT\tALLOW_SYNC")
		for _, r := range rows {
			current := ""
			if r.Current {
				current = "*"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%t\n", current, r.Name, r.URL, r.Environment, r.AllowSync)
		}
		return w.Flush()
	}
}
