package http

import (
    "regexp"
    "sort"
    "strings"
    "sync/atomic"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
)

func NewRouter() *Router {
    return NewRouterWithRouteRegistry(NewRouteRegistry())
}

func NewRouterWithRouteRegistry(routeRegistry *RouteRegistry) *Router {
    return &Router{
        routeRegistry: routeRegistry,
        routeTreeRoot: nil,
    }
}

type Router struct {
    routeRegistry *RouteRegistry
    routeTreeRoot *routeTreeNode
    /* raised by the kernel when it builds its handler; the registration doors read it and refuse. */
    serving atomic.Bool
}

func (instance *Router) RouteRegistry() httpcontract.RouteRegistry {
    return instance.routeRegistry
}

func (instance *Router) Handle(method string, pattern string, handler httpcontract.Handler) {
    instance.HandleWithOptions(
        pattern,
        handler,
        &RouteOptions{
            methods: []string{method},
        },
    )
}

func (instance *Router) HandleNamed(name string, method string, pattern string, handler httpcontract.Handler) {
    instance.HandleWithOptions(
        pattern,
        handler,
        &RouteOptions{
            name:    name,
            methods: []string{method},
        },
    )
}

func (instance *Router) HandleController(
    method string,
    pattern string,
    controller any,
) {
    handler := wrapControllerWithContainer(controller)

    instance.HandleWithOptions(
        pattern,
        handler,
        &RouteOptions{
            methods: []string{method},
        },
    )
}

func (instance *Router) HandleNamedController(
    name string,
    method string,
    pattern string,
    controller any,
) {
    handler := wrapControllerWithContainer(controller)

    instance.HandleWithOptions(
        pattern,
        handler,
        &RouteOptions{
            name:    name,
            methods: []string{method},
        },
    )
}

func (instance *Router) HandleWithOptions(pattern string, handler httpcontract.Handler, options httpcontract.RouteOptions) {
    instance.addRoute(pattern, handler, options)
}

func (instance *Router) addRoute(pattern string, handler httpcontract.Handler, options httpcontract.RouteOptions) {
    instance.refuseRegistrationWhileServing(pattern)

    if nil == handler {
        exception.Panic(
            exception.NewError(
                "handler may not be nil",
                map[string]any{
                    "pattern": pattern,
                },
                nil,
            ),
        )
    }

    if nil == options {
        options = &RouteOptions{}
    }

    /* an empty pattern, which JoinPaths produces under a root group, splits as "/" does and registers the root route */
    parts := splitPath(pattern)
    normalizedPattern := strings.Join(parts, "/")

    requirements := make(map[string]*regexp.Regexp)
    requirementSources := map[string]string{}
    for key, value := range options.Requirements() {
        if "" == key {
            continue
        }
        if "" == value {
            continue
        }

        /* the requirement must match the whole value, so it is wrapped in a non-capturing group before it is anchored: alternation binds looser than the anchors */
        patternValue := "^(?:" + value + ")$"

        requiredRegex, compileErr := regexp.Compile(patternValue)
        if nil != compileErr {
            exception.Panic(
                exception.NewError(
                    "route requirement regex is invalid",
                    map[string]any{
                        "pattern":       normalizedPattern,
                        "parameterName": key,
                        "regex":         value,
                    },
                    compileErr,
                ),
            )
        }

        requirements[key] = requiredRegex
        requirementSources[key] = value
    }

    defaults := map[string]string{}
    for key, value := range options.Defaults() {
        if "" == key {
            continue
        }

        defaults[key] = value
    }

    rejectNonTrailingOptionalParameter(parts, normalizedPattern, defaults)
    rejectDuplicateParameterName(parts, normalizedPattern)
    rejectForeignParameterSyntax(parts, normalizedPattern)
    rejectIncoherentLocaleDeclaration(parts, normalizedPattern, options.Locales(), defaults)
    rejectMalformedExposureAttributes(options.Attributes(), options.Name(), normalizedPattern)

    attributes := map[string]any{}
    for key, value := range options.Attributes() {
        if "" == key {
            continue
        }

        attributes[key] = value
    }

    if "" != options.Name() {
        attributes[RouteAttributeName] = options.Name()
    }
    attributes[RouteAttributePattern] = normalizedPattern
    if 0 < len(options.Methods()) {
        attributes[RouteAttributeMethods] = append([]string{}, options.Methods()...)
    }
    if "" != options.Host() {
        attributes[RouteAttributeHost] = options.Host()
    }
    if 0 < len(options.Schemes()) {
        attributes[RouteAttributeSchemes] = append([]string{}, options.Schemes()...)
    }
    if 0 < len(options.Locales()) {
        attributes[RouteAttributeLocales] = append([]string{}, options.Locales()...)
    }

    routeStored := instance.routeRegistry.registerRoute(
        route{
            name:               options.Name(),
            pattern:            normalizedPattern,
            parts:              parts,
            handler:            handler,
            methods:            append([]string{}, options.Methods()...),
            host:               options.Host(),
            schemes:            append([]string{}, options.Schemes()...),
            requirements:       requirements,
            requirementSources: requirementSources,
            defaults:           defaults,
            locales:            append([]string{}, options.Locales()...),
            priority:           options.Priority(),
            attributes:         attributes,
        },
    )

    /* a route the registry declined is not put in the matching tree, whose entries must name their own stored route */
    if false == routeStored {
        return
    }

    routeIndex := len(instance.routeRegistry.routesInternal()) - 1

    if nil == instance.routeTreeRoot {
        instance.routeTreeRoot = &routeTreeNode{segment: ""}
    }

    patternSegments := parts
    if 1 <= len(patternSegments) {
        patternSegments = patternSegments[1:]
    }

    instance.registerRouteInTree(instance.routeTreeRoot, patternSegments, routeIndex)
}

