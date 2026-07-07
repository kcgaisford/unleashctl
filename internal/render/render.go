// Package render formats differ.Result for terminal output (table/json/yaml,
// per spec §3's --output convention).
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kcgaisford/unleashctl/internal/differ"
)

// Diff writes result to w in the requested format ("table", "json", "yaml").
// An unrecognized format falls back to "table".
func Diff(w io.Writer, format string, result differ.Result) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case "yaml":
		enc := yaml.NewEncoder(w)
		defer enc.Close()
		return enc.Encode(result)
	default:
		return diffTable(w, result)
	}
}

// actionSymbol gives each Action a Terraform-plan-style marker: "+" for a
// full resource body (Create/Revive), "~" for a field-level diff (Update).
func actionSymbol(a differ.Action) string {
	switch a {
	case differ.ActionCreate, differ.ActionRevive:
		return "+"
	case differ.ActionUpdate:
		return "~"
	default:
		return " "
	}
}

func diffTable(w io.Writer, result differ.Result) error {
	if !result.HasChanges() {
		fmt.Fprintln(w, "No changes.")
	}
	for _, c := range result.Changes {
		sym := actionSymbol(c.Action)
		action := strings.ToUpper(string(c.Action))
		fmt.Fprintf(w, "%s %-7s %s\n", sym, action, c.FeatureName)
		for _, d := range c.Details {
			fmt.Fprintf(w, "    %s %s\n", sym, d)
		}
	}
	if len(result.Informational) > 0 {
		fmt.Fprintf(w, "\n%d flag(s) in this service have no local file — not archiving; rerun with --archive-missing to review:\n", len(result.Informational))
		for _, name := range result.Informational {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	if len(result.Archive) > 0 {
		fmt.Fprintf(w, "\n%d flag(s) in this service have no local file — will be archived (--archive-missing):\n", len(result.Archive))
		for _, name := range result.Archive {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	return nil
}

// ContextDiff writes result to w in the requested format ("table", "json", "yaml").
// An unrecognized format falls back to "table".
func ContextDiff(w io.Writer, format string, result differ.ContextResult) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case "yaml":
		enc := yaml.NewEncoder(w)
		defer enc.Close()
		return enc.Encode(result)
	default:
		return contextDiffTable(w, result)
	}
}

func contextActionSymbol(a differ.ContextAction) string {
	if a == differ.ContextActionCreate {
		return "+"
	}
	return "~"
}

func contextDiffTable(w io.Writer, result differ.ContextResult) error {
	if !result.HasChanges() {
		fmt.Fprintln(w, "No changes.")
	}
	for _, c := range result.Changes {
		sym := contextActionSymbol(c.Action)
		action := strings.ToUpper(string(c.Action))
		fmt.Fprintf(w, "%s %-7s %s\n", sym, action, c.Name)
		for _, d := range c.Details {
			fmt.Fprintf(w, "    %s %s\n", sym, d)
		}
	}
	if len(result.Informational) > 0 {
		fmt.Fprintf(w, "\n%d context field(s) have no local file — not deleting; rerun with --delete-missing to review:\n", len(result.Informational))
		for _, name := range result.Informational {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	if len(result.Delete) > 0 {
		fmt.Fprintf(w, "\n%d context field(s) have no local file — will be deleted (--delete-missing):\n", len(result.Delete))
		for _, name := range result.Delete {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	return nil
}
