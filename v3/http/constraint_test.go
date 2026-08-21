package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
)

func TestNewRequirement_CarriesBothHalvesOfTheConstraint(t *testing.T) {
    requirement := NewRequirement("id", ConstraintNumeric)

    if "id" != requirement.ParameterName() {
        t.Fatalf("unexpected parameter name: %q", requirement.ParameterName())
    }

    if ConstraintNumeric != requirement.Pattern() {
        t.Fatalf("unexpected pattern: %q", requirement.Pattern())
    }
}

func TestNewRequirements_MapsEveryCompleteRequirement(t *testing.T) {
    requirements := NewRequirements(
        *NewRequirement("id", ConstraintNumeric),
        *NewRequirement("slug", ConstraintAlphaLowercase),
    )

    if 2 != len(requirements) {
        t.Fatalf("expected both requirements, got: %v", requirements)
    }

    if ConstraintNumeric != requirements["id"] {
        t.Fatalf("unexpected pattern for id: %q", requirements["id"])
    }

    if ConstraintAlphaLowercase != requirements["slug"] {
        t.Fatalf("unexpected pattern for slug: %q", requirements["slug"])
    }
}

func TestNewRequirements_DropsARequirementMissingEitherHalf(t *testing.T) {
    requirements := NewRequirements(
        *NewRequirement("", ConstraintNumeric),
        *NewRequirement("slug", ""),
        *NewRequirement("id", ConstraintNumeric),
    )

    if 1 != len(requirements) {
        t.Fatalf("expected only the complete requirement to survive, got: %v", requirements)
    }

    if _, present := requirements[""]; true == present {
        t.Fatalf("a requirement with no parameter name was stored under an empty key")
    }

    if _, present := requirements["slug"]; true == present {
        t.Fatalf("a requirement with no pattern was stored")
    }

    if ConstraintNumeric != requirements["id"] {
        t.Fatalf("expected the complete requirement to survive, got: %v", requirements)
    }
}

func TestNewRequirements_OfNothingIsAnEmptyMapRatherThanNil(t *testing.T) {
    requirements := NewRequirements()

    if nil == requirements {
        t.Fatalf("expected an empty map rather than nil")
    }

    if 0 != len(requirements) {
        t.Fatalf("expected no entries, got: %v", requirements)
    }
}

func TestRequireShorthands_EachNamesItsOwnCharacterClass(t *testing.T) {
    for _, testCase := range []struct {
        name            string
        requirement     *Requirement
        expectedPattern string
    }{
        {"RequireAlphaLowercase", RequireAlphaLowercase("slug"), ConstraintAlphaLowercase},
        {"RequireAlpha", RequireAlpha("slug"), ConstraintAlpha},
        {"RequireNumeric", RequireNumeric("id"), ConstraintNumeric},
        {"RequireAlphaNumeric", RequireAlphaNumeric("token"), ConstraintAlphaNumeric},
    } {
        if testCase.expectedPattern != testCase.requirement.Pattern() {
            t.Fatalf("%s: unexpected pattern %q", testCase.name, testCase.requirement.Pattern())
        }
    }

    if ConstraintAlphaLowercase == ConstraintAlpha {
        t.Fatalf("the lowercase and the mixed-case constraints must differ")
    }

    if ConstraintNumeric == ConstraintAlphaNumeric {
        t.Fatalf("the numeric and the alphanumeric constraints must differ")
    }
}

func TestRequireShorthands_AreEnforcedByTheRouter(t *testing.T) {
    router := NewRouter()

    router.HandleWithOptions(
        "/articles/:id",
        routeRegistryTestHandler(),
        &RouteOptions{
            methods:      []string{nethttp.MethodGet},
            requirements: NewRequirements(*RequireNumeric("id")),
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    numericRecorder := httptest.NewRecorder()
    handler.ServeHTTP(numericRecorder, httptest.NewRequest(nethttp.MethodGet, "/articles/42", nil))

    if nethttp.StatusOK != numericRecorder.Code {
        t.Fatalf("expected a numeric identifier to satisfy the constraint, got: %d", numericRecorder.Code)
    }

    alphabeticRecorder := httptest.NewRecorder()
    handler.ServeHTTP(alphabeticRecorder, httptest.NewRequest(nethttp.MethodGet, "/articles/melody", nil))

    if nethttp.StatusOK == alphabeticRecorder.Code {
        t.Fatalf("expected a non-numeric identifier to be refused by the constraint")
    }
}

func TestRequireAlphaLowercase_RefusesAnUppercaseSpelling(t *testing.T) {
    router := NewRouter()

    router.HandleWithOptions(
        "/pages/:slug",
        routeRegistryTestHandler(),
        &RouteOptions{
            methods:      []string{nethttp.MethodGet},
            requirements: NewRequirements(*RequireAlphaLowercase("slug")),
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    lowercaseRecorder := httptest.NewRecorder()
    handler.ServeHTTP(lowercaseRecorder, httptest.NewRequest(nethttp.MethodGet, "/pages/about", nil))

    if nethttp.StatusOK != lowercaseRecorder.Code {
        t.Fatalf("expected a lowercase slug to be admitted, got: %d", lowercaseRecorder.Code)
    }

    uppercaseRecorder := httptest.NewRecorder()
    handler.ServeHTTP(uppercaseRecorder, httptest.NewRequest(nethttp.MethodGet, "/pages/About", nil))

    if nethttp.StatusOK == uppercaseRecorder.Code {
        t.Fatalf("expected an uppercase slug to be refused by the lowercase constraint")
    }
}

func TestRequirement_IsAnchoredToTheWholeParameterValue(t *testing.T) {
    router := NewRouter()

    router.HandleWithOptions(
        "/:locale/home",
        routeRegistryTestHandler(),
        &RouteOptions{
            methods:      []string{nethttp.MethodGet},
            requirements: NewRequirements(*NewRequirement("locale", "en|de|fr")),
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    exactRecorder := httptest.NewRecorder()
    handler.ServeHTTP(exactRecorder, httptest.NewRequest(nethttp.MethodGet, "/de/home", nil))

    if nethttp.StatusOK != exactRecorder.Code {
        t.Fatalf("expected an exact member of the alternation to be admitted, got: %d", exactRecorder.Code)
    }

    containingRecorder := httptest.NewRecorder()
    handler.ServeHTTP(containingRecorder, httptest.NewRequest(nethttp.MethodGet, "/aden/home", nil))

    if nethttp.StatusOK == containingRecorder.Code {
        t.Fatalf("expected a value that merely contains a member to be refused")
    }
}
