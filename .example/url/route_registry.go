package url

import (
    "encoding/json"

    containercontract "github.com/precision-soft/melody/container/contract"
    melodyhttp "github.com/precision-soft/melody/http"
)

type jsRouteDefinition struct {
    Name    string `json:"name"`
    Pattern string `json:"pattern"`
}

/* jsRouteManifest is the document the frontend RouteGenerator (assets/melody-routes.ts) reads: an object carrying the exposed routes under a "routes" key, not a bare array. The envelope is what lets the shape grow — a version, a generated-at stamp, per-route methods — without every reader of the manifest having to tell the old form from the new one by inspecting the JSON's type. */
type jsRouteManifest struct {
    Routes []jsRouteDefinition `json:"routes"`
}

/* RoutesJsonFromContainer projects the named routes into the manifest the frontend generates URLs from, injected into every page as window.melodyRoutes by page.Render. A route without a name is skipped: the frontend addresses routes BY name, so an unnamed one could never be asked for and would only widen what the browser is told about the server's routing. The document is always well formed, the failure path included, because the caller splices it into a JavaScript string literal where a half-written value would break every page's scripting rather than one route's URL. */
func RoutesJsonFromContainer(serviceContainer containercontract.Container) (string, error) {
    routeRegistry := melodyhttp.RouteRegistryMustFromContainer(serviceContainer)
    definitions := routeRegistry.RouteDefinitions()

    jsRoutes := make([]jsRouteDefinition, 0, len(definitions))

    for _, definition := range definitions {
        if nil == definition {
            continue
        }

        name := definition.Name()
        if "" == name {
            continue
        }

        jsRoutes = append(jsRoutes, jsRouteDefinition{
            Name:    name,
            Pattern: definition.Pattern(),
        })
    }

    payload, marshalErr := json.Marshal(jsRouteManifest{Routes: jsRoutes})
    if nil != marshalErr {
        return `{"routes":[]}`, marshalErr
    }

    return string(payload), nil
}
