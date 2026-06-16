package openapi

import (
    "reflect"
)

type DescribeOption func(*Descriptor)

func WithSummary(summary string) DescribeOption {
    return func(descriptor *Descriptor) {
        descriptor.Summary = summary
    }
}

func WithDescription(description string) DescribeOption {
    return func(descriptor *Descriptor) {
        descriptor.Description = description
    }
}

func WithTags(tags ...string) DescribeOption {
    return func(descriptor *Descriptor) {
        descriptor.Tags = tags
    }
}

func WithResponse[T any](status int) DescribeOption {
    return func(descriptor *Descriptor) {
        descriptor.Responses[status] = TypeOf[T]()
    }
}

func DescribeTyped[Req any, Resp any](registry *Registry, routeName string, status int, options ...DescribeOption) *Registry {
    descriptor := Descriptor{
        RequestType: TypeOf[Req](),
        Responses: map[int]reflect.Type{
            status: TypeOf[Resp](),
        },
    }

    for _, option := range options {
        option(&descriptor)
    }

    return registry.Describe(routeName, descriptor)
}
