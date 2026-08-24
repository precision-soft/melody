package openapi

import (
    "reflect"
    "strings"
    "testing"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

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

func keysOf(paths map[string]PathItem) []string {
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

func TestGenerate_MultiMethodRouteEmitsDistinctOperationsAndUniqueOperationIds(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "thing.handle", pattern: "/thing/", methods: []string{"GET", "POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

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
    registry := NewRegistry()
    registry.Describe("products.create", Descriptor{
        Summary:     "Create a product",
        Tags:        []string{"products"},
        RequestType: TypeOf[createProductRequest](),
        Responses: map[int]reflect.Type{
            201: TypeOf[productResponse](),
        },
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "products.read", pattern: "/products/api/read/:id/", methods: []string{"GET"}},
        fakeRoute{name: "products.create", pattern: "/products/api/create/", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

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
    registry := NewRegistry()
    registry.Describe("orders.create", Descriptor{
        RequestType: TypeOf[numericRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "orders.create", pattern: "/orders/", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    schema := document.Components.Schemas["numericRequest"]
    if nil == schema {
        t.Fatalf("expected the numericRequest component schema")
    }

    /* inverted with the validation repairs: min on an integer no longer passes silently — the length constraint refuses a non-string value, so the field is advertised unsatisfiable (the empty exclusive window) instead of as an unconstrained integer */
    quantity := schema.Properties["quantity"]
    if nil == quantity || "integer" != quantity.Type || nil != quantity.MinLength ||
        nil == quantity.Minimum || 0 != *quantity.Minimum || nil == quantity.ExclusiveMinimum ||
        nil == quantity.Maximum || 0 != *quantity.Maximum || nil == quantity.ExclusiveMaximum {
        t.Fatalf("expected min=1 on an integer advertised as the empty exclusive window without a minLength, got: %+v", quantity)
    }
    if false == containsString(schema.Required, "quantity") {
        t.Fatalf("expected the reject-all quantity field listed required, got: %v", schema.Required)
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
    registry := NewRegistry()
    registry.Describe("shadow.create", Descriptor{
        RequestType: TypeOf[shadowedEmbedRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "shadow.create", pattern: "/shadow/", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

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
    registry := NewRegistry()
    registry.Describe("depth.create", Descriptor{
        RequestType: TypeOf[embedDepthRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "depth.create", pattern: "/depth/", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

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
    registry := NewRegistry()
    registry.Describe("diamond.create", Descriptor{
        RequestType: TypeOf[diamondEmbedRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "diamond.create", pattern: "/diamond/", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

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
    registry := NewRegistry()
    registry.Describe("nullable.create", Descriptor{
        RequestType: TypeOf[nullableRefRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "nullable.create", pattern: "/nullable/", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

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

/* inverted with the validation repairs: min/max measure a genuine string and refuse every other shape outright, so on a []string field they no longer pass silently — the array is advertised unsatisfiable (impossible items window) instead of unconstrained — while the string field keeps its exact length bounds */
func TestGenerate_MinMaxAppliesOnlyToStringLength(t *testing.T) {
    registry := NewRegistry()
    registry.Describe("tags.create", Descriptor{
        RequestType: TypeOf[taggedRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "tags.create", pattern: "/tags/", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    tags := document.Components.Schemas["taggedRequest"].Properties["tags"]
    if nil == tags || "array" != tags.Type {
        t.Fatalf("expected an array tags property, got: %+v", tags)
    }

    if nil == tags.MinItems || 1 != *tags.MinItems || nil == tags.MaxItems || 0 != *tags.MaxItems {
        t.Fatalf("expected min/max on an array advertised unsatisfiable (minItems 1, maxItems 0 — the validator refuses a non-string value), got: %+v", tags)
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
    registry := NewRegistry()
    registry.Describe("first.create", Descriptor{RequestType: firstSameNamedType()})
    registry.Describe("second.create", Descriptor{RequestType: secondSameNamedType()})

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "first.create", pattern: "/first/", methods: []string{"POST"}},
        fakeRoute{name: "second.create", pattern: "/second/", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

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

func TestGenerate_NormalizesWildcardSegments(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "files.read", pattern: "/files/*path", methods: []string{"GET"}},
        fakeRoute{name: "assets.read", pattern: "/assets/*rest...", methods: []string{"GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

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

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

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

/* a brace segment is a literal path component to the router, so an optional marker inside braces must not mint the shortened path the ":name?" spelling legitimately serves */
func TestGenerate_BraceOptionalMarkerDoesNotMintAShortenedPath(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "page.show", pattern: "/page/{slug?}", methods: []string{"GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    if _, ok := document.Paths["/page"]; true == ok {
        t.Fatalf("expected no shortened path for a brace segment the router matches literally, got %v", keysOf(document.Paths))
    }

    if 1 != len(document.Paths) {
        t.Fatalf("expected exactly one path, got %v", keysOf(document.Paths))
    }
}

func TestGenerate_BareWildcardGetsPositionalName(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "catchall", pattern: "/files/*", methods: []string{"GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

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

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

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

    registry := NewRegistry()
    registry.Describe("codes.create", Descriptor{RequestType: TypeOf[stringConstraintRequest]()})

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "codes.create", pattern: "/codes", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    schema := document.Components.Schemas["stringConstraintRequest"]
    if nil == schema {
        t.Fatalf("expected a component schema for the request type, got %v", document.Components)
    }

    codeSchema := schema.Properties["code"]
    if nil == codeSchema {
        t.Fatalf("expected a 'code' property schema")
    }

    if nil != codeSchema.Minimum {
        t.Fatalf("greaterThan must not set a numeric minimum on a string field: %+v", codeSchema)
    }

    /* the validator rejects every value of a string field tagged greaterThan ("value must be numeric"), so the spec must advertise it unsatisfiable (an impossible length window) rather than as a satisfiable string a client would trust */
    if nil == codeSchema.MinLength || 1 != *codeSchema.MinLength || nil == codeSchema.MaxLength || 0 != *codeSchema.MaxLength {
        t.Fatalf("expected greaterThan on a string to advertise an unsatisfiable string (minLength 1, maxLength 0), got %+v", codeSchema)
    }
}

func TestGenerate_NoRequestBodyOnBodylessMethods(t *testing.T) {
    registry := NewRegistry()
    registry.Describe("things.handle", Descriptor{
        RequestType: TypeOf[createProductRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "things.handle", pattern: "/things", methods: []string{"GET", "POST", "DELETE", "HEAD"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

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

type nullableNumericRequest struct {
    MinCount *int     `json:"minCount" validate:"greaterThan=0"`
    MaxScore *float64 `json:"maxScore" validate:"lessThan=100"`
    Optional *int     `json:"optional"`
}

func TestGenerate_PointerNumericWithBoundIsNotNullable(t *testing.T) {
    registry := NewRegistry()
    registry.Describe("metrics.create", Descriptor{
        RequestType: TypeOf[nullableNumericRequest](),
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "metrics.create", pattern: "/metrics/", methods: []string{"POST"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    schema := document.Components.Schemas["nullableNumericRequest"]
    if nil == schema {
        t.Fatalf("expected the nullableNumericRequest component schema")
    }

    minCount := schema.Properties["minCount"]
    if nil == minCount || true == minCount.Nullable {
        t.Fatalf("expected minCount to not be nullable (greaterThan rejects null), got: %+v", minCount)
    }
    if nil == minCount.Minimum || 0 != *minCount.Minimum || nil == minCount.ExclusiveMinimum || false == *minCount.ExclusiveMinimum {
        t.Fatalf("expected an exclusive minimum of 0 on minCount, got: %+v", minCount)
    }

    maxScore := schema.Properties["maxScore"]
    if nil == maxScore || true == maxScore.Nullable {
        t.Fatalf("expected maxScore to not be nullable (lessThan rejects null), got: %+v", maxScore)
    }
    if nil == maxScore.Maximum || 100 != *maxScore.Maximum || nil == maxScore.ExclusiveMaximum || false == *maxScore.ExclusiveMaximum {
        t.Fatalf("expected an exclusive maximum of 100 on maxScore, got: %+v", maxScore)
    }

    optional := schema.Properties["optional"]
    if nil == optional || false == optional.Nullable {
        t.Fatalf("expected a bound-less pointer field to remain nullable, got: %+v", optional)
    }
}

func TestGenerate_ResponseWithoutBodyTypeIsDescribed(t *testing.T) {
    registry := NewRegistry()
    registry.Describe("products.delete", Descriptor{
        Responses: map[int]reflect.Type{
            204: nil,
        },
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "products.delete", pattern: "/products/:id/", methods: []string{"DELETE"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    pathItem, exists := document.Paths["/products/{id}/"]
    if false == exists {
        t.Fatalf("expected the delete path")
    }

    response, hasResponse := pathItem.Delete.Responses["204"]
    if false == hasResponse {
        t.Fatalf("expected the 204 response")
    }

    mediaType, hasMediaType := response.Content["application/json"]
    if false == hasMediaType {
        t.Fatalf("expected the json media type")
    }

    if nil == mediaType.Schema {
        t.Fatalf("expected a schema for the bodyless response")
    }

    if "" != mediaType.Schema.Type {
        t.Fatalf("unexpected schema type: %s", mediaType.Schema.Type)
    }
}

func TestGenerate_OptionalTailSegmentIsMirroredAsBothPaths(t *testing.T) {
    registry := NewRegistry().Describe(
        "page.show",
        Descriptor{
            Summary: "show a page",
            Tags:    []string{"page"},
        },
    )

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "page.show", pattern: "/page/:slug?", methods: []string{"GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    long, present := document.Paths["/page/{slug}"]
    if false == present {
        t.Fatalf("expected the supplied-parameter path /page/{slug}, got %v", keysOf(document.Paths))
    }

    short, present := document.Paths["/page"]
    if false == present {
        t.Fatalf("expected the omitted-parameter path /page, got %v", keysOf(document.Paths))
    }

    if nil == short.Get || nil == long.Get {
        t.Fatalf("expected a GET operation on both paths")
    }

    if 0 != len(short.Get.Parameters) {
        t.Fatalf("expected no path parameter on the omitted-parameter path, got %+v", short.Get.Parameters)
    }

    if 1 != len(long.Get.Parameters) ||
        "slug" != long.Get.Parameters[0].Name ||
        "path" != long.Get.Parameters[0].In ||
        false == long.Get.Parameters[0].Required {
        t.Fatalf("expected a single required slug path parameter, got %+v", long.Get.Parameters)
    }

    if long.Get.OperationId == short.Get.OperationId {
        t.Fatalf("expected distinct operation ids across the two paths, got %q", long.Get.OperationId)
    }

    if short.Get.Summary != long.Get.Summary || false == containsString(short.Get.Tags, "page") {
        t.Fatalf("expected both paths to share the described operation, got %+v", short.Get)
    }
}

func TestGenerate_OptionalTailSegmentAtTheRootMirrorsTheRootPath(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "home", pattern: "/:locale?", methods: []string{"GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    for _, path := range []string{"/", "/{locale}"} {
        if _, present := document.Paths[path]; false == present {
            t.Fatalf("expected the path %q, got %v", path, keysOf(document.Paths))
        }
    }
}

func TestGenerate_RequiredTailSegmentEmitsASinglePath(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "page.show", pattern: "/page/:slug", methods: []string{"GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    if 1 != len(document.Paths) {
        t.Fatalf("expected a single path for a required parameter, got %v", keysOf(document.Paths))
    }
}

func TestGenerate_MirroredPathDoesNotDisplaceARouteRegisteredThere(t *testing.T) {
    for _, routes := range [][]httpcontract.RouteDefinition{
        {
            fakeRoute{name: "page.show", pattern: "/page/:slug?", methods: []string{"GET"}},
            fakeRoute{name: "page.index", pattern: "/page", methods: []string{"GET"}},
        },
        {
            fakeRoute{name: "page.index", pattern: "/page", methods: []string{"GET"}},
            fakeRoute{name: "page.show", pattern: "/page/:slug?", methods: []string{"GET"}},
        },
    } {
        document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

        operation := document.Paths["/page"].Get
        if nil == operation {
            t.Fatalf("expected a GET operation on /page")
        }

        if "page.index" != operation.OperationId {
            t.Fatalf("expected the route registered at /page to own the operation, got %q", operation.OperationId)
        }
    }
}

/* the range over descriptor.Responses is the one unordered driver of first-touch component naming: iterated directly, whichever type a run visits first takes the bare name and the other takes the numbered sibling, so two runs over one registry disagree on every $ref to a colliding name — the statuses are visited sorted, and thirty-two fresh generations pin the order because a surviving inversion would have to win a coin flip every time. */
func TestGenerate_ResponsesAreVisitedInStatusOrder(t *testing.T) {
    for iteration := 0; iteration < 32; iteration++ {
        registry := NewRegistry()
        registry.Describe("pages.read", Descriptor{
            Responses: map[int]reflect.Type{
                200: TypeOf[genericPage[genericPageUser]](),
                409: TypeOf[genericPage[*genericPageUser]](),
            },
        })

        routes := []httpcontract.RouteDefinition{
            fakeRoute{name: "pages.read", pattern: "/pages/", methods: []string{"GET"}},
        }

        document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

        operation := document.Paths["/pages/"].Get
        if nil == operation {
            t.Fatalf("expected the GET operation")
        }

        okRef := operation.Responses["200"].Content["application/json"].Schema.Ref
        conflictRef := operation.Responses["409"].Content["application/json"].Schema.Ref

        if okRef+"2" != conflictRef {
            t.Fatalf("iteration %d: expected status 200 to name the component first, got %q and %q", iteration, okRef, conflictRef)
        }
    }
}

/* the router treats an empty method list as answering every verb, so an operation-less path item would read as an endpoint answering nothing while the server answers everything — the document spells the eight path item verbs out, each with its own operationId. */
func TestGenerate_ARouteWithoutMethodsDocumentsEveryPathItemVerb(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "webhook.catch", pattern: "/webhook/", methods: nil},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    pathItem := document.Paths["/webhook/"]

    operations := []*Operation{
        pathItem.Get, pathItem.Post, pathItem.Put, pathItem.Patch,
        pathItem.Delete, pathItem.Options, pathItem.Head, pathItem.Trace,
    }

    for _, operation := range operations {
        if nil == operation {
            t.Fatalf("expected every path item verb to carry an operation, got %+v", pathItem)
        }
    }

    if "webhook.catch.get" != pathItem.Get.OperationId || "webhook.catch.trace" != pathItem.Trace.OperationId {
        t.Fatalf("expected per-verb operationIds, got %q and %q", pathItem.Get.OperationId, pathItem.Trace.OperationId)
    }
}

/* a verb outside the eight the format models has no slot in a path item; the operation used to be built and dropped without a trace, an endpoint answering in production and absent from the spec — the route now stays in the document with the undescribed verb named. */
func TestGenerate_ANonStandardVerbIsNamedOnThePathItem(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "cache.purge", pattern: "/cache/", methods: []string{"PURGE", "GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    pathItem := document.Paths["/cache/"]

    if nil == pathItem.Get {
        t.Fatalf("expected the representable verb to keep its operation")
    }

    if false == strings.Contains(pathItem.Description, "PURGE") {
        t.Fatalf("expected the undescribed verb named on the path item, got %q", pathItem.Description)
    }
}

/* the router reads the "..." suffix as a catch-all and its registration RETURNS there: every segment written after it is discarded and never matched, so the converted path mirrors that instead of advertising a template no request the route answers can ever spell; a mid-pattern "*name" without the dots is a single-segment wildcard and keeps its tail. */
func TestGenerate_ACatchAllPatternDropsTheSegmentsTheRouterDrops(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "assets.read", pattern: "/assets/*rest.../thumbnail", methods: []string{"GET"}},
        fakeRoute{name: "mirrors.read", pattern: "/mirrors/*host/status", methods: []string{"GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    if _, exists := document.Paths["/assets/{rest}"]; false == exists {
        t.Fatalf("expected the catch-all path truncated at the catch-all, got %v", keysOf(document.Paths))
    }

    if _, exists := document.Paths["/assets/{rest}/thumbnail"]; true == exists {
        t.Fatalf("expected no path for the segments the router discards")
    }

    if _, exists := document.Paths["/mirrors/{host}/status"]; false == exists {
        t.Fatalf("expected the single-segment wildcard to keep its tail, got %v", keysOf(document.Paths))
    }
}

/* two routes whose patterns converge on one converted path — a placeholder against a brace literal — must not silently replace each other's operations: the earlier registration wins, exactly as it does in the router's match order. */
func TestGenerate_ALaterRouteDoesNotDisplaceAnEarlierRoutesOperation(t *testing.T) {
    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "users.read", pattern: "/users/:id", methods: []string{"GET"}},
        fakeRoute{name: "users.read.literal", pattern: "/users/{id}", methods: []string{"GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, nil)

    operation := document.Paths["/users/{id}"].Get
    if nil == operation || "users.read" != operation.OperationId {
        t.Fatalf("expected the earlier route to keep the slot, got %+v", operation)
    }
}

/* a status outside the registered table answers an empty status text, and the response description is required by the format — an empty string is a spec violation most tooling rejects. */
func TestGenerate_AnUnregisteredStatusCodeKeepsADescription(t *testing.T) {
    registry := NewRegistry()
    registry.Describe("things.read", Descriptor{
        Responses: map[int]reflect.Type{
            599: TypeOf[productResponse](),
        },
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "things.read", pattern: "/things/", methods: []string{"GET"}},
    }

    document := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    response := document.Paths["/things/"].Get.Responses["599"]
    if "response" != response.Description {
        t.Fatalf("expected a non-empty description for the unregistered status, got %q", response.Description)
    }
}

/* the descriptor arrives by value but its Tags slice shares the registry's backing array; a document that aliases it hands every post-processing write through into the boot-time registry and every later generation. */
func TestGenerate_TheDocumentDoesNotAliasTheRegistryTags(t *testing.T) {
    registry := NewRegistry()
    registry.Describe("products.read", Descriptor{
        Tags: []string{"products"},
        Responses: map[int]reflect.Type{
            200: TypeOf[productResponse](),
        },
    })

    routes := []httpcontract.RouteDefinition{
        fakeRoute{name: "products.read", pattern: "/products/", methods: []string{"GET"}},
    }

    first := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)
    first.Paths["/products/"].Get.Tags[0] = "mutated"

    second := Generate(Info{Title: "Example", Version: "1.0.0"}, routes, registry)

    if "products" != second.Paths["/products/"].Get.Tags[0] {
        t.Fatalf("expected the registry tags untouched by a document write, got %q", second.Paths["/products/"].Get.Tags[0])
    }
}
