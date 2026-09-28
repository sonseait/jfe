package route

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	reflectschema "github.com/invopop/jsonschema"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type Schema = map[string]any

type Document struct {
	OpenAPI    string                                   `json:"openapi"`
	Info       Info                                     `json:"info"`
	Paths      map[string]map[string]*DocumentOperation `json:"paths"`
	Components Components                               `json:"components"`
}
type Info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}
type Components struct {
	Schemas         map[string]any            `json:"schemas"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes"`
}
type SecurityScheme struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme"`
}
type DocumentOperation struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Tags        []string              `json:"tags"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}
type Parameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
	Schema   Schema `json:"schema"`
}
type RequestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]MediaType `json:"content"`
}
type MediaType struct {
	Schema Schema `json:"schema"`
}
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
	Headers     map[string]Header    `json:"headers,omitempty"`
}
type Header struct {
	Description string `json:"description"`
	Schema      Schema `json:"schema"`
}

func (r *Registry) schema(typ reflect.Type) Schema {
	if typ.Kind() != reflect.Struct || typ.Name() == "" {
		panic("route DTO must be a named struct")
	}
	if previous, ok := r.types[typ.Name()]; ok && previous != typ {
		panic("duplicate DTO name: " + typ.Name())
	}
	r.types[typ.Name()] = typ
	reflector := reflectschema.Reflector{Anonymous: true}
	data, err := json.Marshal(reflector.ReflectFromType(typ))
	if err != nil {
		panic(err)
	}
	var doc Schema
	if err = json.Unmarshal(data, &doc); err != nil {
		panic(err)
	}
	rewriteRefs(doc)
	defs, _ := doc["$defs"].(map[string]any)
	for name, value := range defs {
		if previous, ok := r.Spec.Components.Schemas[name]; ok && !reflect.DeepEqual(previous, value) {
			panic("conflicting DTO schema: " + name)
		}
		r.Spec.Components.Schemas[name] = value
	}
	delete(doc, "$defs")
	delete(doc, "$schema")
	return doc
}

func rewriteRefs(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "$ref" {
				if ref, ok := child.(string); ok {
					v[key] = strings.Replace(ref, "#/$defs/", "#/components/schemas/", 1)
				}
			} else {
				rewriteRefs(child)
			}
		}
	case []any:
		for _, child := range v {
			rewriteRefs(child)
		}
	}
}
func (r *Registry) resolve(schema Schema) Schema {
	if ref, ok := schema["$ref"].(string); ok {
		return r.Spec.Components.Schemas[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
	}
	return schema
}
func required(schema Schema, name string) bool {
	fields, _ := schema["required"].([]any)
	for _, field := range fields {
		if field == name {
			return true
		}
	}
	return false
}
func (r *Registry) validator(schema Schema) *jsonschema.Schema {
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	root := Schema{"$schema": "https://json-schema.org/draft/2020-12/schema", "components": Schema{"schemas": r.Spec.Components.Schemas}}
	for key, value := range schema {
		root[key] = value
	}
	const location = "https://jfe.invalid/dto.json"
	if err := compiler.AddResource(location, root); err != nil {
		panic(err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		panic(fmt.Sprintf("invalid DTO schema: %v", err))
	}
	return compiled
}
