package openapi

import (
    nethttp "net/http"
    "reflect"
    "sort"
    "strconv"
    "strings"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* pathItemMethods is every verb a path item can carry, in document order; a route with no method list answers all of them, so each is spelled out. */
var pathItemMethods = []string{
    nethttp.MethodGet,
    nethttp.MethodPost,
    nethttp.MethodPut,
    nethttp.MethodPatch,
    nethttp.MethodDelete,
    nethttp.MethodOptions,
    nethttp.MethodHead,
    nethttp.MethodTrace,
}

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

    /* who holds each slot: a route registered at the shortened path displaces the mirror of an optional tail, whichever of the two is reached first, and between two routes the one the router would serve keeps it */
    slotOwners := make(map[string]slotOwner)

    for _, routeDefinition := range routeDefinitions {
        descriptor := Descriptor{}
        hasDescriptor := false
        if nil != registry {
            descriptor, hasDescriptor = registry.Get(routeDefinition.Name())
        }

        methods := routeDefinition.Methods()
        if 0 == len(methods) {
            methods = pathItemMethods
        }

        for _, expansion := range expandOptionalTailSegment(routeDefinition.Pattern()) {
            path, pathParameters := convertPattern(expansion.pattern)

            pathItem := document.Paths[path]
            for _, method := range methods {
                /* a verb outside the eight the format models has no slot, so the route stays in the document with the verb named instead */
                if false == pathItemCarriesMethod(method) {
                    note := "the route also answers " + strings.ToUpper(method) + ", which an OpenAPI path item cannot describe"
                    if false == strings.Contains(pathItem.Description, note) {
                        if "" != pathItem.Description {
                            pathItem.Description = pathItem.Description + "; "
                        }
                        pathItem.Description = pathItem.Description + note
                    }

                    continue
                }

                /* a mirror always yields a taken slot and a route always displaces a mirror; between two routes converging on one converted path the one the router would serve wins, the higher Priority() first and then the earlier registration */
                slotKey := path + " " + strings.ToUpper(method)
                candidate := slotOwner{priority: routeDefinition.Priority(), mirror: expansion.omitsParameter}
                if nil != operationFor(&pathItem, method) {
                    if true == candidate.mirror {
                        continue
                    }

                    if holder := slotOwners[slotKey]; false == holder.mirror && false == routeOutranks(candidate, holder) {
                        continue
                    }
                }

                operationId := operationIdFor(routeDefinition.Name(), method, len(methods))
                if true == expansion.omitsParameter && "" != operationId {
                    operationId = operationId + ".without"
                    if "" != expansion.omittedParameter {
                        operationId = operationId + "." + expansion.omittedParameter
                    }
                }

                operation := buildOperation(operationId, method, pathParameters, descriptor, hasDescriptor, components, componentNames)
                assignOperation(&pathItem, method, operation)
                slotOwners[slotKey] = candidate
            }

            document.Paths[path] = pathItem
        }
    }

    if 0 < len(components) {
        document.Components = &Components{Schemas: components}
    }

    return document
}

/* slotOwner is what the generator remembers of the operation holding a slot */
type slotOwner struct {
    priority int
    mirror   bool
}

/* routeOutranks reports whether the router serves the candidate over the holder; registration order is the loop's order, so a later candidate of equal priority never does */
func routeOutranks(candidate slotOwner, holder slotOwner) bool {
    return candidate.priority > holder.priority
}

