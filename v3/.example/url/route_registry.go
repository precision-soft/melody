package url

import (
    "encoding/json"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

/* RoutesJsonFromContainer projects the named routes into the RouteManifest shape the frontend RouteGenerator (melody-routes.ts) consumes, injected into every page as window.melodyRoutes. Unlike the melody:routes:manifest export command it is not limited to the expose-opted-in routes, because the example's own pages reference their routes by name directly. */
func RoutesJsonFromContainer(serviceContainer containercontract.Container) (string, error) {
    routeRegistry := melodyhttp.RouteRegistryMustFromContainer(serviceContainer)
    definitions := routeRegistry.RouteDefinitions()

    entries := make([]melodyhttp.RouteManifestEntry, 0, len(definitions))

    for _, definition := range definitions {
        if nil == definition {
            continue
        }

        name := definition.Name()
        if "" == name {
            continue
        }

        entries = append(entries, melodyhttp.RouteManifestEntry{
            Name:         name,
            Pattern:      definition.Pattern(),
            Methods:      definition.Methods(),
            Requirements: definition.Requirements(),
            Defaults:     definition.Defaults(),
        })
    }

    payload, marshalErr := json.Marshal(melodyhttp.RouteManifest{Routes: entries})
    if nil != marshalErr {
        return `{"routes":[]}`, marshalErr
    }

    return string(payload), nil
}
