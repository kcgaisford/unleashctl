package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kcgaisford/unleashctl/internal/client"
	"github.com/kcgaisford/unleashctl/internal/differ"
	"github.com/kcgaisford/unleashctl/internal/ownership"
	"github.com/kcgaisford/unleashctl/internal/render"
	"github.com/kcgaisford/unleashctl/internal/state"
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show pending create/update changes between flags/*.yaml and a live instance",
	Long: `diff compares the desired state in flags/*.yaml against the live instance
for --context, scoped to --service if given (otherwise every service present
in flags/). It is always additive-only: a remote feature with no local file
is reported informationally, never as a delete candidate (spec §6.1).

Exit codes: 0 = no changes, 2 = changes pending, other = error.`,
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().StringVar(&flagService, "service", "", "limit to features tagged with this service (default: every service in flags/)")
	diffCmd.Flags().StringVar(&flagFlagsDir, "flags-dir", "flags", "directory containing *.yaml desired-state files")
	diffCmd.Flags().BoolVar(&flagArchiveMissing, "archive-missing", false, "treat remote features in scope with no local file as archive candidates (requires --service)")
	rootCmd.AddCommand(diffCmd)
}

// requireServiceForArchiveMissing enforces spec §6.1: --archive-missing
// requires --service, since scope can't be inferred from local files once
// the last file for a service is gone.
func requireServiceForArchiveMissing() error {
	if flagArchiveMissing && flagService == "" {
		return fmt.Errorf("--archive-missing requires --service")
	}
	return nil
}

// runDiffScoped runs the fetch+ownership+diff pipeline for one service and
// returns the comparison result. Shared by diff and apply so both commands
// see identical output (spec §6.3).
func runDiffScoped(ctx context.Context, c *client.Client, conn *connection, service string, files []*state.File, archiveMissing bool) (differ.Result, []ownership.Conflict, error) {
	scoped := state.FilterByService(files, service)

	// Unleash's tag-scoped export filters on tag_value only, ignoring tag_type
	// (confirmed against feature-tag-store.ts's getAllFeaturesForTag) — so the
	// filter value is the bare service name, not "service:<name>".
	remote, err := c.ExportByTag(ctx, conn.Environment, service)
	if err != nil {
		return differ.Result{}, nil, fmt.Errorf("fetching remote state for service %s: %w", service, err)
	}

	conflicts, err := ownership.Check(ctx, c, service, scoped, remote)
	if err != nil {
		return differ.Result{}, nil, fmt.Errorf("checking ownership for service %s: %w", service, err)
	}
	if len(conflicts) > 0 {
		return differ.Result{}, conflicts, nil
	}

	result := differ.Diff(scoped, remote, conn.Environment, conn.ContextName, conn.UIManagedEnabled, archiveMissing)
	return result, nil, nil
}

func servicesInScope(files []*state.File) ([]string, error) {
	if flagService != "" {
		return []string{flagService}, nil
	}
	services := state.Services(files)
	if len(services) == 0 {
		return nil, fmt.Errorf("no service-tagged flags found in %s (pass --service, or add metadata.service to at least one file)", flagFlagsDir)
	}
	return services, nil
}

func runDiff(_ *cobra.Command, _ []string) error {
	if err := requireServiceForArchiveMissing(); err != nil {
		return err
	}

	conn, err := resolveConnection()
	if err != nil {
		return err
	}

	files, err := state.Load(flagFlagsDir)
	if err != nil {
		return err
	}

	services, err := servicesInScope(files)
	if err != nil {
		return err
	}

	if conn.UIManagedEnabled {
		fmt.Printf("enabled/disabled is UI-managed for context %s — not diffed or applied\n", conn.ContextName)
	}

	c := client.New(conn.URL, conn.Token)
	ctx := context.Background()

	hasChanges := false
	for _, service := range services {
		result, conflicts, err := runDiffScoped(ctx, c, conn, service, files, flagArchiveMissing)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			for _, conflict := range conflicts {
				fmt.Fprintf(os.Stderr, "refusing service %s: %s\n", service, conflict.String())
			}
			os.Exit(1)
		}
		if len(services) > 1 {
			fmt.Printf("== service: %s ==\n", service)
		}
		if err := render.Diff(os.Stdout, flagOutput, result); err != nil {
			return err
		}
		if result.HasChanges() {
			hasChanges = true
		}
	}

	if hasChanges {
		os.Exit(2)
	}
	return nil
}
