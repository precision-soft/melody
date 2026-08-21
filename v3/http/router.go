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

    /* an empty pattern — which JoinPaths produces legitimately for an empty pattern under a root group
       — splits exactly as "/" does, so it registers the root route rather than a phantom. It used to
       split to [""], and the tree registration drops the first segment, so the route was inserted under
       NO node at all: it stayed in the registry, resolvable by name and generable, while being
       unreachable by every request including "/", and it took the dispatch identity of "/" with it, so
       registering the real root route afterwards was refused as a duplicate of one that could never
       answer. splitNormalizedPath is where that is now decided, for patterns and request paths alike. */
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

        /* the requirement must match the WHOLE parameter value, so it is wrapped in a non-capturing group before it is anchored: alternation binds looser than the anchors, and concatenating them onto "en|de|fr" would compile "^en|de|fr$" — read by the engine as (^en)|(de)|(fr$) — which matches "aden" and "en'; DROP TABLE users--" alike, turning a whitelist into a prefix/suffix test. A caller's own anchors survive the wrapping unharmed. */
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

    instance.routeRegistry.registerRoute(
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

/* an omitted optional parameter is dropped wherever it sits in the pattern, while a match only ever ends early at the tail: a pattern like "/blog/:locale?/posts" therefore lets the url generator mint "/blog/posts", which this router answers with a 404. Only a trailing optional keeps the two sides in agreement, so anything else is refused at the definition site instead of shipping links nothing serves. */
/* rejectDuplicateParameterName refuses a pattern that names one parameter twice — /orgs/:id/members/:id.
   The extraction writes both segments under one map key, so the handler can only ever read one of the
   two values and cannot tell which; the route is ambiguous by construction and the openapi document
   emitted for it is spec-invalid on duplicate path parameters. A parameter with no name at all —
   a bare ":" or ":?" — is refused here too rather than treated as an anonymous wildcard: it binds
   nothing, so the segment it occupies is matched and then discarded in silence. */
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


/* rejectForeignParameterSyntax refuses a segment written in a parameter syntax this router does not
   speak. The router binds ":name" and "*name...", and everything else is a literal segment — so
   "/users/{id}", the spelling every other Go router and every openapi document uses, registered a
   route that matches only the eight-character url "/users/%7Bid%7D", binds nothing, and is refused by
   no validator. The developer sees a route in the table, the url generator emits the braces back
   unescaped, and every real request 404s. The mistake is in the declaration, so it is refused where
   the declaration is. */
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

/* rejectIncoherentLocaleDeclaration refuses the two shapes in which a route's locale declaration and
   its pattern contradict each other, each of which fails silently and in opposite directions.

   A route that declares Locales but whose pattern carries no ":_locale" segment can never match
   anything: the gate reads the parameter, finds nothing, and refuses — so the route is dead for every
   url, with no error at registration and no record at request time. A default supplies the value too,
   which is why the defaults are consulted here rather than the pattern alone.

   A route whose pattern carries ":_locale" but declares no Locales list is the inverse: the gate
   returns early, the segment binds whatever the client sent, and the kernel publishes it verbatim as
   the request's locale — an unvalidated, client-chosen value reaching the translator and every
   consumer of the locale attribute. Declaring the list is what makes the segment a whitelist. */
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
/* rejectMalformedExposureAttributes refuses the three shapes that made a route silently absent from
   the manifest it was deliberately opted into: an exposure attribute that is not a bool and a zone
   that is not a string both fail the projection's type assertion and drop the route with no
   diagnostic, and an exposed route with no name cannot be referenced by the consumer at all. Each was
   a developer stating an intention the artifact then contradicted in silence. */
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

        /* a non-empty default keeps the segment in every path the generator mints — GeneratePath substitutes it both for an absent parameter and for one supplied empty — so the pattern stays matchable. An empty default cannot: it leaves nothing to emit, and an empty segment satisfies no parameter. */
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

        /* the defaults are merged BEFORE the locale gate reads them, because a default is how a route supplies the locale a url does not carry: declaring Locales{"en", "de"} beside Defaults{"_locale": "en"} used to make the route unreachable by every url, the gate rejecting it sixteen lines before the value meant to satisfy the gate was filled in. The kernel below reads the same map after this merge, so the router and the kernel now agree on whether a default counts as the request locale. */
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
                /* an empty method never matches, so advertising it would put a bare comma in the Allow header, which is not a valid method token */
                if "" == allowedMethod {
                    continue
                }

                allowedMethodsSet[allowedMethod] = struct{}{}
            }
            continue
        }

        /* priority first, then registration order — the lowest index wins a tie. Specificity is deliberately not a factor: a static segment does not outrank a parameter, so the first declaration of two equally-ranked matches is the one that answers. The rule is written on the RouteHandler contract, because it is the caller who orders the declarations. */
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

    /* the winning route's attributes are the registry's own map, alive for every request of the
       process: handed out uncopied, a sort or an append through the match result — or through
       request.Attributes(), where the kernel publishes these values — rewrote the route table with no
       lock. The copy is deep, so the methods slice and any nested value a route registered are the
       caller's to mutate; what the copy does not descend into (a pointer, a struct) is shared state
       by the same boundary the session copy documents. */
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

/* freezeRouterForServing closes a router's registration doors once the kernel has built its handler.
   It reaches the router through an unexported method, so it binds only to an implementation declared
   in this package: a router supplied from outside cannot be frozen and is held to the written
   contract alone, which is the most a foreign implementation can be held to. */
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

/* refuseRegistrationWhileServing refuses a route registered after the kernel started serving. The
   route tree is a tree of plain maps read by every request goroutine, so writing to it concurrently is
   an unrecoverable fatal error rather than a torn read — there is no degraded mode to fall back to,
   which is why this is a refusal at the door and not a lock. Routes are configuration: they are
   declared at boot, from the composition root or a module's registration hook. */
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
