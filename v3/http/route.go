package http

import (
    "regexp"

    "github.com/precision-soft/melody/v3/exception"
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

    /* RouteAttributeExpose, set to the bool true in a route's options attributes, opts the route into the frontend route manifest. RouteAttributeZone tags it with one of the RouteZone* values. */
    RouteAttributeExpose = "expose"
    RouteAttributeZone   = "zone"

    RouteZonePublic   = "public"
    RouteZoneInternal = "internal"
    RouteZoneFrontend = "frontend"
    RouteZoneClient   = "client"
)

/* ExposedRouteAttributes builds the options-attributes map that opts a route into the frontend manifest under the given zone; an empty zone exposes it without one. A zone that is not one of the RouteZone* values is refused by name. */
func ExposedRouteAttributes(zone string) map[string]any {
    attributes := map[string]any{RouteAttributeExpose: true}
    if "" != zone {
        if false == IsRouteZone(zone) {
            exception.Panic(
                exception.NewError(
                    "route zone is not one of the declared zones",
                    map[string]any{
                        "zone":          zone,
                        "declaredZones": RouteZones(),
                    },
                    nil,
                ),
            )
        }

        attributes[RouteAttributeZone] = zone
    }

    return attributes
}

/* RouteZones answers the declared zones in declaration order. */
func RouteZones() []string {
    return []string{
        RouteZonePublic,
        RouteZoneInternal,
        RouteZoneFrontend,
        RouteZoneClient,
    }
}

func IsRouteZone(zone string) bool {
    for _, declaredZone := range RouteZones() {
        if declaredZone == zone {
            return true
        }
    }

    return false
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

    /* the requirement as declared, kept beside the compiled, anchored form, so introspection publishes the developer's spelling */
    requirementSources map[string]string

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
