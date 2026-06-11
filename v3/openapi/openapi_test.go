package openapi_test

import (
    "reflect"
    "strings"
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

func TestGenerate_MultiMethodRouteEmitsDistinctOperationsAndUniqueOperationIds(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "thing.handle", pattern: "/thing/", methods: []string{"GET", "POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    pathItem, hasPath := document.Paths["/thing/"]
    if false == hasPath {
        t.Fatalf("expected the /thing/ path")
    }

    if nil == pathItem.Get || nil == pathItem.Post {
        t.Fatalf("expected both GET and POST operations")
    }

    if pathItem.Get == pathItem.Post {
        t.Fatalf("expected distinct operation instances per method")
    }

    if pathItem.Get.OperationId == pathItem.Post.OperationId {
        t.Fatalf("expected a unique operationId per method, got %q for both", pathItem.Get.OperationId)
    }

    if "thing.handle.get" != pathItem.Get.OperationId {
        t.Fatalf("unexpected GET operationId: %q", pathItem.Get.OperationId)
    }

    if "thing.handle.post" != pathItem.Post.OperationId {
        t.Fatalf("unexpected POST operationId: %q", pathItem.Post.OperationId)
    }
}

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

type shadowedEmbedRequest struct {
    embeddedAudit
    CreatedBy int64 `json:"createdBy"`
}

func TestGenerate_OuterFieldShadowsEmbeddedField(t *testing.T) {
    registry := openapi.NewRegistry()
    registry.Describe("shadow.create", openapi.Descriptor{
        RequestType: openapi.TypeOf[shadowedEmbedRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "shadow.create", pattern: "/shadow/", methods: []string{"POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    createdBy := document.Components.Schemas["shadowedEmbedRequest"].Properties["createdBy"]
    if nil == createdBy || "integer" != createdBy.Type {
        t.Fatalf("expected the outer int64 createdBy to shadow the embedded string field (encoding/json semantics), got: %+v", createdBy)
    }
}

type deepMarker struct {
    Marker string `json:"marker"`
}

type midMarkerEmbed struct {
    deepMarker
}

type shallowMarkerEmbed struct {
    Marker int64 `json:"marker"`
}

type embedDepthRequest struct {
    midMarkerEmbed
    shallowMarkerEmbed
}

func TestGenerate_ShallowerEmbeddedFieldWinsRegardlessOfOrder(t *testing.T) {
    registry := openapi.NewRegistry()
    registry.Describe("depth.create", openapi.Descriptor{
        RequestType: openapi.TypeOf[embedDepthRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "depth.create", pattern: "/depth/", methods: []string{"POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    marker := document.Components.Schemas["embedDepthRequest"].Properties["marker"]
    if nil == marker || "integer" != marker.Type {
        t.Fatalf("expected the shallower (depth-1) int64 marker to win over the deeper string field, got: %+v", marker)
    }
}

type diamondEmbedBase struct {
    Shared string `json:"shared"`
}

type diamondEmbedLeft struct {
    diamondEmbedBase
}

type diamondEmbedRight struct {
    diamondEmbedBase
}

type diamondEmbedRequest struct {
    diamondEmbedLeft
    diamondEmbedRight
    Own string `json:"own"`
}

func TestGenerate_DiamondEmbeddedFieldDroppedAsAmbiguous(t *testing.T) {
    registry := openapi.NewRegistry()
    registry.Describe("diamond.create", openapi.Descriptor{
        RequestType: openapi.TypeOf[diamondEmbedRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "diamond.create", pattern: "/diamond/", methods: []string{"POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    properties := document.Components.Schemas["diamondEmbedRequest"].Properties

    if _, present := properties["shared"]; true == present {
        t.Fatalf("expected the field reachable through two equal-depth embedded paths to be dropped as ambiguous (encoding/json omits it), got: %+v", properties)
    }
    if _, present := properties["own"]; false == present {
        t.Fatalf("expected the root's own field to survive, got: %+v", properties)
    }
}

type nullableRefRequest struct {
    Audit *embeddedAudit `json:"audit,omitempty"`
}

func TestGenerate_NullablePointerToStructUsesAllOf(t *testing.T) {
    registry := openapi.NewRegistry()
    registry.Describe("nullable.create", openapi.Descriptor{
        RequestType: openapi.TypeOf[nullableRefRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "nullable.create", pattern: "/nullable/", methods: []string{"POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    auditField := document.Components.Schemas["nullableRefRequest"].Properties["audit"]
    if nil == auditField {
        t.Fatalf("expected the audit property")
    }

    if "" != auditField.Ref {
        t.Fatalf("a nullable $ref must not set Ref directly (OAS 3.0 ignores $ref siblings): %+v", auditField)
    }

    if false == auditField.Nullable {
        t.Fatalf("expected the audit property to be nullable: %+v", auditField)
    }

    if 1 != len(auditField.AllOf) || "#/components/schemas/embeddedAudit" != auditField.AllOf[0].Ref {
        t.Fatalf("expected allOf wrapping the $ref, got: %+v", auditField)
    }
}

type taggedRequest struct {
    Tags []string `json:"tags" validate:"min=1,max=5"`
    Code string   `json:"code" validate:"min=2,max=8"`
}

func TestGenerate_MinMaxAppliesOnlyToStringLength(t *testing.T) {
    registry := openapi.NewRegistry()
    registry.Describe("tags.create", openapi.Descriptor{
        RequestType: openapi.TypeOf[taggedRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "tags.create", pattern: "/tags/", methods: []string{"POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    tags := document.Components.Schemas["taggedRequest"].Properties["tags"]
    if nil == tags || "array" != tags.Type {
        t.Fatalf("expected an array tags property, got: %+v", tags)
    }

    if nil != tags.MinItems || nil != tags.MaxItems || nil != tags.Minimum || nil != tags.Maximum {
        t.Fatalf("min/max must not emit array or numeric bounds (the validator enforces string length), got: %+v", tags)
    }

    code := document.Components.Schemas["taggedRequest"].Properties["code"]
    if nil == code || nil == code.MinLength || 2 != *code.MinLength || nil == code.MaxLength || 8 != *code.MaxLength {
        t.Fatalf("expected min/max to set string length bounds on code, got: %+v", code)
    }
}

func firstSameNamedType() reflect.Type {
    type Request struct {
        Alpha string `json:"alpha"`
    }

    return reflect.TypeOf(Request{})
}

func secondSameNamedType() reflect.Type {
    type Request struct {
        Beta int `json:"beta"`
    }

    return reflect.TypeOf(Request{})
}

func TestGenerate_SameNamedTypesGetDistinctComponents(t *testing.T) {
    registry := openapi.NewRegistry()
    registry.Describe("first.create", openapi.Descriptor{RequestType: firstSameNamedType()})
    registry.Describe("second.create", openapi.Descriptor{RequestType: secondSameNamedType()})

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "first.create", pattern: "/first/", methods: []string{"POST"}},
        fakeRoute{name: "second.create", pattern: "/second/", methods: []string{"POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    first := document.Components.Schemas["Request"]
    second := document.Components.Schemas["Request2"]
    if nil == first || nil == second {
        t.Fatalf("expected two distinct component schemas for the same-named types, got: %v", document.Components.Schemas)
    }

    if nil == first.Properties["alpha"] {
        t.Fatalf("expected the first Request schema to carry its own alpha field, got: %+v", first)
    }

    if nil == second.Properties["beta"] {
        t.Fatalf("expected the disambiguated Request2 schema to carry its own beta field, got: %+v", second)
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

func TestGenerate_NormalizesWildcardSegments(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "files.read", pattern: "/files/*path", methods: []string{"GET"}},
        fakeRoute{name: "assets.read", pattern: "/assets/*rest...", methods: []string{"GET"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    if _, ok := document.Paths["/files/{path}"]; false == ok {
        t.Fatalf("expected wildcard segment normalized to /files/{path}, got %v", keysOf(document.Paths))
    }
    if _, ok := document.Paths["/assets/{rest}"]; false == ok {
        t.Fatalf("expected catch-all segment normalized to /assets/{rest}, got %v", keysOf(document.Paths))
    }
    if _, ok := document.Paths["/files/*path"]; true == ok {
        t.Fatalf("raw wildcard path key must not be emitted")
    }

    operation := document.Paths["/files/{path}"].Get
    if nil == operation {
        t.Fatalf("expected a GET operation for /files/{path}")
    }

    found := false
    for _, parameter := range operation.Parameters {
        if "path" == parameter.Name && "path" == parameter.In {
            found = true
        }
    }
    if false == found {
        t.Fatalf("expected a path parameter named 'path', got %+v", operation.Parameters)
    }
}

func TestGenerate_StripsOptionalPathParameterMarker(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "page.show", pattern: "/page/:slug?", methods: []string{"GET"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    if _, ok := document.Paths["/page/{slug}"]; false == ok {
        t.Fatalf("expected optional parameter normalized to /page/{slug}, got %v", keysOf(document.Paths))
    }
    if _, ok := document.Paths["/page/{slug?}"]; true == ok {
        t.Fatalf("path template must not contain the optional marker '?'")
    }

    operation := document.Paths["/page/{slug}"].Get
    if nil == operation {
        t.Fatalf("expected a GET operation for /page/{slug}")
    }
    for _, parameter := range operation.Parameters {
        if true == strings.Contains(parameter.Name, "?") {
            t.Fatalf("parameter name must not contain '?': %+v", parameter)
        }
    }
}

func TestGenerate_BareWildcardGetsPositionalName(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "catchall", pattern: "/files/*", methods: []string{"GET"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    for path := range document.Paths {
        if true == strings.Contains(path, "*") {
            t.Fatalf("raw '*' must not appear in a path key, got %v", keysOf(document.Paths))
        }
    }
}

func TestGenerate_EmitsOptionsAndHeadOperations(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "health", pattern: "/health", methods: []string{"HEAD", "OPTIONS"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    pathItem, ok := document.Paths["/health"]
    if false == ok {
        t.Fatalf("expected the /health route to appear in the document, got %v", keysOf(document.Paths))
    }
    if nil == pathItem.Head {
        t.Fatalf("expected a HEAD operation for /health")
    }
    if nil == pathItem.Options {
        t.Fatalf("expected an OPTIONS operation for /health")
    }
}

func TestGenerate_NumericConstraintsAreNotEmittedOnStringFields(t *testing.T) {
    type stringConstraintRequest struct {
        Code string `json:"code" validate:"greaterThan=0,regex=^x$"`
    }

    registry := openapi.NewRegistry()
    registry.Describe("codes.create", openapi.Descriptor{RequestType: openapi.TypeOf[stringConstraintRequest]()})

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "codes.create", pattern: "/codes", methods: []string{"POST"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    schema := document.Components.Schemas["stringConstraintRequest"]
    if nil == schema {
        t.Fatalf("expected a component schema for the request type, got %v", document.Components)
    }

    codeSchema := schema.Properties["code"]
    if nil == codeSchema {
        t.Fatalf("expected a 'code' property schema")
    }

    if nil != codeSchema.Minimum {
        t.Fatalf("greaterThan must not set minimum on a string field: %+v", codeSchema)
    }
}

func TestGenerate_NoRequestBodyOnBodylessMethods(t *testing.T) {
    registry := openapi.NewRegistry()
    registry.Describe("things.handle", openapi.Descriptor{
        RequestType: openapi.TypeOf[createProductRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "things.handle", pattern: "/things", methods: []string{"GET", "POST", "DELETE", "HEAD"}},
    }

    document := openapi.Generate(openapi.Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    pathItem := document.Paths["/things"]
    if nil == pathItem.Get || nil == pathItem.Post || nil == pathItem.Delete || nil == pathItem.Head {
        t.Fatalf("expected all four operations to be present")
    }

    if nil != pathItem.Get.RequestBody {
        t.Fatalf("GET must not carry a requestBody")
    }
    if nil != pathItem.Head.RequestBody {
        t.Fatalf("HEAD must not carry a requestBody")
    }
    if nil != pathItem.Delete.RequestBody {
        t.Fatalf("DELETE must not carry a requestBody")
    }
    if nil == pathItem.Post.RequestBody {
        t.Fatalf("POST must carry the request body")
    }
}
