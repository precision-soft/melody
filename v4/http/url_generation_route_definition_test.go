package http

import (
    "regexp"
    "testing"
)

func TestUrlGenerationRouteDefinition_Defaults_ReturnsCopy(t *testing.T) {
    routeValue := route{
        pattern: "/test/:id",
        defaults: map[string]string{
            "id": "42",
        },
        requirements: map[string]*regexp.Regexp{},
    }

    definition := NewUrlGenerationRouteDefinition(routeValue)

    defaults := definition.Defaults()
    defaults["id"] = "modified"

    original := definition.Defaults()
    if "42" != original["id"] {
        t.Fatalf("expected original default to be '42', got: %s", original["id"])
    }
}

func TestUrlGenerationRouteDefinition_Requirements_ReturnsCopy(t *testing.T) {
    regex := regexp.MustCompile(`\d+`)

    routeValue := route{
        pattern:  "/test/:id",
        defaults: map[string]string{},
        requirements: map[string]*regexp.Regexp{
            "id": regex,
        },
    }

    definition := NewUrlGenerationRouteDefinition(routeValue)

    requirements := definition.Requirements()
    requirements["id"] = regexp.MustCompile(`\w+`)

    original := definition.Requirements()
    if regex != original["id"] {
        t.Fatalf("expected original requirement to be unchanged")
    }
}

func TestUrlGenerationRouteDefinition_Defaults_EmptyMap(t *testing.T) {
    routeValue := route{
        pattern:      "/test",
        defaults:     map[string]string{},
        requirements: map[string]*regexp.Regexp{},
    }

    definition := NewUrlGenerationRouteDefinition(routeValue)

    defaults := definition.Defaults()
    if 0 != len(defaults) {
        t.Fatalf("expected empty defaults map, got %d entries", len(defaults))
    }
}

func TestUrlGenerationRouteDefinition_Pattern(t *testing.T) {
    routeValue := route{
        pattern:      "/test/:id",
        defaults:     map[string]string{},
        requirements: map[string]*regexp.Regexp{},
    }

    definition := NewUrlGenerationRouteDefinition(routeValue)

    if "/test/:id" != definition.Pattern() {
        t.Fatalf("expected pattern '/test/:id', got: %s", definition.Pattern())
    }
}
