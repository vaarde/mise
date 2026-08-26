package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
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

		if filepath.Base(path) == rootConfigName {
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

	return allResources, nil
}

// loadResourceFile parses a single resource YAML file.
func loadResourceFile(path string) ([]ResourceDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	var rf ResourceFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
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
	}

	return rf.Resources, nil
}
