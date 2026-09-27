package contract

type RouteRegistry interface {
    RouteDefinitions() []RouteDefinition

    RouteDefinition(routeName string) (RouteDefinition, bool)

    RouteDefinitionForUrlGeneration(routeName string) (UrlGenerationRouteDefinition, bool)
}