/* rejectDuplicateParameterName refuses a pattern that names one parameter twice, since the handler could read only one of the two values, and a parameter with no name, which would bind nothing. */
func rejectDuplicateParameterName(parts []string, normalizedPattern string) {
    seenParameterNames := map[string]struct{}{}

    for _, part := range parts {
        parameterName := ""

        if true == strings.HasPrefix(part, ":") {
            parameterName = strings.TrimSuffix(strings.TrimPrefix(part, ":"), "?")
        } else if true == strings.HasPrefix(part, "*") {
            parameterName = strings.TrimSuffix(strings.TrimPrefix(part, "*"), "...")
        } else {
            continue
        }

        if "" == parameterName {
            if true == strings.HasPrefix(part, "*") {
                /* an unnamed catch-all is the deliberate spelling of "swallow the rest and bind nothing" */
                continue
            }

            exception.Panic(
                exception.NewError(
                    "route parameter must be named",
                    map[string]any{
                        "pattern": normalizedPattern,
                        "segment": part,
                    },
                    nil,
                ),
            )
        }

        if _, exists := seenParameterNames[parameterName]; true == exists {
            exception.Panic(
                exception.NewError(
                    "route parameter name is declared twice in one pattern",
                    map[string]any{
                        "pattern":       normalizedPattern,
                        "parameterName": parameterName,
                    },
                    nil,
                ),
            )
        }

        seenParameterNames[parameterName] = struct{}{}
    }
}

/* rejectForeignParameterSyntax refuses a segment in a parameter syntax this router does not speak, such as "/users/{id}": the router binds ":name" and "*name...", and such a segment would be a literal that no real request matches. */
func rejectForeignParameterSyntax(parts []string, normalizedPattern string) {
    for _, part := range parts {
        if false == strings.HasPrefix(part, "{") {
            continue
        }

        if false == strings.HasSuffix(part, "}") {
            continue
        }

        exception.Panic(
            exception.NewError(
                "route parameter must be written as :name, not {name}",
                map[string]any{
                    "pattern": normalizedPattern,
                    "segment": part,
                },
                nil,
            ),
        )
    }
}

