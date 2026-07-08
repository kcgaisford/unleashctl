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

// segmentsCmd groups segment subcommands. Segments are global (or
// project-scoped) reusable constraint sets that can be attached to
// activation strategies — a flat resource like ContextField, with no
// project/environment/service scoping of its own.
var segmentsCmd = &cobra.Command{
	Use:   "segments",
	Short: "Manage Unleash segments as code",
}

var segmentsDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show pending create/update changes between segments/*.yaml and a live instance",
	Long: `diff compares the desired state in segments/*.yaml against every segment
configured on the live instance. Segments are global — there's no
--service/environment scoping like Feature flags have. A remote segment
with no local file is reported informationally, never as a delete
candidate, unless --delete-missing is passed.

Exit codes: 0 = no changes, 2 = changes pending, other = error.`,
	RunE: runSegmentsDiff,
}

var segmentsApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply segments/*.yaml to a live instance",
	Long: `apply resolves and prints the same comparison diff would show, then
creates/updates each pending segment individually (there's no batch import
endpoint for segments, unlike Feature flags).`,
	RunE: runSegmentsApply,
}

func init() {
	segmentsCmd.PersistentFlags().StringVar(&flagSegmentsDir, "segments-dir", "segments", "directory containing *.yaml desired-state files")
	segmentsCmd.PersistentFlags().BoolVar(&flagDeleteMissing, "delete-missing", false, "delete remote segments with no local file")

	segmentsApplyCmd.Flags().BoolVar(&flagYes, "yes", false, "apply without an interactive confirmation prompt")
	segmentsApplyCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "print the planned requests; make no network call")
	segmentsApplyCmd.Flags().BoolVarP(&flagInteractive, "interactive", "i", false, "confirm each delete candidate individually instead of one batch confirmation")
	segmentsApplyCmd.MarkFlagsMutuallyExclusive("yes", "interactive")

	segmentsCmd.AddCommand(segmentsDiffCmd)
	segmentsCmd.AddCommand(segmentsApplyCmd)
	rootCmd.AddCommand(segmentsCmd)
}

// runSegmentsScoped fetches remote segments and diffs them against files.
// Shared by diff and apply so both commands see identical output.
func runSegmentsScoped(ctx context.Context, c *client.Client, files []*state.SegmentFile) (differ.SegmentResult, error) {
	remote, err := c.ListSegments(ctx)
	if err != nil {
		return differ.SegmentResult{}, fmt.Errorf("fetching remote segments: %w", err)
	}
	return differ.DiffSegments(files, remote, flagDeleteMissing), nil
}

func runSegmentsDiff(_ *cobra.Command, _ []string) error {
	conn, err := resolveConnection()
	if err != nil {
		return err
	}

	files, err := state.LoadSegments(flagSegmentsDir)
	if err != nil {
		return err
	}

	c := client.New(conn.URL, conn.Token)
	ctx := context.Background()

	result, err := runSegmentsScoped(ctx, c, files)
	if err != nil {
		return err
	}

	return render.SegmentDiff(os.Stdout, flagOutput, result)
}

func runSegmentsApply(_ *cobra.Command, _ []string) error {
	conn, err := resolveConnection()
	if err != nil {
		return err
	}

	files, err := state.LoadSegments(flagSegmentsDir)
	if err != nil {
		return err
	}
	filesByName := make(map[string]*state.SegmentFile, len(files))
	for _, f := range files {
		filesByName[f.Metadata.Name] = f
	}

	c := client.New(conn.URL, conn.Token)
	ctx := context.Background()

	result, err := runSegmentsScoped(ctx, c, files)
	if err != nil {
		return err
	}

	if err := render.SegmentDiff(os.Stdout, flagOutput, result); err != nil {
		return err
	}
	if !result.HasChanges() {
		return nil
	}

	if flagDryRun {
		for _, ch := range result.Changes {
			printSegmentDryRun(ch, filesByName[ch.Name])
		}
		if len(result.Delete) > 0 {
			fmt.Printf("\n--dry-run: would delete %d segment(s) (no network call made):\n", len(result.Delete))
			for _, candidate := range result.Delete {
				fmt.Printf("  DELETE /api/admin/segments/%d (%s)\n", candidate.ID, candidate.Name)
			}
		}
		return nil
	}

	if len(result.Changes) > 0 {
		if !flagYes && !confirm(fmt.Sprintf("Apply %d change(s) to segments?", len(result.Changes))) {
			fmt.Println("Skipped.")
		} else {
			for _, ch := range result.Changes {
				file := filesByName[ch.Name]
				if ch.Action == differ.SegmentActionCreate {
					if err := c.CreateSegment(ctx, buildUpsertSegmentRequest(file)); err != nil {
						return fmt.Errorf("creating segment %s: %w", ch.Name, err)
					}
				} else {
					if err := c.UpdateSegment(ctx, *ch.ID, buildUpsertSegmentRequest(file)); err != nil {
						return fmt.Errorf("updating segment %s: %w", ch.Name, err)
					}
				}
			}
			fmt.Printf("Applied %d change(s) to segments.\n", len(result.Changes))
		}
	}

	if len(result.Delete) > 0 {
		candidates := result.Delete
		switch {
		case flagInteractive:
			var aborted bool
			candidates, aborted = confirmSegmentDeleteInteractive(candidates)
			if aborted {
				fmt.Println("Delete aborted.")
				candidates = nil
			}
		case !flagYes:
			if !confirm(fmt.Sprintf("Delete %d segment(s) with no local file?", len(candidates))) {
				candidates = nil
			}
		}

		if len(candidates) > 0 {
			for _, candidate := range candidates {
				if err := c.DeleteSegment(ctx, candidate.ID); err != nil {
					return fmt.Errorf("deleting segment %s: %w", candidate.Name, err)
				}
			}
			fmt.Printf("Deleted %d segment(s).\n", len(candidates))
		} else if len(result.Delete) > 0 {
			fmt.Println("Skipped deleting.")
		}
	}

	return nil
}

func printSegmentDryRun(ch differ.SegmentChange, file *state.SegmentFile) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if ch.Action == differ.SegmentActionCreate {
		fmt.Printf("\n--dry-run: POST /api/admin/segments (no network call made)\n")
		_ = enc.Encode(buildUpsertSegmentRequest(file))
	} else {
		fmt.Printf("\n--dry-run: PUT /api/admin/segments/%d (no network call made)\n", *ch.ID)
		_ = enc.Encode(buildUpsertSegmentRequest(file))
	}
}

func buildUpsertSegmentRequest(file *state.SegmentFile) gen.UpsertSegmentSchema {
	return gen.UpsertSegmentSchema{
		Name:        file.Metadata.Name,
		Description: file.Spec.Description,
		Project:     file.Spec.Project,
		Constraints: toGenConstraints(file.Spec.Constraints),
	}
}

func toGenConstraints(constraints *[]state.Constraint) []gen.ConstraintSchema {
	if constraints == nil {
		// The Admin API requires constraints to be an array, not null — a
		// segment with no constraints: block still needs [] on the wire.
		return []gen.ConstraintSchema{}
	}
	out := make([]gen.ConstraintSchema, len(*constraints))
	for i, c := range *constraints {
		out[i] = gen.ConstraintSchema{
			ContextName:     c.ContextName,
			Operator:        gen.ConstraintSchemaOperator(c.Operator),
			CaseInsensitive: c.CaseInsensitive,
			Inverted:        c.Inverted,
			Value:           c.Value,
			Values:          c.Values,
		}
	}
	return out
}

// confirmSegmentDeleteInteractive walks candidates one at a time, prompting
// y/N/a for each, mirroring confirmDeleteInteractive (cmd/contextfields.go)
// but keyed on SegmentDeleteCandidate since segment deletes need an id, not
// just a name.
func confirmSegmentDeleteInteractive(candidates []differ.SegmentDeleteCandidate) (selected []differ.SegmentDeleteCandidate, aborted bool) {
	reader := bufio.NewReader(os.Stdin)
	for _, candidate := range candidates {
		fmt.Printf("Delete %s? [y/N/a] ", candidate.Name)
		line, _ := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "a", "abort":
			return nil, true
		case "y", "yes":
			selected = append(selected, candidate)
		}
	}
	return selected, false
}
