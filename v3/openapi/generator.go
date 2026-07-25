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
        descriptor := Descriptor{}
        hasDescriptor := false
        if nil != registry {
            descriptor, hasDescriptor = registry.Get(routeDefinition.Name())
        }

        methods := routeDefinition.Methods()

        for _, expansion := range expandOptionalTailSegment(routeDefinition.Pattern()) {
            path, pathParameters := convertPattern(expansion.pattern)

            pathItem := document.Paths[path]
            for _, method := range methods {
                /* a route registered at the shortened path itself describes it better than this mirror does, and it may be reached either before or after this route */
                if true == expansion.omitsParameter && nil != operationFor(&pathItem, method) {
                    continue
                }

                operationId := operationIdFor(routeDefinition.Name(), method, len(methods))
                if true == expansion.omitsParameter {
                    operationId = operationId + ".without"
                    if "" != expansion.omittedParameter {
                        operationId = operationId + "." + expansion.omittedParameter
                    }
                }

                operation := buildOperation(operationId, method, pathParameters, descriptor, hasDescriptor, components, componentNames)
                assignOperation(&pathItem, method, operation)
            }

            document.Paths[path] = pathItem
        }
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

func methodAcceptsRequestBody(method string) bool {
    switch strings.ToUpper(method) {
    case nethttp.MethodGet, nethttp.MethodHead, nethttp.MethodDelete, nethttp.MethodOptions, nethttp.MethodTrace:
        return false
    default:
        return true
    }
}

func buildOperation(
    operationId string,
    method string,
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

        if nil != descriptor.RequestType && true == methodAcceptsRequestBody(method) {
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

type patternExpansion struct {
    pattern          string
    omitsParameter   bool
    omittedParameter string
}

/* the router serves a trailing optional parameter both ways, so a single path key describes only half of what answers. "in: path" forbids "required: false", which leaves one path item per shape: the pattern without the optional segment, and the pattern with it. Only the final segment can carry the marker, so the shortened form is always the pattern minus its last segment. */
func expandOptionalTailSegment(pattern string) []patternExpansion {
    segments := strings.Split(pattern, "/")

    parameterName, optional := optionalTailParameterName(segments[len(segments)-1])
    if false == optional {
        return []patternExpansion{{pattern: pattern}}
    }

    shortened := strings.Join(segments[:len(segments)-1], "/")
    if "" == shortened {
        shortened = "/"
    }

    return []patternExpansion{
        {
            pattern:          shortened,
            omitsParameter:   true,
            omittedParameter: parameterName,
        },
        {
            pattern: pattern,
        },
    }
}

/* only the ":name?" spelling is expanded into an omitted and a supplied form, because only that spelling is a placeholder to the router: a brace segment is matched literally (http/router.go registers and matches ":" and "*" segments and nothing else), so expanding "{name?}" would mint a shortened path no route answers. convertPattern still renders a brace segment as a path parameter, which keeps the spec's rendering of brace patterns unchanged and wrong in the same pre-existing way rather than inventing an endpoint on top of it. */
func optionalTailParameterName(segment string) (string, bool) {
    if false == strings.HasPrefix(segment, ":") {
        return "", false
    }

    if false == strings.HasSuffix(segment, "?") {
        return "", false
    }

    return strings.TrimSuffix(segment[1:], "?"), true
}

func convertPattern(pattern string) (string, []Parameter) {
    segments := strings.Split(pattern, "/")

    var parameters []Parameter

    for index, segment := range segments {
        name := ""
        placeholder := false

        if true == strings.HasPrefix(segment, ":") {
            placeholder = true
            name = strings.TrimSuffix(segment[1:], "?")
        } else if true == strings.HasPrefix(segment, "{") && true == strings.HasSuffix(segment, "}") {
            placeholder = true
            name = strings.TrimSuffix(segment[1:len(segment)-1], "?")
        } else if true == strings.HasPrefix(segment, "*") {
            placeholder = true
            name = strings.TrimSuffix(segment[1:], "...")
        }

        if false == placeholder {
            continue
        }

        if "" == name {
            name = "param" + strconv.Itoa(index)
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

func operationFor(pathItem *PathItem, method string) *Operation {
    switch strings.ToUpper(method) {
    case nethttp.MethodGet:
        return pathItem.Get
    case nethttp.MethodPost:
        return pathItem.Post
    case nethttp.MethodPut:
        return pathItem.Put
    case nethttp.MethodPatch:
        return pathItem.Patch
    case nethttp.MethodDelete:
        return pathItem.Delete
    case nethttp.MethodOptions:
        return pathItem.Options
    case nethttp.MethodHead:
        return pathItem.Head
    case nethttp.MethodTrace:
        return pathItem.Trace
    }

    return nil
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
    case nethttp.MethodOptions:
        pathItem.Options = operation
    case nethttp.MethodHead:
        pathItem.Head = operation
    case nethttp.MethodTrace:
        pathItem.Trace = operation
    }
}
