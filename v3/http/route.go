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

    /* RouteAttributeExpose, when set to the bool true in a route's options attributes, opts the route into the frontend route manifest (melody:routes:manifest). RouteAttributeZone tags it with one of the RouteZone* values so the manifest can be scoped per zone. */
    RouteAttributeExpose = "expose"
    RouteAttributeZone   = "zone"

    RouteZonePublic   = "public"
    RouteZoneInternal = "internal"
    RouteZoneFrontend = "frontend"
    RouteZoneClient   = "client"
)

/* ExposedRouteAttributes builds the options-attributes map that opts a route into the frontend manifest under the given zone (pass an empty zone to expose without one). Merge it into a route's RouteOptions attributes.

   A zone that is not one of the RouteZone* values is refused by name. Accepted, it produced an artifact no filter can ever select — the manifest command compares the requested zone against the route's own string, so a misspelled zone on the route silently omits it from the zoned export while the unfiltered one still carries it, and a misspelled zone on the command writes an empty manifest over the good one and reports success. */
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

/* RouteZones answers the declared zones, in the order they are declared, so a command that takes a zone can name the accepted spellings in its refusal instead of restating them. */
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

    /* the pattern the caller declared, kept beside the compiled one: the compiled form carries the anchoring and the non-capturing wrapper the registration adds, so introspection — the route manifest, the openapi document, the debug listing — published "^(?:en|de)$" where the developer wrote "en|de", a spelling that is not theirs, that re-wraps on every round trip, and that carries RE2-only syntax into consumers whose engine is ECMA-262. */
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
