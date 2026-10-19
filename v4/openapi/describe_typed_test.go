package openapi

import (
    nethttp "net/http"
    "testing"
)

type describeTypedRequest struct {
    Name string `json:"name"`
}

type describeTypedResponse struct {
    Id string `json:"id"`
}

func TestDescribeTyped_RegistersRequestAndResponseTypes(t *testing.T) {
    registry := NewRegistry()

    DescribeTyped[describeTypedRequest, describeTypedResponse](
        registry,
        "products.create",
        nethttp.StatusCreated,
        WithSummary("Create a product"),
        WithTags("products"),
    )

    descriptor, exists := registry.Get("products.create")
    if false == exists {
        t.Fatalf("expected the route to be described")
    }

    if TypeOf[describeTypedRequest]() != descriptor.RequestType {
        t.Fatalf("unexpected request type %v", descriptor.RequestType)
    }

    if TypeOf[describeTypedResponse]() != descriptor.Responses[nethttp.StatusCreated] {
        t.Fatalf("unexpected response type for 201")
    }

    if "Create a product" != descriptor.Summary {
        t.Fatalf("unexpected summary %q", descriptor.Summary)
    }
}
