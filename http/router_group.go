package http

import (
	"github.com/precision-soft/melody/exception"
	httpcontract "github.com/precision-soft/melody/http/contract"
)

func NewRouteGroup(router *Router, pathPrefix string) *RouteGroup {
	return &RouteGroup{
		router:       router,
		pathPrefix:   pathPrefix,
		namePrefix:   "",
		defaults:     map[string]string{},
		requirements: map[string]string{},
	}
}

type RouteGroup struct {
	router       *Router
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

func (instance *RouteGroup) HandleWithOptions(pattern string, handler httpcontract.Handler, options *RouteOptions) error {
	if nil == instance.router {
		return exception.NewError("router is nil", map[string]any{"pattern": pattern}, nil)
	}

	if nil == options {
		return exception.NewError("the route options is nil", map[string]any{"pattern": pattern}, nil)
	}

	groupedPattern := JoinPaths(instance.pathPrefix, pattern)

	if "" != instance.namePrefix && "" != options.Name() {
		options.SetName(instance.namePrefix + options.Name())
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

	options.SetRequirements(requirements)

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

	options.SetDefaults(defaults)

	instance.router.HandleWithOptions(groupedPattern, handler, options)

	return nil
}

func (instance *Router) Group(pathPrefix string) *RouteGroup {
	return NewRouteGroup(instance, pathPrefix)
}
