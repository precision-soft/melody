package contract

/* Router registration is boot-only and is frozen when the kernel builds its handler. Read-only route inspection remains available while serving. */
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

/* RouteHandler registers routes at boot. Matches are chosen by descending priority, then registration order. Specificity has no precedence: declare /users/new before /users/:id or give it higher priority. */
type RouteHandler interface {
    Handle(method string, pattern string, handler Handler)

    HandleNamed(name string, method string, pattern string, handler Handler)

    HandleController(method string, pattern string, controller any)

    HandleNamedController(name string, method string, pattern string, controller any)

    HandleWithOptions(pattern string, handler Handler, options RouteOptions)
}
