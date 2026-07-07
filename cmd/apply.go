package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kcgaisford/unleashctl/internal/client"
	"github.com/kcgaisford/unleashctl/internal/differ"
	"github.com/kcgaisford/unleashctl/internal/render"
	"github.com/kcgaisford/unleashctl/internal/state"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply flags/*.yaml to a live instance",
	Long: `apply resolves and prints the same comparison diff would show (spec §6.1),
then applies it via the scoped validate-then-import flow, scoped to --service
if given (otherwise every service present in flags/).`,
	RunE: runApply,
}

func init() {
	applyCmd.Flags().StringVar(&flagService, "service", "", "limit to features tagged with this service (default: every service in flags/)")
	applyCmd.Flags().StringVar(&flagFlagsDir, "flags-dir", "flags", "directory containing *.yaml desired-state files")
	applyCmd.Flags().BoolVar(&flagYes, "yes", false, "apply without an interactive confirmation prompt")
	applyCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "print the request payload; make no network call")
	applyCmd.Flags().BoolVar(&flagArchiveMissing, "archive-missing", false, "treat remote features in scope with no local file as archive candidates (requires --service)")
	applyCmd.Flags().BoolVarP(&flagInteractive, "interactive", "i", false, "confirm each archive candidate individually instead of one batch confirmation")
	applyCmd.MarkFlagsMutuallyExclusive("yes", "interactive")
	rootCmd.AddCommand(applyCmd)
}

func runApply(_ *cobra.Command, _ []string) error {
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
		if !result.HasChanges() {
			continue
		}

		if flagDryRun {
			if len(result.Changes) > 0 {
				scoped := state.FilterByService(files, service)
				payload := differ.BuildImportPayload(scoped, conn.Environment, conn.ContextName, conn.UIManagedEnabled)
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				fmt.Printf("\n--dry-run: request payload for service %s (no network call made)\n", service)
				if err := enc.Encode(payload); err != nil {
					return err
				}
			}
			if len(result.Archive) > 0 {
				fmt.Printf("\n--dry-run: would archive %d flag(s) in service %s (no network call made):\n", len(result.Archive), service)
				for _, name := range result.Archive {
					fmt.Printf("  %s\n", name)
				}
			}
			continue
		}

		if len(result.Changes) > 0 {
			if !flagYes && !confirm(fmt.Sprintf("Apply %d change(s) to service %s?", len(result.Changes), service)) {
				fmt.Println("Skipped.")
			} else {
				var reviveNames []string
				for _, ch := range result.Changes {
					if ch.Action == differ.ActionRevive {
						reviveNames = append(reviveNames, ch.FeatureName)
					}
				}
				if len(reviveNames) > 0 {
					if err := c.ReviveFeatures(ctx, defaultProject, reviveNames); err != nil {
						return fmt.Errorf("reviving for service %s: %w", service, err)
					}
				}

				scoped := state.FilterByService(files, service)
				payload := differ.BuildImportPayload(scoped, conn.Environment, conn.ContextName, conn.UIManagedEnabled)

				validation, err := c.ValidateImport(ctx, defaultProject, conn.Environment, payload)
				if err != nil {
					return fmt.Errorf("validating import for service %s: %w", service, err)
				}
				if len(validation.Errors) > 0 {
					for _, e := range validation.Errors {
						fmt.Fprintf(os.Stderr, "validation error: %s\n", e.Message)
					}
					return fmt.Errorf("import validation failed for service %s", service)
				}
				for _, w := range validation.Warnings {
					fmt.Fprintf(os.Stderr, "validation warning: %s\n", w.Message)
				}

				if err := c.Import(ctx, defaultProject, conn.Environment, payload); err != nil {
					return fmt.Errorf("importing for service %s: %w", service, err)
				}
				fmt.Printf("Applied %d change(s) to service %s.\n", len(result.Changes), service)
			}
		}

		if len(result.Archive) > 0 {
			names := result.Archive
			switch {
			case flagInteractive:
				var aborted bool
				names, aborted = confirmArchiveInteractive(names)
				if aborted {
					fmt.Println("Archive aborted.")
					names = nil
				}
			case !flagYes:
				if !confirm(fmt.Sprintf("Archive %d flag(s) with no local file in service %s?", len(names), service)) {
					names = nil
				}
			}

			if len(names) > 0 {
				if err := c.ArchiveFeatures(ctx, defaultProject, names); err != nil {
					return fmt.Errorf("archiving for service %s: %w", service, err)
				}
				fmt.Printf("Archived %d flag(s) in service %s.\n", len(names), service)
			} else if len(result.Archive) > 0 {
				fmt.Println("Skipped archiving.")
			}
		}
	}

	return nil
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// confirmArchiveInteractive walks names one at a time, prompting y/N/a for
// each (spec §6.1's -i: confirm, skip, or abort per flag). Choosing "a"
// aborts and discards the whole batch — nothing gets archived, including
// names already confirmed earlier in the walk, since no archive call has
// been made yet at that point.
func confirmArchiveInteractive(names []string) (selected []string, aborted bool) {
	reader := bufio.NewReader(os.Stdin)
	for _, name := range names {
		fmt.Printf("Archive %s? [y/N/a] ", name)
		line, _ := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "a", "abort":
			return nil, true
		case "y", "yes":
			selected = append(selected, name)
		}
	}
	return selected, false
}
