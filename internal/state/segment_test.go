package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSegments(t *testing.T) {
	dir := t.TempDir()
	content := `apiVersion: unleashctl/v1
kind: Segment
metadata:
  name: betaUsers
spec:
  description: Users opted into the beta program
  constraints:
    - contextName: userId
      operator: IN
      values:
        - user-1
        - user-2
`
	if err := os.WriteFile(filepath.Join(dir, "betaUsers.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	files, err := LoadSegments(dir)
	if err != nil {
		t.Fatalf("LoadSegments: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d", len(files))
	}
	f := files[0]
	if f.Metadata.Name != "betaUsers" {
		t.Fatalf("Name = %q, want betaUsers", f.Metadata.Name)
	}
	if f.Spec.Constraints == nil || len(*f.Spec.Constraints) != 1 {
		t.Fatalf("Constraints = %v, want 1 entry", f.Spec.Constraints)
	}
	c := (*f.Spec.Constraints)[0]
	if c.ContextName != "userId" || c.Operator != "IN" {
		t.Fatalf("Constraint = %+v, want contextName=userId operator=IN", c)
	}
	if c.Values == nil || len(*c.Values) != 2 {
		t.Fatalf("Constraint.Values = %v, want 2 entries", c.Values)
	}
}

func TestLoadSegmentsRequiresName(t *testing.T) {
	dir := t.TempDir()
	content := `apiVersion: unleashctl/v1
kind: Segment
spec:
  description: no name
`
	if err := os.WriteFile(filepath.Join(dir, "no-name.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	if _, err := LoadSegments(dir); err == nil {
		t.Fatalf("want error for missing metadata.name, got nil")
	}
}
