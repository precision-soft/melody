package http

import (
    "sort"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func NewRouteRegistry() *RouteRegistry {
    return &RouteRegistry{
        routes:                  make([]route, 0),
        routeByName:             make(map[string]route),
        routeByDispatchIdentity: make(map[string]struct{}),
    }
}


const (
    BootCollisionKindHttpRoute     = "httpRoute"
    BootCollisionKindHttpRouteName = "httpRouteName"
)

/* RouteRegistry retains process routing configuration. Register routes and install the boot recorder before concurrent request handling; mutations are not synchronized. */
type RouteRegistry struct {
    routes      []route
    routeByName map[string]route

    routeByDispatchIdentity map[string]struct{}

    bootCollisionRecorder func(kind string, name string)
}

/* SetBootCollisionRecorder arms the aggregated collision channel: with a recorder set, registerRoute records a duplicate — the first registration wins — instead of panicking over each one in isolation, so a boot collects every collision into the one report that names them all. A nil recorder disarms the channel and restores the immediate refusal. */
func (instance *RouteRegistry) SetBootCollisionRecorder(recorder func(kind string, name string)) {
    instance.bootCollisionRecorder = recorder
}

func (instance *RouteRegistry) RouteDefinitions() []httpcontract.RouteDefinition {
    definitions := make([]httpcontract.RouteDefinition, 0, len(instance.routes))

    for _, routeValue := range instance.routes {
        definition := mapRouteToDefinition(routeValue)
        definitions = append(definitions, definition)
    }

    return definitions
}

func (instance *RouteRegistry) RouteDefinition(routeName string) (httpcontract.RouteDefinition, bool) {
    routeValue, exists := instance.routeByNameInternal(routeName)
    if false == exists {
        return &RouteDefinition{}, false
    }

    return mapRouteToDefinition(routeValue), true
}

func (instance *RouteRegistry) RouteDefinitionForUrlGeneration(routeName string) (httpcontract.UrlGenerationRouteDefinition, bool) {
    routeValue, exists := instance.routeByNameInternal(routeName)
    if false == exists {
        return nil, false
    }

    return NewUrlGenerationRouteDefinition(routeValue), true
}

func (instance *RouteRegistry) registerRoute(routeValue route) bool {

    dispatchIdentity := routeDispatchIdentity(routeValue)
    if _, exists := instance.routeByDispatchIdentity[dispatchIdentity]; true == exists {
        if nil != instance.bootCollisionRecorder {
            instance.bootCollisionRecorder(BootCollisionKindHttpRoute, routeCollisionName(routeValue))

            return false
        }

        exception.Panic(
            exception.NewError(
                "route already registered with an identical pattern, methods, host, schemes, locales, requirements and priority; the later registration could never be dispatched",
                map[string]any{
                    "pattern":   routeValue.pattern,
                    "methods":   append([]string{}, routeValue.methods...),
                    "routeName": routeValue.name,
                },
                nil,
            ),
        )
    }

    instance.routes = append(instance.routes, routeValue)
    instance.routeByDispatchIdentity[dispatchIdentity] = struct{}{}

    if "" == routeValue.name {
        return true
    }

    if _, exists := instance.routeByName[routeValue.name]; true == exists {

        if nil != instance.bootCollisionRecorder {
            instance.bootCollisionRecorder(BootCollisionKindHttpRouteName, routeValue.name)

            return true
        }

        exception.Panic(
            exception.NewError(
                "route name already exists",
                map[string]any{
                    "routeName": routeValue.name,
                },
                nil,
            ),
        )
    }

    instance.routeByName[routeValue.name] = routeValue

    return true
}

func routeCollisionName(routeValue route) string {
    methods := append([]string{}, routeValue.methods...)
    sort.Strings(methods)

    if 0 == len(methods) {
        return routeValue.pattern
    }

    return strings.Join(methods, ",") + " " + routeValue.pattern
}

func routeDispatchIdentity(routeValue route) string {
    methods := append([]string{}, routeValue.methods...)
    sort.Strings(methods)

    schemes := append([]string{}, routeValue.schemes...)
    sort.Strings(schemes)

    locales := append([]string{}, routeValue.locales...)
    sort.Strings(locales)

    requirementKeys := make([]string, 0, len(routeValue.requirements))
    for requirementKey := range routeValue.requirements {
        requirementKeys = append(requirementKeys, requirementKey)
    }
    sort.Strings(requirementKeys)

    requirements := make([]string, 0, len(requirementKeys))
    for _, requirementKey := range requirementKeys {
        requirements = append(requirements, requirementKey+"="+routeValue.requirements[requirementKey].String())
    }

    identityParts := []string{
        routeValue.pattern,
        strings.Join(methods, ","),
        routeValue.host,
        strings.Join(schemes, ","),
        strings.Join(locales, ","),
        strings.Join(requirements, ","),
        strconv.Itoa(routeValue.priority),
    }

    return strings.Join(identityParts, "\x00")
}

func (instance *RouteRegistry) routeByNameInternal(routeName string) (route, bool) {
    routeValue, exists := instance.routeByName[routeName]
    return routeValue, exists
}

func (instance *RouteRegistry) routesInternal() []route {
    return instance.routes
}

var _ httpcontract.RouteRegistry = (*RouteRegistry)(nil)
