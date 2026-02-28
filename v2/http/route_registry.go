package http

import (
    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
)

func NewRouteRegistry() *RouteRegistry {
    return &RouteRegistry{
        routes:      make([]route, 0),
        routeByName: make(map[string]route),
    }
}

type RouteRegistry struct {
    routes      []route
    routeByName map[string]route
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
    instance.routes = append(instance.routes, routeValue)

    if "" == routeValue.name {
        return
    }

    if _, exists := instance.routeByName[routeValue.name]; true == exists {
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

func (instance *RouteRegistry) routeByNameInternal(routeName string) (route, bool) {
    routeValue, exists := instance.routeByName[routeName]
    return routeValue, exists
}

func (instance *RouteRegistry) routesInternal() []route {
    return instance.routes
}

var _ httpcontract.RouteRegistry = (*RouteRegistry)(nil)
