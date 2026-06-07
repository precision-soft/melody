package openapi

import (
    nethttp "net/http"
    "reflect"
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
    componentNames := make(map[reflect.Type]string)

    for _, routeDefinition := range routeDefinitions {
        path, pathParameters := convertPattern(routeDefinition.Pattern())

        descriptor := Descriptor{}
        hasDescriptor := false
        if nil != registry {
            descriptor, hasDescriptor = registry.Get(routeDefinition.Name())
        }

        methods := routeDefinition.Methods()

        pathItem := document.Paths[path]
        for _, method := range methods {
            operationId := operationIdFor(routeDefinition.Name(), method, len(methods))
            operation := buildOperation(operationId, pathParameters, descriptor, hasDescriptor, components, componentNames)
            assignOperation(&pathItem, method, operation)
        }

        document.Paths[path] = pathItem
    }

    if 0 < len(components) {
        document.Components = &Components{Schemas: components}
    }

    return document
}

func operationIdFor(routeName string, method string, methodCount int) string {
    if methodCount <= 1 {
        return routeName
    }

    return routeName + "." + strings.ToLower(method)
}

func buildOperation(
    operationId string,
    pathParameters []Parameter,
    descriptor Descriptor,
    hasDescriptor bool,
    components map[string]*Schema,
    names map[reflect.Type]string,
) *Operation {
    operation := &Operation{
        OperationId: operationId,
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
                    "application/json": {Schema: schemaFromType(descriptor.RequestType, components, names)},
                },
            }
        }

        for status, responseType := range descriptor.Responses {
            operation.Responses[strconv.Itoa(status)] = ResponseObject{
                Description: nethttp.StatusText(status),
                Content: map[string]MediaType{
                    "application/json": {Schema: schemaFromType(responseType, components, names)},
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
