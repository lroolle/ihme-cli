// Package schema is a small fluent builder for the JSON-Schema
// objects that tools expose as map[string]any. Hand-written maps
// remain valid input everywhere; the builder only prevents typos.
package schema

// Prop is a named property with an optional required flag.
type Prop struct {
	name     string
	schema   map[string]any
	required bool
}

// Property names a schema property.
func Property(name string, s map[string]any) Prop {
	return Prop{name: name, schema: s}
}

// Required marks the property as required.
func (p Prop) Required() Prop {
	p.required = true
	return p
}

// Object builds an object schema from properties.
func Object(props ...Prop) map[string]any {
	properties := make(map[string]any, len(props))
	var required []string
	for _, p := range props {
		properties[p.name] = p.schema
		if p.required {
			required = append(required, p.name)
		}
	}
	obj := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		obj["required"] = required
	}
	return obj
}

// String returns a string schema.
func String(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// Int returns an integer schema.
func Int(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

// Number returns a number schema.
func Number(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

// Bool returns a boolean schema.
func Bool(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// Enum returns a string enum schema.
func Enum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}

// Array returns an array schema with the given item schema.
func Array(desc string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": items}
}
