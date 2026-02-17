package http

import (
	"regexp"

	httpcontract "github.com/precision-soft/melody/v2/http/contract"
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
