package http

import (
    "sort"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* RouteManifest is the frontend-facing view of the exposed routes: a stable JSON document the frontend loads to generate URLs by route name instead of hardcoding paths (the melody equivalent of a JS routing bundle). Only routes that opt in via RouteAttributeExpose appear, and only the fields a URL generator needs are exported — handlers and internal attributes are never leaked. */
type RouteManifest struct {
    Routes []RouteManifestEntry `json:"routes"`
}

type RouteManifestEntry struct {
    Name         string            `json:"name"`
    Pattern      string            `json:"pattern"`
    Methods      []string          `json:"methods,omitempty"`
    Requirements map[string]string `json:"requirements,omitempty"`
    Defaults     map[string]string `json:"defaults,omitempty"`
    Zone         string            `json:"zone,omitempty"`
}

/* BuildRouteManifest projects the route definitions into a manifest, keeping only routes that opt in through RouteAttributeExpose and carry a name (an unnamed route cannot be referenced by the frontend). Entries are sorted by name so the output is deterministic across runs. */
func BuildRouteManifest(definitions []httpcontract.RouteDefinition) RouteManifest {
    entries := make([]RouteManifestEntry, 0, len(definitions))

    for _, definition := range definitions {
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
            Requirements: definition.Requirements(),
            Defaults:     definition.Defaults(),
            Zone:         routeZone(definition),
        })
    }

    sort.Slice(entries, func(first int, second int) bool {
        return entries[first].Name < entries[second].Name
    })

    return RouteManifest{Routes: entries}
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
