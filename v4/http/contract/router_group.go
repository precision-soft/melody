package contract

type RouteGroup interface {
    RouteHandler

    WithNamePrefix(namePrefix string)

    WithRequirements(requirements map[string]string)

    WithDefaults(defaults map[string]string)
}
