package openapi_test

import (
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v3/openapi"
)

type describeTypedRequest struct {
    Name string `json:"name"`
}

type describeTypedResponse struct {
    Id string `json:"id"`
}

func TestDescribeTyped_RegistersRequestAndResponseTypes(t *testing.T) {
    registry := openapi.NewRegistry()

    openapi.DescribeTyped[describeTypedRequest, describeTypedResponse](
        registry,
        "products.create",
        nethttp.StatusCreated,
        openapi.WithSummary("Create a product"),
        openapi.WithTags("products"),
    )

    descriptor, exists := registry.Get("products.create")
    if false == exists {
        t.Fatalf("expected the route to be described")
    }

    if openapi.TypeOf[describeTypedRequest]() != descriptor.RequestType {
        t.Fatalf("unexpected request type %v", descriptor.RequestType)
    }

    if openapi.TypeOf[describeTypedResponse]() != descriptor.Responses[nethttp.StatusCreated] {
        t.Fatalf("unexpected response type for 201")
    }

    if "Create a product" != descriptor.Summary {
        t.Fatalf("unexpected summary %q", descriptor.Summary)
    }
}
