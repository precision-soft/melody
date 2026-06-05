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

    bodySchema := createPath.Post.RequestBody.Content["application/json"].Schema
    if nil == bodySchema || "#/components/schemas/createProductRequest" != bodySchema.Ref {
        t.Fatalf("expected the request body to reference the component schema, got: %+v", bodySchema)
    }

    if nil == document.Components {
        t.Fatalf("expected components to be populated")
    }

    schema := document.Components.Schemas["createProductRequest"]
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

type embeddedAudit struct {
    CreatedBy string `json:"createdBy"`
}

type numericRequest struct {
    embeddedAudit
    Quantity int      `json:"quantity" validate:"min=1"`
    Discount *float64 `json:"discount,omitempty"`
    MinTotal float64  `json:"minTotal" validate:"greaterThan=0"`
}

func TestGenerate_NumericConstraintsEmbeddingAndNullability(t *testing.T) {
    registry := openapi.NewRegistry()
    registry.Describe("orders.create", openapi.Descriptor{
        RequestType: openapi.TypeOf[numericRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "orders.create", pattern: "/orders/", methods: []string{"POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    schema := document.Components.Schemas["numericRequest"]
    if nil == schema {
        t.Fatalf("expected the numericRequest component schema")
    }

    quantity := schema.Properties["quantity"]
    if nil == quantity || "integer" != quantity.Type || nil != quantity.MinLength {
        t.Fatalf("expected an integer quantity without minLength, got: %+v", quantity)
    }

    minTotal := schema.Properties["minTotal"]
    if nil == minTotal || nil == minTotal.Minimum || 0 != *minTotal.Minimum || nil == minTotal.ExclusiveMinimum || false == *minTotal.ExclusiveMinimum {
        t.Fatalf("expected an exclusive minimum of 0 on minTotal, got: %+v", minTotal)
    }

    discount := schema.Properties["discount"]
    if nil == discount || "number" != discount.Type || false == discount.Nullable {
        t.Fatalf("expected a nullable number discount, got: %+v", discount)
    }

    if nil == schema.Properties["createdBy"] {
        t.Fatalf("expected the embedded createdBy field to be promoted, properties: %v", schema.Properties)
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
