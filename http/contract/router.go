package contract

type Router interface {
	RouteDefinitions() []RouteDefinition

	RouteDefinition(routeName string) (RouteDefinition, bool)

	Handle(method string, pattern string, handler Handler)

	HandleNamed(name string, method string, pattern string, handler Handler)

	Match(method string, path string, host string, scheme string) (*MatchResult, bool)
}

type MatchResult struct {
	Handler         Handler
	Params          map[string]string
	RouteAttributes map[string]any
}