/* rejectIncoherentLocaleDeclaration refuses a route that declares Locales while neither its pattern nor its defaults carry "_locale", which could never match, and a pattern carrying ":_locale" with no Locales list, which would publish a client-chosen locale unvalidated. */
func rejectIncoherentLocaleDeclaration(
    parts []string,
    normalizedPattern string,
    locales []string,
    defaults map[string]string,
) {
    patternCarriesLocale := false

    for _, part := range parts {
        parameterName := ""

        if true == strings.HasPrefix(part, ":") {
            parameterName = strings.TrimSuffix(strings.TrimPrefix(part, ":"), "?")
        } else if true == strings.HasPrefix(part, "*") {
            parameterName = strings.TrimSuffix(strings.TrimPrefix(part, "*"), "...")
        } else {
            continue
        }

        if RouteAttributeLocale == parameterName {
            patternCarriesLocale = true

            break
        }
    }

    _, defaultCarriesLocale := defaults[RouteAttributeLocale]

    if 0 < len(locales) && false == patternCarriesLocale && false == defaultCarriesLocale {
        exception.Panic(
            exception.NewError(
                "route declares locales but neither its pattern nor its defaults supply "+RouteAttributeLocale,
                map[string]any{
                    "pattern": normalizedPattern,
                    "locales": append([]string{}, locales...),
                },
                nil,
            ),
        )
    }

    if 0 == len(locales) && true == patternCarriesLocale {
        exception.Panic(
            exception.NewError(
                "route pattern carries "+RouteAttributeLocale+" but declares no locales to validate it against",
                map[string]any{
                    "pattern": normalizedPattern,
                },
                nil,
            ),
        )
    }
}
/* rejectMalformedExposureAttributes refuses an exposure attribute that is not a bool, a zone that is not a string and an exposed route with no name, each of which would drop the route from the manifest it opted into. */
func rejectMalformedExposureAttributes(attributes map[string]any, routeName string, normalizedPattern string) {
    exposeValue, hasExpose := attributes[RouteAttributeExpose]
    if false == hasExpose {
        return
    }

    exposed, isBool := exposeValue.(bool)
    if false == isBool {
        exception.Panic(
            exception.NewError(
                "route expose attribute must be a bool",
                map[string]any{
                    "pattern": normalizedPattern,
                },
                nil,
            ),
        )
    }

    if false == exposed {
        return
    }

    if "" == routeName {
        exception.Panic(
            exception.NewError(
                "an exposed route must be named, since the manifest references it by name",
                map[string]any{
                    "pattern": normalizedPattern,
                },
                nil,
            ),
        )
    }

    zoneValue, hasZone := attributes[RouteAttributeZone]
    if false == hasZone {
        return
    }

    zone, isString := zoneValue.(string)
    if false == isString {
        exception.Panic(
            exception.NewError(
                "route zone attribute must be a string",
                map[string]any{
                    "pattern": normalizedPattern,
                },
                nil,
            ),
        )
    }

    if "" != zone && false == IsRouteZone(zone) {
        exception.Panic(
            exception.NewError(
                "route zone is not one of the declared zones",
                map[string]any{
                    "pattern":       normalizedPattern,
                    "zone":          zone,
                    "declaredZones": RouteZones(),
                },
                nil,
            ),
        )
    }
}

/* only a trailing optional parameter is allowed: an omitted optional elsewhere would let the url generator mint a path this router answers with 404 */
func rejectNonTrailingOptionalParameter(parts []string, normalizedPattern string, defaults map[string]string) {
    for index, part := range parts {
        if false == strings.HasPrefix(part, ":") {
            continue
        }

        if false == strings.HasSuffix(part, "?") {
            continue
        }

        if index == len(parts)-1 {
            continue
        }

        parameterName := strings.TrimSuffix(strings.TrimPrefix(part, ":"), "?")

        /* a non-empty default keeps the segment in every path the generator mints; an empty default leaves nothing to emit */
        if "" != defaults[parameterName] {
            continue
        }

        exception.Panic(
            exception.NewError(
                "optional route parameter must be the last pattern segment unless it has a default",
                map[string]any{
                    "pattern":       normalizedPattern,
                    "parameterName": parameterName,
                },
                nil,
            ),
        )
    }
}

