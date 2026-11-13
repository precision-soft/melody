package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
)

func TestRouteOptions_ZeroValueRegistersARouteForEveryMethodHostAndScheme(t *testing.T) {
    router := NewRouter()

    router.HandleWithOptions("/articles", routeRegistryTestHandler(), RouteOptions{})

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    for _, target := range []string{"https://api.example.com/articles", "http://admin.example.com/articles"} {
        for _, method := range []string{nethttp.MethodGet, nethttp.MethodPost, nethttp.MethodDelete} {
            recorder := httptest.NewRecorder()
            handler.ServeHTTP(recorder, httptest.NewRequest(method, target, nil))

            if nethttp.StatusOK != recorder.Code {
                t.Fatalf("expected %s %s to be answered by a route with no options, got: %d", method, target, recorder.Code)
            }
        }
    }
}

func TestRouteOptions_TheCallersMapsWrittenAfterRegistrationDoNotReachTheRoute(t *testing.T) {
    router := NewRouter()

    requirements := map[string]string{"id": ConstraintNumeric}
    defaults := map[string]string{"format": "json"}

    router.HandleWithOptions(
        "/articles/:id",
        routeRegistryTestHandler(),
        RouteOptions{
            Name:         "article.show",
            Methods:      []string{nethttp.MethodGet},
            Requirements: requirements,
            Defaults:     defaults,
        },
    )

    requirements["id"] = "rewritten"
    requirements["injected"] = "rewritten"
    defaults["format"] = "rewritten"

    definition, found := router.RouteDefinition("article.show")
    if false == found {
        t.Fatalf("expected the route to be registered under its name")
    }

    if ConstraintNumeric != definition.Requirements()["id"] {
        t.Fatalf("expected the registration to copy the requirements, got: %v", definition.Requirements())
    }

    if _, present := definition.Requirements()["injected"]; true == present {
        t.Fatalf("a key added to the caller's map after the registration reached the route")
    }

    if "json" != definition.Defaults()["format"] {
        t.Fatalf("expected the registration to copy the defaults, got: %v", definition.Defaults())
    }
}

func TestRouteOptions_TheCallersListsWrittenAfterRegistrationDoNotReachTheRoute(t *testing.T) {
    router := NewRouter()

    methods := []string{nethttp.MethodGet}
    schemes := []string{"https"}
    locales := []string{"en"}
    attributes := map[string]any{"owner": "catalog"}

    router.HandleWithOptions(
        "/:_locale/articles",
        routeRegistryTestHandler(),
        RouteOptions{
            Name:       "article.list",
            Methods:    methods,
            Schemes:    schemes,
            Locales:    locales,
            Attributes: attributes,
        },
    )

    methods[0] = nethttp.MethodDelete
    schemes[0] = "http"
    locales[0] = "fr"
    attributes["owner"] = "rewritten"

    definition, found := router.RouteDefinition("article.list")
    if false == found {
        t.Fatalf("expected the route to be registered under its name")
    }

    if 1 != len(definition.Methods()) || nethttp.MethodGet != definition.Methods()[0] {
        t.Fatalf("expected the registration to copy the methods, got: %v", definition.Methods())
    }

    if 1 != len(definition.Schemes()) || "https" != definition.Schemes()[0] {
        t.Fatalf("expected the registration to copy the schemes, got: %v", definition.Schemes())
    }

    if 1 != len(definition.Locales()) || "en" != definition.Locales()[0] {
        t.Fatalf("expected the registration to copy the locales, got: %v", definition.Locales())
    }

    if "catalog" != definition.Attributes()["owner"] {
        t.Fatalf("expected the registration to copy the attributes, got: %v", definition.Attributes())
    }

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "https://example.com/en/articles", nil))

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the route to keep answering GET https /en, got: %d", recorder.Code)
    }
}
