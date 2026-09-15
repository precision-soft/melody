package url

import (
    "encoding/json"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

/* RouteManifestForRuntime projects exposed named routes using the framework manifest builder. Anonymous callers receive public routes; authenticated callers also receive frontend routes. Internal routes remain server-side. */
func RouteManifestForRuntime(runtimeInstance runtimecontract.Runtime) melodyhttp.RouteManifest {
    routeRegistry := melodyhttp.RouteRegistryMustFromContainer(runtimeInstance.Container())

    zones := []string{melodyhttp.RouteZonePublic}
    if true == callerIsAuthenticated(runtimeInstance) {
        zones = append(zones, melodyhttp.RouteZoneFrontend)
    }

    manifest := melodyhttp.BuildRouteManifest(routeRegistry.RouteDefinitions())

    entries := make([]melodyhttp.RouteManifestEntry, 0, len(manifest.Routes))
    for _, zone := range zones {
        entries = append(entries, melodyhttp.FilterRouteManifestByZone(manifest, zone).Routes...)
    }

    return melodyhttp.RouteManifest{Routes: entries}
}

/* RoutesJsonFromRuntime renders the same projection as the JSON document every page carries. */
func RoutesJsonFromRuntime(runtimeInstance runtimecontract.Runtime) (string, error) {
    payload, marshalErr := json.Marshal(RouteManifestForRuntime(runtimeInstance))
    if nil != marshalErr {
        return `{"routes":[]}`, marshalErr
    }

    return string(payload), nil
}

func callerIsAuthenticated(runtimeInstance runtimecontract.Runtime) bool {
    securityContext, found := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
    if false == found {
        return false
    }

    token := securityContext.Token()
    if nil == token {
        return false
    }

    return token.IsAuthenticated()
}
