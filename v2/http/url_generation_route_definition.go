package http

import (
	"regexp"

	httpcontract "github.com/precision-soft/melody/v2/http/contract"
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
	return instance.defaults
}

func (instance *UrlGenerationRouteDefinition) Requirements() map[string]*regexp.Regexp {
	return instance.requirements
}

var _ httpcontract.UrlGenerationRouteDefinition = (*UrlGenerationRouteDefinition)(nil)