/* operationIdFor names an operation after its route; an unnamed route carries no operationId, the field being optional and two unnamed routes otherwise sharing one */
func operationIdFor(routeName string, method string, methodCount int) string {
    if "" == routeName {
        return ""
    }

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

func copyPathParameters(parameters []Parameter) []Parameter {
    if nil == parameters {
        return nil
    }

    copied := make([]Parameter, len(parameters))
    for index, parameter := range parameters {
        copied[index] = parameter
        if nil != parameter.Schema {
            schema := *parameter.Schema
            copied[index].Schema = &schema
        }
    }

    return copied
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
    /* the path parameters are copied per operation, so a post-processor writing into one method's parameter does not rewrite its sibling's */
    operation := &Operation{
        OperationId: operationId,
        Parameters:  copyPathParameters(pathParameters),
        Responses:   make(map[string]ResponseObject),
    }

    if true == hasDescriptor {
        operation.Summary = descriptor.Summary
        operation.Description = descriptor.Description

        /* each operation gets its own tag slice, so a post-processor writing a tag into one method does not rewrite its sibling's */
        if 0 < len(descriptor.Tags) {
            operation.Tags = append([]string(nil), descriptor.Tags...)
        }

        if nil != descriptor.RequestType && true == methodAcceptsRequestBody(method) {
            operation.RequestBody = &RequestBody{
                Required: true,
                Content: map[string]MediaType{
                    "application/json": {Schema: schemaFromType(descriptor.RequestType, components, names)},
                },
            }
        }

        /* the statuses are visited in sorted order: the first touch names a colliding component, and map order would make two runs disagree on every $ref */
        statuses := make([]int, 0, len(descriptor.Responses))
        for status := range descriptor.Responses {
            statuses = append(statuses, status)
        }
        sort.Ints(statuses)

        for _, status := range statuses {
            /* the response description is required by the format, and a code outside the table answers an empty status text */
            description := nethttp.StatusText(status)
            if "" == description {
                description = "response"
            }

            operation.Responses[strconv.Itoa(status)] = ResponseObject{
                Description: description,
                Content: map[string]MediaType{
                    "application/json": {Schema: schemaFromType(descriptor.Responses[status], components, names)},
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

/* the router ends a route wherever only optional segments remain and "in: path" forbids "required: false", so the pattern is described once per prefix from which only optional segments remain, then whole */
func expandOptionalTailSegment(pattern string) []patternExpansion {
    segments := strings.Split(pattern, "/")

    runStart := len(segments)
    parameterNames := make([]string, len(segments))
    for 0 < runStart {
        parameterName, optional := optionalTailParameterName(segments[runStart-1])
        if false == optional {
            break
        }

        runStart = runStart - 1
        parameterNames[runStart] = parameterName
    }

    expansions := make([]patternExpansion, 0, len(segments)-runStart+1)
    for prefixLength := runStart; prefixLength < len(segments); prefixLength = prefixLength + 1 {
        shortened := strings.Join(segments[:prefixLength], "/")
        if "" == shortened {
            shortened = "/"
        }

        expansions = append(expansions, patternExpansion{
            pattern:          shortened,
            omitsParameter:   true,
            omittedParameter: strings.Join(parameterNames[prefixLength:], "."),
        })
    }

    return append(expansions, patternExpansion{pattern: pattern})
}

/* only the ":name?" spelling is expanded: the router matches a brace segment literally, so expanding one would describe a path no route answers */
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
        catchAll := false

        if true == strings.HasPrefix(segment, ":") {
            placeholder = true
            name = strings.TrimSuffix(segment[1:], "?")
        } else if true == strings.HasPrefix(segment, "{") && true == strings.HasSuffix(segment, "}") {
            placeholder = true
            name = strings.TrimSuffix(segment[1:len(segment)-1], "?")
        } else if true == strings.HasPrefix(segment, "*") {
            placeholder = true
            name = segment[1:]

            /* the router discards every segment after a catch-all, so the converted path stops there too */
            if true == strings.HasSuffix(name, "...") {
                name = strings.TrimSuffix(name, "...")
                catchAll = true
            }
            if index == len(segments)-1 {
                catchAll = true
            }
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

        if true == catchAll {
            segments = segments[:index+1]

            break
        }
    }

    return strings.Join(segments, "/"), parameters
}

func pathItemCarriesMethod(method string) bool {
    switch strings.ToUpper(method) {
    case nethttp.MethodGet, nethttp.MethodPost, nethttp.MethodPut, nethttp.MethodPatch,
        nethttp.MethodDelete, nethttp.MethodOptions, nethttp.MethodHead, nethttp.MethodTrace:
        return true
    }

    return false
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
