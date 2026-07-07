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
	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/differ"
	"github.com/kcgaisford/unleashctl/internal/render"
	"github.com/kcgaisford/unleashctl/internal/state"
)

// contextFieldsCmd groups context-field subcommands under a name distinct
// from the pre-existing `context` command (CLI connection profiles,
// cmd/context.go) — this manages Unleash's own "custom context fields"
// feature instead, an unrelated concept that happens to share the word
// "context".
var contextFieldsCmd = &cobra.Command{
	Use:   "context-fields",
	Short: "Manage Unleash custom context fields as code",
}

var contextFieldsDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show pending create/update changes between contexts/*.yaml and a live instance",
	Long: `diff compares the desired state in contexts/*.yaml against every custom
context field configured on the live instance. Context fields are global —
there's no --service/environment scoping like Feature flags have. A remote
context field with no local file is reported informationally, never as a
delete candidate, unless --delete-missing is passed.

Exit codes: 0 = no changes, 2 = changes pending, other = error.`,
	RunE: runContextFieldsDiff,
}

var contextFieldsApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply contexts/*.yaml to a live instance",
	Long: `apply resolves and prints the same comparison diff would show, then
creates/updates each pending context field individually (there's no batch
import endpoint for context fields, unlike Feature flags).`,
	RunE: runContextFieldsApply,
}

func init() {
	contextFieldsCmd.PersistentFlags().StringVar(&flagContextsDir, "contexts-dir", "contexts", "directory containing *.yaml desired-state files")
	contextFieldsCmd.PersistentFlags().BoolVar(&flagDeleteMissing, "delete-missing", false, "delete remote context fields with no local file")

	contextFieldsApplyCmd.Flags().BoolVar(&flagYes, "yes", false, "apply without an interactive confirmation prompt")
	contextFieldsApplyCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "print the planned requests; make no network call")
	contextFieldsApplyCmd.Flags().BoolVarP(&flagInteractive, "interactive", "i", false, "confirm each delete candidate individually instead of one batch confirmation")
	contextFieldsApplyCmd.MarkFlagsMutuallyExclusive("yes", "interactive")

	contextFieldsCmd.AddCommand(contextFieldsDiffCmd)
	contextFieldsCmd.AddCommand(contextFieldsApplyCmd)
	rootCmd.AddCommand(contextFieldsCmd)
}

// runContextFieldsScoped fetches remote context fields and diffs them
// against files. Shared by diff and apply so both commands see identical
// output.
func runContextFieldsScoped(ctx context.Context, c *client.Client, files []*state.ContextFile) (differ.ContextResult, error) {
	remote, err := c.ListContextFields(ctx)
	if err != nil {
		return differ.ContextResult{}, fmt.Errorf("fetching remote context fields: %w", err)
	}
	return differ.DiffContexts(files, remote, flagDeleteMissing), nil
}

func runContextFieldsDiff(_ *cobra.Command, _ []string) error {
	conn, err := resolveConnection()
	if err != nil {
		return err
	}

	files, err := state.LoadContexts(flagContextsDir)
	if err != nil {
		return err
	}

	c := client.New(conn.URL, conn.Token)
	ctx := context.Background()

	result, err := runContextFieldsScoped(ctx, c, files)
	if err != nil {
		return err
	}

	if err := render.ContextDiff(os.Stdout, flagOutput, result); err != nil {
		return err
	}

	if result.HasChanges() {
		os.Exit(2)
	}
	return nil
}

func runContextFieldsApply(_ *cobra.Command, _ []string) error {
	conn, err := resolveConnection()
	if err != nil {
		return err
	}

	files, err := state.LoadContexts(flagContextsDir)
	if err != nil {
		return err
	}
	filesByName := make(map[string]*state.ContextFile, len(files))
	for _, f := range files {
		filesByName[f.Metadata.Name] = f
	}

	c := client.New(conn.URL, conn.Token)
	ctx := context.Background()

	result, err := runContextFieldsScoped(ctx, c, files)
	if err != nil {
		return err
	}

	if err := render.ContextDiff(os.Stdout, flagOutput, result); err != nil {
		return err
	}
	if !result.HasChanges() {
		return nil
	}

	if flagDryRun {
		for _, ch := range result.Changes {
			printContextFieldDryRun(ch, filesByName[ch.Name])
		}
		if len(result.Delete) > 0 {
			fmt.Printf("\n--dry-run: would delete %d context field(s) (no network call made):\n", len(result.Delete))
			for _, name := range result.Delete {
				fmt.Printf("  DELETE /api/admin/context/%s\n", name)
			}
		}
		return nil
	}

	if len(result.Changes) > 0 {
		if !flagYes && !confirm(fmt.Sprintf("Apply %d change(s) to context fields?", len(result.Changes))) {
			fmt.Println("Skipped.")
		} else {
			for _, ch := range result.Changes {
				file := filesByName[ch.Name]
				if ch.Action == differ.ContextActionCreate {
					if err := c.CreateContextField(ctx, buildCreateContextFieldRequest(file)); err != nil {
						return fmt.Errorf("creating context field %s: %w", ch.Name, err)
					}
				} else {
					if err := c.UpdateContextField(ctx, ch.Name, buildUpdateContextFieldRequest(file)); err != nil {
						return fmt.Errorf("updating context field %s: %w", ch.Name, err)
					}
				}
			}
			fmt.Printf("Applied %d change(s) to context fields.\n", len(result.Changes))
		}
	}

	if len(result.Delete) > 0 {
		names := result.Delete
		switch {
		case flagInteractive:
			var aborted bool
			names, aborted = confirmDeleteInteractive(names)
			if aborted {
				fmt.Println("Delete aborted.")
				names = nil
			}
		case !flagYes:
			if !confirm(fmt.Sprintf("Delete %d context field(s) with no local file?", len(names))) {
				names = nil
			}
		}

		if len(names) > 0 {
			for _, name := range names {
				if err := c.DeleteContextField(ctx, name); err != nil {
					return fmt.Errorf("deleting context field %s: %w", name, err)
				}
			}
			fmt.Printf("Deleted %d context field(s).\n", len(names))
		} else if len(result.Delete) > 0 {
			fmt.Println("Skipped deleting.")
		}
	}

	return nil
}

func printContextFieldDryRun(ch differ.ContextChange, file *state.ContextFile) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if ch.Action == differ.ContextActionCreate {
		fmt.Printf("\n--dry-run: POST /api/admin/context (no network call made)\n")
		_ = enc.Encode(buildCreateContextFieldRequest(file))
	} else {
		fmt.Printf("\n--dry-run: PUT /api/admin/context/%s (no network call made)\n", ch.Name)
		_ = enc.Encode(buildUpdateContextFieldRequest(file))
	}
}

func buildCreateContextFieldRequest(file *state.ContextFile) gen.CreateContextFieldSchema {
	return gen.CreateContextFieldSchema{
		Name:        file.Metadata.Name,
		Description: file.Spec.Description,
		Stickiness:  file.Spec.Stickiness,
		LegalValues: toGenLegalValues(file.Spec.LegalValues),
	}
}

func buildUpdateContextFieldRequest(file *state.ContextFile) gen.UpdateContextFieldSchema {
	return gen.UpdateContextFieldSchema{
		Description: file.Spec.Description,
		Stickiness:  file.Spec.Stickiness,
		LegalValues: toGenLegalValues(file.Spec.LegalValues),
	}
}

func toGenLegalValues(values *[]state.LegalValue) *[]gen.LegalValueSchema {
	if values == nil {
		return nil
	}
	out := make([]gen.LegalValueSchema, len(*values))
	for i, v := range *values {
		out[i] = gen.LegalValueSchema{Value: v.Value, Description: v.Description}
	}
	return &out
}

// confirmDeleteInteractive walks names one at a time, prompting y/N/a for
// each, mirroring confirmArchiveInteractive (cmd/apply.go) but for context
// field deletion. Choosing "a" aborts and discards the whole batch.
func confirmDeleteInteractive(names []string) (selected []string, aborted bool) {
	reader := bufio.NewReader(os.Stdin)
	for _, name := range names {
		fmt.Printf("Delete %s? [y/N/a] ", name)
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
