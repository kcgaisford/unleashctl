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
	rootCmd.AddCommand(applyCmd)
}

func runApply(_ *cobra.Command, _ []string) error {
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

	c := client.New(conn.URL, conn.Token)
	ctx := context.Background()

	for _, service := range services {
		result, conflicts, err := runDiffScoped(ctx, c, conn, service, files)
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

		scoped := state.FilterByService(files, service)
		payload := differ.BuildImportPayload(scoped, conn.Environment, conn.ContextName)

		if flagDryRun {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			fmt.Printf("\n--dry-run: request payload for service %s (no network call made)\n", service)
			if err := enc.Encode(payload); err != nil {
				return err
			}
			continue
		}

		if !flagYes && !confirm(fmt.Sprintf("Apply %d change(s) to service %s?", len(result.Changes), service)) {
			fmt.Println("Skipped.")
			continue
		}

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

	return nil
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
