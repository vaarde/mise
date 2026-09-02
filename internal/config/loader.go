package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadRoot reads and parses the root mise.yaml file.
func LoadRoot(path string) (*RootConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file %s: %w", path, err)
	}

	var cfg RootConfig
	if err := strictUnmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("cannot parse config file %s: %w", path, err)
	}

	if cfg.Version == "" {
		cfg.Version = "1"
	}

	return &cfg, nil
}

// LoadResources discovers and parses all resource YAML files in the
// workspace. It walks the directory tree looking for .yaml/.yml
// files (excluding mise.yaml itself) and parses each one.
//
// Two definitions of the same "type.name" are a hard error. Later
// stages key resources by that name — state, the dependency graph, the
// plan — so a duplicate would not conflict loudly, it would silently
// win or lose depending on which file the walk reached last.
func LoadResources(workDir string, rootConfigName string) ([]ResourceDef, error) {
	var allResources []ResourceDef

	err := filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories, non-YAML files, the root config,
		// and anything inside .mise/
		if info.IsDir() {
			if info.Name() == ".mise" || info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		// mise.yaml is the workspace config, and locations.yaml is
		// reference data fetch writes so an operator can look up the IDs
		// their groups refer to. Neither declares resources.
		if base := filepath.Base(path); base == rootConfigName || base == LocationsFileName {
			return nil
		}

		// Parse resource file
		resources, err := loadResourceFile(path)
		if err != nil {
			return fmt.Errorf("error in %s: %w", path, err)
		}

		allResources = append(allResources, resources...)
		return nil
	})

	if err != nil {
		return nil, err
	}

	if err := checkDuplicates(allResources); err != nil {
		return nil, err
	}

	return allResources, nil
}

// checkDuplicates rejects two definitions of the same "type.name".
func checkDuplicates(resources []ResourceDef) error {
	seen := make(map[string]string, len(resources))
	duplicates := map[string][]string{}

	for _, res := range resources {
		name := res.FullName()
		source := res.SourceFile
		if source == "" {
			source = "(unknown file)"
		}

		first, ok := seen[name]
		if !ok {
			seen[name] = source
			continue
		}
		if len(duplicates[name]) == 0 {
			duplicates[name] = []string{first}
		}
		duplicates[name] = append(duplicates[name], source)
	}

	if len(duplicates) == 0 {
		return nil
	}

	names := make([]string, 0, len(duplicates))
	for name := range duplicates {
		names = append(names, name)
	}
	sort.Strings(names)

	details := make([]string, 0, len(names))
	for _, name := range names {
		details = append(details, fmt.Sprintf("  %s is declared in %s",
			name, strings.Join(duplicates[name], " and ")))
	}

	return fmt.Errorf("the same resource is declared more than once:\n%s\n"+
		"Rename one of them or delete the copy — Mise identifies a resource by its type and name",
		strings.Join(details, "\n"))
}

// loadResourceFile parses a single resource YAML file.
func loadResourceFile(path string) ([]ResourceDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	var rf ResourceFile
	if err := strictUnmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", path, err)
	}

	// Tag each resource with its source file for error reporting
	for i := range rf.Resources {
		if rf.Resources[i].Type == "" {
			return nil, fmt.Errorf("resource at index %d in %s is missing 'type'", i, path)
		}
		if rf.Resources[i].Name == "" {
			return nil, fmt.Errorf("resource %q at index %d in %s is missing 'name'", rf.Resources[i].Type, i, path)
		}
		rf.Resources[i].SourceFile = path
	}

	return rf.Resources, nil
}

// strictUnmarshal parses YAML and rejects any field the target struct
// does not declare.
//
// A silently ignored field is the worst kind of config bug: "location:"
// instead of "locations:" would plan and apply cleanly while scoping the
// resource to somewhere the operator never asked for. Refusing the file
// turns that into a message naming the line.
func strictUnmarshal(data []byte, target interface{}) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err := decoder.Decode(target); err != nil {
		// An empty file is not an error — it simply declares nothing.
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
