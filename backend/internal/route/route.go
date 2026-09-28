// Package route binds typed DTOs and registers Fiber routes and OpenAPI together.
package route

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog/log"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type Empty struct{}
type Principal struct{ ID, Role string }
type principalKey struct{}

func User(ctx context.Context) Principal { p, _ := ctx.Value(principalKey{}).(Principal); return p }

type Problem struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (p *Problem) Error() string           { return p.Detail }
func Fail(status int, detail string) error { return &Problem{status, http.StatusText(status), detail} }

type Operation struct {
	Method, Path, ID, Summary, Tag, Access string
	Status                                 int
	Stream                                 string
}
type Input[B, Q, P any] struct {
	Body   B
	Query  Q
	Params P
}
type Output[R any] struct {
	Body R
	Send func(fiber.Ctx) error
}
type Registry struct {
	App   *fiber.App
	Spec  *Document
	Auth  func(context.Context, string) (Principal, error)
	IDs   map[string]bool
	Paths map[string]bool
	types map[string]reflect.Type
}

func New(app *fiber.App) *Registry {
	return &Registry{App: app, IDs: map[string]bool{}, Paths: map[string]bool{}, types: map[string]reflect.Type{}, Spec: &Document{
		OpenAPI: "3.1.0", Info: Info{Title: "JFE API", Version: "0.1.0"}, Paths: map[string]map[string]*DocumentOperation{},
		Components: Components{Schemas: map[string]any{}, SecuritySchemes: map[string]SecurityScheme{"bearer": {Type: "http", Scheme: "bearer"}}},
	}}
}