func (instance *Router) registerRouteInTree(root *routeTreeNode, patternSegments []string, routeIndex int) {
    currentNode := root

    for segmentIndex, segment := range patternSegments {
        isLast := segmentIndex == len(patternSegments)-1

        if true == strings.HasPrefix(segment, ":") {
            paramName := strings.TrimPrefix(segment, ":")
            isOptional := false
            if true == strings.HasSuffix(paramName, "?") {
                isOptional = true
                paramName = strings.TrimSuffix(paramName, "?")
            }

            if true == isOptional {
                if true == instance.routeMayEndHere(patternSegments[segmentIndex:]) {
                    currentNode.routeIndices = append(currentNode.routeIndices, routeIndex)
                }
            }

            if nil == currentNode.paramChild {
                currentNode.paramChild = &routeTreeNode{segment: ":" + paramName}
            }
            currentNode = currentNode.paramChild

            if true == isLast {
                currentNode.routeIndices = append(currentNode.routeIndices, routeIndex)
            }

            continue
        }

        if true == strings.HasPrefix(segment, "*") {
            wildcardName := strings.TrimPrefix(segment, "*")
            isCatchAll := false
            if true == strings.HasSuffix(wildcardName, "...") {
                isCatchAll = true
                wildcardName = strings.TrimSuffix(wildcardName, "...")
            }
            if true == isLast {
                isCatchAll = true
            }

            if true == isCatchAll {
                if nil == currentNode.wildcardCatchAllChild {
                    currentNode.wildcardCatchAllChild = &routeTreeNode{segment: "*" + wildcardName + "..."}
                }
                currentNode.wildcardCatchAllChild.routeIndices = append(currentNode.wildcardCatchAllChild.routeIndices, routeIndex)
                return
            }

            if nil == currentNode.wildcardSegmentChild {
                currentNode.wildcardSegmentChild = &routeTreeNode{segment: "*" + wildcardName}
            }
            currentNode = currentNode.wildcardSegmentChild

            if true == isLast {
                currentNode.routeIndices = append(currentNode.routeIndices, routeIndex)
            }

            continue
        }

        if nil == currentNode.staticChildren {
            currentNode.staticChildren = make(map[string]*routeTreeNode)
        }

        childNode, exists := currentNode.staticChildren[segment]
        if false == exists {
            childNode = &routeTreeNode{segment: segment}
            currentNode.staticChildren[segment] = childNode
        }

        currentNode = childNode

        if true == isLast {
            currentNode.routeIndices = append(currentNode.routeIndices, routeIndex)
        }
    }
}

func (instance *Router) routeMayEndHere(remainingSegments []string) bool {
    if 0 == len(remainingSegments) {
        return true
    }

    for index, segment := range remainingSegments {
        if true == strings.HasPrefix(segment, ":") {
            paramName := strings.TrimPrefix(segment, ":")
            if true == strings.HasSuffix(paramName, "?") {
                continue
            }

            return false
        }

        if true == strings.HasPrefix(segment, "*") {
            wildcardName := strings.TrimPrefix(segment, "*")
            if true == strings.HasSuffix(wildcardName, "...") {
                return true
            }

            if index == len(remainingSegments)-1 {
                return true
            }

            return false
        }

        return false
    }

    return true
}

