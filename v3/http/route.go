package http

import (
    "regexp"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

const (
    RouteAttributeName    = "_route"
    RouteAttributePattern = "_pattern"
    RouteAttributeMethods = "_methods"
    RouteAttributeHost    = "_host"
    RouteAttributeSchemes = "_schemes"
    RouteAttributeLocales = "_locales"
    RouteAttributeLocale  = "_locale"

    /* RouteAttributeExpose, when set to the bool true in a route's options attributes, opts the route into the frontend route manifest (melody:routes:manifest). RouteAttributeZone tags it with one of the RouteZone* values so the manifest can be scoped per zone. */
    RouteAttributeExpose = "expose"
    RouteAttributeZone   = "zone"

    RouteZonePublic   = "public"
    RouteZoneInternal = "internal"
    RouteZoneFrontend = "frontend"
    RouteZoneClient   = "client"
)

/* ExposedRouteAttributes builds the options-attributes map that opts a route into the frontend manifest under the given zone (pass an empty zone to expose without one). Merge it into a route's RouteOptions attributes. */
func ExposedRouteAttributes(zone string) map[string]any {
    attributes := map[string]any{RouteAttributeExpose: true}
    if "" != zone {
        attributes[RouteAttributeZone] = zone
    }

    return attributes
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
