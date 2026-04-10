package http

import (
    "regexp"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func NewUrlGenerationRouteDefinition(routeValue route) *UrlGenerationRouteDefinition {
    return &UrlGenerationRouteDefinition{
        pattern:      routeValue.pattern,
        defaults:     routeValue.defaults,
        requirements: routeValue.requirements,
    }
}

type UrlGenerationRouteDefinition struct {
    pattern      string
    defaults     map[string]string
    requirements map[string]*regexp.Regexp
}

func (instance *UrlGenerationRouteDefinition) Pattern() string {
    return instance.pattern
}

func (instance *UrlGenerationRouteDefinition) Defaults() map[string]string {
    copied := make(map[string]string, len(instance.defaults))
    for key, value := range instance.defaults {
        copied[key] = value
    }

    return copied
}

func (instance *UrlGenerationRouteDefinition) Requirements() map[string]*regexp.Regexp {
    copied := make(map[string]*regexp.Regexp, len(instance.requirements))
    for key, value := range instance.requirements {
        copied[key] = value
    }

    return copied
}

var _ httpcontract.UrlGenerationRouteDefinition = (*UrlGenerationRouteDefinition)(nil)
