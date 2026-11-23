package http

import (
    "reflect"
    "regexp"
    "slices"

    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
)

const (
    RouteAttributeName    = "_route"
    /* set by Match on the path and methods of a definition no handler answers; an unserved request there is still a 404 */
    RouteAttributeDefinitionOnly = "_definitionOnly"
    RouteAttributePattern = "_pattern"
    RouteAttributeMethods = "_methods"
    RouteAttributeHost    = "_host"
    RouteAttributeSchemes = "_schemes"
    RouteAttributeLocales = "_locales"
    RouteAttributeLocale  = "_locale"

    /* RouteAttributeProtection carries the route's RouteOptions.Protection, written only when it turns something off; read it through RouteProtection. */
    RouteAttributeProtection = "_protection"

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

/* RouteProtection answers the protections the matched route turned off, the zero value when it turned none off or no route matched. */
func RouteProtection(request httpcontract.Request) httpcontract.RouteProtectionConfig {
    if true == internal.IsNilInterface(request) || true == internal.IsNilInterface(request.Attributes()) {
        return httpcontract.RouteProtectionConfig{}
    }

    value, exists := request.Attributes().Get(RouteAttributeProtection)
    if false == exists {
        return httpcontract.RouteProtectionConfig{}
    }

    /* a value of another type answers the zero value the failed assertion yields */
    protection, _ := value.(httpcontract.RouteProtectionConfig)
    protection.SecurityHeadersDisabled = slices.Clone(protection.SecurityHeadersDisabled)

    return protection
}

func routeProtectionTurnsNothingOff(protection httpcontract.RouteProtectionConfig) bool {
    return false == protection.CsrfDisabled && 0 == len(protection.SecurityHeadersDisabled) && true == protection.MaxBodyBytes.IsAbsent()
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

    defaults   map[string]string
    locales    []string
    priority   int
    attributes map[string]any

    requestType reflect.Type
}

type routeTreeNode struct {
    segment               string
    staticChildren        map[string]*routeTreeNode
    paramChild            *routeTreeNode
    wildcardSegmentChild  *routeTreeNode
    wildcardCatchAllChild *routeTreeNode
    routeIndices          []int
}
