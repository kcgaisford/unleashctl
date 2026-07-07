package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadContexts(t *testing.T) {
	dir := t.TempDir()
	content := `apiVersion: unleashctl/v1
kind: ContextField
metadata:
  name: subscriptionTier
spec:
  description: The user's subscription tier
  stickiness: true
  legalValues:
    - value: gold
      description: Gold tier
    - value: silver
`
	if err := os.WriteFile(filepath.Join(dir, "subscriptionTier.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	files, err := LoadContexts(dir)
	if err != nil {
		t.Fatalf("LoadContexts: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d", len(files))
	}
	f := files[0]
	if f.Metadata.Name != "subscriptionTier" {
		t.Fatalf("Name = %q, want subscriptionTier", f.Metadata.Name)
	}
	if f.Spec.Stickiness == nil || !*f.Spec.Stickiness {
		t.Fatalf("Stickiness = %v, want true", f.Spec.Stickiness)
	}
	if f.Spec.LegalValues == nil || len(*f.Spec.LegalValues) != 2 {
		t.Fatalf("LegalValues = %v, want 2 entries", f.Spec.LegalValues)
	}
	if (*f.Spec.LegalValues)[0].Value != "gold" {
		t.Fatalf("LegalValues[0].Value = %q, want gold", (*f.Spec.LegalValues)[0].Value)
	}
}

func TestLoadContextsRequiresName(t *testing.T) {
	dir := t.TempDir()
	content := `apiVersion: unleashctl/v1
kind: ContextField
spec:
  stickiness: true
`
	if err := os.WriteFile(filepath.Join(dir, "no-name.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	if _, err := LoadContexts(dir); err == nil {
		t.Fatalf("want error for missing metadata.name, got nil")
	}
}
