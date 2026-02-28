package contract

type Router interface {
    RouteHandler

    RouteDefinitions() []RouteDefinition

    RouteDefinition(routeName string) (RouteDefinition, bool)

    Match(method string, path string, host string, scheme string) (*MatchResult, bool)

    Group(pathPrefix string) RouteGroup
}

type MatchResult struct {
    Handler         Handler
    Params          map[string]string
    RouteAttributes map[string]any
}

type RouteHandler interface {
    Handle(method string, pattern string, handler Handler)

    HandleNamed(name string, method string, pattern string, handler Handler)

    HandleController(method string, pattern string, controller any)

    HandleNamedController(name string, method string, pattern string, controller any)

    HandleWithOptions(pattern string, handler Handler, options RouteOptions)
}
