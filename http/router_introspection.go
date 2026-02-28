package http

import (
    "encoding/json"
    "regexp"

    httpcontract "github.com/precision-soft/melody/http/contract"
)

func NewRouteDefinition(
    name string,
    pattern string,
    methods []string,
    host string,
    schemes []string,
    requirements map[string]string,
    defaults map[string]string,
    locales []string,
    priority int,
    attributes map[string]any,
) *RouteDefinition {
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

    return &RouteDefinition{
        name:         name,
        pattern:      pattern,
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

type RouteDefinition struct {
    name         string
    pattern      string
    methods      []string
    host         string
    schemes      []string
    requirements map[string]string
    defaults     map[string]string
    locales      []string
    priority     int
    attributes   map[string]any
}

func (instance *RouteDefinition) Name() string { return instance.name }

func (instance *RouteDefinition) Pattern() string { return instance.pattern }

func (instance *RouteDefinition) Methods() []string {
    if nil == instance.methods {
        return nil
    }

    return append([]string{}, instance.methods...)
}

func (instance *RouteDefinition) Host() string { return instance.host }

func (instance *RouteDefinition) Schemes() []string {
    if nil == instance.schemes {
        return nil
    }

    return append([]string{}, instance.schemes...)
}

func (instance *RouteDefinition) Requirements() map[string]string {
    if nil == instance.requirements {
        return nil
    }

    copied := make(map[string]string, len(instance.requirements))
    for key, value := range instance.requirements {
        copied[key] = value
    }

    return copied
}

func (instance *RouteDefinition) Defaults() map[string]string {
    if nil == instance.defaults {
        return nil
    }

    copied := make(map[string]string, len(instance.defaults))
    for key, value := range instance.defaults {
        copied[key] = value
    }

    return copied
}

func (instance *RouteDefinition) Locales() []string {
    if nil == instance.locales {
        return nil
    }

    return append([]string{}, instance.locales...)
}

func (instance *RouteDefinition) Priority() int { return instance.priority }

func (instance *RouteDefinition) Attributes() map[string]any {
    if nil == instance.attributes {
        return nil
    }

    copied := make(map[string]any, len(instance.attributes))
    for key, value := range instance.attributes {
        copied[key] = value
    }

    return copied
}

func (instance *RouteDefinition) MarshalJSON() ([]byte, error) {
    type jsonDefinition struct {
        Name         string            `json:"name"`
        Pattern      string            `json:"pattern"`
        Methods      []string          `json:"methods"`
        Host         string            `json:"host"`
        Schemes      []string          `json:"schemes"`
        Requirements map[string]string `json:"requirements"`
        Defaults     map[string]string `json:"defaults"`
        Locales      []string          `json:"locales"`
        Priority     int               `json:"priority"`
        Attributes   map[string]any    `json:"attributes"`
    }

    return json.Marshal(jsonDefinition{
        Name:         instance.Name(),
        Pattern:      instance.Pattern(),
        Methods:      instance.Methods(),
        Host:         instance.Host(),
        Schemes:      instance.Schemes(),
        Requirements: instance.Requirements(),
        Defaults:     instance.Defaults(),
        Locales:      instance.Locales(),
        Priority:     instance.Priority(),
        Attributes:   instance.Attributes(),
    })
}

var _ httpcontract.RouteDefinition = (*RouteDefinition)(nil)

func (instance *Router) RouteDefinitions() []httpcontract.RouteDefinition {
    return instance.routeRegistry.RouteDefinitions()
}

func (instance *Router) RouteDefinition(routeName string) (httpcontract.RouteDefinition, bool) {
    return instance.routeRegistry.RouteDefinition(routeName)
}

func mapRouteToDefinition(routeValue route) *RouteDefinition {
    requirements := map[string]string{}
    for key, regexValue := range routeValue.requirements {
        if "" == key {
            continue
        }

        if nil == regexValue {
            continue
        }

        requirements[key] = regexValue.String()
    }

    defaults := map[string]string{}
    for key, value := range routeValue.defaults {
        if "" == key {
            continue
        }

        defaults[key] = value
    }

    attributes := map[string]any{}
    for key, value := range routeValue.attributes {
        if "" == key {
            continue
        }

        attributes[key] = value
    }

    return NewRouteDefinition(
        routeValue.name,
        routeValue.pattern,
        routeValue.methods,
        routeValue.host,
        routeValue.schemes,
        requirements,
        defaults,
        routeValue.locales,
        routeValue.priority,
        attributes,
    )
}

var _ = regexp.Regexp{}
