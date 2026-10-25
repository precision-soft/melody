package http

import (
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* GroupConfig is the contract's type under this package's name, the one Router.Group and NewRouteGroup take. */
type GroupConfig = httpcontract.GroupConfig

func NewRouteGroup(router httpcontract.Router, groupConfig httpcontract.GroupConfig) httpcontract.RouteGroup {
    return &RouteGroup{
        router:       router,
        pathPrefix:   groupConfig.PathPrefix,
        namePrefix:   groupConfig.NamePrefix,
        defaults:     copyGroupMap(groupConfig.Defaults),
        requirements: copyGroupMap(groupConfig.Requirements),
    }
}

type RouteGroup struct {
    router       httpcontract.Router
    pathPrefix   string
    namePrefix   string
    defaults     map[string]string
    requirements map[string]string
}

func copyGroupMap(values map[string]string) map[string]string {
    copied := make(map[string]string, len(values))
    for key, value := range values {
        copied[key] = value
    }

    return copied
}

func (instance *RouteGroup) Handle(method string, pattern string, handler httpcontract.Handler) {
    instance.HandleWithOptions(
        pattern,
        handler,
        &RouteOptions{
            methods: []string{method},
        },
    )
}

func (instance *RouteGroup) HandleNamed(name string, method string, pattern string, handler httpcontract.Handler) {
    instance.HandleWithOptions(
        pattern,
        handler,
        &RouteOptions{
            name:    name,
            methods: []string{method},
        },
    )
}

func (instance *RouteGroup) HandleController(
    method string,
    pattern string,
    controller any,
) {
    handler := wrapControllerWithContainer(controller)

    instance.HandleWithOptions(
        pattern,
        handler,
        &RouteOptions{
            methods: []string{method},
        },
    )
}

func (instance *RouteGroup) HandleNamedController(
    name string,
    method string,
    pattern string,
    controller any,
) {
    handler := wrapControllerWithContainer(controller)

    instance.HandleWithOptions(
        pattern,
        handler,
        &RouteOptions{
            name:    name,
            methods: []string{method},
        },
    )
}

func (instance *RouteGroup) HandleWithOptions(pattern string, handler httpcontract.Handler, options httpcontract.RouteOptions) {
    if nil == instance.router {
        exception.Panic(
            exception.NewError("router is nil", map[string]any{"pattern": pattern}, nil),
        )
    }

    if nil == options {
        exception.Panic(
            exception.NewError("the route options is nil", map[string]any{"pattern": pattern}, nil),
        )
    }

    groupedPattern := JoinPaths(instance.pathPrefix, pattern)

    groupedName := options.Name()
    if "" != instance.namePrefix && "" != groupedName {
        groupedName = instance.namePrefix + groupedName
    }

    requirements := options.Requirements()
    if nil == requirements {
        requirements = map[string]string{}
    }

    for key, value := range instance.requirements {
        if "" == key {
            continue
        }

        _, exists := requirements[key]
        if true == exists {
            continue
        }

        requirements[key] = value
    }

    defaults := options.Defaults()
    if nil == defaults {
        defaults = map[string]string{}
    }

    for key, value := range instance.defaults {
        if "" == key {
            continue
        }

        _, exists := defaults[key]
        if true == exists {
            continue
        }

        defaults[key] = value
    }

    /* the group registers options of its own, so an options value reused across two grouped registrations does not accumulate the prefix or the group values */
    groupedOptions := NewRouteOptions(
        groupedName,
        options.Methods(),
        options.Host(),
        options.Schemes(),
        requirements,
        defaults,
        options.Locales(),
        options.Priority(),
        options.Attributes(),
    )

    instance.router.HandleWithOptions(groupedPattern, handler, groupedOptions)
}

var _ httpcontract.RouteGroup = (*RouteGroup)(nil)

func (instance *Router) Group(groupConfig httpcontract.GroupConfig) httpcontract.RouteGroup {
    return NewRouteGroup(instance, groupConfig)
}