// Register wraps Fiber route registration with DTO validation and OpenAPI metadata.
func Register[B, Q, P, R any](reg *Registry, op Operation, handler func(context.Context, Input[B, Q, P]) (Output[R], error)) {
	op.Method = strings.ToUpper(op.Method)
	switch op.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		panic("unsupported HTTP method")
	}
	if !strings.HasPrefix(op.Path, "/") {
		panic("route path must start with /")
	}
	if op.Access != "" && op.Access != "user" && op.Access != "admin" {
		panic("unsupported route access")
	}
	if op.ID == "" || reg.IDs[op.ID] || reg.Paths[op.Method+op.Path] {
		panic("duplicate or empty route: " + op.ID)
	}
	if op.Status == 0 {
		op.Status = 200
	}
	bodyType, queryType, paramType := reflect.TypeFor[B](), reflect.TypeFor[Q](), reflect.TypeFor[P]()
	bodySchema := reg.schema(bodyType)
	querySchema := reg.resolve(reg.schema(queryType))
	paramSchema := reg.resolve(reg.schema(paramType))
	responseSchema := reg.schema(reflect.TypeFor[R]())
	problemSchema := reg.schema(reflect.TypeFor[Problem]())
	path := op.Path
	operation := &DocumentOperation{OperationID: op.ID, Summary: op.Summary, Tags: []string{op.Tag}, Responses: map[string]Response{}}
	for _, group := range []struct {
		typ      reflect.Type
		schema   Schema
		location string
	}{{queryType, querySchema, "query"}, {paramType, paramSchema, "path"}} {
		for i := 0; i < group.typ.NumField(); i++ {
			field := group.typ.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" || !field.IsExported() || field.Anonymous {
				panic("parameter DTO fields need explicit exported json tags")
			}
			kind := field.Type
			for kind.Kind() == reflect.Pointer {
				kind = kind.Elem()
			}
			switch kind.Kind() {
			case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
			default:
				panic("query/path parameters must be scalar")
			}
			if group.location == "path" {
				if !required(group.schema, name) {
					panic("path DTO fields must be required")
				}
				segments := strings.Split(path, "/")
				found := false
				for n, segment := range segments {
					if segment == ":"+name {
						segments[n] = "{" + name + "}"
						found = true
					}
				}
				if !found {
					panic("path DTO mismatch")
				}
				path = strings.Join(segments, "/")
			}
			properties := group.schema["properties"].(map[string]any)
			operation.Parameters = append(operation.Parameters, Parameter{Name: name, In: group.location, Required: group.location == "path" || required(group.schema, name), Schema: properties[name].(map[string]any)})
		}
	}
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, ":") || strings.ContainsAny(segment, "*?") {
			panic("missing or unsupported path DTO field")
		}
	}
	// Fiber treats parameter names as equivalent when matching routes.
	canonical := strings.Split(path, "/")
	for i, part := range canonical {
		if strings.HasPrefix(part, "{") {
			canonical[i] = "{}"
		}
	}
	routeKey := op.Method + strings.Join(canonical, "/")
	if reg.Paths[routeKey] {
		panic("duplicate route shape")
	}
	hasBody := bodyType != reflect.TypeFor[Empty]()
	if hasBody {
		if op.Method == "GET" || op.Method == "HEAD" {
			panic("GET/HEAD must use Empty body")
		}
		operation.RequestBody = &RequestBody{Required: true, Content: map[string]MediaType{"application/json": {Schema: bodySchema}}}
	}
	content := map[string]MediaType{"application/json": {Schema: responseSchema}}
	if op.Stream != "" {
		content = map[string]MediaType{op.Stream: {Schema: Schema{"type": "string", "format": "binary"}}}
	}
	if op.Status == 204 {
		content = nil
	}
	operation.Responses[strconv.Itoa(op.Status)] = Response{Description: http.StatusText(op.Status), Content: content}
	if op.Stream != "" {
		headers := map[string]Header{"Accept-Ranges": {Description: "Supported range unit", Schema: Schema{"type": "string"}}, "Content-Range": {Description: "Returned byte range", Schema: Schema{"type": "string"}}}
		operation.Responses["206"] = Response{Description: "Partial content", Content: content, Headers: headers}
	}
	for _, code := range []int{400, 401, 403, 404, 409, 416, 422, 429, 500, 503} {
		operation.Responses[strconv.Itoa(code)] = Response{Description: http.StatusText(code), Content: map[string]MediaType{"application/problem+json": {Schema: problemSchema}}}
	}
	if op.Access != "" {
		operation.Security = []map[string][]string{{"bearer": {}}}
	}
	bodyValidator, queryValidator, paramValidator := reg.validator(bodySchema), reg.validator(querySchema), reg.validator(paramSchema)
	reg.IDs[op.ID] = true
	reg.Paths[routeKey] = true
	if reg.Spec.Paths[path] == nil {
		reg.Spec.Paths[path] = map[string]*DocumentOperation{}
	}
	reg.Spec.Paths[path][strings.ToLower(op.Method)] = operation
	reg.App.Add([]string{op.Method}, op.Path, func(c fiber.Ctx) error {
		ctx := c.Context()
		if op.Access != "" {
			authorization := c.Get("Authorization")
			if !strings.HasPrefix(authorization, "Bearer ") {
				return writeError(c, Fail(401, "Authentication required"))
			}
			if reg.Auth == nil {
				return writeError(c, Fail(503, "Authentication unavailable"))
			}
			principal, err := reg.Auth(ctx, strings.TrimPrefix(authorization, "Bearer "))
			if err != nil {
				return writeError(c, err)
			}
			if op.Access == "admin" && principal.Role != "admin" {
				return writeError(c, Fail(403, "Administrator required"))
			}
			ctx = context.WithValue(ctx, principalKey{}, principal)
		}
		var input Input[B, Q, P]
		if hasBody {
			var value any
			decoder := json.NewDecoder(bytes.NewReader(c.Body()))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				return writeError(c, Fail(400, "Invalid JSON"))
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				return writeError(c, Fail(400, "Invalid JSON"))
			}
			if err := validate(bodyValidator, value); err != nil {
				return writeError(c, err)
			}
			if err := json.Unmarshal(c.Body(), &input.Body); err != nil {
				return writeError(c, Fail(422, "Invalid body"))
			}
		}
		if err := bind(queryValidator, queryType, func(k string) (string, bool) { return c.Query(k), c.Request().URI().QueryArgs().Has(k) }, &input.Query); err != nil {
			return writeError(c, err)
		}
		if err := bind(paramValidator, paramType, func(k string) (string, bool) { return c.Params(k), c.Params(k) != "" }, &input.Params); err != nil {
			return writeError(c, err)
		}
		out, err := handler(ctx, input)
		if err != nil {
			return writeError(c, err)
		}
		if out.Send != nil {
			return out.Send(c)
		}
		if op.Status == 204 {
			return c.SendStatus(204)
		}
		return c.Status(op.Status).JSON(out.Body)
	})
}
func validate(schema *jsonschema.Schema, value any) error {
	if err := schema.Validate(value); err != nil {
		return Fail(422, "Request does not match the DTO schema")
	}
	return nil
}
func bind(schema *jsonschema.Schema, typ reflect.Type, get func(string) (string, bool), target any) error {
	values := map[string]any{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		raw, present := get(name)
		if !present {
			continue
		}
		var value any = raw
		var err error
		kind := field.Type
		for kind.Kind() == reflect.Pointer {
			kind = kind.Elem()
		}
		switch kind.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			var n int64
			n, err = strconv.ParseInt(raw, 10, kind.Bits())
			value = json.Number(strconv.FormatInt(n, 10))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			var n uint64
			n, err = strconv.ParseUint(raw, 10, kind.Bits())
			value = json.Number(strconv.FormatUint(n, 10))
		case reflect.Float32, reflect.Float64:
			value, err = strconv.ParseFloat(raw, kind.Bits())
		case reflect.Bool:
			value, err = strconv.ParseBool(raw)
		}
		if err != nil {
			return Fail(422, "Invalid parameter: "+name)
		}
		values[name] = value
	}
	if err := validate(schema, values); err != nil {
		return err
	}
	data, err := json.Marshal(values)
	if err != nil {
		return Fail(422, "Invalid parameter")
	}
	if err = json.Unmarshal(data, target); err != nil {
		return Fail(422, "Invalid parameter")
	}
	return nil
}
func writeError(c fiber.Ctx, err error) error {
	return ErrorHandler(c, err)
}

