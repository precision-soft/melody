package http

import (
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* RouteManifest is the frontend-facing view of the exposed routes: a stable JSON document the frontend loads to generate URLs by route name instead of hardcoding paths (the melody equivalent of a JS routing bundle). Only routes that opt in via RouteAttributeExpose appear, and only the fields a URL generator needs are exported — handlers and internal attributes are never leaked. */
type RouteManifest struct {
    Routes []RouteManifestEntry `json:"routes"`
}

/* RouteManifestEntry carries every field the matcher discriminates on that a generated url has to satisfy. Three of them used to be absent — host, schemes and locales — and each absence produced the same outcome: the frontend minted a relative path against the current origin, the current scheme and no locale, and the router refused it. A route bound to api.example.com was advertised to a page served from www; a route restricted to https was advertised to a page served over http; and a route restricted to a locale set was advertised with nothing saying which locales it accepts — or, when it declared locales and carried no locale segment at all, advertised while unreachable by any url whatsoever. Priority travels for the same reason one step further out: two exposed routes can answer one generated path, and the consumer could not see which one the router will pick.

   Requirements are the patterns the developer DECLARED, and they are UNANCHORED: the router compiles each one as ^(?:...)$ and matches the whole segment, so a consumer validating with them has to anchor them itself — a bare new RegExp("\\d+").test("12abc") answers true where the router answers 404. The declared spelling is published rather than the compiled one because the wrapper is not the developer's, it re-wraps on every round trip through NewRequirements, and it would hand RE2-only syntax to an engine that cannot parse it at all. */
type RouteManifestEntry struct {
    Name         string            `json:"name"`
    Pattern      string            `json:"pattern"`
    Methods      []string          `json:"methods,omitempty"`
    Host         string            `json:"host,omitempty"`
    Schemes      []string          `json:"schemes,omitempty"`
    Locales      []string          `json:"locales,omitempty"`
    Requirements map[string]string `json:"requirements,omitempty"`
    Defaults     map[string]string `json:"defaults,omitempty"`
    Priority     int               `json:"priority,omitempty"`
    Zone         string            `json:"zone,omitempty"`
}

/* BuildRouteManifest projects the route definitions into a manifest, keeping only routes that opt in through RouteAttributeExpose and carry a name (an unnamed route cannot be referenced by the frontend; the registration refuses an exposed route without one, so nothing reaches here that a boot did not already report). Entries are sorted by name, and by pattern under an equal name, so the output is deterministic across runs: sort.Slice is not stable, so a name alone stopped deciding the order for exactly the duplicate-name case in which the order decides which entry a consumer keyed by name keeps — and the consumer keeping the last disagreed with the server's generator, which keeps the first. */
func BuildRouteManifest(definitions []httpcontract.RouteDefinition) RouteManifest {
    entries := make([]RouteManifestEntry, 0, len(definitions))

    for _, definition := range definitions {
        if nil == definition {
            continue
        }

        if "" == definition.Name() {
            continue
        }

        if false == routeIsExposed(definition) {
            continue
        }

        entries = append(entries, RouteManifestEntry{
            Name:         definition.Name(),
            Pattern:      definition.Pattern(),
            Methods:      definition.Methods(),
            Host:         definition.Host(),
            Schemes:      definition.Schemes(),
            Locales:      definition.Locales(),
            Requirements: definition.Requirements(),
            Defaults:     definition.Defaults(),
            Priority:     definition.Priority(),
            Zone:         routeZone(definition),
        })
    }

    sort.Slice(entries, func(first int, second int) bool {
        if entries[first].Name != entries[second].Name {
            return entries[first].Name < entries[second].Name
        }

        return entries[first].Pattern < entries[second].Pattern
    })

    return RouteManifest{Routes: entries}
}

/* FilterRouteManifestByZone narrows a manifest to one zone. It is exported because the gate existed only inside the cli command: an application projecting the manifest in-process — into a page, into a bundle — had no way to apply it, so the zone travelled as a label on an artifact that carried every zone to every consumer, the anonymous ones included.

   The zone is read here for every caller, the command included, which hands its --zone flag over as typed: an empty zone is no gate and answers the manifest whole, a zone that is not one of the declared ones is refused by name, and a declared one surrounded by space is that zone. The command used to trim before calling, so the two disagreed about a zone that is nothing but space — refused here, read there as no gate. Accepted, a misspelled zone matched no entry and answered an empty manifest in silence — the very artifact the command refuses to write over the good one — so a page carried no route at all with nothing saying why.

   Emptiness is asked of the zone AS GIVEN, before the trim: a caller asking for no gate passes no zone, while a zone that arrives as whitespace — a configuration value, a template that rendered to spaces — is a zone nobody declared, and reading it as no gate would answer every zone to a consumer that asked for one. Refused by name, like any other zone that is not declared — and the name in the refusal is the zone AS GIVEN, since the trimmed spelling of a zone made of spaces is the empty one, which the sentence above says is no gate at all. */
func FilterRouteManifestByZone(manifest RouteManifest, zone string) (RouteManifest, error) {
    if "" == zone {
        return manifest, nil
    }

    givenZone := zone
    zone = strings.TrimSpace(zone)

    if false == IsRouteZone(zone) {
        return RouteManifest{}, exception.NewError(
            "route zone is not one of the declared zones",
            map[string]any{
                "zone":          givenZone,
                "declaredZones": RouteZones(),
            },
            nil,
        )
    }

    filtered := make([]RouteManifestEntry, 0, len(manifest.Routes))

    for _, entry := range manifest.Routes {
        if zone == entry.Zone {
            filtered = append(filtered, entry)
        }
    }

    return RouteManifest{Routes: filtered}, nil
}

func routeIsExposed(definition httpcontract.RouteDefinition) bool {
    exposed, isBool := definition.Attributes()[RouteAttributeExpose].(bool)

    return true == isBool && true == exposed
}

func routeZone(definition httpcontract.RouteDefinition) string {
    zone, isString := definition.Attributes()[RouteAttributeZone].(string)
    if false == isString {
        return ""
    }

    return zone
}
