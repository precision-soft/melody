package http

import (
    "errors"
    nethttp "net/http"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func manifestTestHandler(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
    return TextResponse(200, "ok"), nil
}

func manifestTestRouter() *Router {
    router := NewRouter()

    router.Handle(
        "/users/:id",
        manifestTestHandler,
        RouteOptions{
            Name:         "user_show",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"id": `\d+`},
            Attributes:   ExposedRouteAttributes(RouteZoneFrontend),
        },
    )

    router.Handle(
        "/internal/health",
        manifestTestHandler,
        RouteOptions{Name: "health", Methods: []string{nethttp.MethodGet}},
    )

    router.Handle(
        "/account",
        manifestTestHandler,
        RouteOptions{Name: "account_show", Methods: []string{nethttp.MethodGet}, Attributes: ExposedRouteAttributes(RouteZoneClient)},
    )

    return router
}

func TestBuildRouteManifest_OnlyExposedNamedRoutes(t *testing.T) {
    manifest := BuildRouteManifest(manifestTestRouter().RouteDefinitions())

    if 2 != len(manifest.Routes) {
        t.Fatalf("expected only the two exposed named routes, got %d: %+v", len(manifest.Routes), manifest.Routes)
    }

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

    if `\d+` != user.Requirements["id"] {
        t.Fatalf("expected the declared requirement to be carried, got %+v", user.Requirements)
    }
}

func TestRouterRegistration_RefusesAnExposedRouteWithNoName(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewRouter().Handle(
                "/anonymous",
                manifestTestHandler,
                RouteOptions{Methods: []string{nethttp.MethodGet}, Attributes: ExposedRouteAttributes(RouteZoneFrontend)},
            )
        },
        "an exposed route must be named",
    )
}

func TestRouterRegistration_RefusesAnExposeAttributeThatIsNotABool(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewRouter().Handle(
                "/typo",
                manifestTestHandler,
                RouteOptions{Name: "typo", Methods: []string{nethttp.MethodGet}, Attributes: map[string]any{RouteAttributeExpose: "true"}},
            )
        },
        "route expose attribute must be a bool",
    )
}

func TestRouterRegistration_RefusesAZoneThatIsNotDeclared(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewRouter().Handle(
                "/typo",
                manifestTestHandler,
                RouteOptions{Name: "typo", Methods: []string{nethttp.MethodGet}, Attributes: map[string]any{RouteAttributeExpose: true, RouteAttributeZone: "frontent"}},
            )
        },
        "route zone is not one of the declared zones",
    )
}

func TestBuildRouteManifest_CarriesEveryMatchDiscriminatorAGeneratedUrlMustSatisfy(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/:_locale/blog",
        manifestTestHandler,
        RouteOptions{
            Name:       "blog",
            Methods:    []string{nethttp.MethodGet},
            Host:       "api.example.com",
            Schemes:    []string{"https"},
            Locales:    []string{"en", "de"},
            Priority:   7,
            Attributes: ExposedRouteAttributes(RouteZoneFrontend),
        },
    )

    manifest := BuildRouteManifest(router.RouteDefinitions())
    if 1 != len(manifest.Routes) {
        t.Fatalf("expected the exposed route, got %+v", manifest.Routes)
    }

    entry := manifest.Routes[0]

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

    frontendOnly, filterErr := FilterRouteManifestByZone(manifest, RouteZoneFrontend)
    if nil != filterErr {
        t.Fatalf("unexpected refusal: %v", filterErr)
    }

    if 1 != len(frontendOnly.Routes) || "user_show" != frontendOnly.Routes[0].Name {
        t.Fatalf("expected only the frontend route, got %+v", frontendOnly.Routes)
    }
}

func TestFilterRouteManifestByZone_RefusesAZoneThatIsNotDeclared(t *testing.T) {
    manifest := BuildRouteManifest(manifestTestRouter().RouteDefinitions())

    filtered, filterErr := FilterRouteManifestByZone(manifest, "frontned")
    if nil == filterErr {
        t.Fatalf("expected the misspelled zone to be refused, got %d routes", len(filtered.Routes))
    }

    typedError, ok := filterErr.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got %T", filterErr)
    }

    if "route zone is not one of the declared zones" != typedError.Message() {
        t.Fatalf("unexpected message: %s", typedError.Message())
    }

    if "frontned" != typedError.Context()["zone"] {
        t.Fatalf("expected the refusal to name the zone, got %+v", typedError.Context())
    }

    declaredZones, isList := typedError.Context()["declaredZones"].([]string)
    if false == isList || len(RouteZones()) != len(declaredZones) {
        t.Fatalf("expected the refusal to list the declared zones, got %+v", typedError.Context()["declaredZones"])
    }

    if 0 != len(filtered.Routes) {
        t.Fatalf("expected no manifest beside the refusal, got %+v", filtered.Routes)
    }
}

func TestFilterRouteManifestByZone_ReadsAZoneWithSurroundingSpaceAsTheCommandDoes(t *testing.T) {
    manifest := BuildRouteManifest(manifestTestRouter().RouteDefinitions())

    trimmed, trimmedErr := FilterRouteManifestByZone(manifest, " "+RouteZoneFrontend+" ")
    if nil != trimmedErr {
        t.Fatalf("expected the padded zone read as the declared one, got %v", trimmedErr)
    }

    exact, exactErr := FilterRouteManifestByZone(manifest, RouteZoneFrontend)
    if nil != exactErr {
        t.Fatalf("unexpected refusal: %v", exactErr)
    }

    if len(exact.Routes) != len(trimmed.Routes) || 0 == len(trimmed.Routes) {
        t.Fatalf("expected the padded zone to answer the %d routes of the exact one, got %d", len(exact.Routes), len(trimmed.Routes))
    }

}

func TestFilterRouteManifestByZone_AZoneMadeOnlyOfSpaceIsRefusedRatherThanReadAsNoGate(t *testing.T) {
    manifest := BuildRouteManifest(manifestTestRouter().RouteDefinitions())

    filtered, filterErr := FilterRouteManifestByZone(manifest, "   ")

    if nil == filterErr {
        t.Fatalf("expected a blank zone to be refused, got %d routes", len(filtered.Routes))
    }

    if 0 != len(filtered.Routes) {
        t.Fatalf("expected no routes beside the refusal, got %d", len(filtered.Routes))
    }

    whole, wholeErr := FilterRouteManifestByZone(manifest, "")
    if nil != wholeErr || 0 == len(whole.Routes) {
        t.Fatalf("expected the empty zone to stay the one spelling of no gate, got %d routes and %v", len(whole.Routes), wholeErr)
    }

    var refusal *exception.Error
    if false == errors.As(filterErr, &refusal) {
        t.Fatalf("expected a melody error, got %T", filterErr)
    }

    if "   " != refusal.Context()["zone"] {
        t.Fatalf("expected the refusal to name the zone as given, got %q", refusal.Context()["zone"])
    }
}

func TestFilterRouteManifestByZone_AnEmptyZoneAnswersTheWholeManifest(t *testing.T) {
    manifest := BuildRouteManifest(manifestTestRouter().RouteDefinitions())

    whole, filterErr := FilterRouteManifestByZone(manifest, "")
    if nil != filterErr {
        t.Fatalf("unexpected refusal: %v", filterErr)
    }

    if len(manifest.Routes) != len(whole.Routes) || 0 == len(whole.Routes) {
        t.Fatalf("expected the whole manifest of %d routes, got %d", len(manifest.Routes), len(whole.Routes))
    }
}
