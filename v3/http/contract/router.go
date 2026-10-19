package contract

/* Router resolves a request to the handler registered for it. The registration doors it inherits from RouteHandler are boot-only, since a concurrent write to the route tree is a fatal error; the framework router refuses a later registration by name. The reading doors stay open for the life of the process. */
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

/* RouteHandler is the registration surface of a router and of every group carved out of one. Among routes that all match a request, priority decides first and registration order second, the first declared winning; specificity is not a factor, so "/users/new" registered after "/users/:id" is answered by "/users/:id". Every door is boot-only; see Router. */
type RouteHandler interface {
    Handle(method string, pattern string, handler Handler)

    HandleNamed(name string, method string, pattern string, handler Handler)

    HandleController(method string, pattern string, controller any)

    HandleNamedController(name string, method string, pattern string, controller any)

    HandleWithOptions(pattern string, handler Handler, options RouteOptions)
}
