package http

import (
    "regexp"
    "sort"
    "strings"

    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
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

    parts := splitPath(pattern)
    normalizedPattern := strings.Join(parts, "/")
    if "" == normalizedPattern {
        normalizedPattern = "/"
    }

    requirements := make(map[string]*regexp.Regexp)
    for key, value := range options.Requirements() {
        if "" == key {
            continue
        }
        if "" == value {
            continue
        }

        patternValue := value
        if false == strings.HasPrefix(patternValue, "^") {
            patternValue = "^" + patternValue
        }
        if false == strings.HasSuffix(patternValue, "$") {
            patternValue = patternValue + "$"
        }

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
    }

    defaults := map[string]string{}
    for key, value := range options.Defaults() {
        if "" == key {
            continue
        }

        defaults[key] = value
    }

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

    instance.routeRegistry.registerRoute(
        route{
            name:         options.Name(),
            pattern:      normalizedPattern,
            parts:        parts,
            handler:      handler,
            methods:      append([]string{}, options.Methods()...),
            host:         options.Host(),
            schemes:      append([]string{}, options.Schemes()...),
            requirements: requirements,
            defaults:     defaults,
            locales:      append([]string{}, options.Locales()...),
            priority:     options.Priority(),
            attributes:   attributes,
        },
    )

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
    pathParts := splitPath(path)

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

        if 0 != len(routeDefinition.locales) {
            localeValue := ""
            if value, exists := params[RouteAttributeLocale]; true == exists {
                localeValue = value
            }

            if "" == localeValue {
                continue
            }

            allowed := false
            for _, allowedLocale := range routeDefinition.locales {
                if allowedLocale == localeValue {
                    allowed = true

                    break
                }
            }

            if false == allowed {
                continue
            }
        }

        if false == matchesMethod(routeDefinition.methods, method) {
            for _, allowedMethod := range routeDefinition.methods {
                allowedMethodsSet[allowedMethod] = struct{}{}
            }
            continue
        }

        for key, defaultValue := range routeDefinition.defaults {
            if _, exists := params[key]; false == exists {
                params[key] = defaultValue
            }
        }

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

    return bestHandler, bestParams, bestAttributes
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
