package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Load reads every *.yaml/*.yml file directly under dir and parses it as a
// state.File. It does not recurse into subdirectories.
func Load(dir string) ([]*File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading flags dir %s: %w", dir, err)
	}

	var files []*File
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		f, err := loadFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func loadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	f.Path = path
	if f.Metadata.Name == "" {
		return nil, fmt.Errorf("%s: metadata.name is required", path)
	}
	return &f, nil
}

// FilterByService returns the subset of files whose metadata.service matches.
func FilterByService(files []*File, service string) []*File {
	if service == "" {
		return files
	}
	var out []*File
	for _, f := range files {
		if f.Metadata.Service == service {
			out = append(out, f)
		}
	}
	return out
}

// Services returns the sorted, de-duplicated set of metadata.service values
// present across files. Files with no service are excluded — per spec §5.1,
// a flag with no service opts out of service-scoped operations entirely.
func Services(files []*File) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if f.Metadata.Service == "" {
			continue
		}
		if !seen[f.Metadata.Service] {
			seen[f.Metadata.Service] = true
			out = append(out, f.Metadata.Service)
		}
	}
	sort.Strings(out)
	return out
}
