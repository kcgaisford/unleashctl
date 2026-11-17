package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kcgaisford/unleashctl/internal/client"
	"github.com/kcgaisford/unleashctl/internal/client/gen"
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
	diffCmd.Flags().BoolVar(&flagForceEnabled, "force-enabled", false, "on a ui_managed_enabled context, diff enabled from flags/*.yaml as a pending change (preview of apply --force-enabled)")
	diffCmd.Flags().BoolVar(&flagForceConstr, "force-constraints", false, "on a ui_managed_constraints context, diff strategy constraints from flags/*.yaml as pending changes (preview of apply --force-constraints)")
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
// The fetched remote export is returned too, so apply can carry live
// UI-managed constraints into the import payload.
func runDiffScoped(ctx context.Context, c *client.Client, conn *connection, service string, files []*state.File, archiveMissing bool) (differ.Result, *gen.ExportResultSchema, []ownership.Conflict, error) {
	scoped := state.FilterByService(files, service)

	// Unleash's tag-scoped export filters on tag_value only, ignoring tag_type
	// (confirmed against feature-tag-store.ts's getAllFeaturesForTag) — so the
	// filter value is the bare service name, not "service:<name>".
	remote, err := c.ExportByTag(ctx, conn.Environment, service)
	if err != nil {
		return differ.Result{}, nil, nil, fmt.Errorf("fetching remote state for service %s: %w", service, err)
	}

	conflicts, err := ownership.Check(ctx, c, service, scoped, remote)
	if err != nil {
		return differ.Result{}, nil, nil, fmt.Errorf("checking ownership for service %s: %w", service, err)
	}
	if len(conflicts) > 0 {
		return differ.Result{}, nil, conflicts, nil
	}

	result := differ.Diff(scoped, remote, conn.Environment, conn.ContextName, conn.UIManaged, archiveMissing)
	return result, remote, nil, nil
}

// applyUIManagedOverride prints a note for each UI-managed field on the
// context and, with --force-enabled / --force-constraints, turns that field's
// override off for this run so flags/*.yaml is diffed and pushed as normal.
func applyUIManagedOverride(conn *connection) {
	note := func(field string, managed *bool, force bool, flag string) {
		if !*managed {
			return
		}
		if force {
			fmt.Printf("%s is UI-managed for context %s — overridden by %s, flags/*.yaml wins\n", field, conn.ContextName, flag)
			*managed = false
			return
		}
		fmt.Printf("%s is UI-managed for context %s — drift reported, never applied\n", field, conn.ContextName)
	}
	note("enabled/disabled", &conn.UIManaged.Enabled, flagForceEnabled, "--force-enabled")
	note("strategy constraints", &conn.UIManaged.Constraints, flagForceConstr, "--force-constraints")
}

// applyLegalValuesOverride is applyUIManagedOverride's counterpart for
// context-fields diff/apply.
func applyLegalValuesOverride(conn *connection) {
	if !conn.UIManaged.LegalValues {
		return
	}
	if flagForceLegal {
		fmt.Printf("legal values are UI-managed for context %s — overridden by --force-legal-values, contexts/*.yaml wins\n", conn.ContextName)
		conn.UIManaged.LegalValues = false
		return
	}
	fmt.Printf("legal values are UI-managed for context %s — drift reported, never applied\n", conn.ContextName)
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

	applyUIManagedOverride(conn)

	c := client.New(conn.URL, conn.Token)
	ctx := context.Background()

	for _, service := range services {
		result, _, conflicts, err := runDiffScoped(ctx, c, conn, service, files, flagArchiveMissing)
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
	}

	return nil
}
