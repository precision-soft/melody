package http

import (
	"regexp"

	httpcontract "github.com/precision-soft/melody/http/contract"
)

const (
	RouteAttributeName    = "_route"
	RouteAttributePattern = "_pattern"
	RouteAttributeMethods = "_methods"
	RouteAttributeHost    = "_host"
	RouteAttributeSchemes = "_schemes"
	RouteAttributeLocales = "_locales"
	RouteAttributeLocale  = "_locale"
)

func NewRouteOptions(
	name string,
	methods []string,
	host string,
	schemes []string,
	requirements map[string]string,
	defaults map[string]string,
	locales []string,
	priority int,
	attributes map[string]any,
) *RouteOptions {
	copiedMethods := []string{}
	if nil != methods {
		copiedMethods = append([]string{}, methods...)
	}

	copiedSchemes := []string{}
	if nil != schemes {
		copiedSchemes = append([]string{}, schemes...)
	}

	copiedLocales := []string{}
	if nil != locales {
		copiedLocales = append([]string{}, locales...)
	}

	copiedRequirements := map[string]string{}
	if nil != requirements {
		copiedRequirements = make(map[string]string, len(requirements))
		for key, value := range requirements {
			copiedRequirements[key] = value
		}
	}

	copiedDefaults := map[string]string{}
	if nil != defaults {
		copiedDefaults = make(map[string]string, len(defaults))
		for key, value := range defaults {
			copiedDefaults[key] = value
		}
	}

	copiedAttributes := map[string]any{}
	if nil != attributes {
		copiedAttributes = make(map[string]any, len(attributes))
		for key, value := range attributes {
			copiedAttributes[key] = value
		}
	}

	return &RouteOptions{
		name:         name,
		methods:      copiedMethods,
		host:         host,
		schemes:      copiedSchemes,
		requirements: copiedRequirements,
		defaults:     copiedDefaults,
		locales:      copiedLocales,
		priority:     priority,
		attributes:   copiedAttributes,
	}
}

type RouteOptions struct {
	name         string
	methods      []string
	host         string
	schemes      []string
	requirements map[string]string
	defaults     map[string]string
	locales      []string
	priority     int
	attributes   map[string]any
}

func (instance *RouteOptions) Name() string { return instance.name }

func (instance *RouteOptions) SetName(name string) { instance.name = name }

func (instance *RouteOptions) Methods() []string {
	if nil == instance.methods {
		return nil
	}

	return append([]string{}, instance.methods...)
}

func (instance *RouteOptions) Host() string { return instance.host }

func (instance *RouteOptions) Schemes() []string {
	if nil == instance.schemes {
		return nil
	}

	return append([]string{}, instance.schemes...)
}

func (instance *RouteOptions) Requirements() map[string]string {
	if nil == instance.requirements {
		return nil
	}

	copied := make(map[string]string, len(instance.requirements))
	for key, value := range instance.requirements {
		copied[key] = value
	}

	return copied
}

func (instance *RouteOptions) SetRequirements(requirements map[string]string) {
	if nil == requirements {
		instance.requirements = nil
		return
	}

	copied := make(map[string]string, len(requirements))
	for key, value := range requirements {
		copied[key] = value
	}

	instance.requirements = copied
}

func (instance *RouteOptions) Defaults() map[string]string {
	if nil == instance.defaults {
		return nil
	}

	copied := make(map[string]string, len(instance.defaults))
	for key, value := range instance.defaults {
		copied[key] = value
	}

	return copied
}

func (instance *RouteOptions) SetDefaults(defaults map[string]string) {
	if nil == defaults {
		instance.defaults = nil
		return
	}

	copied := make(map[string]string, len(defaults))
	for key, value := range defaults {
		copied[key] = value
	}

	instance.defaults = copied
}

func (instance *RouteOptions) Locales() []string {
	if nil == instance.locales {
		return nil
	}

	return append([]string{}, instance.locales...)
}

func (instance *RouteOptions) Priority() int { return instance.priority }

func (instance *RouteOptions) Attributes() map[string]any {
	if nil == instance.attributes {
		return nil
	}

	copied := make(map[string]any, len(instance.attributes))
	for key, value := range instance.attributes {
		copied[key] = value
	}

	return copied
}

type route struct {
	name         string
	pattern      string
	parts        []string
	handler      httpcontract.Handler
	methods      []string
	host         string
	schemes      []string
	requirements map[string]*regexp.Regexp
	defaults     map[string]string
	locales      []string
	priority     int
	attributes   map[string]any
}

type routeTreeNode struct {
	segment               string
	staticChildren        map[string]*routeTreeNode
	paramChild            *routeTreeNode
	wildcardSegmentChild  *routeTreeNode
	wildcardCatchAllChild *routeTreeNode
	routeIndices          []int
}
