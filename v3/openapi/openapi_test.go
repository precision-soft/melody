package openapi_test

import (
    "reflect"
    "testing"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/openapi"
)

type createProductRequest struct {
    Name  string  `json:"name" validate:"notBlank,min=2"`
    Email string  `json:"email" validate:"email"`
    Price float64 `json:"price"`
}

type productResponse struct {
    Id   string `json:"id"`
    Name string `json:"name"`
}

type fakeRoute struct {
    name    string
    pattern string
    methods []string
}

func (instance fakeRoute) Name() string                    { return instance.name }
func (instance fakeRoute) Pattern() string                 { return instance.pattern }
func (instance fakeRoute) Methods() []string               { return instance.methods }
func (instance fakeRoute) Host() string                    { return "" }
func (instance fakeRoute) Schemes() []string               { return nil }
func (instance fakeRoute) Requirements() map[string]string { return nil }
func (instance fakeRoute) Defaults() map[string]string     { return nil }
func (instance fakeRoute) Locales() []string               { return nil }
func (instance fakeRoute) Priority() int                   { return 0 }
func (instance fakeRoute) Attributes() map[string]any      { return nil }

func TestGenerate_BuildsPathsParametersAndSchemas(t *testing.T) {
    registry := openapi.NewRegistry()
    registry.Describe("products.create", openapi.Descriptor{
        Summary:     "Create a product",
        Tags:        []string{"products"},
        RequestType: openapi.TypeOf[createProductRequest](),
        Responses: map[int]reflect.Type{
            201: openapi.TypeOf[productResponse](),
        },
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "products.read", pattern: "/products/api/read/:id/", methods: []string{"GET"}},
        fakeRoute{name: "products.create", pattern: "/products/api/create/", methods: []string{"POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    if "3.0.3" != document.OpenApi {
        t.Fatalf("unexpected openapi version: %s", document.OpenApi)
    }

    readPath, hasReadPath := document.Paths["/products/api/read/{id}/"]
    if false == hasReadPath {
        t.Fatalf("expected converted read path, paths: %v", keysOf(document.Paths))
    }

    if nil == readPath.Get || 1 != len(readPath.Get.Parameters) || "id" != readPath.Get.Parameters[0].Name {
        t.Fatalf("expected an id path parameter on the read operation")
    }

    createPath, hasCreatePath := document.Paths["/products/api/create/"]
    if false == hasCreatePath || nil == createPath.Post {
        t.Fatalf("expected a create POST operation")
    }

    if nil == createPath.Post.RequestBody {
        t.Fatalf("expected a request body on the create operation")
    }

    schema := createPath.Post.RequestBody.Content["application/json"].Schema
    if nil == schema || nil == schema.Properties["name"] || "string" != schema.Properties["name"].Type {
        t.Fatalf("expected a string name property")
    }

    if nil == schema.Properties["name"].MinLength || 2 != *schema.Properties["name"].MinLength {
        t.Fatalf("expected minLength 2 on name")
    }

    if "email" != schema.Properties["email"].Format {
        t.Fatalf("expected email format on email property")
    }

    if false == containsString(schema.Required, "name") {
        t.Fatalf("expected name to be required, got: %v", schema.Required)
    }

    if _, hasResponse := createPath.Post.Responses["201"]; false == hasResponse {
        t.Fatalf("expected a 201 response")
    }
}

func keysOf(paths map[string]openapi.PathItem) []string {
    keys := make([]string, 0, len(paths))
    for key := range paths {
        keys = append(keys, key)
    }
    return keys
}

func containsString(values []string, target string) bool {
    for _, value := range values {
        if target == value {
            return true
        }
    }
    return false
}
