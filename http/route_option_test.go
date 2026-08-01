package http

import (
    nethttp "net/http"
    "testing"
)

/* @info the three setters are how a route group rewrites the options it was handed and how an application adjusts a shared options value; none had a test. SetRequirements and SetDefaults each keep their own copy, which is what stopped a group from writing its merged maps back into the caller's value — the bug where one options value reused across two grouped registrations accumulated the first group's prefix. */

func TestRouteOptions_SetNameReplacesTheName(t *testing.T) {
    options := &RouteOptions{}

    if "" != options.Name() {
        t.Fatalf("expected a fresh options value to name nothing")
    }

    options.SetName("article.show")

    if "article.show" != options.Name() {
        t.Fatalf("unexpected name: %q", options.Name())
    }

    options.SetName("")

    if "" != options.Name() {
        t.Fatalf("expected the name to be clearable, got: %q", options.Name())
    }
}

func TestRouteOptions_SetRequirementsKeepsItsOwnCopy(t *testing.T) {
    options := &RouteOptions{}

    supplied := map[string]string{"id": ConstraintNumeric}
    options.SetRequirements(supplied)

    supplied["id"] = "rewritten"
    supplied["injected"] = "rewritten"

    if ConstraintNumeric != options.Requirements()["id"] {
        t.Fatalf("expected the setter to copy the caller's map, got: %v", options.Requirements())
    }

    if _, present := options.Requirements()["injected"]; true == present {
        t.Fatalf("a key added to the caller's map after the call reached the options")
    }

    options.Requirements()["id"] = "rewritten"

    if ConstraintNumeric != options.Requirements()["id"] {
        t.Fatalf("expected the accessor to hand out a copy as well")
    }
}

func TestRouteOptions_SetDefaultsKeepsItsOwnCopy(t *testing.T) {
    options := &RouteOptions{}

    supplied := map[string]string{"format": "json"}
    options.SetDefaults(supplied)

    supplied["format"] = "rewritten"
    supplied["injected"] = "rewritten"

    if "json" != options.Defaults()["format"] {
        t.Fatalf("expected the setter to copy the caller's map, got: %v", options.Defaults())
    }

    if _, present := options.Defaults()["injected"]; true == present {
        t.Fatalf("a key added to the caller's map after the call reached the options")
    }

    options.Defaults()["format"] = "rewritten"

    if "json" != options.Defaults()["format"] {
        t.Fatalf("expected the accessor to hand out a copy as well")
    }
}

/* @info the setters have to reach the registered route, not merely the options value: a name set through SetName is the name the url generator resolves, and a requirement set through SetRequirements is the one the router enforces. */

func TestRouteOptions_SettersReachTheRegisteredRoute(t *testing.T) {
    router := NewRouter()

    options := &RouteOptions{methods: []string{nethttp.MethodGet}}
    options.SetName("article.show")
    options.SetRequirements(map[string]string{"id": ConstraintNumeric})
    options.SetDefaults(map[string]string{"format": "json"})

    router.HandleWithOptions("/articles/:id", routeRegistryTestHandler(), options)

    definition, found := router.RouteDefinition("article.show")
    if false == found {
        t.Fatalf("expected the name set through the setter to reach the registry")
    }

    if 0 == len(definition.Requirements()) {
        t.Fatalf("expected the requirement set through the setter to reach the registry")
    }

    if "json" != definition.Defaults()["format"] {
        t.Fatalf("expected the default set through the setter to reach the registry, got: %v", definition.Defaults())
    }
}
