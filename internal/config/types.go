// Package config handles loading and validating Mise configuration
// files — the root mise.yaml and the resource YAML files that
// define the desired POS state.
package config

import "github.com/vaarde/mise/internal/provider"

// RootConfig represents the top-level mise.yaml file.
type RootConfig struct {
	Version        string                     `yaml:"version"`
	Provider       provider.ProviderConfig    `yaml:"provider"`
	LocationGroups map[string]LocationGroup   `yaml:"location_groups"`
}

// LocationGroup defines a named set of locations that resources
// can be scoped to. Groups are resolved at plan/apply time by
// matching location metadata against the filter.
//
// Example:
//
//	location_groups:
//	  georgia:
//	    filter:
//	      state: GA
//	  all:
//	    filter: "*"
type LocationGroup struct {
	Filter interface{} `yaml:"filter"` // "*" for all, or a map of field→value matchers
}

// ResourceFile represents a YAML file containing resource definitions
// (e.g. taxes/georgia.yaml, menu/beverages.yaml).
type ResourceFile struct {
	Resources []ResourceDef `yaml:"resources"`
}

// ResourceDef is a single resource definition from a config file.
// It declares what a resource should look like on the POS.
type ResourceDef struct {
	Type       string                 `yaml:"type"`       // e.g. "square_catalog_tax"
	Name       string                 `yaml:"name"`       // unique name within this type
	Locations  string                 `yaml:"locations"`  // location group reference, e.g. "${group.georgia}"
	Properties map[string]interface{} `yaml:"properties"` // resource-specific properties
}

// FullName returns the fully qualified resource name: "type.name"
// (e.g. "square_catalog_tax.ga_state_sales_tax").
func (r ResourceDef) FullName() string {
	return r.Type + "." + r.Name
}
