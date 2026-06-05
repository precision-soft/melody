package openapi

import (
    nethttp "net/http"
    "strconv"
    "strings"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func Generate(
    info Info,
    routeDefinitions []httpcontract.RouteDefinition,
    registry *Registry,
) *Document {
    document := &Document{
        OpenApi: "3.0.3",
        Info:    info,
        Paths:   make(map[string]PathItem),
    }

    components := make(map[string]*Schema)

    for _, routeDefinition := range routeDefinitions {
        path, pathParameters := convertPattern(routeDefinition.Pattern())

        descriptor := Descriptor{}
        hasDescriptor := false
        if nil != registry {
            descriptor, hasDescriptor = registry.Get(routeDefinition.Name())
        }

        operation := buildOperation(routeDefinition, pathParameters, descriptor, hasDescriptor, components)

        pathItem := document.Paths[path]
        for _, method := range routeDefinition.Methods() {
            assignOperation(&pathItem, method, operation)
        }

        document.Paths[path] = pathItem
    }

    if 0 < len(components) {
        document.Components = &Components{Schemas: components}
    }

    return document
}

func buildOperation(
    routeDefinition httpcontract.RouteDefinition,
    pathParameters []Parameter,
    descriptor Descriptor,
    hasDescriptor bool,
    components map[string]*Schema,
) *Operation {
    operation := &Operation{
        OperationId: routeDefinition.Name(),
        Parameters:  pathParameters,
        Responses:   make(map[string]ResponseObject),
    }

    if true == hasDescriptor {
        operation.Summary = descriptor.Summary
        operation.Description = descriptor.Description
        operation.Tags = descriptor.Tags

        if nil != descriptor.RequestType {
            operation.RequestBody = &RequestBody{
                Required: true,
                Content: map[string]MediaType{
                    "application/json": {Schema: schemaFromType(descriptor.RequestType, components)},
                },
            }
        }

        for status, responseType := range descriptor.Responses {
            operation.Responses[strconv.Itoa(status)] = ResponseObject{
                Description: nethttp.StatusText(status),
                Content: map[string]MediaType{
                    "application/json": {Schema: schemaFromType(responseType, components)},
                },
            }
        }
    }

    if 0 == len(operation.Responses) {
        operation.Responses["default"] = ResponseObject{Description: "response"}
    }

    return operation
}

func convertPattern(pattern string) (string, []Parameter) {
    segments := strings.Split(pattern, "/")

    var parameters []Parameter

    for index, segment := range segments {
        name := ""

        if true == strings.HasPrefix(segment, ":") {
            name = segment[1:]
        } else if true == strings.HasPrefix(segment, "{") && true == strings.HasSuffix(segment, "}") {
            name = segment[1 : len(segment)-1]
        }

        if "" == name {
            continue
        }

        segments[index] = "{" + name + "}"
        parameters = append(parameters, Parameter{
            Name:     name,
            In:       "path",
            Required: true,
            Schema:   &Schema{Type: "string"},
        })
    }

    return strings.Join(segments, "/"), parameters
}

func assignOperation(pathItem *PathItem, method string, operation *Operation) {
    switch strings.ToUpper(method) {
    case nethttp.MethodGet:
        pathItem.Get = operation
    case nethttp.MethodPost:
        pathItem.Post = operation
    case nethttp.MethodPut:
        pathItem.Put = operation
    case nethttp.MethodPatch:
        pathItem.Patch = operation
    case nethttp.MethodDelete:
        pathItem.Delete = operation
    }
}
