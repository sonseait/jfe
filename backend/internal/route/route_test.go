package route

import (
	"context"
	"encoding/json"
	"github.com/gofiber/fiber/v3"
	"net/http/httptest"
	"strings"
	"testing"
)

type testBody struct {
	Name string `json:"name" jsonschema:"minLength=3"`
}
type testQuery struct {
	Limit int `json:"limit" jsonschema:"minimum=1,maximum=20"`
}
type testParams struct {
	ID string `json:"id" jsonschema:"format=uuid"`
}
type testResponse struct {
	Name  string `json:"name"`
	Limit int    `json:"limit"`
	ID    string `json:"id"`
}

func TestRegistrationBindingValidationAndAuthorization(t *testing.T) {
	app := fiber.New()
	r := New(app)
	r.Auth = func(_ context.Context, token string) (Principal, error) {
		if token != "admin" {
			return Principal{}, Fail(401, "unauthorized")
		}
		return Principal{"user", "admin"}, nil
	}
	Register(r, Operation{Method: "POST", Path: "/items/:id", ID: "save", Access: "admin"}, func(ctx context.Context, in Input[testBody, testQuery, testParams]) (Output[testResponse], error) {
		if User(ctx).ID != "user" {
			t.Fatal("principal missing")
		}
		return Output[testResponse]{Body: testResponse{in.Body.Name, in.Query.Limit, in.Params.ID}}, nil
	})
	cases := []struct {
		path, body, token string
		status            int
	}{{"/items/26f025a9-1acb-43d0-aa3a-fbd542a96fbf?limit=2", `{"name":"movie"}`, "admin", 200}, {"/items/invalid?limit=2", `{"name":"movie"}`, "admin", 422}, {"/items/26f025a9-1acb-43d0-aa3a-fbd542a96fbf?limit=50", `{"name":"movie"}`, "admin", 422}, {"/items/26f025a9-1acb-43d0-aa3a-fbd542a96fbf?limit=2", `{"name":"x"}`, "admin", 422}, {"/items/26f025a9-1acb-43d0-aa3a-fbd542a96fbf?limit=2", `{"name":"movie"}`, "", 401}}
	for _, tc := range cases {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Content-Type", "application/json")
		res, e := app.Test(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Errorf("%s got %d want %d", tc.path, res.StatusCode, tc.status)
		}
	}
	op := r.Spec.Paths["/items/{id}"]["post"]
	if op == nil || len(op.Parameters) != 2 || op.RequestBody == nil || len(op.Security) != 1 {
		t.Fatal("incomplete OpenAPI")
	}
	if _, e := r.JSON(); e != nil {
		t.Fatal(e)
	}
}
func TestEmptyBodyAndDuplicateRoute(t *testing.T) {
	r := New(fiber.New())
	handler := func(_ context.Context, _ Input[Empty, Empty, Empty]) (Output[testResponse], error) {
		return Output[testResponse]{}, nil
	}
	Register(r, Operation{Method: "GET", Path: "/items", ID: "items"}, handler)
	b, _ := r.JSON()
	var doc map[string]json.RawMessage
	if e := json.Unmarshal(b, &doc); e != nil {
		t.Fatal(e)
	}
	if r.Spec.Paths["/items"]["get"].RequestBody != nil {
		t.Fatal("GET has body")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate route accepted")
		}
	}()
	Register(r, Operation{Method: "GET", Path: "/items", ID: "items2"}, handler)
}

type constrainedBody struct {
	Role     string   `json:"role" jsonschema:"enum=admin,enum=user"`
	Enabled  bool     `json:"enabled"`
	Names    []string `json:"names" jsonschema:"minItems=1"`
	Note     string   `json:"note,omitempty" jsonschema:"minLength=2"`
	Sequence int64    `json:"sequence" jsonschema:"maximum=9007199254740992"`
}
type optionalQuery struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=20"`
	Search string `json:"search,omitempty" jsonschema:"minLength=2"`
}

