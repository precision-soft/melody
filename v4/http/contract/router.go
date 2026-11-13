package contract

/* Router resolves a request to the handler registered for it. The registration doors it inherits from RouteHandler are boot-only, since a concurrent write to the route tree is a fatal error; the framework router refuses a later registration by name. The reading doors stay open for the life of the process. */
type Router interface {
    RouteHandler

    RouteDefinitions() []RouteDefinition

    RouteDefinition(routeName string) (RouteDefinition, bool)

    Match(method string, path string, host string, scheme string) (*MatchResult, bool)

    Group(groupConfig GroupConfig) RouteGroup
}

type MatchResult struct {
    Handler         Handler
    Parameters      map[string]string
    RouteAttributes map[string]any
}

/* RouteHandler is the registration surface of a router and of every group carved out of one. Among routes that all match a request, priority decides first and registration order second, the first declared winning; specificity is not a factor, so "/users/new" registered after "/users/:id" is answered by "/users/:id". Every door is boot-only; see Router. */
type RouteHandler interface {
    /* Handle registers handler for one method and pattern, every other option of the route left at its zero value. */
    Handle(method string, pattern string, handler Handler)

    /* HandleWithOptions registers handler for pattern with what the options declare: a name, the methods, every method when they are empty, and the rest of RouteOptions. */
    HandleWithOptions(pattern string, handler Handler, options RouteOptions)
}
