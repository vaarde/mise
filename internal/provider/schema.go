package provider

// SchemaProvider is an optional capability. An adapter implements it
// when it can say which properties each of its resource types accepts.
//
// Mise checks declared configuration against these schemas before a plan
// is computed. Without them, a misspelled property is invisible: nothing
// rejects it, the plan shows it as a value to be set, the adapter drops
// it on the way out because it recognizes only the names it knows, and
// the apply reports success. The next plan then shows the same change
// again, forever, while the POS never receives it — and a typo in a tax
// rate's "percentage" means writing a tax with no rate at all.
//
// An adapter that does not implement this is not validated, which is the
// behavior every adapter had before. Declaring a schema is how an
// adapter opts into catching its users' typos.
type SchemaProvider interface {
	// ResourceSchema returns the schema for a resource type, and whether
	// one is declared. Returning false for a type Mise supports means
	// that type simply is not validated.
	ResourceSchema(resourceType string) (ResourceSchema, bool)
}

// ResourceSchema describes the properties one resource type accepts.
//
// It deliberately covers names rather than types. An unrecognized name
// is silently discarded and can never work; a wrong type reaches the POS
// and comes back as a real API error naming the field. Only the first of
// those needs Mise to catch it.
type ResourceSchema struct {
	// Properties maps each accepted property name to its description.
	Properties map[string]PropertySchema

	// Required lists properties without which the resource cannot be
	// written at all.
	Required []string
}

// PropertySchema describes one property.
type PropertySchema struct {
	// Description is one line explaining the property, used in error
	// messages to help an operator who misspelled a neighbouring name.
	Description string

	// Elem describes the shape of the objects inside a list-valued
	// property — an item's variations, a modifier list's modifiers. It
	// is nil for scalars, references, and free-form maps such as
	// price_money, whose keys the platform defines rather than Mise.
	Elem *ResourceSchema
}

// Accepts reports whether a property name is in the schema.
func (s ResourceSchema) Accepts(name string) bool {
	_, ok := s.Properties[name]
	return ok
}