func TestBodyAndQueryConstraints(t *testing.T) {
	r := New(fiber.New())
	Post(r, Operation{Path: "/items", ID: "create"}, func(_ context.Context, in Input[constrainedBody, Empty, Empty]) (Output[testResponse], error) {
		return Output[testResponse]{Body: testResponse{Name: in.Body.Role}}, nil
	})
	Get(r, Operation{Path: "/items", ID: "list"}, func(_ context.Context, in Input[Empty, optionalQuery, Empty]) (Output[testResponse], error) {
		return Output[testResponse]{Body: testResponse{Limit: in.Query.Limit}}, nil
	})
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/items", `{"role":"user","enabled":false,"names":["one"],"sequence":0}`, 200},
		{"POST", "/items", `{"role":"owner","enabled":false,"names":["one"],"sequence":0}`, 422},
		{"POST", "/items", `{"role":"user","names":["one"],"sequence":0}`, 422},
		{"POST", "/items", `{"role":"user","enabled":false,"names":[],"sequence":0}`, 422},
		{"POST", "/items", `{"role":"user","enabled":false,"names":null,"sequence":0}`, 422},
		{"POST", "/items", `{"role":"user","enabled":false,"names":["one"],"note":"","sequence":0}`, 422},
		{"POST", "/items", `{"role":"user","enabled":false,"names":["one"],"sequence":9007199254740993}`, 422},
		{"POST", "/items", `{"role":"user","enabled":false,"names":["one"],"sequence":0,"extra":true}`, 422},
		{"POST", "/items", `{} {}`, 400},
		{"POST", "/items", `null`, 422},
		{"GET", "/items", "", 200},
		{"GET", "/items?limit=2", "", 200},
		{"GET", "/items?limit=1.5", "", 422},
		{"GET", "/items?limit=0", "", 422},
		{"GET", "/items?search=", "", 422},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		res, err := r.App.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Errorf("%s %s %s: status=%d want=%d", tc.method, tc.path, tc.body, res.StatusCode, tc.status)
		}
		if tc.status >= 400 && res.Header.Get("Content-Type") != "application/problem+json" {
			t.Errorf("unexpected problem content type: %s", res.Header.Get("Content-Type"))
		}
	}
	if r.Spec.Paths["/items"]["get"].RequestBody != nil {
		t.Fatal("GET wrapper emitted body")
	}
	schema := r.Spec.Components.Schemas["constrainedBody"].(map[string]any)
	props := schema["properties"].(map[string]any)
	if props["names"].(map[string]any)["minItems"] != float64(1) {
		t.Fatal("runtime constraint missing from OpenAPI")
	}
}

func TestWrapperMethodsAndStreaming(t *testing.T) {
	r := New(fiber.New())
	handler := func(_ context.Context, _ Input[Empty, Empty, Empty]) (Output[Empty], error) {
		return Output[Empty]{}, nil
	}
	Get(r, Operation{Path: "/method", ID: "get"}, handler)
	Post(r, Operation{Path: "/method", ID: "post"}, handler)
	Put(r, Operation{Path: "/method", ID: "put"}, handler)
	Patch(r, Operation{Path: "/method", ID: "patch"}, handler)
	Delete(r, Operation{Path: "/method", ID: "delete"}, handler)
	Head(r, Operation{Path: "/method", ID: "head"}, handler)
	Options(r, Operation{Path: "/method", ID: "options"}, handler)
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		res, err := r.App.Test(httptest.NewRequest(method, "/method", nil))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 || r.Spec.Paths["/method"][strings.ToLower(method)] == nil {
			t.Fatalf("missing Fiber/OpenAPI method %s", method)
		}
	}
	Get(r, Operation{Path: "/stream", ID: "stream", Stream: "video/mp4"}, func(_ context.Context, _ Input[Empty, Empty, Empty]) (Output[Empty], error) {
		return Output[Empty]{Send: func(c fiber.Ctx) error { return c.Type("mp4").Send([]byte("video")) }}, nil
	})
	res, err := r.App.Test(httptest.NewRequest("GET", "/stream", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Type") != "video/mp4" {
		t.Fatal("stream wrapped as JSON")
	}
	op := r.Spec.Paths["/stream"]["get"]
	if op.Responses["200"].Content["video/mp4"].Schema["format"] != "binary" || op.Responses["206"].Headers["Content-Range"].Schema == nil {
		t.Fatal("missing stream descriptor")
	}
}

func TestDuplicateRouteShape(t *testing.T) {
	r := New(fiber.New())
	Get(r, Operation{Path: "/items/:id", ID: "one"}, func(_ context.Context, _ Input[Empty, Empty, testParams]) (Output[Empty], error) {
		return Output[Empty]{}, nil
	})
	type otherParams struct {
		Other string `json:"other"`
	}
	defer func() {
		if recover() == nil {
			t.Fatal("equivalent Fiber route shape accepted twice")
		}
	}()
	Get(r, Operation{Path: "/items/:other", ID: "two"}, func(_ context.Context, _ Input[Empty, Empty, otherParams]) (Output[Empty], error) {
		return Output[Empty]{}, nil
	})
}
