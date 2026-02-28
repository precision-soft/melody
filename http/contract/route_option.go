package contract

type RouteOptions interface {
    Name() string

    SetName(name string)

    Methods() []string

    Host() string

    Schemes() []string

    Requirements() map[string]string

    SetRequirements(requirements map[string]string)

    Defaults() map[string]string

    SetDefaults(defaults map[string]string)

    Locales() []string

    Priority() int

    Attributes() map[string]any
}