func (instance *Router) match(method string, path string, host string, scheme string) (httpcontract.Handler, map[string]string, map[string]any) {
    if false == requestPathIsRoutable(path) {
        return nil, nil, map[string]any{}
    }

    pathParts := splitRequestPath(path)

    pathSegments := pathParts
    if 1 <= len(pathSegments) {
        pathSegments = pathSegments[1:]
    }

    candidates := instance.findRouteCandidates(pathSegments)
    if 0 == len(candidates) {
        return nil, nil, map[string]any{}
    }

    bestHandler := httpcontract.Handler(nil)
    var bestParams map[string]string
    var bestAttributes map[string]any

    allowedMethodsSet := make(map[string]struct{})
    bestPriority := 0
    bestIndex := -1
    hasBest := false

    for _, index := range candidates {
        if 0 > index {
            continue
        }
        if len(instance.routeRegistry.routesInternal()) <= index {
            continue
        }

        routeDefinition := instance.routeRegistry.routesInternal()[index]

        if false == matchesHost(routeDefinition.host, host) {
            continue
        }

        if false == matchesScheme(routeDefinition.schemes, scheme) {
            continue
        }

        params, matched := matchPath(routeDefinition, pathParts)
        if false == matched {
            continue
        }

        /* the defaults are merged before the locale gate reads them, since a default is how a route supplies a locale its url does not carry; the kernel reads the same merged map */
        for key, defaultValue := range routeDefinition.defaults {
            if _, exists := params[key]; false == exists {
                params[key] = defaultValue
            }
        }

        if false == matchesLocale(routeDefinition.locales, params) {
            continue
        }

        if false == matchesMethod(routeDefinition.methods, method) {
            for _, allowedMethod := range routeDefinition.methods {
                /* an empty method never matches and is not a valid Allow token */
                if "" == allowedMethod {
                    continue
                }

                allowedMethodsSet[allowedMethod] = struct{}{}
            }
            continue
        }

        /* priority first, then registration order; specificity is deliberately not a factor, as the RouteHandler contract states */
        if false == hasBest ||
            routeDefinition.priority > bestPriority ||
            (routeDefinition.priority == bestPriority && (0 > bestIndex || index < bestIndex)) {
            bestHandler = routeDefinition.handler
            bestParams = params
            bestAttributes = routeDefinition.attributes
            bestPriority = routeDefinition.priority
            bestIndex = index
            hasBest = true
        }
    }

    if false == hasBest {
        if 0 < len(allowedMethodsSet) {
            allowedMethods := make([]string, 0)
            for methodName := range allowedMethodsSet {
                allowedMethods = append(allowedMethods, methodName)
            }
            sort.Strings(allowedMethods)

            attributes := make(map[string]any)
            attributes[RouteAttributeMethods] = allowedMethods

            return nil, nil, attributes
        }

        return nil, nil, map[string]any{}
    }

    /* the attributes are deep-copied, since the registry's map lives for the process; a pointer or struct inside stays shared */
    return bestHandler, bestParams, internal.CopyAnyMap(bestAttributes)
}

func (instance *Router) findRouteCandidates(pathSegments []string) []int {
    result := make([]int, 0)

    if nil == instance.routeTreeRoot {
        return result
    }

    instance.routeTreeRoot.collectCandidates(pathSegments, 0, &result)

    return result
}

func (instance *routeTreeNode) collectCandidates(
    pathSegments []string,
    segmentIndex int,
    result *[]int,
) {
    if len(pathSegments) == segmentIndex {
        if 0 != len(instance.routeIndices) {
            *result = append(*result, instance.routeIndices...)
        }

        if nil != instance.wildcardCatchAllChild {
            if 0 != len(instance.wildcardCatchAllChild.routeIndices) {
                *result = append(*result, instance.wildcardCatchAllChild.routeIndices...)
            }
        }

        return
    }

    segment := pathSegments[segmentIndex]

    if nil != instance.staticChildren {
        child, exists := instance.staticChildren[segment]
        if true == exists {
            child.collectCandidates(pathSegments, segmentIndex+1, result)
        }
    }

    if nil != instance.paramChild {
        instance.paramChild.collectCandidates(pathSegments, segmentIndex+1, result)
    }

    if nil != instance.wildcardSegmentChild {
        instance.wildcardSegmentChild.collectCandidates(pathSegments, segmentIndex+1, result)
    }

    if nil != instance.wildcardCatchAllChild {
        if 0 != len(instance.wildcardCatchAllChild.routeIndices) {
            *result = append(*result, instance.wildcardCatchAllChild.routeIndices...)
        }
    }
}

var _ httpcontract.Router = (*Router)(nil)

/* freezeRouterForServing closes a router's registration doors once the kernel built its handler, through an unexported method, so only a router of this package can be frozen. */
func freezeRouterForServing(router httpcontract.Router) {
    freezable, isFreezable := router.(interface{ freezeForServing() })
    if false == isFreezable {
        return
    }

    freezable.freezeForServing()
}

func (instance *Router) freezeForServing() {
    instance.serving.Store(true)
}

/* refuseRegistrationWhileServing refuses a route registered after the kernel started serving, since a concurrent write to the route tree's maps is a fatal error. Routes are declared at boot. */
func (instance *Router) refuseRegistrationWhileServing(pattern string) {
    if false == instance.serving.Load() {
        return
    }

    exception.Panic(
        exception.NewError(
            "may not register a route after the http kernel started serving",
            map[string]any{
                "pattern": pattern,
            },
            nil,
        ),
    )
}
