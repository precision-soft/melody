package contract

/* Router resolves a request to the handler registered for it.

The registration doors it inherits from RouteHandler are BOOT-ONLY: the route table is a tree of plain
maps that every request goroutine reads, so writing to it while requests are in flight is an
unrecoverable fatal error rather than a torn read. Routes are declared from the composition root or a
module's registration hook, before the kernel builds its handler; the framework router refuses a
later registration by name. The reading doors below stay open for the whole life of the process — the
route table is introspected from inside handlers, which is how the openapi document and the route
manifest are served. */
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

/* RouteHandler is the registration surface of a router and of every group carved out of one.

Among routes whose declarations all match a request, the one that answers is chosen by PRIORITY first
and, at equal priority, by REGISTRATION ORDER — the first one declared wins. Specificity is not a
factor: a static segment does not outrank a parameter, so "/users/new" registered after "/users/:id"
is answered by "/users/:id". Declare the specific route first, or lift it with a higher priority.

Every door here is boot-only; see Router. */
type RouteHandler interface {
    Handle(method string, pattern string, handler Handler)

    HandleNamed(name string, method string, pattern string, handler Handler)

    HandleController(method string, pattern string, controller any)

    HandleNamedController(name string, method string, pattern string, controller any)

    HandleWithOptions(pattern string, handler Handler, options RouteOptions)
}
