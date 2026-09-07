package cli

import (
	"reflect"
	"sort"
	"strings"
)

// maxSchemaDepth bounds the walk through nested types. Nothing described here
// is deeper than a few levels, and a bound is what keeps a type that came to
// refer to itself from being described forever.
const maxSchemaDepth = 8

// schemaOf describes a value's JSON shape from the type that produces it, so
// that a caller reading the schema learns the field names instead of fetching
// a row and guessing from whichever fields that row happened to have set.
func schemaOf(sample any) map[string]any {
	return typeSchema(reflect.TypeOf(sample), 0)
}

// listOutputOf is the list envelope with the shape of one item in it.
func listOutputOf(sample any) map[string]any {
	return objectSchema(map[string]any{
		"items": map[string]any{"type": "array", "items": schemaOf(sample)},
		"page":  map[string]any{"type": "object"},
		"meta":  map[string]any{"type": "object"},
	}, "items", "meta")
}

// dataOutputOf is the single-item envelope with the shape of the thing in it,
// the counterpart of listOutputOf for a command that answers with one.
func dataOutputOf(sample any) map[string]any {
	return objectSchema(map[string]any{
		"data": schemaOf(sample),
		"meta": map[string]any{"type": "object"},
	}, "data", "meta")
}

func typeSchema(typeOf reflect.Type, depth int) map[string]any {
	for typeOf != nil && typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	// An unconstrained schema is what "anything may be here" looks like, which
	// is the honest answer for an interface and for a type described too deep.
	if typeOf == nil || depth > maxSchemaDepth {
		return map[string]any{}
	}
	switch typeOf.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": typeSchema(typeOf.Elem(), depth+1)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": typeSchema(typeOf.Elem(), depth+1)}
	case reflect.Struct:
		return structSchema(typeOf, depth)
	}
	return map[string]any{}
}

// structSchema reads the json tags the encoder itself reads. A field without
// omitempty is always written and so is required; one with it may be absent,
// which is the difference between a field that is missing from a row and a
// field that does not exist -- a reader who cannot tell them apart concludes
// the wrong thing from the first row that leaves one out.
func structSchema(typeOf reflect.Type, depth int) map[string]any {
	properties := map[string]any{}
	var required []string
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		if !field.IsExported() {
			continue
		}
		options := strings.Split(field.Tag.Get("json"), ",")
		name := options[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		property := typeSchema(field.Type, depth+1)
		// A field whose name does not say what it holds gets a sentence that
		// does, since the schema is the only place a caller reads about it.
		if note := field.Tag.Get("desc"); note != "" {
			property["description"] = note
		}
		properties[name] = property
		if field.Type.Kind() != reflect.Pointer && !omitsEmpty(options[1:]) {
			required = append(required, name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}
	return schema
}

func omitsEmpty(options []string) bool {
	for _, option := range options {
		if option == "omitempty" {
			return true
		}
	}
	return false
}