// ErrorHandler preserves diagnostics server-side while keeping internal errors private.
func ErrorHandler(c fiber.Ctx, err error) error {
	var p *Problem
	if !errors.As(err, &p) {
		p = &Problem{500, "Internal Server Error", "The request could not be completed"}
		var fe *fiber.Error
		if errors.As(err, &fe) {
			p.Status, p.Title = fe.Code, http.StatusText(fe.Code)
			if fe.Code < 500 {
				p.Detail = p.Title
			}
		}
	}
	if p.Status >= 500 {
		log.Error().Err(err).Str("requestId", c.GetRespHeader("X-Request-ID")).
			Str("method", c.Method()).Str("route", c.Route().Path).
			Int("status", p.Status).Msg("request failed")
	}
	return c.Status(p.Status).JSON(p, "application/problem+json")
}
func (r *Registry) JSON() ([]byte, error) { return json.MarshalIndent(r.Spec, "", "  ") }
func (r *Registry) Docs() {
	r.App.Get("/openapi.json", func(c fiber.Ctx) error {
		data, err := r.JSON()
		if err != nil {
			return err
		}
		c.Type("json")
		return c.Send(data)
	})
	r.App.Get("/docs", func(c fiber.Ctx) error {
		c.Type("html")
		return c.SendString(fmt.Sprintf(`<!doctype html><html><head><title>JFE API</title></head><body><h1>JFE API</h1><p><a href="/openapi.json">OpenAPI 3.1 JSON</a></p><script id="api-reference" data-url="/openapi.json"></script><script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script></body></html>`))
	})
}
func WithUser(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
