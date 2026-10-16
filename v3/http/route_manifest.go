package http

import (
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* RouteManifest is the frontend-facing view of the exposed routes: a stable JSON document a frontend loads to generate urls by route name. Only routes that opt in through RouteAttributeExpose appear, with only the fields a url generator needs. */
type RouteManifest struct {
    Routes []RouteManifestEntry `json:"routes"`
}

/* RouteManifestEntry carries every field the matcher discriminates on that a generated url has to satisfy: host, schemes, locales and priority beside the pattern. Requirements are published as declared and unanchored; the router matches each as ^(?:...)$, so a consumer validating with them anchors them itself. */
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

/* BuildRouteManifest projects the route definitions into a manifest, keeping only named routes that opt in through RouteAttributeExpose. Entries are sorted by name, then by pattern, so the output is deterministic. */
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

/* FilterRouteManifestByZone narrows a manifest to one zone. An empty zone is no gate and answers the manifest whole; a zone that is not declared is refused by name, one of whitespace included; a declared zone surrounded by space is that zone. */
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
