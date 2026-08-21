package http

import (
    nethttp "net/http"
    "testing"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
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

    /* the manifest carries the pattern the caller DECLARED, not the anchored, non-capturing form the
       registration compiles: the wrapped spelling is not the developer's, it re-wraps on every round
       trip through NewRequirements, and it carries RE2-only syntax to consumers whose engine is not */
    if `\d+` != user.Requirements["id"] {
        t.Fatalf("expected the declared requirement to be carried, got %+v", user.Requirements)
    }
}

func TestRouterRegistration_RefusesAnExposedRouteWithNoName(t *testing.T) {
    /* the projection used to drop it in silence: the developer stated the intention and the artifact
       contradicted it with no diagnostic anywhere */
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewRouter().HandleWithOptions(
                "/anonymous",
                manifestTestHandler,
                NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, ExposedRouteAttributes(RouteZoneFrontend)),
            )
        },
        "an exposed route must be named",
    )
}

func TestRouterRegistration_RefusesAnExposeAttributeThatIsNotABool(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewRouter().HandleWithOptions(
                "/typo",
                manifestTestHandler,
                NewRouteOptions("typo", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, map[string]any{RouteAttributeExpose: "true"}),
            )
        },
        "route expose attribute must be a bool",
    )
}

func TestRouterRegistration_RefusesAZoneThatIsNotDeclared(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewRouter().HandleWithOptions(
                "/typo",
                manifestTestHandler,
                NewRouteOptions("typo", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, map[string]any{RouteAttributeExpose: true, RouteAttributeZone: "frontent"}),
            )
        },
        "route zone is not one of the declared zones",
    )
}

func TestExposedRouteAttributes_RefusesAZoneThatIsNotDeclared(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            ExposedRouteAttributes("fronted")
        },
        "route zone is not one of the declared zones",
    )
}

func TestBuildRouteManifest_CarriesEveryMatchDiscriminatorAGeneratedUrlMustSatisfy(t *testing.T) {
    router := NewRouter()
    router.HandleWithOptions(
        "/:_locale/blog",
        manifestTestHandler,
        NewRouteOptions(
            "blog",
            []string{nethttp.MethodGet},
            "api.example.com",
            []string{"https"},
            nil,
            nil,
            []string{"en", "de"},
            7,
            ExposedRouteAttributes(RouteZoneFrontend),
        ),
    )

    manifest := BuildRouteManifest(router.RouteDefinitions())
    if 1 != len(manifest.Routes) {
        t.Fatalf("expected the exposed route, got %+v", manifest.Routes)
    }

    entry := manifest.Routes[0]

    /* each of these three used to be absent, and each absence made the frontend mint a url the router
       refuses: the wrong origin, the wrong scheme, and no locale at all */
    if "api.example.com" != entry.Host {
        t.Fatalf("expected the host to be carried, got %q", entry.Host)
    }

    if 1 != len(entry.Schemes) || "https" != entry.Schemes[0] {
        t.Fatalf("expected the schemes to be carried, got %+v", entry.Schemes)
    }

    if 2 != len(entry.Locales) || "en" != entry.Locales[0] || "de" != entry.Locales[1] {
        t.Fatalf("expected the locales to be carried, got %+v", entry.Locales)
    }

    if 7 != entry.Priority {
        t.Fatalf("expected the priority to be carried, got %d", entry.Priority)
    }
}

func TestFilterManifestByZone(t *testing.T) {
    manifest := BuildRouteManifest(manifestTestRouter().RouteDefinitions())

    frontendOnly := FilterRouteManifestByZone(manifest, RouteZoneFrontend)
    if 1 != len(frontendOnly.Routes) || "user_show" != frontendOnly.Routes[0].Name {
        t.Fatalf("expected only the frontend route, got %+v", frontendOnly.Routes)
    }
}
