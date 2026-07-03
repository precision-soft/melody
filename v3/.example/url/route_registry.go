package url

import (
    "encoding/json"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

/* RoutesJsonFromContainer projects the exposed named routes into the RouteManifest shape the frontend RouteGenerator (melody-routes.ts) consumes, injected into every page as window.melodyRoutes. It reuses the framework BuildRouteManifest so it applies the same RouteAttributeExpose opt-in filter as the melody:routes:manifest export command: the example's frontend-facing routes already opt in via ExposedRouteAttributes (see config/http.go), and an example must model shipping only deliberately-exposed route metadata to the browser rather than dumping every route's pattern, requirements and defaults — internal routes stay server-side. */
func RoutesJsonFromContainer(serviceContainer containercontract.Container) (string, error) {
    routeRegistry := melodyhttp.RouteRegistryMustFromContainer(serviceContainer)

    manifest := melodyhttp.BuildRouteManifest(routeRegistry.RouteDefinitions())

    payload, marshalErr := json.Marshal(manifest)
    if nil != marshalErr {
        return `{"routes":[]}`, marshalErr
    }

    return string(payload), nil
}
