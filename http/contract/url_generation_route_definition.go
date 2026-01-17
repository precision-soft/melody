package contract

import (
	"regexp"
)

type UrlGenerationRouteDefinition interface {
	Pattern() string

	Defaults() map[string]string

	Requirements() map[string]*regexp.Regexp
}
