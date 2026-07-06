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

func diffTable(w io.Writer, result differ.Result) error {
	if len(result.Changes) == 0 {
		fmt.Fprintln(w, "No changes.")
	}
	for _, c := range result.Changes {
		action := strings.ToUpper(string(c.Action))
		fmt.Fprintf(w, "%-7s %s\n", action, c.FeatureName)
		for _, d := range c.Details {
			fmt.Fprintf(w, "        %s\n", d)
		}
	}
	if len(result.Informational) > 0 {
		fmt.Fprintf(w, "\n%d flag(s) in this service have no local file — not archiving; rerun with --archive-missing to review:\n", len(result.Informational))
		for _, name := range result.Informational {
			fmt.Fprintf(w, "  %s\n", name)
		}
	}
	return nil
}
