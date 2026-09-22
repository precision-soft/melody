package url

import (
    "encoding/json"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    examplesecurity "github.com/precision-soft/melody/v3/.example/security"
)

/* RouteManifestForRuntime projects the exposed named routes into the RouteManifest shape the frontend RouteGenerator (melody-routes.ts) consumes, injected into every page as window.melodyRoutes. It reuses the framework BuildRouteManifest so it applies the same RouteAttributeExpose opt-in filter as the melody:routes:manifest export command: an example must model shipping only deliberately-exposed route metadata to the browser rather than dumping every route's pattern, requirements and defaults — internal routes stay server-side.

   the ZONE gate is applied here too, and it is applied against the caller. The zone gate used to live only inside the cli command, so the in-process door carried every zone to every page — and the frontend zone is the admin surface (the product and user api routes, each behind RoleEditor/RoleAdmin), enumerated with its patterns and methods into the anonymous login page. The public zone is what an unauthenticated visitor needs (login, logout, health, the openapi document); the frontend zone joins it once the caller is authenticated. */
func RouteManifestForRuntime(runtimeInstance runtimecontract.Runtime) (melodyhttp.RouteManifest, error) {
    routeRegistry := melodyhttp.RouteRegistryMustFromContainer(runtimeInstance.Container())

    zones := []string{melodyhttp.RouteZonePublic}
    if true == callerIsAuthenticated(runtimeInstance) {
        zones = append(zones, melodyhttp.RouteZoneFrontend)
    }

    manifest := melodyhttp.BuildRouteManifest(routeRegistry.RouteDefinitions())

    entries := make([]melodyhttp.RouteManifestEntry, 0, len(manifest.Routes))
    for _, zone := range zones {
        zoned, filterErr := melodyhttp.FilterRouteManifestByZone(manifest, zone)
        if nil != filterErr {
            return melodyhttp.RouteManifest{}, filterErr
        }

        entries = append(entries, zoned.Routes...)
    }

    return melodyhttp.RouteManifest{Routes: entries}, nil
}

/* RoutesJsonFromRuntime renders the same projection as the JSON document every page carries. */
func RoutesJsonFromRuntime(runtimeInstance runtimecontract.Runtime) (string, error) {
    manifest, manifestErr := RouteManifestForRuntime(runtimeInstance)
    if nil != manifestErr {
        return EmptyRoutesJson, manifestErr
    }

    payload, marshalErr := json.Marshal(manifest)
    if nil != marshalErr {
        return EmptyRoutesJson, marshalErr
    }

    return string(payload), nil
}

/* EmptyRoutesJson is the manifest a reader answers when it has none to give.
The page renders it so its scripts still parse something, and the loss is
journaled beside it rather than carried to the browser in silence. */
const EmptyRoutesJson = `{"routes":[]}`

func callerIsAuthenticated(runtimeInstance runtimecontract.Runtime) bool {
    token, found := examplesecurity.TokenFromRuntime(runtimeInstance)
    if false == found {
        return false
    }

    return token.IsAuthenticated()
}
