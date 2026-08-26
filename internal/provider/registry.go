package provider

import "fmt"

// ProviderFactory is a constructor function that returns a new
// Provider instance. Each adapter registers one of these.
type ProviderFactory func() Provider

// registry holds the mapping of platform names to their factories.
var registry = map[string]ProviderFactory{}

// Register adds a provider factory to the registry. Called by
// each adapter's init() function.
//
// Example: In the square adapter package:
//
//	func init() {
//	    provider.Register("square", func() provider.Provider {
//	        return &SquareProvider{}
//	    })
//	}
func Register(name string, factory ProviderFactory) {
	registry[name] = factory
}

// Get returns a new provider instance for the given platform name.
// Returns an error if no adapter is registered for that platform.
func Get(name string) (Provider, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q — available providers: %v", name, AvailableProviders())
	}
	return factory(), nil
}

// AvailableProviders returns the names of all registered providers.
func AvailableProviders() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
