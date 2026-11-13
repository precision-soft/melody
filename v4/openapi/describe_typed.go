package openapi

import (
    "reflect"
)

/* ResponseDescription names one more response of a described route, built by Response. */
type ResponseDescription struct {
    Status int
    Type   reflect.Type
}

/* Response answers the description of a response of type T under status, for DescribeConfig.Responses. */
func Response[T any](status int) ResponseDescription {
    return ResponseDescription{
        Status: status,
        Type:   TypeOf[T](),
    }
}

/* DescribeConfig is what DescribeTyped adds to the typed request and the success response; the zero value adds nothing. */
type DescribeConfig struct {
    Summary     string
    Description string
    Tags        []string

    /* Responses adds a response per entry, an entry repeating a status replacing the earlier one, the success response included. */
    Responses []ResponseDescription
}

/* DescribeTyped describes the route from its typed request and its success response under status, with what the config adds. */
func DescribeTyped[Req any, Resp any](registry *Registry, routeName string, status int, config DescribeConfig) *Registry {
    descriptor := Descriptor{
        RequestType: TypeOf[Req](),
        Summary:     config.Summary,
        Description: config.Description,
        Tags:        config.Tags,
        Responses: map[int]reflect.Type{
            status: TypeOf[Resp](),
        },
    }

    for _, response := range config.Responses {
        descriptor.Responses[response.Status] = response.Type
    }

    return registry.Describe(routeName, descriptor)
}
