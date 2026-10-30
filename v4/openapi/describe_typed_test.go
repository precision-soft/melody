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
        DescribeConfig{
            Summary: "Create a product",
            Tags:    []string{"products"},
        },
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

type describeTypedNotFound struct {
    Reason string `json:"reason"`
}

func TestDescribeTyped_TheZeroConfigDescribesTheRequestAndTheOneResponse(t *testing.T) {
    registry := NewRegistry()

    DescribeTyped[describeTypedRequest, describeTypedResponse](registry, "products.create", nethttp.StatusCreated, DescribeConfig{})

    descriptor, _ := registry.Get("products.create")
    if 1 != len(descriptor.Responses) || "" != descriptor.Summary || nil != descriptor.Tags {
        t.Fatalf("expected the request and the one response alone, got %+v", descriptor)
    }
}

func TestDescribeTyped_ResponsesAddAStatusAndTheTagsAreCopied(t *testing.T) {
    registry := NewRegistry()
    tags := []string{"products"}

    DescribeTyped[describeTypedRequest, describeTypedResponse](
        registry,
        "products.create",
        nethttp.StatusCreated,
        DescribeConfig{
            Description: "Creates a product",
            Tags:        tags,
            Responses:   []ResponseDescription{Response[describeTypedNotFound](nethttp.StatusNotFound)},
        },
    )
    tags[0] = "mutated"

    descriptor, _ := registry.Get("products.create")
    if TypeOf[describeTypedNotFound]() != descriptor.Responses[nethttp.StatusNotFound] || TypeOf[describeTypedResponse]() != descriptor.Responses[nethttp.StatusCreated] {
        t.Fatalf("expected the 404 beside the 201, got %v", descriptor.Responses)
    }

    if "Creates a product" != descriptor.Description || 1 != len(descriptor.Tags) || "products" != descriptor.Tags[0] {
        t.Fatalf("expected the description and the copied tags, got %q %v", descriptor.Description, descriptor.Tags)
    }
}
