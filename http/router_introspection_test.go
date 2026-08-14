package http

import (
    "encoding/json"
    nethttp "net/http"
    "testing"
)

func newIntrospectedRouter(t *testing.T) *Router {
    t.Helper()

    router := NewRouter()

    router.HandleWithOptions(
        "/articles/:id",
        routeRegistryTestHandler(),
        NewRouteOptions(
            "article.show",
            []string{nethttp.MethodGet, nethttp.MethodHead},
            "api.example.com",
            []string{"https"},
            map[string]string{"id": ConstraintNumeric},
            map[string]string{"format": "json"},
            []string{"en", "de"},
            50,
            map[string]any{"audit": true},
        ),
    )

    return router
}

func TestRouteDefinition_ReportsEveryFacetOfTheRoute(t *testing.T) {
    definition, found := newIntrospectedRouter(t).RouteDefinition("article.show")

    if false == found {
        t.Fatalf("expected the named route to be found")
    }

    if "article.show" != definition.Name() {
        t.Fatalf("unexpected name: %q", definition.Name())
    }

    if "api.example.com" != definition.Host() {
        t.Fatalf("unexpected host: %q", definition.Host())
    }

    if 1 != len(definition.Schemes()) || "https" != definition.Schemes()[0] {
        t.Fatalf("unexpected schemes: %v", definition.Schemes())
    }

    /* the listing carries the COMPILED requirement, which addRoute wrapped and anchored — "^(?:^[0-9]+$)$" for a caller who wrote "^[0-9]+$". It is the pattern the router actually enforces rather than the one the caller typed, so a consumer rendering it (an openapi schema, a route listing) shows the wrapped spelling. Pinned as it stands. */
    if "^(?:"+ConstraintNumeric+")$" != definition.Requirements()["id"] {
        t.Fatalf("unexpected requirements: %v", definition.Requirements())
    }

    if "json" != definition.Defaults()["format"] {
        t.Fatalf("unexpected defaults: %v", definition.Defaults())
    }

    if 2 != len(definition.Locales()) {
        t.Fatalf("unexpected locales: %v", definition.Locales())
    }

    if 50 != definition.Priority() {
        t.Fatalf("unexpected priority: %d", definition.Priority())
    }

    if true != definition.Attributes()["audit"] {
        t.Fatalf("unexpected attributes: %v", definition.Attributes())
    }
}

func TestRouteDefinition_AccessorsHandOutCopies(t *testing.T) {
    definition, _ := newIntrospectedRouter(t).RouteDefinition("article.show")

    definition.Requirements()["id"] = "rewritten"
    definition.Defaults()["format"] = "rewritten"
    definition.Attributes()["audit"] = "rewritten"
    definition.Schemes()[0] = "rewritten"
    definition.Locales()[0] = "rewritten"

    if "^(?:"+ConstraintNumeric+")$" != definition.Requirements()["id"] {
        t.Fatalf("expected Requirements to hand out a copy")
    }

    if "json" != definition.Defaults()["format"] {
        t.Fatalf("expected Defaults to hand out a copy")
    }

    if true != definition.Attributes()["audit"] {
        t.Fatalf("expected Attributes to hand out a copy")
    }

    if "https" != definition.Schemes()[0] {
        t.Fatalf("expected Schemes to hand out a copy")
    }

    if "en" != definition.Locales()[0] {
        t.Fatalf("expected Locales to hand out a copy")
    }
}

func TestRouteDefinition_MarshalJsonCarriesEveryKey(t *testing.T) {
    definition, _ := newIntrospectedRouter(t).RouteDefinition("article.show")

    encoded, marshalErr := json.Marshal(definition)
    if nil != marshalErr {
        t.Fatalf("unexpected marshal error: %v", marshalErr)
    }

    decoded := map[string]any{}
    if unmarshalErr := json.Unmarshal(encoded, &decoded); nil != unmarshalErr {
        t.Fatalf("unexpected unmarshal error: %v", unmarshalErr)
    }

    for _, key := range []string{
        "name", "pattern", "methods", "host", "schemes",
        "requirements", "defaults", "locales", "priority", "attributes",
    } {
        if _, present := decoded[key]; false == present {
            t.Fatalf("expected the rendered definition to carry %q, got: %s", key, string(encoded))
        }
    }

    if "article.show" != decoded["name"] {
        t.Fatalf("unexpected rendered name: %v", decoded["name"])
    }

    if "api.example.com" != decoded["host"] {
        t.Fatalf("unexpected rendered host: %v", decoded["host"])
    }

    if float64(50) != decoded["priority"] {
        t.Fatalf("unexpected rendered priority: %v", decoded["priority"])
    }
}

func TestRouteDefinition_UnknownNameIsReportedRatherThanReturnedAsNil(t *testing.T) {
    definition, found := newIntrospectedRouter(t).RouteDefinition("nothing.here")

    if true == found {
        t.Fatalf("expected an unregistered name not to be found")
    }

    if nil == definition {
        t.Fatalf("expected an empty definition rather than nil")
    }

    if "" != definition.Name() {
        t.Fatalf("expected the empty definition to name nothing, got: %q", definition.Name())
    }
}

func TestRouter_ForwardsIntrospectionToItsRegistry(t *testing.T) {
    router := newIntrospectedRouter(t)

    definitions := router.RouteDefinitions()
    if 1 != len(definitions) {
        t.Fatalf("expected the registered route to be listed, got: %d", len(definitions))
    }

    if "article.show" != definitions[0].Name() {
        t.Fatalf("unexpected listed route: %q", definitions[0].Name())
    }

    registryDefinitions := router.RouteRegistry().RouteDefinitions()
    if len(definitions) != len(registryDefinitions) {
        t.Fatalf("expected the router and its registry to list the same routes")
    }

    registryDefinition, found := router.RouteRegistry().RouteDefinition("article.show")
    if false == found {
        t.Fatalf("expected the registry to find the named route")
    }

    if definitions[0].Pattern() != registryDefinition.Pattern() {
        t.Fatalf("expected the router and its registry to describe the route identically")
    }
}
