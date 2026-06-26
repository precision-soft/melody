package http

import (
    nethttp "net/http"
    "testing"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func manifestTestHandler(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
    return TextResponse(200, "ok"), nil
}

func manifestTestRouter() *Router {
    router := NewRouter()

    /* exposed + named + zoned */
    router.HandleWithOptions(
        "/users/:id",
        manifestTestHandler,
        NewRouteOptions(
            "user_show",
            []string{nethttp.MethodGet},
            "",
            nil,
            map[string]string{"id": `\d+`},
            nil,
            nil,
            0,
            ExposedRouteAttributes(RouteZoneFrontend),
        ),
    )

    /* exposed but unnamed → excluded (cannot be referenced) */
    router.HandleWithOptions(
        "/anonymous",
        manifestTestHandler,
        NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, ExposedRouteAttributes(RouteZoneFrontend)),
    )

    /* named but not exposed → excluded */
    router.HandleWithOptions(
        "/internal/health",
        manifestTestHandler,
        NewRouteOptions("health", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    /* exposed + named, different zone */
    router.HandleWithOptions(
        "/account",
        manifestTestHandler,
        NewRouteOptions("account_show", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, ExposedRouteAttributes(RouteZoneClient)),
    )

    return router
}

func TestBuildRouteManifest_OnlyExposedNamedRoutes(t *testing.T) {
    manifest := BuildRouteManifest(manifestTestRouter().RouteDefinitions())

    if 2 != len(manifest.Routes) {
        t.Fatalf("expected only the two exposed named routes, got %d: %+v", len(manifest.Routes), manifest.Routes)
    }

    /* sorted by name: account_show before user_show */
    if "account_show" != manifest.Routes[0].Name || "user_show" != manifest.Routes[1].Name {
        t.Fatalf("expected deterministic name order, got %+v", manifest.Routes)
    }

    user := manifest.Routes[1]
    if "/users/:id" != user.Pattern {
        t.Fatalf("unexpected pattern %q", user.Pattern)
    }

    if RouteZoneFrontend != user.Zone {
        t.Fatalf("expected frontend zone, got %q", user.Zone)
    }

    /* the router anchors requirement patterns, so the manifest carries the normalized form */
    if `^\d+$` != user.Requirements["id"] {
        t.Fatalf("expected requirements to be carried, got %+v", user.Requirements)
    }
}

func TestFilterManifestByZone(t *testing.T) {
    manifest := BuildRouteManifest(manifestTestRouter().RouteDefinitions())

    frontendOnly := filterManifestByZone(manifest, RouteZoneFrontend)
    if 1 != len(frontendOnly.Routes) || "user_show" != frontendOnly.Routes[0].Name {
        t.Fatalf("expected only the frontend route, got %+v", frontendOnly.Routes)
    }
}
