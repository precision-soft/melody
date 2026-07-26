package http

import (
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
)

func NewRouteGroup(router httpcontract.Router, pathPrefix string) httpcontract.RouteGroup {
    return &RouteGroup{
        router:       router,
        pathPrefix:   pathPrefix,
        namePrefix:   "",
        defaults:     map[string]string{},
        requirements: map[string]string{},
    }
}

type RouteGroup struct {
    router       httpcontract.Router
    pathPrefix   string
    namePrefix   string
    defaults     map[string]string
    requirements map[string]string
}

func (instance *RouteGroup) WithNamePrefix(namePrefix string) {
    instance.namePrefix = namePrefix
}

func (instance *RouteGroup) WithRequirements(requirements map[string]string) {
    if nil == requirements {
        instance.requirements = map[string]string{}
        return
    }

    copied := make(map[string]string, len(requirements))
    for key, value := range requirements {
        copied[key] = value
    }

    instance.requirements = copied
}

func (instance *RouteGroup) WithDefaults(defaults map[string]string) {
    if nil == defaults {
        instance.defaults = map[string]string{}
        return
    }

    copied := make(map[string]string, len(defaults))
    for key, value := range defaults {
        copied[key] = value
    }

    instance.defaults = copied
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

    /* the group registers options of its own instead of writing the name prefix and the merged group values back into the caller's: one options value reused across two grouped registrations would otherwise accumulate the prefix, so the second route would register under a name nothing can generate, and the first group's requirements would leak into a later registration through a different group */
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

func (instance *Router) Group(pathPrefix string) httpcontract.RouteGroup {
    return NewRouteGroup(instance, pathPrefix)
}
