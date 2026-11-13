package contract

type RouteDefinition interface {
    Name() string

    Pattern() string

    Methods() []string

    Host() string

    Schemes() []string

    Requirements() map[string]string

    Defaults() map[string]string

    Locales() []string

    Priority() int

    Attributes() map[string]any
}
