package http

import (
    "sort"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
)

func NewRouteRegistry() *RouteRegistry {
    return &RouteRegistry{
        routes:                  make([]route, 0),
        routeByName:             make(map[string]route),
        routeByDispatchIdentity: make(map[string]struct{}),
    }
}

/* the kinds a route collision is recorded under: one for two routes indistinguishable at dispatch, one for two distinct routes claiming the same name */
const (
    BootCollisionKindHttpRoute     = "httpRoute"
    BootCollisionKindHttpRouteName = "httpRouteName"
)

type RouteRegistry struct {
    routes      []route
    routeByName map[string]route
    /* keyed by everything the matcher discriminates on: two routes behind one key are indistinguishable at dispatch, so the later one could never be selected — the tie falls to the first registered — and would shadow silently. Routes that differ in any discriminator (host, methods, schemes, locales, requirements, priority) are legitimately distinct and stay accepted. */
    routeByDispatchIdentity map[string]struct{}
    /* armed only for the boot window, by the application that owns the aggregated collision report; a registry serving outside that window keeps the immediate refusal */
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

func (instance *RouteRegistry) registerRoute(routeValue route) {
    /* an exact dispatch duplicate is refused before anything is stored: registration was the single channel with no collision handling — services, parameters and cli commands all report duplicates — and the second registration is unreachable by construction, which is precisely the silent kind of shadowing an operator cannot see */
    dispatchIdentity := routeDispatchIdentity(routeValue)
    if _, exists := instance.routeByDispatchIdentity[dispatchIdentity]; true == exists {
        if nil != instance.bootCollisionRecorder {
            instance.bootCollisionRecorder(BootCollisionKindHttpRoute, routeCollisionName(routeValue))

            return
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
        return
    }

    if _, exists := instance.routeByName[routeValue.name]; true == exists {
        /* the route itself stays registered — it is distinct at dispatch, only its name collides — while the name keeps pointing at the first claimant */
        if nil != instance.bootCollisionRecorder {
            instance.bootCollisionRecorder(BootCollisionKindHttpRouteName, routeValue.name)

            return
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
}

/* routeCollisionName renders the route for the aggregated report: the dispatch identity itself is a NUL-joined machine key, so the report carries the human spelling — the methods and the pattern — that names the route in the application's own code. */
func routeCollisionName(routeValue route) string {
    methods := append([]string{}, routeValue.methods...)
    sort.Strings(methods)

    if 0 == len(methods) {
        return routeValue.pattern
    }

    return strings.Join(methods, ",") + " " + routeValue.pattern
}

/* routeDispatchIdentity renders the parts of a route the matcher can distinguish. The name and the defaults stay out on purpose: neither participates in matching, so two routes differing only there are still the same route at dispatch. */
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
