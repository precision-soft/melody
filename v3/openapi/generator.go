package openapi

import (
    nethttp "net/http"
    "reflect"
    "sort"
    "strconv"
    "strings"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* pathItemMethods is every verb a path item can carry, in the order the document lists them. A route registered with no method list answers all of them — matchesMethod treats the empty list as a match for every verb — so the document spells that surface out instead of writing an operation-less path item that reads as an endpoint answering nothing. */
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

    /* mirrorOwnedSlots remembers which path+method slots were written by the shortened mirror of an optional tail, so a route registered at that path later still displaces the mirror — the mirror may be reached either before or after the route it stands in for */
    mirrorOwnedSlots := make(map[string]bool)

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
                /* a verb outside the eight the format models — the router registers any string — has no slot in a path item; the route stays in the document with the undescribed verb named, instead of the operation being built and dropped without a trace */
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

                /* a taken slot is kept by whoever holds it best: the mirror of an optional tail always yields — a route registered at the shortened path itself describes it better, and it may be reached either before or after this route — while a route yields only to another ROUTE, never to a mirror; and two routes whose patterns converge on one converted path — a placeholder against a brace literal — must not silently replace each other's operations, so the earlier registration wins exactly as it does in the router's match order */
                slotKey := path + " " + strings.ToUpper(method)
                if nil != operationFor(&pathItem, method) {
                    if true == expansion.omitsParameter || false == mirrorOwnedSlots[slotKey] {
                        continue
                    }
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
                mirrorOwnedSlots[slotKey] = expansion.omitsParameter
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

        /* the document must not alias registry memory: the descriptor arrives by value but its slice shares the registry's backing array, and a caller post-processing the returned document would write through into every later generation */
        if 0 < len(descriptor.Tags) {
            operation.Tags = append(make([]string, 0, len(descriptor.Tags)), descriptor.Tags...)
        }

        if nil != descriptor.RequestType && true == methodAcceptsRequestBody(method) {
            operation.RequestBody = &RequestBody{
                Required: true,
                Content: map[string]MediaType{
                    "application/json": {Schema: schemaFromType(descriptor.RequestType, components, names)},
                },
            }
        }

        /* the statuses are visited in order: this range is the one unordered driver of first-touch component naming, and iterating the map directly hands the bare name and its numbered siblings to whichever type a given run visits first, so two runs over one registry disagree on every $ref to a colliding name */
        statuses := make([]int, 0, len(descriptor.Responses))
        for status := range descriptor.Responses {
            statuses = append(statuses, status)
        }
        sort.Ints(statuses)

        for _, status := range statuses {
            /* a code outside the registered table answers an empty status text, and the response description is required by the format */
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

            /* the router reads the "..." suffix — and a trailing bare "*" — as a catch-all, and its registration RETURNS there: every segment written after a catch-all is discarded and never matched. The converted path mirrors that, or the document would advertise a template — "/assets/{rest}/thumbnail" — no request the route answers can ever spell. */
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

/* pathItemCarriesMethod reports whether the verb has a slot in a path item — the same eight assignOperation writes and operationFor reads. */
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
