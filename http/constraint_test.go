package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
)

/* @info the whole file was at zero coverage, including NewRequirements — which is not an accessor but a filter: it drops a requirement whose parameter name or pattern is empty. A dropped requirement is a route constraint the caller wrote and does not get, and the parameter it was meant to constrain then accepts anything, so the silence is the part that needs a test. */

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

/* @info a requirement missing either half is dropped whole rather than stored under an empty key or with an empty pattern. Storing an empty key would put an entry in the map that names no parameter, and addRoute skips both spellings anyway — so the drop happens once, here, where the caller can be shown it did. */

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

/* @info the four shorthands are the names an application actually writes, and each has to name its own character class: a shorthand pointing at the wrong constant would widen or narrow a route constraint silently — RequireAlphaLowercase accepting uppercase is the sharpest, because a path matched case-insensitively reaches a handler that was promised lowercase. */

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

/* @info the constraints are only worth anything if the router enforces them, so the shorthands are driven through a registered route: the value the class admits reaches the handler and the value it refuses does not match the route at all. A constraint that never reached the router would read as configured on every request whose parameter happened to be well formed. */

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

/* @info the lowercase class refuses an uppercase spelling. The router folds nothing about a parameter value, so this is the one place the distinction is made — and a slug constraint that admitted "Admin" beside "admin" would hand a handler two spellings of what it treats as one key. */

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

/* @info the constraint is anchored to the WHOLE value by addRoute, which wraps it in a non-capturing group first. A constraint spelled as an alternation is where that matters — "^en|de|fr$" without the group is read as (^en)|(de)|(fr$) and matches "aden" — so a locale whitelist written the obvious way has to refuse a value that merely contains one of its members. */

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
